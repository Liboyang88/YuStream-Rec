/** 展示层文案处理：中英文标题/摘要选择、日期格式化、来源美化。 */

/**
 * 标题展示。
 *
 * 规则：
 * - 中文文献（is_chinese === true）：恒取原文 title —— 它本身就是中文，
 *   title_chinese 只是同义副本，「中文翻译」开关对它无效（开、关都展示 title）。
 * - 非中文文献：translate === true 时取中文翻译 title_chinese，否则取原文 title。
 *
 * @param {object} item
 * @param {{ translate?: boolean }} [options]
 */
export function displayTitle(item, options = {}) {
  if (!item) return ''
  const { translate = false } = options
  if (item.is_chinese) return item.title || item.title_chinese || '未命名内容'
  if (translate && item.title_chinese) return item.title_chinese
  return item.title || item.title_chinese || '未命名内容'
}

/**
 * 卡片上的「推荐描述」。
 *
 * @param {object} item
 * @param {number} [limit] 截断长度，0 表示不截断
 * @param {{ preferSummary?: boolean, translate?: boolean }} [options]
 *   preferSummary !== false → 优先二次概括 summary（默认）
 *   preferSummary === false → 优先原文 abstract
 *   translate === true      → 仅非中文文献：原文摘要侧优先取 abstract_chinese
 *   中文文献与 translate 关闭时，摘要侧恒取原文 abstract（缺失才回退中文）
 *   注意：preferSummary 生效时 summary 恒定优先，translate 不影响它
 */
export function displayDescription(item, limit = 0, options = {}) {
  if (!item) return ''
  const { preferSummary = true, translate = false } = options
  // 中文文献的 abstract 本身就是中文，无需（也不该）换成 abstract_chinese
  const useChineseAbstract = !!translate && !item.is_chinese
  const abstractCandidates = useChineseAbstract
    ? [item.abstract_chinese, item.abstract]
    : [item.abstract, item.abstract_chinese]
  const candidates = preferSummary ? [item.summary, ...abstractCandidates] : abstractCandidates

  let text = ''
  for (const candidate of candidates) {
    if (typeof candidate === 'string' && candidate.trim()) {
      text = candidate.trim()
      break
    }
  }
  text = text.replace(/\s+/g, ' ')
  if (limit > 0 && text.length > limit) return `${text.slice(0, limit).trimEnd()}…`
  return text
}

/**
 * 详情弹窗中的完整摘要，不做截断、保留段落。
 *
 * 规则同 displayDescription：中文文献恒取 abstract；非中文文献在
 * translate === true 时取 abstract_chinese。
 *
 * @param {object} item
 * @param {{ translate?: boolean }} [options]
 */
export function displayAbstract(item, options = {}) {
  if (!item) return ''
  const { translate = false } = options
  const candidates = translate && !item.is_chinese
    ? [item.abstract_chinese, item.abstract]
    : [item.abstract, item.abstract_chinese]
  for (const candidate of candidates) {
    if (typeof candidate === 'string' && candidate.trim()) return candidate.trim()
  }
  return ''
}

/** 模型生成的个性化解读（详情页单独成块展示）。 */
export function displaySummary(item) {
  if (!item) return ''
  return typeof item.summary === 'string' ? item.summary.trim() : ''
}

/**
 * 发布时间格式化：兼容 '2026-08-01'、'2026/08/01'、ISO 串与纯时间戳。
 * 解析失败时原样返回，避免显示 "Invalid Date"。
 */
export function formatPublishDate(value) {
  if (value == null || value === '') return '时间未知'
  const raw = String(value).trim()

  // 纯数字字符串（秒或毫秒时间戳）
  if (/^\d{10}$/.test(raw)) return formatDateObject(new Date(Number(raw) * 1000), raw)
  if (/^\d{13}$/.test(raw)) return formatDateObject(new Date(Number(raw)), raw)

  const normalized = raw.replace(/\//g, '-').replace(/\./g, '-')
  const parsed = new Date(normalized)
  if (Number.isNaN(parsed.getTime())) {
    // 退化为按 - 切分，兼容 '2026-8-1'
    const parts = normalized.split('-')
    if (parts.length === 3) {
      const [y, m, d] = parts.map((p) => Number.parseInt(p, 10))
      if (y && m && d) return `${y}年${m}月${d}日`
    }
    return raw
  }
  return formatDateObject(parsed, raw)
}

function formatDateObject(date, fallback) {
  if (Number.isNaN(date.getTime())) return fallback
  const y = date.getFullYear()
  const m = date.getMonth() + 1
  const d = date.getDate()
  return `${y}年${m}月${d}日`
}

/** 相对时间（用于卡片角标，比如「3 天前」）。 */
export function timeAgo(value) {
  if (!value) return ''
  const normalized = String(value).replace(/\//g, '-')
  const date = new Date(normalized)
  if (Number.isNaN(date.getTime())) return ''

  const diff = Date.now() - date.getTime()
  if (diff < 0) return '即将发布'
  const day = 24 * 60 * 60 * 1000
  if (diff < 60 * 60 * 1000) return '刚刚'
  if (diff < day) return `${Math.floor(diff / (60 * 60 * 1000))} 小时前`
  if (diff < 30 * day) return `${Math.floor(diff / day)} 天前`
  if (diff < 365 * day) return `${Math.floor(diff / (30 * day))} 个月前`
  return `${Math.floor(diff / (365 * day))} 年前`
}

/** 来源名美化：'arxiv' → 'arXiv'。 */
export function sourceLabel(item) {
  const raw = item?.web_source
  if (!raw) return '未知来源'
  const key = String(raw).trim()
  const known = {
    arxiv: 'arXiv',
    acl: 'ACL',
    ieee: 'IEEE',
    acm: 'ACM',
    springer: 'Springer',
    nature: 'Nature',
    science: 'Science',
  }
  return known[key.toLowerCase()] || key
}

/** 生成稳定的列表 key（后端 id 缺失时退化为内容指纹）。 */
export function itemKey(item) {
  if (!item) return ''
  if (item.id != null && item.id !== 0) return `id:${item.id}`
  return `fp:${displayTitle(item)}|${item.publish_date || ''}`.slice(0, 120)
}

/** 摘要阅读时长估算（中文按 350 字/分钟）。 */
export function readingMinutes(text) {
  if (!text) return 1
  return Math.max(1, Math.round(String(text).length / 350))
}
