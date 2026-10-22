import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { fetchRecommendations } from '@/api/items'
import { itemKey } from '@/utils/format'

/**
 * 推荐流状态。
 *
 * 关键设计：**不做“到底”永久封存**。
 * 后端是流式/窗口式推荐，同一次下滑请求可能返回新内容、也可能返回已看过的窗口，
 * 因此「每次下滑到底部」都应当真实再发一次请求（由 FeedStream 控制触发时机），
 * 本 store 只负责：请求、按 id 去重、顺序追加，并如实记录本轮新增了多少条。
 */
export const useFeedStore = defineStore('feed', () => {
  const items = ref([])
  const source = ref('')
  /** 最近一轮推荐返回的概要描述（outline），用于 scroll 互动的 detail 上报 */
  const outline = ref('')
  const loading = ref(false)
  const error = ref('')
  const loadedOnce = ref(false)

  /** 最近一轮请求新增的条数（0 表示后端这次没有给出新内容） */
  const lastRoundAdded = ref(0)
  /** 请求次数（用于调试/展示） */
  const requestCount = ref(0)

  /**
   * 硬阻塞：登录失效（401）或服务不可达时，继续自动请求没有意义，
   * 需要用户手动「重试」才恢复。普通「没有新内容」不属于阻塞，仍会继续请求。
   */
  const blocked = ref(false)

  const seen = ref(new Set())

  const isEmpty = computed(() => loadedOnce.value && items.value.length === 0)
  const count = computed(() => items.value.length)
  const sourceLabel = computed(() =>
    source.value === 'llm' ? '模型精排' : source.value === 'coarse' ? '检索粗排' : '',
  )
  /** 上一次请求没有带回新内容（用于底部提示，不影响后续继续请求） */
  const noNewContent = computed(
    () => loadedOnce.value && !loading.value && !error.value && lastRoundAdded.value === 0 && items.value.length > 0,
  )

  /**
   * 发一次推荐请求，并把新内容按顺序追加到列表尾部。
   * - 按 id 去重：已有卡片的位置与顺序绝不改动
   * - 从不因「重复内容」而停止：是否继续请求由触发方决定
   */
  async function loadMore() {
    if (loading.value || blocked.value) {
      return { added: 0, skipped: true, blocked: blocked.value }
    }

    loading.value = true
    error.value = ''

    try {
      const data = await fetchRecommendations()
      requestCount.value += 1

      const list = Array.isArray(data?.items) ? data.items : []
      if (data?.source) source.value = data.source
      if (data?.outline != null) outline.value = data.outline

      const fresh = []
      for (const entry of list) {
        const key = itemKey(entry)
        if (!key || seen.value.has(key)) continue
        seen.value.add(key)
        fresh.push(entry)
      }

      if (fresh.length > 0) {
        // 顺序追加，已有卡片位置不变
        items.value = items.value.concat(fresh)
      }
      lastRoundAdded.value = fresh.length

      return { added: fresh.length }
    } catch (err) {
      error.value = err.message
      lastRoundAdded.value = 0
      // 登录失效 / 服务不可达 → 阻塞自动请求，交由用户手动重试
      if (err.status === 401 || err.status === 0) blocked.value = true
      return { added: 0, error: err.message, blocked: blocked.value }
    } finally {
      loading.value = false
      loadedOnce.value = true
    }
  }

  /** 首次加载（已加载过则不重复请求）。 */
  function ensureLoaded() {
    if (loadedOnce.value || loading.value) return
    return loadMore()
  }

  /** 重新开始：清空列表与去重表（重新登录后使用）。 */
  function reset() {
    items.value = []
    source.value = ''
    outline.value = ''
    error.value = ''
    blocked.value = false
    loadedOnce.value = false
    lastRoundAdded.value = 0
    requestCount.value = 0
    seen.value = new Set()
  }

  /** 手动重试：解除阻塞后立即再请求一次。 */
  function retry() {
    blocked.value = false
    error.value = ''
    return loadMore()
  }

  return {
    items,
    source,
    sourceLabel,
    outline,
    loading,
    error,
    blocked,
    loadedOnce,
    lastRoundAdded,
    requestCount,
    isEmpty,
    noNewContent,
    count,
    loadMore,
    ensureLoaded,
    reset,
    retry,
  }
})
