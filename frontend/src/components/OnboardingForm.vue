<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import AppIcon from './AppIcon.vue'
import MathText from './MathText.vue'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

/**
 * 「信息填写 + 生成用户画像」独立区域。
 * - onboarding：注册后首次进入，引导填写个人信息
 * - edit：在「我的」浮窗中修改资料
 */
const props = defineProps({
  mode: { type: String, default: 'onboarding' }, // onboarding | edit
  compact: { type: Boolean, default: false },
})

const emit = defineEmits(['done', 'skip'])

const auth = useAuthStore()
const ui = useUiStore()

const INTEREST_PRESETS = [
  '大语言模型',
  '推荐系统',
  '数据库',
  '计算机视觉',
  '自然语言处理',
  '分布式系统',
  '信息安全',
  '机器人',
  '多模态',
  '强化学习',
  '向量检索',
  '知识图谱',
]

const MAX_INTERESTS = 10

const form = reactive({
  age: '',
  gender: '',
  job: '',
  education: '',
  location: '',
  organization: '',
  interestingFields: [],
})

const customInterest = ref('')
const localError = ref('')

const isEdit = computed(() => props.mode === 'edit')
const busy = computed(() => auth.profileSaving || auth.summaryGenerating)
const summary = computed(() => auth.profile.summary || '')

/** 至少填写一项，后端要求 patch 非空 */
const filledCount = computed(() => {
  let n = 0
  if (form.age !== '' && Number(form.age) > 0) n += 1
  if (form.gender) n += 1
  for (const key of ['job', 'education', 'location', 'organization']) {
    if (String(form[key] || '').trim()) n += 1
  }
  if (form.interestingFields.length) n += 1
  return n
})

function syncFromProfile() {
  const profile = auth.profile
  form.age = profile.age > 0 ? String(profile.age) : ''
  form.gender = profile.gender || ''
  form.job = profile.job || ''
  form.education = profile.education || ''
  form.location = profile.location || ''
  form.organization = profile.organization || ''
  form.interestingFields = Array.isArray(profile.interestingFields)
    ? profile.interestingFields.slice(0, MAX_INTERESTS)
    : []
}

onMounted(syncFromProfile)
watch(() => props.mode, syncFromProfile)

function toggleInterest(tag) {
  const index = form.interestingFields.indexOf(tag)
  if (index >= 0) {
    form.interestingFields.splice(index, 1)
    return
  }
  if (form.interestingFields.length >= MAX_INTERESTS) {
    localError.value = `最多选择 ${MAX_INTERESTS} 个感兴趣领域`
    return
  }
  localError.value = ''
  form.interestingFields.push(tag)
}

function addCustomInterest() {
  const value = customInterest.value.trim()
  if (!value) return
  if (form.interestingFields.includes(value)) {
    customInterest.value = ''
    return
  }
  if (form.interestingFields.length >= MAX_INTERESTS) {
    localError.value = `最多选择 ${MAX_INTERESTS} 个感兴趣领域`
    return
  }
  form.interestingFields.push(value)
  customInterest.value = ''
  localError.value = ''
}

/** 只提交有值的字段：后端为指针 + omitempty 语义，空串会触发校验失败 */
function buildPatch() {
  const patch = {}
  const age = Number(form.age)
  if (form.age !== '' && Number.isFinite(age) && age > 0) patch.age = Math.min(120, Math.round(age))
  if (form.gender) patch.gender = form.gender
  for (const key of ['job', 'education', 'location', 'organization']) {
    const value = String(form[key] || '').trim()
    if (value) patch[key] = value.slice(0, 40)
  }
  if (form.interestingFields.length) {
    patch.interesting_fields = form.interestingFields.slice(0, MAX_INTERESTS)
  }
  return patch
}

async function handleSubmit() {
  localError.value = ''
  auth.clearError()

  if (filledCount.value === 0) {
    localError.value = '请至少填写一项信息'
    return
  }

  const patch = buildPatch()
  // 后端把「填资料」和「生成画像」拆开了：POST /users/info 只落库、不发画像任务。
  //   - 首次填写：落库后由前端显式 PUT /users/summary 触发生成（regenerate: 'client'）
  //   - 编辑资料：走 PUT /users/info，由后端按本次改动字段增量更新画像（regenerate: 'server'）
  const ok = await auth.saveProfile(patch, { regenerate: isEdit.value ? 'server' : 'client' })
  if (ok) {
    emit('done')
    ui.showToast(
      isEdit.value ? '资料已更新，正在更新用户画像…' : '资料已保存，正在生成用户画像…',
      'success',
    )
  }
}

