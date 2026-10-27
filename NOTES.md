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
再 `node scripts/brand/generate.mjs --only sforum.related-topics`。

## 缓存

`related_cache` 存的是**候选集**（id + 相似度），TTL 6 小时，不是最终结果。
展示字段仍然现查，所以缓存过期只会让候选变旧，不会产生死链或过期计数。

## 待办

- 正文加权（把 `plain_text` 摘要放进索引，需要自建 tsvector 与 bigram 分词）。
- 标签重叠（`forum_topics` 视图没有 `tag_slugs`；事件 payload 有，但回填拿不到）。
- 宿主侧给区域 widget 传页面上下文，去掉 URL 解析兜底。

## 运行时踩到的两个真坑（已修，值得记住）

### 1. `pg_trgm` 函数必须带 schema 限定

插件运行时租约的 `search_path` 只有自己的 schema + `sforum_core_v1` + `pg_catalog`，
**不含** `sforum_host_extensions`。所以：

- `CREATE INDEX ... (lower(title) sforum_host_extensions.gin_trgm_ops)` —— 限定过，能建；
- `similarity(lower(title), lower($1))` —— 未限定，运行时 `function similarity(text, text) does not exist`。

`extension test` 与 `validate` 都不会发现这个问题：它们不连库。表现是路由稳定 503
（宿主把插件返回的 `ErrorDetail` 折叠成 `extension.runtime_failed`，原始原因不下发）。
修法是显式写 `sforum_host_extensions.similarity(...)`。

### 2. 扩展路由的裸 JSON 会被解成字符串

`useApiClient().request` 默认返回 `payload.data`（API 信封）。扩展路由返回的是裸
JSON，必须传 `responseMode: 'body'`。即使传了，宿主仍可能按 `text/plain` 返回，
于是 `$fetch` 不解码、`bridge.request` 解析出的是**字符串**。

组件因此必须接受两种形状：`typeof raw === 'string' ? JSON.parse(raw) : raw`。
只写 `payload?.items` 会得到 `undefined`，表现为「请求 200、组件空白」。

### 3. 区域 widget 的挂载/卸载抖动

宿主在区域数据到达、租约复核、可见性变化时会把组件 `dispose` 再挂载。若清理函数
主动摘掉已渲染的节点，晚到的响应就会「渲染了又被抹掉」。现在的做法是：清理只释放
样式，内容留在 target 里（target 被移除时自然消失），并用
`target.querySelector('[data-related-discussions]')` 防重复插入。

另外：**每次改动 `frontend/related.mjs` 都必须 bump
`components[].contractVersion`**，否则升级会被拒（`component registry contract
conflicts with the active snapshot`）。当前是 `@11`。

## 分类名与分类色

`forum_topics` 视图只有 `category_slug`，直接展示会看到 `career` 这种内部标识。
展示用的名字与颜色来自 `sforum_core_v1.public_categories`（`name` / `icon_color`），
在 `liveRows` 里按 `category_id` LEFT JOIN 取：

```sql
SELECT t.id, t.title, t.slug, t.category_slug,
       coalesce(c.name, '') AS category_name,
       coalesce(c.icon_color, '') AS category_color,
       t.comment_count, t.last_activity_at
FROM sforum_core_v1.forum_topics t
LEFT JOIN sforum_core_v1.public_categories c ON c.id = t.category_id
WHERE t.id = ANY($1::bigint[])
```

`icon_color` 可能是空串（`general` 这类没设色的分类），所以组件只接受
`^#[0-9a-f]{6}$`，其它形状回落到中性描边——不把未校验的字符串拼进内联样式。

## 发布包为什么按平台各写一次摘要

`backend/plugin` 的摘要在清单里是**唯一值**，而四个平台的二进制字节不同。所以流水线
不能「构建一次、复制四份」：每个平台必须在自己的 stage 目录里重新
`extension digest --write` 再 `extension package`。这样每个 zip 内的清单摘要与其中的
可执行文件一一对应，宿主 exact-artifact 信任确认才能过。

同一份源码要产出同一份字节，构建参数必须与宿主 dev watcher（`ExtensionPackage.BuildBackend`）
完全一致：`CGO_ENABLED=0` + `-trimpath` + `-buildvcs=false`。发布包额外加 `-ldflags "-s -w"`
只影响体积，不影响跨平台摘要各自独立这件事。

stage 目录用 `rsync --exclude '/backend/*.go' --exclude '/backend/go.mod' --exclude '/backend/go.sum'`
先摘掉作者源码，再交给 `--exclude-source` 打包；两层都做，是因为前者保证摘要计算时不把
`go.mod` 当运行时文件，后者保证清单之外的杂项（`.gitignore`、`testdata/`）也进不去。

## 为什么 CI 不改写 go.mod 的 replace

`go version -m <binary>` 会把 `replace` 的**字面字符串**打进二进制：

```text
dep  github.com/keeptrue/sforum/apps/api  v0.0.0
=>   ../../../../SForum/apps/api  (devel)
```

所以 `go mod edit -replace ...=$GITHUB_WORKSPACE/sforum/apps/api` 这种改写会改变
同一份源码的字节——摘要、乃至任何按字节比对的东西都会漂。CI 现在的做法是把宿主
检出到 `../SForum`（目录名与提交的 replace 逐字一致，大小写敏感），完全不碰 go.mod，
再加一步断言 replace 仍指向那个目录，布局写错就直接红。

同理，清单里那个摘要**不是跨环境不变量**：本地宿主 dev watcher 用自己的 go 重建并
回写，CI 的 setup-go 是另一版工具链。要求两者相等只会制造常红提示。真正需要精确的
是发布流水线——每个平台在自己的 stage 目录里重新 `digest --write` 再打包。
