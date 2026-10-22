import { createRouter, createWebHistory } from 'vue-router'
import HomeView from '@/views/HomeView.vue'
import AuthView from '@/views/AuthView.vue'
import OnboardingView from '@/views/OnboardingView.vue'
import { useAuthStore } from '@/stores/auth'

/**
 * 页面分为两类，使用不同的布局外壳：
 *   1) layout: 'main'  —— 左侧导航 + 中间内容区（首页 / 流式推荐）
 *   2) layout: 'blank' —— 整屏独立引导页，**不带侧边栏**（登录&注册、信息填写）
 *
 * 「我的」「设置」等子页面仍是主页面内的浮窗，不占路由。
 */
const routes = [
  {
    path: '/',
    name: 'home',
    component: HomeView,
    meta: { layout: 'main', requiresAuth: true, title: '首页 · 流式推荐' },
  },
  {
    path: '/auth',
    name: 'auth',
    component: AuthView,
    meta: { layout: 'blank', guestOnly: true, title: '登录 / 注册 · 流式推荐' },
  },
  {
    path: '/welcome',
    name: 'onboarding',
    component: OnboardingView,
    meta: { layout: 'blank', requiresAuth: true, title: '完善资料 · 流式推荐' },
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/',
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: () => ({ top: 0 }),
})

/**
 * 前置守卫：串起「未登录 → 登录/注册页」「已登录但资料为空 → 信息填写页」的引导链路。
 * 注意：要先等资料加载完成，才能判断是否需要进入信息填写引导。
 */
router.beforeEach(async (to) => {
  const auth = useAuthStore()

  if (auth.isLoggedIn && !auth.profileLoaded) {
    await auth.loadProfile({ silent: true })
  }

  if (to.meta.requiresAuth && !auth.isLoggedIn) {
    return {
      name: 'auth',
      query: to.fullPath && to.fullPath !== '/' ? { redirect: to.fullPath } : {},
    }
  }

  // 已登录但资料仍为空 → 强制进入信息填写引导页
  if (auth.isLoggedIn && auth.needsOnboarding && to.name !== 'onboarding') {
    return { name: 'onboarding' }
  }

  // 已登录用户不应再停留在登录/注册页
  if (to.meta.guestOnly && auth.isLoggedIn) {
    return auth.needsOnboarding ? { name: 'onboarding' } : { name: 'home' }
  }

  return true
})

router.afterEach((to) => {
  if (to.meta?.title) document.title = to.meta.title
})

export default router
