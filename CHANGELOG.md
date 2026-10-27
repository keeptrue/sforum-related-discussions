# Changelog

## 1.0.2

发布工程版本，插件运行时行为与 1.0.1 一致。

- 新增 `release` 流水线：推 `v*` 标签即交叉编译 `linux_amd64` / `linux_arm64` /
  `darwin_arm64` / `darwin_amd64` 四个平台，按平台各自重写清单摘要，打成
  `--exclude-source` 的运行时包并连同 SBOM 与 `SHA256SUMS` 挂到 GitHub Release。
- 发布前强制校验「标签版本 == `sforum.extension.json` 的 `version`」，并检查包内
  确实含 `backend/plugin`、`frontend/related.mjs`、`sforum.extension.json`。
- 修复 `ci.yml` 的路径错误：`go mod edit -replace` 与 `extension digest/validate/test`
  原先用相对路径，指向 `$GITHUB_WORKSPACE` 之外不存在的目录；改为绝对路径并加前置断言。
- CI 增加 `go vet` 与 `concurrency` 取消，摘要漂移改为非阻塞提示（工具链差异属正常）。

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
