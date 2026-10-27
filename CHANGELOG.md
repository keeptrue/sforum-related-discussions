# Changelog

## 1.0.0

首个版本。

- 帖子页正文下方（`detail_after`）渲染相关讨论列表，默认 4 条。
- 相关性 = `pg_trgm` 标题相似度 + 同分类加权 + 半年半衰的时间加成；
  阈值判在原始相似度上。
- 索引由 `topic.created` / `topic.updated` / `topic.deleted` 三条 observe hook
  增量维护，每日 `reconcile` 对账，首次访问空索引时自动排 `backfill`。
- 展示字段每次从 `sforum_core_v1.forum_topics` 现查：计数不会过期，
  已删除的帖子不会变成死链。
- 匿名可读；四条设置在后台「相关讨论」页可调，改动即时生效。
