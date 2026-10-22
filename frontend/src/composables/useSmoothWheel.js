import { onBeforeUnmount, onMounted, ref } from 'vue'

/**
 * 鼠标滚轮平滑滚动（类似小红书的「滚一下、滑一段」手感）。
 *
 * 做法：拦截 wheel 事件 → 累加目标位置 → rAF 里用缓动逐帧逼近，
 * 而不是让浏览器瞬间跳一段，因此滚动手感是连续的、带阻尼的。
 *
 * 细节：
 * - 归一化 deltaMode（像素 / 行 / 页），触控板与鼠标滚轮都能正常工作。
 * - 已经贴到顶部/底部时不再 preventDefault，把滚动交还给浏览器。
 * - 触摸、键盘、拖拽滚动条、程序化跳转都会中断动画接管，避免「抢滚动」。
 * - 缓动收敛阈值 >= 1px：scrollTop 会被浏览器量化成整数，阈值过小会导致
 *   rAF 永远收敛不了、持续抢写滚动位置（表现为页面被「拽住」滚不动）。
 */
export function useSmoothWheel(targetRef, options = {}) {
  const { easing = 0.17, lineHeight = 16, wheelScale = 1 } = options

  const isAnimating = ref(false)

  let desired = 0
  let rafId = null
  let hijacking = false
  // 我们最后一次写入的滚动位置，用于区分「自己的动画」与「外部滚动」
  let lastWritten = 0

  function maxScroll(el) {
    return Math.max(0, el.scrollHeight - el.clientHeight)
  }

  function stopAnimation() {
    if (rafId !== null) {
      cancelAnimationFrame(rafId)
      rafId = null
    }
    isAnimating.value = false
  }

  function releaseHijack() {
    hijacking = false
    stopAnimation()
  }

  function tick() {
    const el = targetRef.value
    if (!el) {
      releaseHijack()
      return
    }

    const before = el.scrollTop
    const delta = desired - before

    // 收敛条件：阈值必须 >= 1px —— scrollTop 会被浏览器量化成整数，
    // 若阈值小于 1px，缓动写入可能被舍入抹掉，导致 rAF 永远收敛不了、
    // 一直抢写滚动位置（表现为页面被「拽住」无法滚动）。
    if (Math.abs(delta) <= 1) {
      el.scrollTop = desired
      lastWritten = el.scrollTop
      releaseHijack()
      return
    }

    el.scrollTop = before + delta * easing

    // 写入没有生效（被量化或被夹在边界）→ 直接吸附到目标并收手，避免死循环
    if (Math.abs(el.scrollTop - before) < 0.1) {
      el.scrollTop = desired
      lastWritten = el.scrollTop
      releaseHijack()
      return
    }

    lastWritten = el.scrollTop
    // 目标可能已被外部改动（例如内容变短），重新夹紧
    const max = maxScroll(el)
    if (desired > max) desired = max
    if (desired < 0) desired = 0

    rafId = requestAnimationFrame(tick)
  }

  function normalizeDelta(event) {
    const raw = event.deltaY
    if (!raw) return 0
    if (event.deltaMode === 1) return raw * lineHeight
    if (event.deltaMode === 2) {
      const el = targetRef.value
      return raw * (el ? el.clientHeight : 600)
    }
    return raw * wheelScale
  }

  function onWheel(event) {
    const el = targetRef.value
    if (!el) return
    if (el.scrollHeight <= el.clientHeight + 1) return

    const delta = normalizeDelta(event)
    if (!delta) return

    const max = maxScroll(el)
    const atTop = el.scrollTop <= 0 && delta < 0
    const atBottom = el.scrollTop >= max - 1 && delta > 0
    if (atTop || atBottom) {
      // 边界处交还浏览器（例如整页滚动），避免「滚不动」的粘滞感
      releaseHijack()
      return
    }

    event.preventDefault()

    const base = hijacking ? desired : el.scrollTop
    desired = Math.min(max, Math.max(0, base + delta))
    hijacking = true

    if (rafId === null) {
      isAnimating.value = true
      rafId = requestAnimationFrame(tick)
    }
  }

  // 其它滚动方式（触摸/键盘/拖动滚动条）出现时让出控制权
  function onNativeScrollIntent() {
    if (hijacking) releaseHijack()
  }

  /**
   * 滚动位置不是我们写的 → 说明是外部滚动（程序化跳转、滚动条拖拽、锚点跳转等），
   * 立即释放控制权，绝不把位置「拽」回自己记录的目标。
   */
  function onScroll() {
    if (!hijacking) return
    const el = targetRef.value
    if (!el) return
    if (Math.abs(el.scrollTop - lastWritten) > 2) releaseHijack()
  }

  function onKeydown(event) {
    const keys = ['ArrowUp', 'ArrowDown', 'PageUp', 'PageDown', 'Home', 'End', ' ']
    if (keys.includes(event.key)) onNativeScrollIntent()
  }

  onMounted(() => {
    const el = targetRef.value
    if (!el) return
    el.addEventListener('wheel', onWheel, { passive: false })
    el.addEventListener('scroll', onScroll, { passive: true })
    el.addEventListener('touchstart', onNativeScrollIntent, { passive: true })
    el.addEventListener('pointerdown', onNativeScrollIntent, { passive: true })
    window.addEventListener('keydown', onKeydown)
  })

  onBeforeUnmount(() => {
    const el = targetRef.value
    if (el) {
      el.removeEventListener('wheel', onWheel)
      el.removeEventListener('scroll', onScroll)
      el.removeEventListener('touchstart', onNativeScrollIntent)
      el.removeEventListener('pointerdown', onNativeScrollIntent)
    }
    window.removeEventListener('keydown', onKeydown)
    stopAnimation()
  })

  /** 供外部（例如切换到「首页」时）平滑滚动到指定位置 */
  function scrollTo(top, { smooth = false } = {}) {
    const el = targetRef.value
    if (!el) return
    desired = Math.min(maxScroll(el), Math.max(0, top))
    hijacking = true
    if (smooth) {
      if (rafId === null) rafId = requestAnimationFrame(tick)
    } else {
      el.scrollTop = desired
      lastWritten = el.scrollTop
      releaseHijack()
    }
  }

  return { isAnimating, scrollTo }
}
