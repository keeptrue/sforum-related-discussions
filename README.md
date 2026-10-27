# SForum 相关讨论（`sforum.related-discussions`）

在帖子详情页正文下方、评论区之前，列出与该帖内容相关的其他帖子。读者读完正文正好看到「这个话题还聊过什么」，把被埋没的旧帖重新带回流量。

外部扩展包，不属于 SForum 内置插件。

- 仓库：`~/Code/github/sforum-related-discussions`（仓库根即 SForum 的外部扩展源根）
- 框架检出：`~/Code/github/SForum`（**同级目录**，插件通过本地 `replace` 解析宿主 SDK）
- 扩展 id：`sforum.related-discussions`

## 它解决什么问题

论坛的内容流失有两个方向：新帖没人看，旧帖没人回。搜索要求读者先知道自己要找什么；相关讨论反过来，在读者正在读一篇帖子的时候，把同一话题的旧帖递到眼前。

## 相关性怎么算

只用**标题字面相似度 + 分类**，不读正文、不调模型。

1. 索引里按 `pg_trgm` 的 `similarity(lower(title), lower(当前标题))` 取前 20 个候选（GIN trigram 索引，毫秒级）。
2. Go 侧再排：同分类 `+0.15`，最后活跃时间按半年半衰给最多 `+0.10`。
3. **阈值判在原始相似度上**（默认 `0.15`），不是加成后的总分——否则一个毫不相干但同分类的帖子会靠加成混进结果。
4. 最终取前 N 条（默认 4）。取之前会用 Core 稳定视图现查一次，查不到的（已删除/转私密）直接丢弃，不会给出死链。

标题通常已经概括了主题，且它是唯一能建索引的字段；`forum_topics.plain_text` 虽然可读，但视图没有索引，对全文算相似度就是全表扫描。正文加权留给后续版本。

## 三个运营者可调的边界

| 设置 | 默认 | 作用 |
| --- | --- | --- |
| 启用 | 开 | 关闭后帖子页不再渲染，索引保留 |
| 显示条数 | 4 | 1–8 |
| 最低相似度 | 0.15 | 0–1，越高越严格 |
| 同分类优先 | 开 | 候选集只取同分类，并给同分类更高权重 |

改动立即生效，不需要重启 API。

## 索引怎么维护

| 路径 | 触发 | 作用 |
| --- | --- | --- |
| `topic.created` / `topic.updated` | 宿主事件 | upsert 索引行，顺带让该帖的候选缓存失效 |
| `topic.deleted` | 宿主事件 | 删行 |
| 首次查询发现索引为空 | 请求内入队 | 排一次 `backfill` job，本次仍然返回空 |
| 每日 03:30 UTC | 宿主 schedule | `reconcile`：删掉已不可见的帖子、补齐落后或缺失的帖子 |

事件是 `fail_open` 的：索引写不进去不会反过来挡住发帖。漏掉一条由每日对账兜底。

## 数据与授权

清单申请两个数据库授权：

- `own_schema` —— 自己的 `sforum_related_discussions` schema，存 `related_index` 与 `related_cache`。
- `core_views` —— 只读 `sforum_core_v1.forum_topics`，用来回填索引与现查展示字段。

宿主 v1 只登记 `migrations[]` 而**不执行**插件 SQL，所以建表由插件在首次连上库时自己做（`CREATE TABLE IF NOT EXISTS`）。这也是本包不声明 `migrations[]` 的原因：声明一份永不执行的迁移是误导。

## 安装

开发（宿主 dev watcher 零命令）：

```bash
# 1. 宿主 .env 的 EXTERNAL_EXTENSION_ROOTS 加上本仓库根，重启 API
# 2. 保存任意源文件 → 自动构建/摘要/暂存/启用
```

手动构建与校验：

```bash
scripts/prepare-package.sh
```

它会依次执行 `go build`（`CGO_ENABLED=0 -trimpath -buildvcs=false`，与宿主 dev 构建同参）→ `extension digest --write` → `extension validate` → `extension test`。

## 目录布局

```text
plugins/sforum.related-discussions/
├── sforum.extension.json
├── frontend/related.mjs        # 帖子页正文下方的 L2 列表组件
├── openapi/routes.yaml         # GET /related
├── schemas/                    # 路由响应、hook 输入、job payload、组件 props
└── backend/
    ├── main.go                 # Server 装配（hooks + jobs）
    ├── routes.go               # InvokeRoute + 排序
    ├── hooks.go                # topic.* 事件 → 索引
    ├── jobs.go                 # backfill / reconcile
    ├── store.go                # own_schema 建表与查询
    ├── backfill.go             # 分批回填与对账
    ├── config.go               # SFORUM_SETTING_* 装载与钳制
    ├── trigram.go              # 无数据库降级用的相似度
    └── related_test.go
```

## 已知限制

`SFRegionOutlet` 挂载区域 widget 时不传 `componentProps`，所以组件拿不到页面上下文。当前实现先读 `bridge.props.topicId`（宿主将来若注入就自动生效），再退回 URL 解析 `^/t/(\d+)`；两者都没有时不渲染。路由形态一改，兜底就会失效——这是宿主侧的契约缺口，不是本插件能修的。
