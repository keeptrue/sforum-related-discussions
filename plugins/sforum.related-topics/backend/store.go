package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// cacheTTL 是候选集缓存时长。缓存的是「哪些帖子相似、相似多少」，
// 展示用的计数与链接仍然每次现查，所以过期只会让候选变旧，不会产生死链。
const cacheTTL = 6 * time.Hour

// indexRow 是 related_index 的一行。
//
// 只保留「做相关性判断必须的字段」：标题（算相似度）、分类（加权与过滤）、
// 状态（排除已删）。slug / 计数 / 最后活跃时间是给无数据库的降级模式用的，
// 有数据库时一律现查 sforum_core_v1.forum_topics，避免计数与死链过期。
type indexRow struct {
	TopicID        int64
	Title          string
	Slug           string
	CategoryID     int64
	CategorySlug   string
	Status         string
	CommentCount   int64
	LastActivityAt time.Time
}

// candidate 是缓存进 related_cache.candidates 的一条候选。
type candidate struct {
	ID  int64   `json:"id"`
	Sim float64 `json:"sim"`
}

// liveRow 是从 Core 稳定视图现查到的公开字段。
type liveRow struct {
	TopicID        int64
	Title          string
	Slug           string
	CategorySlug   string
	CategoryName   string
	CategoryColor  string
	CommentCount   int64
	LastActivityAt time.Time
}

// relatedItem 是公开响应里的一条。
type relatedItem struct {
	TopicID        int64  `json:"topicId"`
	Title          string `json:"title"`
	Slug           string `json:"slug"`
	CategorySlug   string `json:"categorySlug"`
	CategoryName   string `json:"categoryName"`
	CategoryColor  string `json:"categoryColor"`
	CommentCount   int64  `json:"commentCount"`
	LastActivityAt string `json:"lastActivityAt"`
}

type store struct {
	db     *sql.DB
	schema string

	mu     sync.RWMutex
	memory map[int64]indexRow
}

func newStore() *store {
	value := &store{memory: map[int64]indexRow{}}
	value.schema = strings.TrimSpace(os.Getenv("SFORUM_DATABASE_SCHEMA"))
	url := strings.TrimSpace(os.Getenv("SFORUM_DATABASE_URL"))
	if url == "" {
		return value
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return value
	}
	value.db = db
	if err := value.ensureSchema(ctx); err != nil {
		// 建表失败不退出：下一次写入会再试，插件仍然能给出空结果而不是崩掉。
		_ = err
	}
	return value
}

func (s *store) connected() bool {
	return s != nil && s.db != nil
}

func (s *store) close() {
	if s != nil && s.db != nil {
		_ = s.db.Close()
	}
}

// ensureSchema 建表。宿主 v1 只登记 manifest migrations[] 而不执行 SQL，
// 所以建表必须由插件自己在首次连上库时完成。
func (s *store) ensureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("database unavailable")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS related_index (
			topic_id         bigint PRIMARY KEY,
			title            text NOT NULL,
			slug             text NOT NULL DEFAULT '',
			category_id      bigint,
			category_slug    text NOT NULL DEFAULT '',
			status           text NOT NULL DEFAULT 'active',
			comment_count    bigint NOT NULL DEFAULT 0,
			last_activity_at timestamptz NOT NULL DEFAULT now(),
			indexed_at       timestamptz NOT NULL DEFAULT now()
		)`,
		// 函数索引：查询侧统一用 lower(title)，索引必须建在同一表达式上才会被用。
		`CREATE INDEX IF NOT EXISTS related_index_title_trgm
			ON related_index USING GIN (lower(title) sforum_host_extensions.gin_trgm_ops)`,
		`CREATE INDEX IF NOT EXISTS related_index_category
			ON related_index (category_id, last_activity_at DESC)`,
		`CREATE TABLE IF NOT EXISTS related_cache (
			topic_id    bigint PRIMARY KEY,
			candidates  jsonb NOT NULL,
			computed_at timestamptz NOT NULL DEFAULT now()
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) upsert(ctx context.Context, row indexRow) error {
	if s == nil {
		return nil
	}
	if row.Status == "" {
		row.Status = "active"
	}
	if row.LastActivityAt.IsZero() {
		row.LastActivityAt = time.Now().UTC()
	}
	if s.db == nil {
		s.mu.Lock()
		s.memory[row.TopicID] = row
		s.mu.Unlock()
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO related_index
  (topic_id, title, slug, category_id, category_slug, status, comment_count, last_activity_at, indexed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8, now())
ON CONFLICT (topic_id) DO UPDATE SET
  title = EXCLUDED.title,
  slug = EXCLUDED.slug,
  category_id = EXCLUDED.category_id,
  category_slug = EXCLUDED.category_slug,
  status = EXCLUDED.status,
  comment_count = EXCLUDED.comment_count,
  last_activity_at = EXCLUDED.last_activity_at,
  indexed_at = now()`,
		row.TopicID, row.Title, row.Slug, nullableID(row.CategoryID), row.CategorySlug,
		row.Status, row.CommentCount, row.LastActivityAt.UTC())
	return err
}

func (s *store) remove(ctx context.Context, topicID int64) error {
	if s == nil {
		return nil
	}
	if s.db == nil {
		s.mu.Lock()
		delete(s.memory, topicID)
		s.mu.Unlock()
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM related_index WHERE topic_id = $1`, topicID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM related_cache WHERE topic_id = $1`, topicID)
	return err
}

func (s *store) invalidateCache(ctx context.Context, topicID int64) {
	if s == nil || s.db == nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM related_cache WHERE topic_id = $1`, topicID)
}

