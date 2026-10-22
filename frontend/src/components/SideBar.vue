<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppIcon from './AppIcon.vue'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

const auth = useAuthStore()
const ui = useUiStore()
const router = useRouter()
const route = useRoute()

const NAV_ITEMS = [
  { key: 'profile', label: '我的', icon: 'user', hint: '资料与画像' },
  { key: 'home', label: '首页', icon: 'home', hint: '流式推荐' },
  { key: 'settings', label: '设置', icon: 'settings', hint: '偏好设置' },
]

/** 有浮窗时高亮对应项，否则高亮「首页」。 */
const activeKey = computed(() => {
  if (ui.activePanel === 'profile') return 'profile'
  if (ui.activePanel === 'settings') return 'settings'
  return 'home'
})

function handleNav(key) {
  if (key === 'home') {
    // 回到「流式推荐」主页面：关闭所有浮窗 + 回到首页路由
    ui.closeAll()
    if (route.name !== 'home') router.push({ name: 'home' })
    return
  }
  ui.openPanel(key)
}

function handleAuthAction() {
  if (auth.isLoggedIn) {
    auth.logout()
    ui.closeAll()
    ui.showToast('已退出登录')
    // 主页面需要登录：清掉登录态后直接回到登录/注册页
    if (route.name !== 'auth') router.replace({ name: 'auth' })
  } else {
    // 登录&注册已是独立页面（无侧边栏），不再用浮窗承载
    router.push({ name: 'auth' })
  }
}
</script>

<template>
  <aside class="sidebar">
    <div class="brand">
      <span class="brand-mark"><AppIcon name="sparkles" :size="19" /></span>
      <span class="brand-text">
        <strong class="brand-name">研遇 LitStream</strong>
        <small class="brand-sub">Streaming Recommender</small>
      </span>
    </div>

    <nav class="nav" aria-label="主导航">
      <button
        v-for="item in NAV_ITEMS"
        :key="item.key"
        type="button"
        class="nav-item"
        :class="{ 'is-active': activeKey === item.key }"
        :aria-current="activeKey === item.key ? 'page' : undefined"
        @click="handleNav(item.key)"
      >
        <span class="nav-icon"><AppIcon :name="item.icon" :size="18" /></span>
        <span class="nav-labels">
          <span class="nav-label">{{ item.label }}</span>
          <span class="nav-hint">{{ item.hint }}</span>
        </span>
        <span class="nav-rail" aria-hidden="true" />
      </button>
    </nav>

    <div class="sidebar-foot">
      <div v-if="auth.isLoggedIn" class="user-card">
        <span class="avatar">{{ auth.initial }}</span>
        <span class="user-meta">
          <strong class="user-name" :title="auth.email || auth.displayName">{{ auth.displayName }}</strong>
          <small class="user-state">
            <template v-if="auth.summaryGenerating">画像生成中…</template>
            <template v-else-if="auth.hasSummary">画像已生成</template>
            <template v-else>待完善资料</template>
          </small>
        </span>
      </div>

      <div v-else class="guest-card">
        <p class="guest-text">登录后即可获得个性化推荐</p>
      </div>

      <button type="button" class="btn btn-block auth-btn" :class="auth.isLoggedIn ? 'btn-ghost' : 'btn-primary'" @click="handleAuthAction">
        <AppIcon :name="auth.isLoggedIn ? 'logout' : 'login'" :size="16" />
        {{ auth.isLoggedIn ? '退出登录' : '登录 / 注册' }}
      </button>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  width: var(--sidebar-w);
  flex: 0 0 var(--sidebar-w);
  height: 100vh;
  display: flex;
  flex-direction: column;
  padding: 22px 14px 16px;
  background: rgba(255, 255, 255, 0.82);
  backdrop-filter: blur(14px) saturate(130%);
  -webkit-backdrop-filter: blur(14px) saturate(130%);
  border-right: 1px solid var(--border);
  z-index: 20;
}

/* 品牌区 */
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 8px 22px;
}
.brand-mark {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 11px;
  background: var(--brand-gradient);
  color: #fff;
  box-shadow: 0 8px 18px -8px rgba(79, 70, 229, 0.9);
  flex: 0 0 auto;
}
.brand-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.brand-name {
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.01em;
  line-height: 1.25;
}
.brand-sub {
  font-size: 10.5px;
  color: var(--text-3);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

/* 导航 */
.nav {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1 1 auto;
}
.nav-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 11px;
  width: 100%;
  padding: 10px 11px;
  border-radius: var(--r-md);
  text-align: left;
  color: var(--text-2);
  transition: background-color 0.18s var(--ease-soft), color 0.18s var(--ease-soft);
}
.nav-item:hover {
  background: var(--surface-2);
  color: var(--text);
}
.nav-item.is-active {
  background: var(--brand-soft);
  color: var(--brand-dark);
}
.nav-icon {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: var(--r-xs);
  background: rgba(255, 255, 255, 0.7);
  color: inherit;
  flex: 0 0 auto;
  transition: background-color 0.18s var(--ease-soft);
}
.nav-item.is-active .nav-icon {
  background: #fff;
  box-shadow: var(--shadow-xs);
}
.nav-labels {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.nav-label {
  font-size: 14px;
  font-weight: 600;
  line-height: 1.3;
}
.nav-hint {
  font-size: 11px;
  color: var(--text-3);
  line-height: 1.3;
}
.nav-item.is-active .nav-hint {
  color: rgba(67, 56, 202, 0.72);
}
.nav-rail {
  position: absolute;
  left: -14px;
  top: 50%;
  transform: translateY(-50%) scaleY(0);
  width: 3px;
  height: 22px;
  border-radius: 0 3px 3px 0;
  background: var(--brand);
  transition: transform 0.22s var(--ease-out);
}
.nav-item.is-active .nav-rail {
  transform: translateY(-50%) scaleY(1);
}

/* 底部登录状态 */
.sidebar-foot {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}
.user-card,
.guest-card {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
  border-radius: var(--r-md);
  background: var(--surface-2);
}
.guest-card {
  background: transparent;
  padding: 2px 4px 4px;
}
.guest-text {
  font-size: 12px;
  color: var(--text-3);
  line-height: 1.5;
}
.avatar {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: var(--brand-gradient);
  color: #fff;
  font-weight: 700;
  font-size: 13px;
  flex: 0 0 auto;
}
.user-meta {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.user-name {
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.user-state {
  font-size: 11px;
  color: var(--text-3);
}
.auth-btn {
  height: 38px;
  font-size: 13px;
}

@media (max-width: 900px) {
  .sidebar {
    width: 72px;
    flex-basis: 72px;
    padding: 18px 10px 14px;
  }
  .brand-text,
  .nav-labels,
  .user-meta,
  .guest-card {
    display: none;
  }
  .brand {
    justify-content: center;
    padding: 0 0 18px;
  }
  .nav-item {
    justify-content: center;
    padding: 9px;
  }
  .nav-rail {
    left: -10px;
  }
  .user-card {
    justify-content: center;
    padding: 6px;
  }
  .auth-btn {
    padding: 0 8px;
  }
  .auth-btn :deep(.app-icon) {
    margin: 0;
  }
}
</style>
