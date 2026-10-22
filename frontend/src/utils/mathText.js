/**
 * 混排文本里的 LaTeX 处理（配合 components/MathText.vue 使用）。
 *
 * 后端数据（论文标题/摘要、模型生成的 summary、用户画像）里经常混着 LaTeX：
 *   「通过$\ell_2$增广优化$\ell_p$敏感度采样边界」
 *   「Capped Vertex Functions for $\text{Hilb}^n (\mathbb{C}^2)$」
 * 这里负责「切段 → 清理非数学文本 → 交给 KaTeX 渲染」，
 * 组件只做展示，逻辑放这里便于单测。
 */

import katex from 'katex'

/** 定界符：按优先级从左到右匹配 */
const OPENERS = [
  { token: '$$', close: '$$', display: true },
  { token: '\\[', close: '\\]', display: true },
  { token: '\\(', close: '\\)', display: false },
  { token: '$', close: '$', display: false },
]

const DOLLAR_INLINE = OPENERS[3]

function matchOpener(src, index) {
  if (src[index] === '$') return src[index + 1] === '$' ? OPENERS[0] : DOLLAR_INLINE
  if (src[index] === '\\') {
    if (src[index + 1] === '[') return OPENERS[1]
    if (src[index + 1] === '(') return OPENERS[2]
  }
  return null
}

/**
 * 从 from 开始找结束定界符；跳过转义对，避免把 `\$` 误当成 `$` 的结束位置。
 */
function findClose(src, from, close) {
  let i = from
  while (i < src.length) {
    if (src.startsWith(close, i)) return i
    if (src[i] === '\\') {
      i += 2
      continue
    }
    i += 1
  }
  return -1
}

/**
 * 把文本切成 [{ type: 'text' | 'math', value, display }]。
 *
 * 容错策略：定界符不闭合（数据本身残缺，或被 UI 截断）时，该定界符按普通文本处理，
 * 绝不吞掉后面的内容——宁可显示原文，也不能让卡片整段消失。
 *
 * @param {string} text
 * @returns {Array<{ type: 'text' | 'math', value: string, display: boolean }>}
 */
export function splitMathText(text) {
  const src = typeof text === 'string' ? text : ''
  const segments = []
  let textStart = 0
  let cursor = 0

  const flushText = (end) => {
    if (end > textStart) segments.push({ type: 'text', value: src.slice(textStart, end), display: false })
  }

  while (cursor < src.length) {
    const opener = matchOpener(src, cursor)

    if (opener) {
      const contentStart = cursor + opener.token.length
      const closeAt = findClose(src, contentStart, opener.close)
      // closeAt > contentStart：要求公式非空（`$$$$` 这类空壳按文本处理）
      if (closeAt > contentStart) {
        flushText(cursor)
        segments.push({
          type: 'math',
          value: src.slice(contentStart, closeAt).trim(),
          display: opener.display,
        })
        cursor = closeAt + opener.close.length
        textStart = cursor
        continue
      }
      cursor += opener.token.length
      continue
    }

    // 转义对整体跳过：\$ \% \{ \\ 等
    if (src[cursor] === '\\') {
      cursor += 2
      continue
    }
    cursor += 1
  }

  flushText(src.length)
  return segments
}

/** 非数学段里残留的排版命令 → 纯文本（`\textit{gemini}` → gemini） */
const BRACE_COMMANDS =
  /\\(?:textit|textbf|textsc|textsl|texttt|textrm|textsf|textup|textmd|textnormal|emph|mbox|hbox|underline|uline|url|text)\s*\{([^{}]*(?:\{[^{}]*\}[^{}]*)*)\}/g

/** 残余的无参排版命令，直接抹掉 */
const BARE_COMMANDS =
  /\\(?:em|bf|it|rm|sl|sc|tt|small|footnotesize|scriptsize|large|Large|LARGE|huge|Huge|normalsize|noindent|hfill|quad|qquad|centering|linebreak|newline)\b\s*/g

/**
 * 清理「不是公式」的那部分文本里混进来的 LaTeX 排版命令。
 * 例如 arXiv 摘要里常见的 `\textit{e.g.}`、`{\em vanishing-difference}`、`\%`、`~`。
 *
 * @param {string} text
 * @returns {string}
 */
export function stripTextCommands(text) {
  if (typeof text !== 'string' || !text) return ''

  let out = text

  // 嵌套的样式命令（\textsc{\textbf{SimSAM}}）需要多轮剥离
  for (let round = 0; round < 5; round += 1) {
    const next = out.replace(BRACE_COMMANDS, '$1')
    if (next === out) break
    out = next
  }

  return out
    .replace(/\\href\s*\{[^{}]*\}\s*\{([^{}]*)\}/g, '$1') // \href{url}{text} → text
    .replace(/\{\s*\\(?:em|bf|it|rm|sl|sc|tt)\s+([^{}]*)\}/g, '$1') // {\em text} → text
    .replace(/\\\\/g, ' ') // LaTeX 换行
    .replace(BARE_COMMANDS, ' ')
    .replace(/\\([%&_#{}])/g, '$1') // \% \& \_ \# \{ \} 还原
    .replace(/~+/g, ' ') // LaTeX 不间断空格
    .replace(/[ \t]{2,}/g, ' ')
}

/* ---------------- KaTeX 渲染 ---------------- */

const KATEX_OPTIONS = {
  throwOnError: false, // 语法错误时原样标红，不抛异常炸页面
  errorColor: '#c0392b',
  strict: 'ignore', // 容忍 \text{中文}、\rm 等非标准写法
  trust: false, // 禁用 \href、\includegraphics 等可注入命令
}

/** 渲染结果缓存：同一公式会在多张卡片、多次渲染中反复出现 */
const renderCache = new Map()
const CACHE_LIMIT = 800

/** 入库数据存在双重转义的公式（`$\\ell_2$`），KaTeX 会把 `\\` 当换行而报错 */
function repairDoubleEscaped(value) {
  return value.replace(/\\\\/g, '\\')
}

function attempt(value, display) {
  try {
    const html = katex.renderToString(value, { ...KATEX_OPTIONS, displayMode: display })
    // throwOnError:false 时错误会以 .katex-error 形式返回，这里视为渲染失败
    return html.includes('katex-error') ? '' : html
  } catch {
    return ''
  }
}

/**
 * 把一段公式渲染成 HTML；失败时返回 ''（由调用方退化为原文）。
 *
 * @param {string} value 公式内容（不含定界符）
 * @param {boolean} display 是否块级
 * @returns {string}
 */
export function renderMath(value, display) {
  const key = `${display ? 'D' : 'I'}${value}`
  const cached = renderCache.get(key)
  if (cached !== undefined) return cached

  let html = attempt(value, display)
  if (!html) {
    const repaired = repairDoubleEscaped(value)
    if (repaired !== value) html = attempt(repaired, display)
  }

  if (renderCache.size >= CACHE_LIMIT) renderCache.clear()
  renderCache.set(key, html)
  return html
}

/**
 * 组件直接消费的入口：文本 → 可渲染段列表。
 * math 段带 html（渲染失败为空串，届时展示原文），text 段已做 LaTeX 命令清理。
 *
 * @param {string} text
 * @param {'auto' | 'inline'} [mode]  'inline' 时块级公式也按行内渲染
 */
export function buildMathSegments(text, mode = 'auto') {
  return splitMathText(text).map((segment) => {
    if (segment.type !== 'math') {
      return { ...segment, value: stripTextCommands(segment.value) }
    }
    const display = mode === 'inline' ? false : segment.display
    return { ...segment, display, html: renderMath(segment.value, display) }
  })
}
