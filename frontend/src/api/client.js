import axios from 'axios'

const ACCESS_TOKEN_KEY = 'dcrec.access_token'
const REFRESH_TOKEN_KEY = 'dcrec.refresh_token'
const USERNAME_KEY = 'dcrec.username'
const EMAIL_KEY = 'dcrec.email'
const ONBOARDING_SKIP_KEY = 'dcrec.onboarding_skipped'

function readLocal(key) {
  try {
    return window.localStorage.getItem(key) || ''
  } catch {
    return ''
  }
}

function writeLocal(key, value) {
  try {
    if (value) window.localStorage.setItem(key, value)
    else window.localStorage.removeItem(key)
  } catch {
    /* localStorage 不可用时静默降级为「本次会话不持久化」 */
  }
}

/**
 * 登录态存储：accessToken 用于 Authorization 头，refreshToken 预留续期。
 * 登录现在以「邮箱」为唯一凭据，用户名只是展示用昵称，
 * 且后端 /users/info 不回传 email/username，因此两者都在本地留一份用于界面展示。
 */
export const session = {
  get accessToken() {
    return readLocal(ACCESS_TOKEN_KEY)
  },
  get refreshToken() {
    return readLocal(REFRESH_TOKEN_KEY)
  },
  get username() {
    return readLocal(USERNAME_KEY)
  },
  get email() {
    return readLocal(EMAIL_KEY)
  },
  get isLoggedIn() {
    return Boolean(readLocal(ACCESS_TOKEN_KEY))
  },
  /** 用户是否主动跳过了「完善资料」引导（持久化，避免刷新后被反复弹回引导页） */
  get onboardingSkipped() {
    return readLocal(ONBOARDING_SKIP_KEY) === '1'
  },
  setOnboardingSkipped(skipped) {
    writeLocal(ONBOARDING_SKIP_KEY, skipped ? '1' : '')
  },
  save({ accessToken, refreshToken, username, email } = {}) {
    if (accessToken !== undefined) writeLocal(ACCESS_TOKEN_KEY, accessToken)
    if (refreshToken !== undefined) writeLocal(REFRESH_TOKEN_KEY, refreshToken)
    if (username !== undefined) writeLocal(USERNAME_KEY, username)
    if (email !== undefined) writeLocal(EMAIL_KEY, email)
  },
  clear() {
    writeLocal(ACCESS_TOKEN_KEY, '')
    writeLocal(REFRESH_TOKEN_KEY, '')
    writeLocal(USERNAME_KEY, '')
    writeLocal(EMAIL_KEY, '')
    writeLocal(ONBOARDING_SKIP_KEY, '')
  },
}

/** 统一的接口错误对象，携带 HTTP 状态码与后端原始响应体。 */
export class ApiError extends Error {
  constructor(message, { status = 0, payload = null } = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.payload = payload
  }
}

/**
 * 后端校验错误用 Go 结构体字段名做 key（见 pkg/binding_validator.go 的 FormatValidationErr），
 * 这里映射成中文，避免界面出现 "Email：邮箱格式错误" 这类混杂文案。
 */
const FIELD_LABELS = {
  Username: '用户名',
  Email: '邮箱',
  Password: '密码',
  RepeatPassword: '确认密码',
  Repassword: '确认密码',
  Code: '验证码',
}

/**
 * 把任意形状的响应体转成一句人话。
 * 后端存在两种错误信封，这里都要兼容：
 *   1) 新版：{ "kind": "...", "msg": "..." } 或 { "kind": "...", "msg": { "Username": "..." } }
 *   2) 旧版：{ "error": "..." } 或 { "error": { "Username": "..." } }
 */
function describe(value) {
  if (value == null) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (Array.isArray(value)) return value.map(describe).filter(Boolean).join('；')
  if (typeof value === 'object') {
    return Object.entries(value)
      .map(([field, detail]) => {
        const label = FIELD_LABELS[field] || field
        if (typeof detail === 'string' && detail) {
          // 字段级校验错误：带上中文字段名更好定位（文案里已含字段名就不重复）
          return detail.includes(label) || detail.includes(field) ? detail : `${label}：${detail}`
        }
        return describe(detail)
      })
      .filter(Boolean)
      .join('；')
  }
  return String(value)
}

export function extractMessage(payload) {
  if (payload == null) return ''
  if (typeof payload === 'string') return payload
  if (typeof payload === 'object') {
    for (const key of ['msg', 'error', 'message', 'detail']) {
      const text = describe(payload[key])
      if (text) return text
    }
  }
  return describe(payload)
}

const client = axios.create({
  baseURL: '/api',
  timeout: 25000,
  headers: { 'Content-Type': 'application/json' },
})

client.interceptors.request.use((config) => {
  const token = session.accessToken
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

let unauthorizedHandler = null

/** 注册 401 回调（由 auth store 注入），用于清理登录态并提示重新登录。 */
export function setUnauthorizedHandler(handler) {
  unauthorizedHandler = handler
}

client.interceptors.response.use(
  (response) => response,
  (error) => {
    const status = error.response?.status ?? 0
    const payload = error.response?.data ?? null

    if (status === 401) {
      session.clear()
      if (typeof unauthorizedHandler === 'function') unauthorizedHandler()
    }

    let message = extractMessage(payload)
    if (!message) {
      if (error.code === 'ECONNABORTED') message = '请求超时，请稍后重试'
      else if (!error.response) message = '无法连接服务器，请确认后端服务（:8080）已启动'
      else message = `请求失败（HTTP ${status}）`
    }
    return Promise.reject(new ApiError(message, { status, payload }))
  },
)

export default client
