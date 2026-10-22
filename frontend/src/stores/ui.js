import { defineStore } from 'pinia'
import { ref } from 'vue'
import { INTERACTION, VIEW_MAX_SECONDS, recordInteraction } from '@/api/interactions'

/**
 * 全局浮层状态。
 * - 侧边栏「我的」/「设置」→ 打开对应子页面浮窗
 * - 卡片点击 → 打开推荐详情浮窗
 * 两个层级互斥：详情浮窗打开时不叠加其它子页面。
 */
export const useUiStore = defineStore('ui', () => {
  /** 'profile' | 'settings' | 'auth' | null */
  const activePanel = ref(null)
  /** 详情浮窗承载的推荐内容 */
  const detailItem = ref(null)
  /** 详情打开的时间戳（毫秒），用于在关闭时结算 view 的浏览时长 */
  const detailOpenedAt = ref(0)
  const toast = ref(null)

  let toastTimer = null

  /**
   * 结算并关闭详情：若之前打开了详情，则上报一次 view 互动。
   * view = 点击卡片进入详情 → 关闭详情，LongTS = 浏览时长（秒，上限 3min）。
   */
  function finishDetailView() {
    const item = detailItem.value
    const openedAt = detailOpenedAt.value
    if (item && openedAt) {
      const seconds = Math.min(VIEW_MAX_SECONDS, Math.max(0, Math.round((Date.now() - openedAt) / 1000)))
      recordInteraction({ type: INTERACTION.VIEW, target: item, long_ts: seconds }).catch(() => {})
    }
    detailItem.value = null
    detailOpenedAt.value = 0
  }

  function openPanel(name) {
    finishDetailView()
    activePanel.value = name
  }

  function closePanel() {
    activePanel.value = null
  }

  function openDetail(item) {
    finishDetailView()
    activePanel.value = null
    detailItem.value = item
    detailOpenedAt.value = Date.now()
  }

  function closeDetail() {
    finishDetailView()
  }

  function closeAll() {
    finishDetailView()
    activePanel.value = null
  }

  function showToast(message, type = 'info', duration = 2600) {
    toast.value = { message, type, id: Date.now() }
    if (toastTimer) clearTimeout(toastTimer)
    toastTimer = setTimeout(() => {
      toast.value = null
    }, duration)
  }

  return {
    activePanel,
    detailItem,
    toast,
    openPanel,
    closePanel,
    openDetail,
    closeDetail,
    closeAll,
    showToast,
  }
})