function handleSkip() {
  auth.skipOnboarding()
  emit('skip')
}
</script>

<template>
  <section class="onboarding" :class="{ 'is-compact': compact }">
    <header v-if="!compact" class="ob-hero">
      <span class="ob-badge"><AppIcon name="target" :size="22" /></span>
      <div class="ob-hero-text">
        <h1 class="ob-title">{{ isEdit ? '更新我的资料' : '完善个人信息，生成你的专属画像' }}</h1>
        <p class="ob-sub">
          {{
            isEdit
              ? '修改后可以重新生成画像，推荐结果会随之调整。'
              : '填写下面的信息，系统会据此生成用户画像，让推荐更贴合你的研究方向。'
          }}
        </p>
      </div>
    </header>

    <div class="ob-card">
      <div class="grid-2">
        <div class="field">
          <label class="field-label" for="ob-age">年龄</label>
          <input
            id="ob-age"
            v-model="form.age"
            class="input"
            type="number"
            min="1"
            max="120"
            placeholder="例如 24"
          />
        </div>

        <div class="field">
          <label class="field-label" for="ob-gender">性别</label>
          <select id="ob-gender" v-model="form.gender" class="select">
            <option value="">不填</option>
            <option value="male">男</option>
            <option value="female">女</option>
          </select>
        </div>

        <div class="field">
          <label class="field-label" for="ob-job">职业</label>
          <input id="ob-job" v-model="form.job" class="input" type="text" placeholder="例如 算法工程师" />
        </div>

        <div class="field">
          <label class="field-label" for="ob-education">学历</label>
          <input id="ob-education" v-model="form.education" class="input" type="text" placeholder="例如 硕士" />
        </div>

        <div class="field">
          <label class="field-label" for="ob-location">所在城市</label>
          <input id="ob-location" v-model="form.location" class="input" type="text" placeholder="例如 杭州" />
        </div>

        <div class="field">
          <label class="field-label" for="ob-organization">所属机构</label>
          <input
            id="ob-organization"
            v-model="form.organization"
            class="input"
            type="text"
            placeholder="例如 某某大学"
          />
        </div>
      </div>

      <div class="field interests">
        <label class="field-label">
          感兴趣领域
          <span class="field-hint">（最多 {{ MAX_INTERESTS }} 个，已选 {{ form.interestingFields.length }}）</span>
        </label>

        <div class="interest-tags">
          <button
            v-for="tag in INTEREST_PRESETS"
            :key="tag"
            type="button"
            class="chip interest-chip"
            :class="{ 'is-on': form.interestingFields.includes(tag) }"
            @click="toggleInterest(tag)"
          >
            <AppIcon v-if="form.interestingFields.includes(tag)" name="check" :size="12" />
            {{ tag }}
          </button>
        </div>

        <!-- 自定义领域 -->
        <div class="custom-row">
          <input
            v-model="customInterest"
            class="input"
            type="text"
            placeholder="自定义领域，回车添加"
            @keydown.enter.prevent="addCustomInterest"
          />
          <button type="button" class="btn btn-ghost add-btn" @click="addCustomInterest">
            <AppIcon name="plus" :size="15" />
            添加
          </button>
        </div>

        <div v-if="form.interestingFields.length" class="selected-tags">
          <span
            v-for="tag in form.interestingFields"
            :key="`sel-${tag}`"
            class="chip chip-brand removable"
            @click="toggleInterest(tag)"
          >
            {{ tag }}
            <AppIcon name="close" :size="12" />
          </span>
        </div>
      </div>

      <div v-if="localError || auth.error" class="alert alert-error">
        <AppIcon name="alert" :size="15" />
        <span>{{ localError || auth.error }}</span>
      </div>

      <!-- 画像生成进度 -->
      <div v-if="auth.summaryGenerating" class="gen-state">
        <span class="gen-pulse" aria-hidden="true" />
        <div class="gen-text">
          <strong>正在生成用户画像…</strong>
          <small>模型正在结合你的资料与历史互动进行分析，请稍候</small>
        </div>
      </div>

      <!-- 生成结果 -->
      <div v-else-if="summary" class="summary-box">
        <h3 class="summary-title">
          <AppIcon name="sparkles" :size="15" />
          我的用户画像
        </h3>
        <p class="summary-text"><MathText :text="summary" /></p>
      </div>

      <div class="ob-actions">
        <button type="button" class="btn btn-primary" :disabled="busy" @click="handleSubmit">
          <AppIcon :name="isEdit ? 'refresh' : 'wand'" :size="16" />
          {{ busy ? '处理中…' : isEdit ? '保存并重新生成画像' : '保存并生成画像' }}
        </button>

        <button v-if="!isEdit" type="button" class="btn btn-quiet" :disabled="busy" @click="handleSkip">
          稍后再说
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.onboarding {
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 720px;
  margin: 0 auto;
}
.onboarding.is-compact {
  max-width: none;
  gap: 14px;
}

