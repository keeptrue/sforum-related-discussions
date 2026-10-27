package main

import "context"

const (
	// backfillBatchSize 控制单批行数：运行时租约给插件连接设了 statement_timeout，
	// 大事务会被直接掐断，所以只能小批推进。
	backfillBatchSize = 500
	// backfillMaxBatches 是一次 job 内的批次上限，避免单次运行占满默认 worker。
	backfillMaxBatches = 200
)

// backfillBatch 从 Core 稳定视图搬一批进索引，返回下一个游标与是否已到底。
func (s *store) backfillBatch(ctx context.Context, cursor int64, limit int) (int64, bool, error) {
	if s == nil || s.db == nil {
		return cursor, true, nil
	}
	rows, err := s.db.QueryContext(ctx, `
INSERT INTO related_index
  (topic_id, title, slug, category_id, category_slug, status, comment_count, last_activity_at, indexed_at)
SELECT id, title, slug, category_id, category_slug, status, comment_count, last_activity_at, now()
FROM sforum_core_v1.forum_topics
WHERE id > $1
ORDER BY id
LIMIT $2
ON CONFLICT (topic_id) DO UPDATE SET
  title = EXCLUDED.title,
  slug = EXCLUDED.slug,
  category_id = EXCLUDED.category_id,
  category_slug = EXCLUDED.category_slug,
  status = EXCLUDED.status,
  comment_count = EXCLUDED.comment_count,
  last_activity_at = EXCLUDED.last_activity_at,
  indexed_at = now()
RETURNING topic_id`, cursor, limit)
	if err != nil {
		return cursor, false, err
	}
	defer rows.Close()

	next := cursor
	count := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return cursor, false, err
		}
		if id > next {
			next = id
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return cursor, false, err
	}
	return next, count < limit, nil
}

// runBackfill 循环推进游标，直到到底、超批次上限或上下文取消。
func (s *store) runBackfill(ctx context.Context, report func(processed int64)) error {
	if s == nil || s.db == nil {
		return nil
	}
	var cursor int64
	var processed int64
	for batch := 0; batch < backfillMaxBatches; batch++ {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		default:
		}
		next, done, err := s.backfillBatch(ctx, cursor, backfillBatchSize)
		if err != nil {
			return err
		}
		if next == cursor {
			// 没有新行：要么到底，要么游标之后没有可见帖子。
			break
		}
		processed += next - cursor
		cursor = next
		if report != nil {
			report(processed)
		}
		if done {
			break
		}
	}
	return nil
}

// reconcile 修两类漂移：索引里已不存在的帖子（删除/转私密）与索引落后的帖子。
// 依赖视图的 updated_at 与索引的 indexed_at 比较，所以事件漏一条不会永久错。
func (s *store) reconcile(ctx context.Context) (int64, int64, error) {
	if s == nil || s.db == nil {
		return 0, 0, nil
	}
	removed, err := s.db.ExecContext(ctx, `
DELETE FROM related_index i
WHERE NOT EXISTS (
  SELECT 1 FROM sforum_core_v1.forum_topics v WHERE v.id = i.topic_id
)`)
	if err != nil {
		return 0, 0, err
	}
	removedRows, _ := removed.RowsAffected()

	added, err := s.db.ExecContext(ctx, `
INSERT INTO related_index
  (topic_id, title, slug, category_id, category_slug, status, comment_count, last_activity_at, indexed_at)
SELECT v.id, v.title, v.slug, v.category_id, v.category_slug, v.status,
       v.comment_count, v.last_activity_at, now()
FROM sforum_core_v1.forum_topics v
LEFT JOIN related_index i ON i.topic_id = v.id
WHERE i.topic_id IS NULL OR v.updated_at > i.indexed_at
ON CONFLICT (topic_id) DO UPDATE SET
  title = EXCLUDED.title,
  slug = EXCLUDED.slug,
  category_id = EXCLUDED.category_id,
  category_slug = EXCLUDED.category_slug,
  status = EXCLUDED.status,
  comment_count = EXCLUDED.comment_count,
  last_activity_at = EXCLUDED.last_activity_at,
  indexed_at = now()`)
	if err != nil {
		return removedRows, 0, err
	}
	addedRows, _ := added.RowsAffected()
	return removedRows, addedRows, nil
}
