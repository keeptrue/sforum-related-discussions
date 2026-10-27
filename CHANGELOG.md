# Changelog

## 1.0.1

- 扩展 id 改为 `sforum.related-topics`（原 `sforum.related-discussions`）。
- 补 lifecycle V2 幂等 no-op 步骤：没有它，宿主 dev watcher 无法把新摘要升级到
  已启用的插件。
- 启动时后台回填索引一次；回填完成后清空候选缓存。
- 修复 `pg_trgm` 函数未做 schema 限定导致的运行时 503。
- 组件接受字符串形态的裸 JSON 响应；清理函数不再摘除已渲染节点。
- 路由增加 panic 恢复与有界错误日志。

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
