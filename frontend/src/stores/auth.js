import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import * as usersApi from '@/api/users'
import { extractMessage, session } from '@/api/client'

/** 资料字段（与后端 domain.InfoRequest / ShowProfileResponse 对齐）。 */
export const PROFILE_TEXT_FIELDS = ['job', 'education', 'location', 'organization']

function emptyProfile() {
  return {
    age: 0,
    gender: '',
    job: '',
    education: '',
    location: '',
    organization: '',
    interestingFields: [],
    summary: '',
  }
}

function normalizeProfile(data) {
  const base = emptyProfile()
  if (!data || typeof data !== 'object') return base
  return {
    age: Number.isFinite(Number(data.age)) ? Number(data.age) : 0,
    gender: data.gender || '',
    job: data.job || '',
    education: data.education || '',
    location: data.location || '',
    organization: data.organization || '',
    interestingFields: Array.isArray(data.interestingFields) ? data.interestingFields : [],
    summary: typeof data.summary === 'string' ? data.summary : '',
  }
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

export const useAuthStore = defineStore('auth', () => {
  const accessToken = ref(session.accessToken)
  /** 展示用昵称：仅在注册时后端要求填写，登录凭据是邮箱。 */
  const username = ref(session.username)
  /** 账号唯一标识（登录凭据），后端资料接口不回传，故在本地留档。 */
  const email = ref(session.email)
  const profile = ref(emptyProfile())
  const profileLoaded = ref(false)

  const submitting = ref(false)
  /** 「获取验证码」按钮进行中（注册 / 找回密码共用）。 */
  const codeSending = ref(false)
  const profileSaving = ref(false)
  const summaryGenerating = ref(false)
  const error = ref('')
  const notice = ref('')

  // 用户主动跳过「完善资料」引导（持久化，避免刷新后被反复弹回引导页）
  const onboardingDismissed = ref(session.onboardingSkipped)

  const isLoggedIn = computed(() => Boolean(accessToken.value))

  /** 是否填写过任意一项画像输入字段（age=0 / 空串视为未填写）。 */
  const hasProfileDetail = computed(() => {
    const p = profile.value
    return Boolean(
      p.age > 0 ||
        p.gender ||
        p.job ||
        p.education ||
        p.location ||
        p.organization ||
        (Array.isArray(p.interestingFields) && p.interestingFields.length > 0),
    )
  })

  const hasSummary = computed(() => Boolean(profile.value.summary && profile.value.summary.trim()))

  /** 注册/登录后资料仍是空白 → 需要进入「信息填写 + 生成画像」引导。 */
  const needsOnboarding = computed(
    () => isLoggedIn.value && profileLoaded.value && !hasProfileDetail.value && !onboardingDismissed.value,
  )

  const displayName = computed(() => username.value || email.value || '访客')

  /** 头像首字母：昵称优先，其次邮箱首字母。 */
  const initial = computed(() => {
    const source = (username.value || email.value || '').trim()
    return source ? source.charAt(0).toUpperCase() : '游'
  })

  /** 副标题：已设置昵称时展示邮箱（账号唯一标识）。 */
  const accountLabel = computed(() => (username.value && email.value ? email.value : ''))

  function applySession(data, { username: name = '', email: mail = '' } = {}) {
    accessToken.value = data?.accessToken || ''
    username.value = name || ''
    email.value = mail || email.value
    session.save({
      accessToken: accessToken.value,
      refreshToken: data?.refreshToken || '',
      username: username.value,
      email: email.value,
    })
  }

  function clearError() {
    error.value = ''
    notice.value = ''
  }

  /**
   * 发送邮箱验证码（注册 / 找回密码共用）。
   * 返回后端提示文案；失败时返回空串并把错误写入 error。
   */
  async function requestCode(apiFn, payload) {
    codeSending.value = true
    clearError()
    try {
      const data = await apiFn(payload)
      return extractMessage(data) || '验证码已发送，请注意查收'
    } catch (err) {
      error.value = err.message
      return ''
    } finally {
      codeSending.value = false
    }
  }

  /** 注册第一步：向邮箱发送 6 位验证码。 */
  function sendSignUpCode(payload) {
    return requestCode(usersApi.sendSignUpCode, payload)
  }

  /** 找回密码第一步：校验邮箱已注册并发送验证码。 */
  function sendResetCode(payload) {
    return requestCode(usersApi.sendResetCode, payload)
  }

  async function register({ username: name, email: mail, password, code }) {
    submitting.value = true
    clearError()
    try {
      const data = await usersApi.signUp({ username: name, email: mail, password, code })
      applySession(data, { username: name, email: mail })
      // 注册后立即拉取（空）资料，触发信息填写引导
      await loadProfile({ silent: true })
      notice.value = '注册成功，请完善资料以生成你的专属画像'
      return true
    } catch (err) {
      error.value = err.message
      return false
    } finally {
      submitting.value = false
    }
  }

  async function login({ email: mail, password }) {
    submitting.value = true
    clearError()
    try {
      const data = await usersApi.logIn({ email: mail, password })
      // 登录只提供邮箱：昵称留空，界面回退展示邮箱
      applySession(data, { username: '', email: mail })
      await loadProfile({ silent: true })
      return true
    } catch (err) {
      error.value = err.message
      return false
    } finally {
      submitting.value = false
    }
  }

  /**
   * 找回密码第二步：邮箱 + 验证码 + 新密码。
   * 后端重置成功后不签发 token，需回到登录页用新密码登录。
   */
  async function resetPassword({ email: mail, password, code }) {
    submitting.value = true
    clearError()
    try {
      await usersApi.resetPassword({ email: mail, password, repassword: password, code })
      notice.value = '密码重置成功，请使用新密码登录'
      return true
    } catch (err) {
      error.value = err.message
      return false
    } finally {
      submitting.value = false
    }
  }

  function logout() {
    accessToken.value = ''
    username.value = ''
    email.value = ''
    profile.value = emptyProfile()
    profileLoaded.value = false
    onboardingDismissed.value = false
    clearError()
    session.clear()
  }

  /** 登录态失效（401）时由 api 层回调。 */
  function handleUnauthorized() {
    if (!accessToken.value) return
    logout()
    notice.value = '登录已过期，请重新登录'
  }

  async function loadProfile({ silent = false } = {}) {
    if (!isLoggedIn.value) return null
    if (!silent) profileLoaded.value = false
    try {
      const data = await usersApi.fetchProfile()
      profile.value = normalizeProfile(data)
      profileLoaded.value = true
      return profile.value
    } catch (err) {
      if (!silent) error.value = err.message
      profileLoaded.value = true
      return null
    }
  }

  /**
   * 保存资料，并按 regenerate 决定「谁投递画像任务」。
   *
   * 后端已把「填资料」与「生成画像」拆成两个接口：
   *   - POST /users/info（FillInfo）：只写库，**不投递任何画像任务**
   *   - PUT  /users/info（UpdateInfo）：写库 + 后端按本次改动字段增量更新画像
   *   - PUT  /users/summary（GenerateSummary）：显式投递「重新生成画像」任务
   *
   * 因此：
   *   - `regenerate: 'client'`（注册引导首次填写）→ POST /users/info 落库后，前端再显式 PUT /users/summary
   *   - `regenerate: 'server'`（「我的」改资料）→ PUT /users/info，由后端按改动字段增量更新画像（保留互动沉淀）
   *   - `regenerate: 'none'` → 只保存资料，不动画像
   *
   * 画像生成是异步任务，这里**不阻塞**表单提交：立即返回，
   * 由 summaryGenerating / profile.summary 驱动 UI 就地更新。
   */
  async function saveProfile(patch, { regenerate = 'client' } = {}) {
    profileSaving.value = true
    clearError()
    try {
      if (regenerate === 'server') await usersApi.updateProfile(patch)
      else await usersApi.fillProfile(patch)
      onboardingDismissed.value = false
      session.setOnboardingSkipped(false)
      // 以服务端返回为准重新拉取资料（本地不做乐观合并，避免字段命名差异）
      await loadProfile({ silent: true })
      if (regenerate === 'client') {
        notice.value = '资料已保存，正在生成用户画像…'
        // 故意不 await：后台轮询，界面通过状态变化自行更新
        generateSummary({ mode: 'regenerate' })
      } else if (regenerate === 'server') {
        notice.value = '资料已更新，正在按新资料更新画像…'
        // 后端任务已投递，这里只等结果，不再重复触发
        awaitSummaryUpdate()
      } else {
        notice.value = '资料已保存'
      }
      return true
    } catch (err) {
      error.value = err.message
      return false
    } finally {
      profileSaving.value = false
    }
  }

  /**
   * 轮询资料接口，直到画像出现/变化（后端画像是异步任务）。
   * summaryGenerating 驱动界面上的「生成中」状态。
   */
  async function pollSummary({ previous = profile.value.summary, timeoutMs = 45000 } = {}) {
    const deadline = Date.now() + timeoutMs
    while (Date.now() < deadline) {
      await sleep(2500)
      const latest = await loadProfile({ silent: true })
      const summary = latest?.summary || ''
      if (summary && summary !== previous) return true
    }
    return false
  }

  /**
   * 触发画像重新生成（PUT /users/summary）。
   * 后端为异步任务（202 Accepted），触发后轮询资料接口直到 summary 出现/变化。
   */
  async function generateSummary({ mode = 'regenerate', timeoutMs = 45000 } = {}) {
    if (!isLoggedIn.value) return false
    const previous = profile.value.summary
    summaryGenerating.value = true
    try {
      if (mode === 'incremental') await usersApi.requestIncrementalSummary()
      else await usersApi.requestRegenerateSummary()

      const done = await pollSummary({ previous, timeoutMs })
      notice.value = done ? '用户画像已生成' : '画像生成任务已提交，稍后可点击「刷新画像」查看结果'
      return done
    } catch (err) {
      error.value = err.message
      return false
    } finally {
      summaryGenerating.value = false
    }
  }

  /** 后端已投递画像任务时（PUT /users/info），只等结果，不再重复触发。 */
  async function awaitSummaryUpdate({ timeoutMs = 45000 } = {}) {
    if (!isLoggedIn.value) return false
    const previous = profile.value.summary
    summaryGenerating.value = true
    try {
      const done = await pollSummary({ previous, timeoutMs })
      notice.value = done ? '用户画像已更新' : '画像更新任务已提交，稍后可在「我的」中查看'
      return done
    } finally {
      summaryGenerating.value = false
    }
  }

  function skipOnboarding() {
    onboardingDismissed.value = true
    session.setOnboardingSkipped(true)
  }

  function reopenOnboarding() {
    onboardingDismissed.value = false
    session.setOnboardingSkipped(false)
  }

  return {
    accessToken,
    username,
    email,
    profile,
    profileLoaded,
    submitting,
    codeSending,
    profileSaving,
    summaryGenerating,
    error,
    notice,
    onboardingDismissed,
    isLoggedIn,
    hasProfileDetail,
    hasSummary,
    needsOnboarding,
    displayName,
    initial,
    accountLabel,
    register,
    login,
    logout,
    sendSignUpCode,
    sendResetCode,
    resetPassword,
    handleUnauthorized,
    loadProfile,
    saveProfile,
    generateSummary,
    awaitSummaryUpdate,
    skipOnboarding,
    reopenOnboarding,
    clearError,
  }
})
