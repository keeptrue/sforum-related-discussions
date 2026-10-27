# NOTES — 设计取舍与核对结论

## 为什么是 `l2Widget` 而不是链接卡

`forum.page.regions` 的 `hostLink` / `extensionRoute` 只渲染单张卡或单按钮；
`forum.topic.sidebar` 的 `topicSidebarCard` 同样只支持单卡。要内联渲染一个列表，
当前机制下只有 `l2Widget` 一条路。

## 为什么放 `detail_after`

侧栏已有浏览量、回复数、贡献者、标签、阅读进度五张卡，再塞一张会挤；
正文下方是「相关阅读」的天然位置。换位置只改 contribution payload 的 `region`。

## 为什么用 trigram 而不是 tsvector

中文没有分词器，`to_tsvector('simple', 中文标题)` 会把整段 CJK 当一个 token，
只有标题几乎相同时才命中。`similarity()` 对中英混排都有效，且是 Core 自己在
`search_documents.fuzzy_text` 上验证过的路线。

`pg_trgm` 由 Core 预装在 `sforum_host_extensions`，migrator 执行了
`GRANT USAGE ON SCHEMA ... TO PUBLIC`（`database/migrator/core_extensions.go:20`），
所以插件角色可以直接引用 `gin_trgm_ops` 与 `similarity()`，不需要额外授权。

## 为什么阈值判在原始相似度上

计划原文写的是「过滤 score < min_similarity」。但 score 含同分类 `+0.15` 与时间
`+0.10` 加成，一个 0.0 相似度的同分类帖子会算出 0.15+ 而越过默认阈值。实现改为
先按原始 `sim` 过滤，再按加成后的 score 排序——这是修正，不是放宽。

## 为什么展示字段每次现查

索引里也存了 slug / comment_count / last_activity_at，但那只是无数据库时的降级
数据。有数据库时一律 `SELECT ... FROM sforum_core_v1.forum_topics WHERE id = ANY($1)`：

- 计数不会因为事件缺失而过期（新评论不触发 `topic.updated`）；
- 已删除或转私密的帖子查不到，天然被丢弃，不会变成死链；
- 多花一次主键查询，换掉一整类一致性 bug。

## 为什么不声明 migrations[]

宿主 v1 的 `recordDeclaredMigrations` 只登记路径与校验和，注释明写
`SQL not executed by host v1 runner`（`Models/Extensions/lifecycle.go:632`）。
声明一份永不执行的迁移只会误导后来者。建表在 `store.ensureSchema` 里做。

## 为什么没有 brand 资产

`brand check` 只读包内 `brand/` 三个文件，不读生成器的 `spec.json`；`extension test`
与宿主 dev 构建都不查 brand。v1 是本地运行包，不走发布流程，所以不做。
真要发布时：在宿主 `scripts/brand/spec.json` 加一条 `external` 记录，
再 `node scripts/brand/generate.mjs --only sforum.related-discussions`。

## 缓存

`related_cache` 存的是**候选集**（id + 相似度），TTL 6 小时，不是最终结果。
展示字段仍然现查，所以缓存过期只会让候选变旧，不会产生死链或过期计数。

## 待办

- 正文加权（把 `plain_text` 摘要放进索引，需要自建 tsvector 与 bigram 分词）。
- 标签重叠（`forum_topics` 视图没有 `tag_slugs`；事件 payload 有，但回填拿不到）。
- 宿主侧给区域 widget 传页面上下文，去掉 URL 解析兜底。
