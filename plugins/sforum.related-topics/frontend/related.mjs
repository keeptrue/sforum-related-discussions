// 相关讨论：帖子页正文下方的 L2 列表组件。
//
// 契约要求**具名导出**（`apiVersion` + `mount`）。公开前端加载器校验的是模块
// 命名空间本身，用 `export default` 会让 apiVersion 落在 default 上而被判为
// contract mismatch。
//
// 页面上下文：区域 widget 目前拿不到 componentProps，所以 topicId 先读
// `bridge.props.topicId`（宿主将来若注入就自动生效），再退回 URL 解析。
// 两者都没有时不渲染——宁可不出现，也不要挂一块空壳。

export const apiVersion = 1

const STYLE_ID = 'sforum-related-discussions-style'
let styleRefs = 0

const COPY = {
  'zh-CN': {
    title: '相关讨论',
    replies: n => `${n} 条回复`,
    empty: ''
  },
  'en-US': {
    title: 'Related discussions',
    replies: n => `${n} ${n === 1 ? 'reply' : 'replies'}`,
    empty: ''
  }
}

function copy(locale) {
  return COPY[String(locale || '').toLowerCase()] || COPY['zh-CN']
}

function ensureStyle(doc) {
  const existing = doc.getElementById(STYLE_ID)
  if (existing) {
    styleRefs += 1
    return existing
  }
  const style = doc.createElement('style')
  style.id = STYLE_ID
  style.textContent = `
.sf-related { margin: 24px 0 8px; }
.sf-related__title {
  margin: 0 0 12px; font-size: 15px; font-weight: 600; letter-spacing: .01em;
  color: inherit; opacity: .92;
}
.sf-related__list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
.sf-related__item { padding: 10px 0; border-top: 1px solid rgba(127,127,127,.18); }
.sf-related__item:first-child { border-top: none; }
.sf-related__link {
  display: block; color: inherit; text-decoration: none; font-size: 14px; line-height: 1.5;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.sf-related__link:hover { text-decoration: underline; text-underline-offset: 3px; }
.sf-related__meta {
  margin-top: 4px; display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 12px; opacity: .62;
}
.sf-related__badge {
  display: inline-flex; align-items: center; gap: 5px;
  border: 1px solid rgba(127,127,127,.2); border-radius: 999px;
  padding: 1px 8px; font-size: 11px; line-height: 1.6;
  background: rgba(127,127,127,.06);
}
/* 分类色只做一个小圆点：整块染成分类色会让黄/绿分类在白底上刺眼。 */
.sf-related__dot { width: 6px; height: 6px; border-radius: 50%; flex: none; }
@media (max-width: 640px) {
  .sf-related__link { white-space: normal; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
}
`
  doc.head.appendChild(style)
  styleRefs += 1
  return style
}

function releaseStyle(doc) {
  styleRefs = Math.max(0, styleRefs - 1)
  if (styleRefs > 0) return
  const style = doc.getElementById(STYLE_ID)
  if (style) style.remove()
}

function resolveTopicId(bridge) {
  const injected = Number(bridge?.props?.topicId)
  if (Number.isInteger(injected) && injected > 0) return injected
  const match = /^\/t\/(\d+)(?:[/?#]|$)/.exec(String(window.location?.pathname || ''))
  if (!match) return 0
  const parsed = Number(match[1])
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 0
}

function relativeTime(iso, locale) {
  const timestamp = Date.parse(iso)
  if (!Number.isFinite(timestamp)) return ''
  const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000))
  const table = [
    [60, 'second', 1],
    [3600, 'minute', 60],
    [86400, 'hour', 3600],
    [2592000, 'day', 86400],
    [31536000, 'month', 2592000],
    [Infinity, 'year', 31536000]
  ]
  const [limit, unit, divisor] = table.find(([cap]) => seconds < cap)
  const value = Math.max(1, Math.floor(seconds / divisor))
  try {
    return new Intl.RelativeTimeFormat(locale || 'zh-CN', { numeric: 'auto' }).format(-value, unit)
  } catch {
    return ''
  }
}

