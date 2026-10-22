/**
 * v-reveal —— 卡片「浮现」入场动画指令。
 *
 * 用法：
 *   <article v-reveal>…</article>
 *   <article v-reveal="{ delay: 80 }">…</article>   // 同排错峰，形成波浪感
 *
 * 行为：元素进入视口后加上 .is-revealed，触发 base.css 中的 cardReveal 动画，
 * 随后立即 unobserve，避免滚动时反复触发。
 */

const REVEALED_CLASS = 'is-revealed'

let observer = null

function getObserver() {
  if (observer) return observer
  if (typeof IntersectionObserver === 'undefined') return null

  observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        entry.target.classList.add(REVEALED_CLASS)
        observer.unobserve(entry.target)
      }
    },
    {
      // root 为 null：以视口为参照。祖先滚动容器的裁剪会被自动计入，
      // 因此「滚出中间内容区」的卡片不会被误判为可见。
      root: null,
      rootMargin: '0px 0px -6% 0px',
      threshold: 0.06,
    },
  )
  return observer
}

export const revealDirective = {
  mounted(el, binding) {
    const delay = Number(binding.value?.delay ?? 0)
    if (delay > 0) el.style.setProperty('--reveal-delay', `${delay}ms`)

    el.classList.add('reveal')

    const io = getObserver()
    if (!io) {
      // 不支持 IntersectionObserver 时直接展示，保证内容可见
      el.classList.add(REVEALED_CLASS)
      return
    }
    io.observe(el)
  },
  unmounted(el) {
    if (observer) observer.unobserve(el)
  },
}
