package main

import (
	"testing"
	"time"
)

func TestTrigramSimilarityRanksIdenticalAboveUnrelated(t *testing.T) {
	identical := trigramSimilarity("如何部署 SForum 到 Docker", "如何部署 SForum 到 Docker")
	if identical != 1 {
		t.Fatalf("identical titles must score 1, got %v", identical)
	}
	related := trigramSimilarity("如何部署 SForum 到 Docker", "SForum Docker 部署踩坑")
	unrelated := trigramSimilarity("如何部署 SForum 到 Docker", "今天晚饭吃什么")
	if !(related > unrelated) {
		t.Fatalf("related %v should beat unrelated %v", related, unrelated)
	}
	if unrelated > 0.2 {
		t.Fatalf("unrelated titles should stay low, got %v", unrelated)
	}
}

func TestTrigramSimilarityHandlesEmptyInput(t *testing.T) {
	if got := trigramSimilarity("", "任何东西"); got != 0 {
		t.Fatalf("empty left must score 0, got %v", got)
	}
	if got := trigramSimilarity("任何东西", "   "); got != 0 {
		t.Fatalf("blank right must score 0, got %v", got)
	}
}

func TestRankItemsAppliesThresholdToRawSimilarity(t *testing.T) {
	source := indexRow{TopicID: 1, Title: "部署 SForum", CategorySlug: "tech"}
	candidates := []candidate{
		{ID: 2, Sim: 0.90},
		{ID: 3, Sim: 0.20},
		{ID: 4, Sim: 0.02}, // 同分类但字面几乎无关，不能靠 0.15 加成混进来
	}
	live := map[int64]liveRow{
		2: {TopicID: 2, Title: "部署 SForum 的三种方式", Slug: "deploy", CategorySlug: "tech", LastActivityAt: time.Now()},
		3: {TopicID: 3, Title: "SForum 迁移", Slug: "migrate", CategorySlug: "life", LastActivityAt: time.Now()},
		4: {TopicID: 4, Title: "晚饭", Slug: "dinner", CategorySlug: "tech", LastActivityAt: time.Now()},
	}
	cfg := Config{Enabled: true, Limit: 4, MinSimilarity: 0.15, IncludeSameCategory: true}
	items := rankItems(source, candidates, live, cfg, time.Now().UTC())

	if len(items) != 2 {
		t.Fatalf("expected 2 items above threshold, got %d: %+v", len(items), items)
	}
	if items[0].TopicID != 2 {
		t.Fatalf("highest similarity must rank first, got %d", items[0].TopicID)
	}
	for _, item := range items {
		if item.TopicID == 4 {
			t.Fatal("below-threshold same-category candidate must not survive the bonus")
		}
	}
}

func TestRankItemsDropsMissingLiveRows(t *testing.T) {
	source := indexRow{TopicID: 1, CategorySlug: "tech"}
	candidates := []candidate{{ID: 2, Sim: 0.9}, {ID: 3, Sim: 0.8}}
	// 3 不在 live 里 = 已被删除或转私密，必须丢成死链而不是照常返回。
	live := map[int64]liveRow{
		2: {TopicID: 2, Title: "还在", Slug: "alive", CategorySlug: "tech", LastActivityAt: time.Now()},
	}
	cfg := Config{Enabled: true, Limit: 4, MinSimilarity: 0.15}
	items := rankItems(source, candidates, live, cfg, time.Now().UTC())
	if len(items) != 1 || items[0].TopicID != 2 {
		t.Fatalf("missing live row must be dropped, got %+v", items)
	}
}

func TestRankItemsRespectsLimitAndIsDeterministic(t *testing.T) {
	source := indexRow{TopicID: 1, CategorySlug: "tech"}
	candidates := []candidate{{ID: 5, Sim: 0.5}, {ID: 3, Sim: 0.5}, {ID: 4, Sim: 0.9}}
	live := map[int64]liveRow{
		3: {TopicID: 3, Title: "b", CategorySlug: "tech"},
		4: {TopicID: 4, Title: "c", CategorySlug: "tech"},
		5: {TopicID: 5, Title: "a", CategorySlug: "tech"},
	}
	cfg := Config{Enabled: true, Limit: 2, MinSimilarity: 0.15}
	items := rankItems(source, candidates, live, cfg, time.Now().UTC())
	if len(items) != 2 {
		t.Fatalf("limit not applied, got %d", len(items))
	}
	if items[0].TopicID != 4 {
		t.Fatalf("top score must be first, got %d", items[0].TopicID)
	}
	// 同分时按 id 升序，保证同一份数据两次请求结果一致。
	if items[1].TopicID != 3 {
		t.Fatalf("tie must break by id asc, got %d", items[1].TopicID)
	}
}

func TestIndexRowFromSnapshotReadsEventPayload(t *testing.T) {
	payload := map[string]any{
		"topicId": float64(42),
		"title":   "如何部署 SForum",
		"topic":   map[string]any{"slug": "deploy-sforum", "status": "active"},
		"category": map[string]any{
			"id": float64(7), "slug": "tech",
		},
	}
	row, ok := indexRowFromSnapshot(payload)
	if !ok {
		t.Fatal("expected payload to be accepted")
	}
	if row.TopicID != 42 || row.Title != "如何部署 SForum" || row.Slug != "deploy-sforum" {
		t.Fatalf("unexpected row %+v", row)
	}
	if row.CategoryID != 7 || row.CategorySlug != "tech" || row.Status != "active" {
		t.Fatalf("unexpected taxonomy %+v", row)
	}
}

func TestIndexRowFromSnapshotRejectsIncompletePayload(t *testing.T) {
	if _, ok := indexRowFromSnapshot(map[string]any{"title": "缺 id"}); ok {
		t.Fatal("payload without topicId must be rejected")
	}
	if _, ok := indexRowFromSnapshot(map[string]any{"topicId": float64(1)}); ok {
		t.Fatal("payload without title must be rejected")
	}
}

func TestLoadConfigClampsOutOfRangeValues(t *testing.T) {
	t.Setenv(settingEnv(settingLimit), "99")
	t.Setenv(settingEnv(settingMinSimilarity), "-3")
	t.Setenv(settingEnv(settingEnabled), "false")
	t.Setenv(settingEnv(settingSameCategory), "false")

	cfg := loadConfig()
	if cfg.Limit != maxLimit {
		t.Fatalf("limit must clamp to %d, got %d", maxLimit, cfg.Limit)
	}
	if cfg.MinSimilarity != 0 {
		t.Fatalf("min similarity must clamp to 0, got %v", cfg.MinSimilarity)
	}
	if cfg.Enabled || cfg.IncludeSameCategory {
		t.Fatalf("boolean settings not honored: %+v", cfg)
	}
}

func TestLoadConfigFallsBackOnGarbage(t *testing.T) {
	t.Setenv(settingEnv(settingLimit), "many")
	cfg := loadConfig()
	if cfg.Limit != defaultLimit {
		t.Fatalf("garbage must fall back to default %d, got %d", defaultLimit, cfg.Limit)
	}
}

func TestSettingEnvMatchesHostDerivation(t *testing.T) {
	got := settingEnv(settingLimit)
	want := "SFORUM_SETTING_SFORUM_RELATED_TOPICS_LIMIT"
	if got != want {
		t.Fatalf("setting env drift: got %q want %q", got, want)
	}
}
