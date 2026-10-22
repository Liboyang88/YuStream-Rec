<script setup>
import { computed, ref } from 'vue'
import AppIcon from './AppIcon.vue'
import MathText from './MathText.vue'
import OnboardingForm from './OnboardingForm.vue'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

/** 「我的」子页面：用户信息 + 用户画像。未登录时内嵌登录/注册指引。 */
const auth = useAuthStore()
const ui = useUiStore()

const editing = ref(false)

const GENDER_LABEL = { male: '男', female: '女' }

const infoRows = computed(() => {
  const p = auth.profile
  return [
    // 登录凭据是邮箱；后端资料接口不回传，这里用本地留档的账号邮箱标识身份
    { icon: 'info', label: '账号邮箱', value: auth.email },
    { icon: 'user', label: '年龄', value: p.age > 0 ? `${p.age} 岁` : '' },
    { icon: 'user', label: '性别', value: GENDER_LABEL[p.gender] || '' },
    { icon: 'layers', label: '职业', value: p.job },
    { icon: 'book', label: '学历', value: p.education },
    { icon: 'compass', label: '所在城市', value: p.location },
    { icon: 'grid', label: '所属机构', value: p.organization },
  ].filter((row) => row.value)
})

const interests = computed(() => auth.profile.interestingFields || [])

async function refreshSummary() {
  const ok = await auth.generateSummary({ mode: 'regenerate' })
  if (ok) ui.showToast('画像已重新生成', 'success')
}

async function incrementalUpdate() {
  const ok = await auth.generateSummary({ mode: 'incremental' })
  if (ok) ui.showToast('已根据最新互动更新画像', 'success')
}

function handleLogout() {
  auth.logout()
  ui.closePanel()
  ui.showToast('已退出登录')
}
</script>

<template>
  <div class="profile">
    <!-- 「我的」只存在于主页面（需登录），未登录属于异常兜底 -->
    <div v-if="!auth.isLoggedIn" class="need-auth">
      <p class="need-auth-text">登录后才能查看个人资料与用户画像。</p>
      <RouterLink class="btn btn-primary" :to="{ name: 'auth' }">前往登录 / 注册</RouterLink>
    </div>

    <template v-else>
      <!-- 顶部身份卡 -->
      <header class="id-card">
        <span class="id-avatar">{{ auth.initial }}</span>
        <div class="id-meta">
          <strong class="id-name">{{ auth.displayName }}</strong>
          <span v-if="auth.accountLabel" class="id-account" :title="auth.email">
            {{ auth.accountLabel }}
          </span>
          <span class="id-sub">
            <AppIcon name="target" :size="12" />
            {{ auth.hasSummary ? '已完成用户画像' : '尚未生成用户画像' }}
          </span>
        </div>
        <button type="button" class="btn btn-ghost edit-btn" @click="editing = !editing">
          <AppIcon :name="editing ? 'close' : 'wand'" :size="15" />
          {{ editing ? '收起' : '编辑资料' }}
        </button>
      </header>

      <!-- 编辑模式 -->
      <OnboardingForm v-if="editing" mode="edit" compact @done="editing = false" />

      <template v-else>
        <!-- 用户画像 -->
        <section class="block">
          <div class="block-head">
            <h3 class="block-title">
              <AppIcon name="sparkles" :size="15" />
              用户画像
            </h3>
            <div class="block-actions">
              <button type="button" class="btn btn-quiet" :disabled="auth.summaryGenerating" @click="incrementalUpdate">
                <AppIcon name="refresh" :size="14" />
                增量更新
              </button>
              <button type="button" class="btn btn-quiet" :disabled="auth.summaryGenerating" @click="refreshSummary">
                <AppIcon name="wand" :size="14" />
                重新生成
              </button>
            </div>
          </div>

          <div v-if="auth.summaryGenerating" class="gen-inline">
            <span class="gen-dot" />
            正在生成画像，请稍候…
          </div>
          <p v-else-if="auth.hasSummary" class="block-text"><MathText :text="auth.profile.summary" /></p>
          <p v-else class="block-empty">
            还没有画像。点击「编辑资料」填写基本信息，系统会自动为你生成专属画像。
          </p>
        </section>

        <!-- 基本信息 -->
        <section class="block">
          <h3 class="block-title">
            <AppIcon name="user" :size="15" />
            基本信息
          </h3>
          <dl v-if="infoRows.length" class="info-grid">
            <div v-for="row in infoRows" :key="row.label" class="info-row">
              <dt>{{ row.label }}</dt>
              <dd>{{ row.value }}</dd>
            </div>
          </dl>
          <p v-else class="block-empty">暂未填写任何资料信息。</p>

          <div v-if="interests.length" class="interests">
            <span class="interests-label">感兴趣领域</span>
            <div class="interest-tags">
              <span v-for="tag in interests" :key="tag" class="chip chip-brand">{{ tag }}</span>
            </div>
          </div>
        </section>

        <button type="button" class="btn btn-ghost logout-btn" @click="handleLogout">
          <AppIcon name="logout" :size="15" />
          退出登录
        </button>
      </template>
    </template>
  </div>