func (s *store) indexCount(ctx context.Context) (int64, error) {
	if s == nil {
		return 0, nil
	}
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return int64(len(s.memory)), nil
	}
	var total int64
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM related_index`).Scan(&total)
	return total, err
}

// ensureIndexed 保证源帖子在索引里。命中索引直接返回；没有则从 Core 视图补一条，
// 覆盖「插件装上之前就存在的帖子」与「事件丢失」两种情况。
func (s *store) ensureIndexed(ctx context.Context, topicID int64) (indexRow, bool, error) {
	if s == nil {
		return indexRow{}, false, nil
	}
	if s.db == nil {
		s.mu.RLock()
		row, ok := s.memory[topicID]
		s.mu.RUnlock()
		return row, ok, nil
	}
	row, ok, err := s.readIndexRow(ctx, topicID)
	if err != nil || ok {
		return row, ok, err
	}
	row, ok, err = s.readLiveRow(ctx, topicID)
	if err != nil || !ok {
		return indexRow{}, false, err
	}
	if err := s.upsert(ctx, row); err != nil {
		return indexRow{}, false, err
	}
	return row, true, nil
}

func (s *store) readIndexRow(ctx context.Context, topicID int64) (indexRow, bool, error) {
	var row indexRow
	var categoryID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT topic_id, title, slug, category_id, category_slug, status, comment_count, last_activity_at
FROM related_index WHERE topic_id = $1`, topicID).
		Scan(&row.TopicID, &row.Title, &row.Slug, &categoryID, &row.CategorySlug,
			&row.Status, &row.CommentCount, &row.LastActivityAt)
	if errors.Is(err, sql.ErrNoRows) {
		return indexRow{}, false, nil
	}
	if err != nil {
		return indexRow{}, false, err
	}
	if categoryID.Valid {
		row.CategoryID = categoryID.Int64
	}
	return row, true, nil
}

