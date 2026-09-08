<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { getProfile, updateInfo, generateSummary, updateSummary } from '../api'

const emit = defineEmits(['close'])

const profile = ref(null)
const editing = ref(false)
const loading = ref(false)
const error = ref('')
const msg = ref('')
const fieldInput = ref('')

const form = reactive({
  age: null,
  gender: '',
  job: '',
  education: '',
  location: '',
  organization: '',
  interestingFields: [],
})

const viewRows = computed(() => {
  const p = profile.value || {}
  return [
    { label: '年龄', value: p.age },
    { label: '性别', value: p.gender === 'male' ? '男' : p.gender === 'female' ? '女' : '' },
    { label: '职业', value: p.job },
    { label: '学历', value: p.education },
    { label: '所在地', value: p.location },
    { label: '所在组织', value: p.organization },
  ]
})

function fillForm() {
  form.age = profile.value.age ?? null
  form.gender = profile.value.gender || ''
  form.job = profile.value.job || ''
  form.education = profile.value.education || ''
  form.location = profile.value.location || ''
  form.organization = profile.value.organization || ''
  form.interestingFields = [...(profile.value.interestingFields || [])]
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    profile.value = await getProfile()
    fillForm()
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function addField() {
  const v = fieldInput.value.trim()
  if (!v) return
  if (form.interestingFields.includes(v)) return
  if (form.interestingFields.length >= 10) return
  form.interestingFields.push(v)
  fieldInput.value = ''
}
function removeField(i) {
  form.interestingFields.splice(i, 1)
}

async function save() {
  error.value = ''
  msg.value = ''
  const payload = {}
  if (form.age !== null && form.age !== '') payload.age = Number(form.age)
  if (form.gender) payload.gender = form.gender
  if (form.job) payload.job = form.job
  if (form.education) payload.education = form.education
  if (form.location) payload.location = form.location
  if (form.organization) payload.organization = form.organization
  if (form.interestingFields.length) payload.interesting_fields = form.interestingFields
  try {
    await updateInfo(payload)
    msg.value = '已保存'
    editing.value = false
    await load()
  } catch (e) {
    error.value = e.message
  }
}

async function genSummary() {
  error.value = ''
  msg.value = ''
  try {
    await generateSummary()
    msg.value = '已提交生成画像任务'
  } catch (e) {
    error.value = e.message
  }
}
async function updSummary() {
  error.value = ''
  msg.value = ''
  try {
    await updateSummary()
    msg.value = '已提交更新画像任务'
  } catch (e) {
    error.value = e.message
  }
}

onMounted(load)
</script>

<template>
  <div class="mask" @click.self="$emit('close')">
    <div class="drawer">
      <div class="drawer-head">
        <h3>我的信息</h3>
        <button class="close" @click="$emit('close')">×</button>
      </div>

      <div v-if="loading" class="center">加载中…</div>

      <template v-else-if="profile">
        <!-- 查看模式 -->
        <div v-if="!editing" class="view">
          <div class="row" v-for="r in viewRows" :key="r.label">
            <span class="label">{{ r.label }}</span>
            <span class="value">{{ r.value || '—' }}</span>
          </div>
          <div class="row">
            <span class="label">兴趣领域</span>
            <span class="value">{{ (profile.interestingFields || []).join('、') || '—' }}</span>
          </div>
          <div class="row">
            <span class="label">学术画像</span>
            <p class="summary">{{ profile.summary || '尚未生成画像' }}</p>
          </div>
          <div class="actions">
            <button class="primary" @click="editing = true">编辑资料</button>
            <button class="ghost" @click="genSummary">生成画像</button>
            <button class="ghost" @click="updSummary">更新画像</button>
          </div>
        </div>

        <!-- 编辑模式 -->
        <div v-else class="edit">
          <div class="grid">
            <label>年龄 <input v-model="form.age" type="number" min="0" max="120" /></label>
            <label>
              性别
              <select v-model="form.gender">
                <option value="">请选择</option>
                <option value="male">男</option>
                <option value="female">女</option>
              </select>
            </label>
            <label>职业 <input v-model="form.job" /></label>
            <label>学历 <input v-model="form.education" /></label>
            <label>所在地 <input v-model="form.location" /></label>
            <label>所在组织 <input v-model="form.organization" /></label>
          </div>
          <div class="fields">
            <label>兴趣领域</label>
            <div class="chips">
              <span v-for="(f, i) in form.interestingFields" :key="f" class="chip">
                {{ f }}
                <button type="button" class="chip-x" @click="removeField(i)">×</button>
              </span>
              <input
                v-model="fieldInput"
                class="chip-input"
                placeholder="输入后回车添加"
                @keydown.enter.prevent="addField"
                @blur="addField"
              />
            </div>
          </div>
          <div class="actions">
            <button class="primary" @click="save">保存</button>
            <button class="ghost" @click="editing = false; fillForm()">取消</button>
          </div>
        </div>

        <p v-if="error" class="error">{{ error }}</p>
        <p v-if="msg" class="msg">{{ msg }}</p>
      </template>
    </div>
  </div>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.35);
  z-index: 50;
  display: flex;
  justify-content: flex-end;
}
.drawer {
  width: 420px;
  max-width: 90vw;
  height: 100%;
  background: #fff;
  box-shadow: -4px 0 20px rgba(0, 0, 0, 0.1);
  padding: 24px;
  overflow-y: auto;
}
.drawer-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.drawer-head h3 {
  margin: 0;
  font-size: 18px;
}
.close {
  border: none;
  background: none;
  font-size: 24px;
  color: var(--text-2);
  line-height: 1;
}
.center {
  color: var(--text-2);
  text-align: center;
  padding: 40px 0;
}
.row {
  display: flex;
  margin-bottom: 12px;
}
.row .label {
  width: 80px;
  flex-shrink: 0;
  color: var(--text-2);
  font-size: 13px;
}
.row .value {
  font-size: 14px;
}
.summary {
  margin: 0;
  font-size: 13px;
  color: var(--text-2);
  line-height: 1.6;
  background: var(--bg);
  padding: 12px;
  border-radius: 8px;
  flex: 1;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 20px;
}
.actions .primary {
  width: 100%;
}
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.grid label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 13px;
  color: var(--text-2);
}
.grid input,
.grid select {
  padding: 9px 11px;
  border: 1px solid var(--border);
  border-radius: 10px;
  font-size: 14px;
  outline: none;
}
.fields {
  margin-top: 14px;
}
.fields > label {
  font-size: 13px;
  color: var(--text-2);
  display: block;
  margin-bottom: 6px;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 8px;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: #ffeef1;
  color: var(--red);
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 13px;
}
.chip-x {
  border: none;
  background: none;
  color: var(--red);
  font-size: 15px;
  line-height: 1;
  padding: 0;
}
.chip-input {
  flex: 1;
  min-width: 120px;
  border: none;
  outline: none;
  font-size: 14px;
  padding: 6px;
}
</style>
