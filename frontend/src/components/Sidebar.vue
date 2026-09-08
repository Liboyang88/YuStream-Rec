<script setup>
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

defineEmits(['refresh', 'profile'])

const router = useRouter()
const auth = useAuthStore()

function logout() {
  auth.logout()
  router.push('/login')
}
</script>

<template>
  <aside class="sidebar">
    <div class="logo">DCPro</div>

    <nav class="nav">
      <button class="nav-btn" @click="$emit('refresh')">🏠 推荐流</button>
      <button class="nav-btn" @click="$emit('profile')">👤 我的信息</button>
      <button class="nav-btn disabled" disabled>⭐ 收藏 <span class="badge">预留</span></button>
      <button class="nav-btn disabled" disabled>🕘 历史 <span class="badge">预留</span></button>
      <button class="nav-btn disabled" disabled>⚙️ 设置 <span class="badge">预留</span></button>
    </nav>

    <div class="spacer"></div>
    <button class="nav-btn logout" @click="logout">退出登录</button>
  </aside>
</template>

<style scoped>
.sidebar {
  width: 220px;
  height: 100vh;
  position: sticky;
  top: 0;
  display: flex;
  flex-direction: column;
  background: #fff;
  border-right: 1px solid var(--border);
  padding: 20px 14px;
}
.logo {
  font-size: 24px;
  font-weight: 800;
  color: var(--red);
  padding: 0 10px 24px;
}
.nav {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.nav-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 11px 14px;
  border: none;
  border-radius: 10px;
  background: transparent;
  color: var(--text);
  font-size: 15px;
  text-align: left;
}
.nav-btn:hover:not(:disabled) {
  background: #ffeef1;
  color: var(--red);
}
.nav-btn.disabled {
  color: var(--text-3);
  cursor: not-allowed;
}
.badge {
  font-size: 10px;
  background: var(--bg);
  color: var(--text-3);
  padding: 1px 6px;
  border-radius: 999px;
}
.spacer {
  flex: 1;
}
.logout {
  color: var(--text-2);
}
.logout:hover:not(:disabled) {
  color: var(--red);
  background: #ffeef1;
}
</style>
