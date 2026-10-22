<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import AppIcon from './AppIcon.vue'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

/**
 * 登录 & 注册 & 找回密码页面主体（由 views/AuthView.vue 在独立整屏布局中渲染）。
 *
 * 后端账号策略调整后：
 *   - 登录凭据为「邮箱」（唯一索引），用户名只是注册时填写的昵称
 *   - 注册需要邮箱验证码（6 位数字 / 5 分钟有效）
 *   - 找回密码同样是「邮箱 + 验证码 + 新密码」两步流程
 */
const emit = defineEmits(['success'])

const auth = useAuthStore()
const ui = useUiStore()

/** 'login' | 'register' | 'forgot' */
const mode = ref('login')
const form = reactive({ username: '', email: '', password: '', repassword: '', code: '' })
const touched = reactive({ username: false, email: false, password: false, repassword: false, code: false })
const localError = ref('')

/** 验证码重发倒计时（秒）。后端对同一邮箱有 60s 冷却：验证码 TTL 5 分钟，剩余 >4 分钟即拒绝重发。 */
const RESEND_SECONDS = 60
const countdown = ref(0)
/** 验证码已发往的邮箱：用于提示「改了邮箱需要重新获取验证码」。 */
const codeSentTo = ref('')
let countdownTimer = null

// 与后端 binding 规则保持一致（pkg/binding_validator.go）
const USERNAME_RE = /^\S{2,15}$/
const EMAIL_RE = /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/
const PASSWORD_RE = /^[a-zA-Z0-9]{8,20}$/
const CODE_RE = /^[0-9]{6}$/

const isRegister = computed(() => mode.value === 'register')
const isForgot = computed(() => mode.value === 'forgot')
/** 需要邮箱验证码的模式（注册 / 找回密码）。 */
const needsCode = computed(() => isRegister.value || isForgot.value)

const email = computed(() => form.email.trim().toLowerCase())

const errors = computed(() => {
  const result = {}

  if (isRegister.value) {
    const username = form.username.trim()
    if (!username) result.username = '请输入用户名'
    else if (!USERNAME_RE.test(username)) result.username = '用户名需 2-15 位，不能包含空格'
  }

  if (!form.email.trim()) result.email = '请输入邮箱'
  else if (!EMAIL_RE.test(form.email.trim())) result.email = '邮箱格式不正确'

  if (!form.password) result.password = isForgot.value ? '请输入新密码' : '请输入密码'
  else if (!PASSWORD_RE.test(form.password)) result.password = '密码需为 8-20 位字母或数字'

  if (needsCode.value) {
    if (!form.repassword) result.repassword = '请再次输入密码'
    else if (form.repassword !== form.password) result.repassword = '两次输入的密码不一致'

    if (!form.code.trim()) result.code = '请输入邮箱验证码'
    else if (!CODE_RE.test(form.code.trim())) result.code = '验证码为 6 位数字'
  }

  return result
})

const isValid = computed(() => Object.keys(errors.value).length === 0)
const displayError = computed(() => localError.value || auth.error)

/** 发送验证码所需字段是否合法（未通过时禁用「获取验证码」）。 */
const canSendCode = computed(() => {
  const e = errors.value
  if (isRegister.value) return !e.username && !e.email && !e.password && !e.repassword
  return !e.email && !e.password && !e.repassword
})

const codeButtonText = computed(() => {
  if (auth.codeSending) return '发送中…'
  if (countdown.value > 0) return `${countdown.value}s 后重发`
  return codeSentTo.value ? '重新获取' : '获取验证码'
})

const codeSentElsewhere = computed(() => Boolean(codeSentTo.value) && codeSentTo.value !== email.value)

const submitText = computed(() => {
  if (auth.submitting) return '处理中…'
  if (isRegister.value) return '注册并生成画像'
  if (isForgot.value) return '重置密码'
  return '登录'
})

function showError(field) {
  return touched[field] ? errors.value[field] : ''
}

function stopCountdown() {
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
  countdown.value = 0
}