</template>

<style scoped>
.profile {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.need-auth {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  padding: 30px 20px;
  text-align: center;
}
.need-auth-text {
  font-size: 13.5px;
  color: var(--text-2);
}

.id-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px;
  border-radius: var(--r-lg);
  background: linear-gradient(135deg, #eef0ff 0%, #f6f0ff 100%);
  border: 1px solid #e4e5ff;
}
.id-avatar {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: var(--brand-gradient);
  color: #fff;
  font-weight: 700;
  font-size: 18px;
  flex: 0 0 auto;
  box-shadow: 0 10px 22px -10px rgba(79, 70, 229, 0.9);
}
.id-meta {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1 1 auto;
}
.id-name {
  font-size: 15.5px;
  font-weight: 700;
}
.id-account {
  font-size: 11.5px;
  color: var(--text-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.id-sub {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: var(--brand-dark);
}
.edit-btn {
  height: 34px;
  padding: 0 12px;
  font-size: 12.5px;
  flex: 0 0 auto;
}

.block {
  padding: 15px 16px;
  border-radius: var(--r-md);
  background: var(--surface-2);
}
.block-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.block-title {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  font-weight: 700;
}
.block-actions {
  display: flex;
  gap: 2px;
}
.block-actions .btn-quiet {
  height: 30px;
  font-size: 12px;
  padding: 0 9px;
}
.block-text {
  font-size: 13.5px;
  line-height: 1.75;
  color: var(--text-2);
  white-space: pre-wrap;
  max-height: 260px;
  overflow-y: auto;
}
.block-empty {
  font-size: 13px;
  color: var(--text-3);
  line-height: 1.6;
}

.gen-inline {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 13px;
  color: var(--brand-dark);
}
.gen-dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--brand);
  animation: pulseDot 1.4s var(--ease-soft) infinite;
}
@keyframes pulseDot {
  0%,
  100% {
    opacity: 0.35;
    transform: scale(0.85);
  }
  50% {
    opacity: 1;
    transform: scale(1);
  }
}

.info-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 9px;
  margin: 0;
}
.info-row {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 9px 11px;
  border-radius: var(--r-sm);
  background: var(--surface);
  border: 1px solid var(--border);
}
.info-row dt {
  font-size: 11.5px;
  color: var(--text-3);
}
.info-row dd {
  margin: 0;
  font-size: 13.5px;
  font-weight: 600;
}

.interests {
  margin-top: 13px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.interests-label {
  font-size: 11.5px;
  color: var(--text-3);
}
.interest-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}

.logout-btn {
  height: 38px;
  color: var(--danger);
  border-color: #f0d6dc;
}
.logout-btn:hover {
  background: var(--danger-soft);
}

@media (max-width: 520px) {
  .info-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