func (s *store) readLiveRow(ctx context.Context, topicID int64) (indexRow, bool, error) {
	var row indexRow
	var categoryID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT id, title, slug, category_id, category_slug, status, comment_count, last_activity_at
FROM sforum_core_v1.forum_topics WHERE id = $1`, topicID).
		Scan(&row.TopicID, &row.Title, &row.Slug, &categoryID, &row.CategorySlug,
			&row.Status, &row.CommentCount, &row.LastActivityAt)
	if errors.Is(err, sql.ErrNoRows) {
		return indexRow{}, false, nil
	}
	if err != nil {
		return indexRow{}, false, err
	}
	if categoryID.Valid {
		row.CategoryID = categoryID.Int64
	}
	return row, true, nil
}

// candidatesFor 返回相似度候选。第二个返回值表示是否命中缓存。
func (s *store) candidatesFor(ctx context.Context, source indexRow, sameCategory bool) ([]candidate, bool, error) {
	if s == nil {
		return nil, false, nil
	}
	if s.db == nil {
		return s.memoryCandidates(source, sameCategory), false, nil
	}
	if cached, ok := s.readCache(ctx, source.TopicID); ok {
		return cached, true, nil
	}
	var category any
	if sameCategory && source.CategoryID > 0 {
		category = source.CategoryID
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT topic_id, sim FROM (
  SELECT topic_id, sforum_host_extensions.similarity(lower(title), lower($2)) AS sim
  FROM related_index
  WHERE topic_id <> $1
    AND status = 'active'
    AND ($3::bigint IS NULL OR category_id = $3)
) ranked
WHERE sim > $4
ORDER BY sim DESC
LIMIT $5`,
		source.TopicID, source.Title, category, candidateFloor, candidateLimit)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := make([]candidate, 0, candidateLimit)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.ID, &item.Sim); err != nil {
			return nil, false, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	s.writeCache(ctx, source.TopicID, out)
	return out, false, nil
}

func (s *store) memoryCandidates(source indexRow, sameCategory bool) []candidate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]candidate, 0, candidateLimit)
	for id, row := range s.memory {
		if id == source.TopicID || row.Status != "active" {
			continue
		}
		if sameCategory && source.CategoryID > 0 && row.CategoryID != source.CategoryID {
			continue
		}
		sim := trigramSimilarity(source.Title, row.Title)
		if sim <= candidateFloor {
			continue
		}
		out = append(out, candidate{ID: id, Sim: sim})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sim == out[j].Sim {
			return out[i].ID < out[j].ID
		}
		return out[i].Sim > out[j].Sim
	})
	if len(out) > candidateLimit {
		out = out[:candidateLimit]
	}
	return out
}

func (s *store) readCache(ctx context.Context, topicID int64) ([]candidate, bool) {
	var raw []byte
	var computedAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT candidates, computed_at FROM related_cache WHERE topic_id = $1`, topicID).
		Scan(&raw, &computedAt)
	if err != nil {
		return nil, false
	}
	if time.Since(computedAt) > cacheTTL {
		return nil, false
	}
	var out []candidate
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return out, true
}

func (s *store) writeCache(ctx context.Context, topicID int64, candidates []candidate) {
	payload, err := json.Marshal(candidates)
	if err != nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `
INSERT INTO related_cache (topic_id, candidates, computed_at)
VALUES ($1, $2, now())
ON CONFLICT (topic_id) DO UPDATE SET candidates = EXCLUDED.candidates, computed_at = now()`,
		topicID, payload)
}

// liveRows 现查公开字段。查不到的 id 直接缺席——这正是「相关帖子已被删除」
// 的死链拦截点，不需要额外判断。
func (s *store) liveRows(ctx context.Context, ids []int64) (map[int64]liveRow, error) {
	out := map[int64]liveRow{}
	if s == nil || len(ids) == 0 {
		return out, nil
	}
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, id := range ids {
			row, ok := s.memory[id]
			if !ok {
				continue
			}
			out[id] = liveRow{
				TopicID: row.TopicID, Title: row.Title, Slug: row.Slug,
				CategorySlug: row.CategorySlug, CategoryName: row.CategorySlug,
				CommentCount: row.CommentCount, LastActivityAt: row.LastActivityAt,
			}
		}
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.title, t.slug, t.category_slug,
       coalesce(c.name, '') AS category_name,
       coalesce(c.icon_color, '') AS category_color,
       t.comment_count, t.last_activity_at
FROM sforum_core_v1.forum_topics t
LEFT JOIN sforum_core_v1.public_categories c ON c.id = t.category_id
WHERE t.id = ANY($1::bigint[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item liveRow
		if err := rows.Scan(&item.TopicID, &item.Title, &item.Slug, &item.CategorySlug,
			&item.CategoryName, &item.CategoryColor,
			&item.CommentCount, &item.LastActivityAt); err != nil {
			return nil, err
		}
		out[item.TopicID] = item
	}
	return out, rows.Err()
}

func nullableID(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}
