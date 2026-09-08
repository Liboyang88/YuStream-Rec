<script setup>
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()

const mode = ref('login') // 'login' | 'signup'
const loading = ref(false)
const error = ref('')

const loginForm = reactive({ username: '', password: '' })
const signupForm = reactive({ username: '', password: '', repassword: '' })

function switchMode(m) {
  mode.value = m
  error.value = ''
}

async function onLogin() {
  if (!loginForm.username || !loginForm.password) {
    error.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await auth.login(loginForm.username, loginForm.password)
    router.push('/')
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function onSignup() {
  if (!signupForm.username || !signupForm.password) {
    error.value = '请输入用户名和密码'
    return
  }
  if (signupForm.password !== signupForm.repassword) {
    error.value = '两次密码不一致'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await auth.signup(signupForm.username, signupForm.password)
    router.push('/info') // 注册后去填写个人信息
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="auth-page">
    <div class="auth-card">
      <h1 class="brand">DCPro</h1>
      <p class="subtitle">数字人文学术文献推荐</p>

      <div class="tabs">
        <button :class="{ active: mode === 'login' }" @click="switchMode('login')">登录</button>
        <button :class="{ active: mode === 'signup' }" @click="switchMode('signup')">注册</button>
      </div>

      <form v-if="mode === 'login'" class="form" @submit.prevent="onLogin">
        <input v-model="loginForm.username" placeholder="用户名" autocomplete="username" />
        <input v-model="loginForm.password" type="password" placeholder="密码" autocomplete="current-password" />
        <button class="primary" type="submit" :disabled="loading">{{ loading ? '登录中…' : '登录' }}</button>
      </form>

      <form v-else class="form" @submit.prevent="onSignup">
        <input v-model="signupForm.username" placeholder="用户名（2-15 位）" autocomplete="username" />
        <input v-model="signupForm.password" type="password" placeholder="密码（8-20 位字母/数字）" autocomplete="new-password" />
        <input v-model="signupForm.repassword" type="password" placeholder="确认密码" autocomplete="new-password" />
        <button class="primary" type="submit" :disabled="loading">{{ loading ? '注册中…' : '注册' }}</button>
      </form>

      <p v-if="error" class="error">{{ error }}</p>
    </div>
  </div>
</template>

<style scoped>
.auth-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #ffeef1 0%, #fff 40%, #f5f6f7 100%);
  padding: 24px;
}
.auth-card {
  width: 100%;
  max-width: 380px;
  background: #fff;
  border-radius: 16px;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.08);
  padding: 36px 32px;
}
.brand {
  margin: 0;
  font-size: 30px;
  font-weight: 800;
  color: var(--red);
  letter-spacing: 1px;
}
.subtitle {
  margin: 4px 0 24px;
  color: var(--text-2);
  font-size: 14px;
}
.tabs {
  display: flex;
  gap: 8px;
  background: var(--bg);
  border-radius: 10px;
  padding: 4px;
  margin-bottom: 20px;
}
.tabs button {
  flex: 1;
  padding: 9px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--text-2);
  font-size: 14px;
  font-weight: 600;
}
.tabs button.active {
  background: #fff;
  color: var(--red);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.08);
}
.form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.form input {
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 10px;
  font-size: 14px;
  outline: none;
}
.form input:focus {
  border-color: var(--red);
}
</style>
