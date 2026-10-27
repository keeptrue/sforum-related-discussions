# AGENTS — sforum-related-discussions

SForum 的「相关讨论」插件。独立仓库，仓库根即外部扩展源根。

- 宿主检出：`~/Code/github/SForum`（同级目录；后端通过 `replace` 解析宿主 SDK）
- 扩展包：`plugins/sforum.related-topics/`
- 扩展 id：`sforum.related-topics`

## 命令

```bash
scripts/prepare-package.sh            # build → digest --write → validate → test
cd plugins/sforum.related-topics/backend && go test ./...
```

宿主 `EXTERNAL_EXTENSION_ROOTS` 已包含本仓库根，保存源文件后 dev watcher 会自动
构建 / 摘要 / 暂存 / 启用，不需要手动跑命令。

## 硬规则

- **摘要必须只由源码决定**：构建一律 `CGO_ENABLED=0 -trimpath -buildvcs=false`，
  与宿主 `ExtensionPackage.BuildBackend` 同参。否则同一份源码在本地与站点两条
  路上产出不同字节，摘要永不收敛。
- **同一扩展 id 只能出现在一个源**：不要同时在 `extensions/dev/` 放副本。
  `DigestTree` 禁止符号链接。
- **`packageFiles` 按路径去重**：同一个文件只能声明一次。两个 hook 需要同一份
  输入 schema 时，共用一个 schema id，不要复制文件。
- **OpenAPI 片段必须带齐四个头**：`operationId` 以 `<namespace>.` 开头、
  `x-sforum-route-id`、`x-sforum-contract-version`、`x-sforum-guard`，
  且 `path` 与 `routes[].path` 逐字一致。
- **不声明永不执行的迁移**：宿主 v1 只登记 `migrations[]` 不跑 SQL，建表在
  `store.ensureSchema` 里做。
- **阈值判在原始相似度上**，不要判加成后的总分。

## 调试

- 插件子进程的 stderr 进宿主日志。
- 路由：`curl 'http://127.0.0.1:8080/api/v1/extensions/sforum.related-topics/related?topicId=1'`
  （需要 `X-SForum-Extension-Id` 等公开 L2 头时，直接从帖子页的 Network 抄）。
- 索引排查：`SELECT count(*) FROM sforum_related_discussions.related_index;`
