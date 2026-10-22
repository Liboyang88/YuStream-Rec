<script setup>
import { computed, markRaw, onMounted, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import MainLayout from '@/layouts/MainLayout.vue'
import BlankLayout from '@/layouts/BlankLayout.vue'
import AppModal from '@/components/AppModal.vue'
import ProfilePanel from '@/components/ProfilePanel.vue'
import SettingsPanel from '@/components/SettingsPanel.vue'
import ItemDetailPanel from '@/components/ItemDetailPanel.vue'
import ToastHost from '@/components/ToastHost.vue'
import { setUnauthorizedHandler } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useFeedStore } from '@/stores/feed'
import { useUiStore } from '@/stores/ui'

const auth = useAuthStore()
const feed = useFeedStore()
const ui = useUiStore()
const route = useRoute()
const router = useRouter()

/** 由路由 meta.layout 决定外壳：主页面带侧边栏，引导页整屏独立（无侧边栏） */
const LAYOUTS = { main: markRaw(MainLayout), blank: markRaw(BlankLayout) }
const layoutComponent = computed(() => LAYOUTS[route.meta?.layout] || LAYOUTS.main)

onMounted(() => {
  // 登录态失效（401）→ 清登录态、清空推荐流、关闭浮窗
  setUnauthorizedHandler(() => {
    auth.handleUnauthorized()
    feed.reset()
    ui.closeAll()
  })

  if (auth.isLoggedIn) auth.loadProfile({ silent: true })
})

watch(
  () => auth.isLoggedIn,
  (loggedIn) => {
    if (loggedIn) return
    feed.reset()
    ui.closeAll()
    // 退出登录后若仍停留在需要登录的页面，回到登录/注册页
    if (route.meta?.requiresAuth) router.replace({ name: 'auth' })
  },
)

watch(
  () => auth.notice,
  (message) => {
    if (message) ui.showToast(message, 'info')
  },
)
</script>

<template>
  <component :is="layoutComponent">
    <RouterView />
  </component>

  <ToastHost />

  <!-- 「我的」子页面：固定浮动窗口（主页面内） -->
  <AppModal
    :open="ui.activePanel === 'profile'"
    title="我的"
    subtitle="个人资料与用户画像"
    icon="user"
    size="md"
    @close="ui.closePanel()"
  >
    <ProfilePanel />
  </AppModal>

  <!-- 「设置」子页面：固定浮动窗口（暂为空白占位） -->
  <AppModal
    :open="ui.activePanel === 'settings'"
    title="设置"
    subtitle="偏好与账号配置"
    icon="settings"
    size="md"
    @close="ui.closePanel()"
  >
    <SettingsPanel />
  </AppModal>

  <!-- 推荐详情：固定浮动窗口 -->
  <AppModal
    :open="Boolean(ui.detailItem)"
    title="推荐详情"
    icon="book"
    size="lg"
    @close="ui.closeDetail()"
  >
    <ItemDetailPanel v-if="ui.detailItem" :item="ui.detailItem" />
  </AppModal>
</template>
