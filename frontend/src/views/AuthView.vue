<script setup>
import { useRoute, useRouter } from 'vue-router'
import AuthPanel from '@/components/AuthPanel.vue'
import { useAuthStore } from '@/stores/auth'

/**
 * 登录 & 注册独立页面（/auth）。
 * 使用引导页布局：整屏、无侧边栏，与主页面完全分离。
 */
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

function handleSuccess() {
  // 注册（或资料仍为空的登录）→ 进入「信息填写」引导页
  if (auth.needsOnboarding) {
    router.replace({ name: 'onboarding' })
    return
  }
  const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
  router.replace(redirect)
}
</script>

<template>
  <AuthPanel class="rise-in" @success="handleSuccess" />
</template>