.ob-hero {
  display: flex;
  gap: 14px;
  align-items: flex-start;
}
.ob-badge {
  display: grid;
  place-items: center;
  width: 50px;
  height: 50px;
  border-radius: 16px;
  background: var(--brand-gradient);
  color: #fff;
  box-shadow: 0 14px 28px -14px rgba(79, 70, 229, 0.9);
  flex: 0 0 auto;
}
.ob-title {
  font-size: 20px;
  font-weight: 750;
  letter-spacing: -0.015em;
}
.ob-sub {
  margin-top: 4px;
  font-size: 13px;
  color: var(--text-2);
  line-height: 1.65;
}

.ob-card {
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 22px;
  border-radius: var(--r-xl);
  background: var(--surface);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-md);
}

.grid-2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 15px;
}

.interests {
  gap: 10px;
}
.interest-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.interest-chip {
  cursor: pointer;
  border: 1px solid var(--border-strong);
  background: var(--surface);
  height: 32px;
  user-select: none;
}
.interest-chip:hover {
  border-color: var(--brand);
  color: var(--brand);
}
.interest-chip.is-on {
  background: var(--brand-soft);
  border-color: transparent;
  color: var(--brand-dark);
  font-weight: 600;
}

.custom-row {
  display: flex;
  gap: 8px;
}
.add-btn {
  height: 44px;
  flex: 0 0 auto;
}

.selected-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  padding-top: 4px;
}
.removable {
  cursor: pointer;
  height: 28px;
}
.removable:hover {
  background: #e6e2ff;
}

.gen-state {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border-radius: var(--r-md);
  background: var(--brand-soft);
}
.gen-pulse {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--brand);
  box-shadow: 0 0 0 0 rgba(79, 70, 229, 0.55);
  animation: pulse 1.5s var(--ease-soft) infinite;
  flex: 0 0 auto;
}
@keyframes pulse {
  0% {
    box-shadow: 0 0 0 0 rgba(79, 70, 229, 0.5);
  }
  70% {
    box-shadow: 0 0 0 12px rgba(79, 70, 229, 0);
  }
  100% {
    box-shadow: 0 0 0 0 rgba(79, 70, 229, 0);
  }
}
.gen-text {
  display: flex;
  flex-direction: column;
}
.gen-text strong {
  font-size: 13.5px;
  color: var(--brand-dark);
}
.gen-text small {
  font-size: 12px;
  color: rgba(67, 56, 202, 0.75);
}

.summary-box {
  padding: 15px 16px;
  border-radius: var(--r-md);
  background: linear-gradient(135deg, #eef0ff 0%, #f7f1ff 100%);
  border: 1px solid #e2e4ff;
}
.summary-title {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  font-weight: 700;
  color: var(--brand-dark);
  margin-bottom: 8px;
}
.summary-text {
  font-size: 13.5px;
  line-height: 1.75;
  color: var(--text-2);
  white-space: pre-wrap;
  max-height: 240px;
  overflow-y: auto;
}

.ob-actions {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}

@media (max-width: 620px) {
  .grid-2 {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