function startCountdown() {
  stopCountdown()
  countdown.value = RESEND_SECONDS
  countdownTimer = setInterval(() => {
    countdown.value -= 1
    if (countdown.value <= 0) stopCountdown()
  }, 1000)
}

onBeforeUnmount(stopCountdown)

function switchMode(next) {
  if (mode.value === next) return
  mode.value = next
  localError.value = ''
  auth.clearError()
  // 邮箱作为账号标识在三种模式间保留，其余敏感字段一律清空
  form.password = ''
  form.repassword = ''
  form.code = ''
  touched.username = false
  touched.email = false
  touched.password = false
  touched.repassword = false
  touched.code = false
  codeSentTo.value = ''
  stopCountdown()
}

watch(mode, () => {
  localError.value = ''
})

async function handleSendCode() {
  touched.email = true
  touched.password = true
  touched.repassword = true
  touched.username = true
  localError.value = ''
  auth.clearError()

  if (!canSendCode.value) {
    localError.value = '请先正确填写上方信息，再获取验证码'
    return
  }

  const payload = {
    email: email.value,
    password: form.password,
    repassword: form.repassword,
  }

  const message = isRegister.value
    ? await auth.sendSignUpCode({ ...payload, username: form.username.trim() })
    : await auth.sendResetCode(payload)

  if (message) {
    codeSentTo.value = email.value
    startCountdown()
    ui.showToast(message, 'success')
    return
  }

  // 冷却期内重发会被后端拒绝（409），按剩余时间恢复倒计时，避免用户反复点击
  if (auth.error.includes('请') && auth.error.includes('重试')) {
    startCountdown()
  }
}

async function handleSubmit() {
  touched.username = true
  touched.email = true
  touched.password = true
  touched.repassword = true
  touched.code = true
  localError.value = ''
  auth.clearError()

  if (!isValid.value) {
    localError.value = '请检查表单填写是否正确'
    return
  }

  if (isForgot.value) {
    const ok = await auth.resetPassword({
      email: email.value,
      password: form.password,
      code: form.code.trim(),
    })
    if (!ok) return
    ui.showToast('密码重置成功，请使用新密码登录', 'success')
    // 回到登录页并保留邮箱，只需重新输入新密码
    switchMode('login')
    return
  }

  const ok = isRegister.value
    ? await auth.register({
        username: form.username.trim(),
        email: email.value,
        password: form.password,
        code: form.code.trim(),
      })
    : await auth.login({ email: email.value, password: form.password })

  if (!ok) return

  ui.showToast(isRegister.value ? '注册成功，请完善资料' : '登录成功', 'success')
  form.password = ''
  form.repassword = ''
  form.code = ''
  emit('success', mode.value)
}
</script>