// normalizeHexColor 只放行 #rrggbb；返回小写形式，便于拼 8 位 alpha 后缀。
function normalizeHexColor(value) {
  const raw = String(value || '').trim()
  return /^#[0-9a-f]{6}$/i.test(raw) ? raw.toLowerCase() : ''
}

function topicHref(item) {
  const slug = String(item.slug || '').trim()
  return slug ? `/t/${item.topicId}/${slug}` : `/t/${item.topicId}`
}

export function mount(target, bridge) {
  const doc = target.ownerDocument
  const topicId = resolveTopicId(bridge)
  if (!topicId || typeof bridge?.request !== 'function') {
    return () => {}
  }

  let section = null
  ensureStyle(doc)

  // 只释放样式，不摘掉已渲染的节点：内容挂在 target 里，target 被宿主移除时
  // 会一起消失。宿主在区域数据到达/租约复核时会把组件 dispose 再挂载，若这里
  // 主动 remove，晚到的响应就会「渲染了又被抹掉」。
  const cleanup = () => {
    releaseStyle(doc)
  }

  bridge
    .request(`/related?topicId=${topicId}`, { timeout: 8000, responseMode: 'body' })
    .then(raw => {
      // 扩展路由是裸 JSON；宿主可能按 text/plain 返回，于是 $fetch 不解码。
      // 两种形状都接受，避免「有数据但组件读不到」。
      const payload = typeof raw === 'string' ? safeParseJSON(raw) : raw
      // 已渲染过就不再重复插入（宿主可能重复挂载同一个 target）。
      if (target.querySelector('[data-related-discussions]')) return
      const items = Array.isArray(payload?.items) ? payload.items : []
      // 没有相关帖子就整块不插入 DOM：不隐藏、不留占位，页面布局零影响。
      if (items.length === 0) return
      section = render(doc, bridge, items)
      target.appendChild(section)
    })
    .catch(() => {
      // 相关讨论是增强内容，取不到就当没有，不影响正文与评论。
    })

  return cleanup
}

function safeParseJSON(value) {
  try {
    return JSON.parse(value)
  } catch {
    return null
  }
}

function render(doc, bridge, items) {
  const text = copy(bridge?.locale)
  const section = doc.createElement('section')
  section.className = 'sf-related'
  section.setAttribute('data-related-discussions', '')
  section.setAttribute('data-topic-count', String(items.length))
  if (bridge?.appearance?.colorMode) {
    section.setAttribute('data-color-mode', String(bridge.appearance.colorMode))
  }

  const heading = doc.createElement('h2')
  heading.className = 'sf-related__title'
  heading.textContent = text.title
  section.appendChild(heading)

  const list = doc.createElement('ul')
  list.className = 'sf-related__list'
  for (const item of items) {
    const entry = doc.createElement('li')
    entry.className = 'sf-related__item'

    const link = doc.createElement('a')
    link.className = 'sf-related__link'
    link.href = topicHref(item)
    link.textContent = String(item.title || '')
    entry.appendChild(link)

    const meta = doc.createElement('div')
    meta.className = 'sf-related__meta'
    const categoryLabel = String(item.categoryName || item.categorySlug || '').trim()
    if (categoryLabel) {
      const badge = doc.createElement('span')
      badge.className = 'sf-related__badge'
      // 分类色来自 Core 的 public_categories.icon_color；只接受 6 位 hex，
      // 其它形状（空、rgb()、异常值）回落到中性灰点，不拼进内联样式。
      const dot = doc.createElement('span')
      dot.className = 'sf-related__dot'
      dot.style.backgroundColor = normalizeHexColor(item.categoryColor) || 'rgba(127,127,127,.45)'
      badge.appendChild(dot)
      badge.appendChild(doc.createTextNode(categoryLabel))
      meta.appendChild(badge)
    }
    const replies = doc.createElement('span')
    replies.textContent = text.replies(Number(item.commentCount) || 0)
    meta.appendChild(replies)
    const when = relativeTime(item.lastActivityAt, bridge?.locale)
    if (when) {
      const separator = doc.createElement('span')
      separator.textContent = '·'
      meta.appendChild(separator)
      const time = doc.createElement('span')
      time.textContent = when
      meta.appendChild(time)
    }
    entry.appendChild(meta)
    list.appendChild(entry)
  }
  section.appendChild(list)
  return section
}
