import client from './client'

/**
 * 账号体系（与后端 backend/api/route/users_route.go 对齐）
 *
 * 登录凭据已从「用户名」改为「邮箱」：邮箱是唯一索引，用户名仅作昵称展示。
 * 注册与找回密码都走「邮箱验证码」两步流程：
 *   1) 先调 xxx/email 发送 6 位验证码（5 分钟内有效，同一邮箱 60 秒内不可重复发送）
 *   2) 再带上 code 完成注册 / 重置密码
 */

/** 发送注册验证码。后端要求用户名/密码一起提交做格式校验，但只使用 email。 */
export function sendSignUpCode({ username, email, password, repassword }) {
  return client.post('/users/signup/email', { username, email, password, repassword }).then((res) => res.data)
}

/** 注册（需带上邮箱验证码）。成功后端直接下发 accessToken / refreshToken。 */
export function ({ username, email, password, code }) {
  return client.post('/users/signup', { username, email, password, code }).then((res) => res.data)
}

/** 登录（邮箱 + 密码）。 */
export function logIn({ email, password }) {
  return client.post('/users/login', { email, password }).then((res) => res.data)
}

/**
 * 发送「找回密码」验证码。
 * 后端会先校验邮箱是否已注册；密码字段仅用于前后端一致的格式校验，重置以 PATCH 为准。
 */
export function sendResetCode({ email, password, repassword }) {
  return client.post('/users/forget-password/email', { email, password, repassword }).then((res) => res.data)
}

/** 重置密码（邮箱 + 新密码 + 验证码），成功后需用新密码重新登录。 */
export function resetPassword({ email, password, repassword, code }) {
  return client.patch('/users/forget-password', { email, password, repassword, code }).then((res) => res.data)
}

/** 读取当前用户资料 + 画像摘要（summary）。 */
export function fetchProfile() {
  return client.get('/users/info').then((res) => res.data)
}

/**
 * 填写基础资料（`POST /users/info` → FillInfo）。
 * 只写库、**不投递画像任务**：注册引导流程需要再显式调用 `requestRegenerateSummary()`。
 */
export function fillProfile(patch) {
  return client.post('/users/info', patch).then((res) => res.data)
}

/**
 * 更新资料（`PUT /users/info` → UpdateInfo，部分更新）。
 * 后端会按本次改动的字段增量更新画像（投递 ReProfileWithNewInfo 任务）。
 * 后端为指针字段 + omitempty 语义，只提交需要变更的字段即可。
 */
export function updateProfile(patch) {
  return client.put('/users/info', patch).then((res) => res.data)
}

/** 手动触发「画像重新生成」（异步任务，立即返回 202）。首次填写资料后由前端显式调用。 */
export function requestRegenerateSummary() {
  return client.put('/users/summary').then((res) => res.data)
}

/** 手动触发「画像增量更新」（异步任务，立即返回 202）：依据最新互动更新画像。 */
export function requestIncrementalSummary() {
  return client.post('/users/summary').then((res) => res.data)
}