<template>
  <section class="auth">
    <header class="auth-hero">
      <span class="hero-badge"><AppIcon name="sparkles" :size="22" /></span>
      <h1 class="hero-title">开启你的专属流式推荐</h1>
      <p class="hero-text">
        使用邮箱即可登录，注册仅需一步。完善资料后系统会为你生成专属画像，推荐会越用越懂你。
      </p>
    </header>

    <div class="auth-card">
      <!-- 登录 / 注册 切换（找回密码由登录表单内的按钮进入） -->
      <div class="tabs" role="tablist">
        <button
          type="button"
          role="tab"
          class="tab"
          :class="{ 'is-active': mode === 'login' }"
          :aria-selected="mode === 'login'"
          @click="switchMode('login')"
        >
          登录
        </button>
        <button
          type="button"
          role="tab"
          class="tab"
          :class="{ 'is-active': mode === 'register' }"
          :aria-selected="mode === 'register'"
          @click="switchMode('register')"
        >
          注册
        </button>
        <span
          class="tab-thumb"
          :class="[`at-${mode}`, { 'is-hidden': mode === 'forgot' }]"
          aria-hidden="true"
        />
      </div>

      <form class="auth-form" novalidate @submit.prevent="handleSubmit">
        <p v-if="isForgot" class="form-head">
          <button type="button" class="link-btn back-btn" @click="switchMode('login')">
            <AppIcon name="arrowRight" :size="13" class="back-icon" />
            返回登录
          </button>
          <span class="form-head-text">通过邮箱验证码重置密码</span>
        </p>

        <div v-if="isRegister" class="field">
          <label class="field-label" for="auth-username">用户名</label>
          <input
            id="auth-username"
            v-model="form.username"
            class="input"
            :class="{ 'is-invalid': showError('username') }"
            type="text"
            autocomplete="nickname"
            placeholder="2-15 位，不含空格（仅作昵称展示）"
            @blur="touched.username = true"
          />
          <span v-if="showError('username')" class="field-error">{{ showError('username') }}</span>
        </div>

        <div class="field">
          <label class="field-label" for="auth-email">邮箱</label>
          <input
            id="auth-email"
            v-model="form.email"
            class="input"
            :class="{ 'is-invalid': showError('email') }"
            type="email"
            inputmode="email"
            autocomplete="email"
            placeholder="用于登录与接收验证码"
            @blur="touched.email = true"
          />
          <span v-if="showError('email')" class="field-error">{{ showError('email') }}</span>
        </div>

        <div class="field">
          <div class="field-head">
            <label class="field-label" for="auth-password">
              {{ isForgot ? '新密码' : '密码' }}
            </label>
            <button
              v-if="mode === 'login'"
              type="button"
              class="link-btn"
              @click="switchMode('forgot')"
            >
              忘记密码？
            </button>
          </div>
          <input
            id="auth-password"
            v-model="form.password"
            class="input"
            :class="{ 'is-invalid': showError('password') }"
            type="password"
            :autocomplete="mode === 'login' ? 'current-password' : 'new-password'"
            placeholder="8-20 位字母或数字"
            @blur="touched.password = true"
          />
          <span v-if="showError('password')" class="field-error">{{ showError('password') }}</span>
        </div>

        <div v-if="needsCode" class="field">
          <label class="field-label" for="auth-repassword">确认密码</label>
          <input
            id="auth-repassword"
            v-model="form.repassword"
            class="input"
            :class="{ 'is-invalid': showError('repassword') }"
            type="password"
            autocomplete="new-password"
            placeholder="再次输入密码"
            @blur="touched.repassword = true"
          />
          <span v-if="showError('repassword')" class="field-error">{{ showError('repassword') }}</span>
        </div>

        <div v-if="needsCode" class="field">
          <label class="field-label" for="auth-code">邮箱验证码</label>
          <div class="code-row">
            <input
              id="auth-code"
              v-model="form.code"
              class="input code-input"
              :class="{ 'is-invalid': showError('code') }"
              type="text"
              inputmode="numeric"
              maxlength="6"
              autocomplete="one-time-code"
              placeholder="6 位数字"
              @blur="touched.code = true"
            />
            <button
              type="button"
              class="btn btn-ghost code-btn"
              :disabled="countdown > 0 || auth.codeSending || !canSendCode"
              @click="handleSendCode"
            >
              {{ codeButtonText }}
            </button>
          </div>
          <span v-if="showError('code')" class="field-error">{{ showError('code') }}</span>
          <span v-else-if="codeSentElsewhere" class="field-error">
            验证码已发往 {{ codeSentTo }}，当前邮箱已修改，请重新获取
          </span>
          <span v-else class="field-hint">验证码 5 分钟内有效，同一邮箱 60 秒后可重新获取</span>
        </div>

        <div v-if="displayError" class="alert alert-error">
          <AppIcon name="alert" :size="15" />
          <span>{{ displayError }}</span>
        </div>

        <button type="submit" class="btn btn-primary btn-block submit" :disabled="auth.submitting">
          <AppIcon :name="isRegister ? 'plus' : isForgot ? 'refresh' : 'login'" :size="16" />
          {{ submitText }}
        </button>

        <button
          v-if="isForgot"
          type="button"
          class="btn btn-quiet btn-block"
          @click="switchMode('login')"
        >
          想起密码了，返回登录
        </button>
      </form>

      <p class="auth-tip">
        <AppIcon name="info" :size="13" />
        <span v-if="isRegister">注册后需填写基本信息，用于生成你的用户画像。</span>
        <span v-else-if="isForgot">重置成功后请使用新密码重新登录。</span>
        <span v-else>还没有账号？点击上方「注册」即可创建；忘记密码可点击「忘记密码？」。</span>
      </p>
    </div>

    <ul class="auth-points">
      <li><AppIcon name="check" :size="14" />邮箱登录：账号唯一，便于找回密码</li>
      <li><AppIcon name="check" :size="14" />流式推荐：滚动到底自动加载更多</li>
      <li><AppIcon name="check" :size="14" />个性化画像：越用越懂你的兴趣</li>
    </ul>
  </section>
</template>

<style scoped>
.auth {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 26px;
  max-width: 460px;
  margin: 0 auto;
  padding: 26px 0 8px;
}
.auth-hero {
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}
.hero-badge {
  display: grid;
  place-items: center;
  width: 56px;
  height: 56px;
  border-radius: 18px;
  background: var(--brand-gradient);
  color: #fff;
  box-shadow: 0 16px 32px -14px rgba(79, 70, 229, 0.9);
}
.hero-title {
  font-size: 23px;
  font-weight: 750;
  letter-spacing: -0.02em;
}
.hero-text {
  font-size: 13.5px;
  line-height: 1.7;
  color: var(--text-2);
  max-width: 400px;
}

.auth-card {
  width: 100%;
  padding: 22px;
  border-radius: var(--r-xl);
  background: var(--surface);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-md);
}

/* 分段切换 */
.tabs {
  position: relative;
  display: grid;
  grid-template-columns: 1fr 1fr;
  padding: 4px;
  border-radius: var(--r-sm);
  background: var(--surface-2);
  margin-bottom: 20px;
}
.tab {
  position: relative;
  z-index: 1;
  height: 34px;
  border-radius: 8px;
  font-size: 13.5px;
  font-weight: 600;
  color: var(--text-2);
  transition: color 0.2s var(--ease-soft);
}
.tab.is-active {
  color: var(--brand-dark);
}
.tab-thumb {
  position: absolute;
  top: 4px;
  left: 4px;
  width: calc(50% - 4px);
  height: 34px;
  border-radius: 8px;
  background: #fff;
  box-shadow: var(--shadow-xs);
  transition: transform 0.28s var(--ease-out), opacity 0.2s var(--ease-soft);
}
.tab-thumb.at-register {
  transform: translateX(100%);
}
/* 找回密码不属于两个页签中的任何一个：隐藏滑块，避免误导 */
.tab-thumb.is-hidden {
  opacity: 0;
}

.auth-form {
  display: flex;
  flex-direction: column;
  gap: 15px;
}
.form-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding-bottom: 2px;
}
.form-head-text {
  font-size: 12px;
  color: var(--text-3);
}
.field-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 18px;
}
.field-error {
  font-size: 12px;
  color: var(--danger);
}
.link-btn {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--brand);
  padding: 0;
  border-radius: 4px;
  transition: color 0.18s var(--ease-soft);
}
.link-btn:hover {
  color: var(--brand-dark);
  text-decoration: underline;
}
.back-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.back-icon {
  transform: rotate(180deg);
}
.code-row {
  display: flex;
  gap: 8px;
}
.code-input {
  flex: 1 1 auto;
  min-width: 0;
  letter-spacing: 0.18em;
}
.code-btn {
  flex: 0 0 auto;
  height: 44px;
  padding: 0 14px;
  font-size: 13px;
}
.submit {
  margin-top: 4px;
}

.auth-tip {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin-top: 15px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
  font-size: 12px;
  color: var(--text-3);
  line-height: 1.5;
}

.auth-points {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  font-size: 12.5px;
  color: var(--text-2);
}
.auth-points li {
  display: flex;
  align-items: center;
  gap: 8px;
}
.auth-points :deep(.app-icon) {
  color: var(--success);
}
</style>
