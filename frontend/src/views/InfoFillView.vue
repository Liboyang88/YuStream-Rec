<script setup>
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { updateInfo } from '../api'

const router = useRouter()
const loading = ref(false)
const error = ref('')
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

function addField() {
  const v = fieldInput.value.trim()
  if (!v) return
  if (form.interestingFields.includes(v)) return
  if (form.interestingFields.length >= 10) {
    error.value = '最多填写 10 个兴趣领域'
    return
  }
  form.interestingFields.push(v)
  fieldInput.value = ''
}
function removeField(i) {
  form.interestingFields.splice(i, 1)
}

async function submit() {
  loading.value = true
  error.value = ''
  const payload = {}
  if (form.age !== null && form.age !== '') payload.age = Number(form.age)
  if (form.gender) payload.gender = form.gender
  if (form.job) payload.job = form.job
  if (form.education) payload.education = form.education
  if (form.location) payload.location = form.location
  if (form.organization) payload.organization = form.organization
  if (form.interestingFields.length) payload.interesting_fields = form.interestingFields

  if (Object.keys(payload).length === 0) {
    error.value = '至少填写一项信息'
    loading.value = false
    return
  }
  try {
    await updateInfo(payload)
    router.push('/')
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function skip() {
  router.push('/')
}
</script>

<template>
  <div class="info-page">
    <div class="info-card">
      <h2>完善个人信息</h2>
      <p class="hint">用于生成你的学术画像与推荐，可稍后补充修改</p>

      <div class="grid">
        <label>
          年龄
          <input v-model="form.age" type="number" min="0" max="120" placeholder="如 24" />
        </label>
        <label>
          性别
          <select v-model="form.gender">
            <option value="">请选择</option>
            <option value="male">男</option>
            <option value="female">女</option>
          </select>
        </label>
        <label>
          职业
          <input v-model="form.job" placeholder="如 研究生" />
        </label>
        <label>
          学历
          <input v-model="form.education" placeholder="如 硕士" />
        </label>
        <label>
          所在地
          <input v-model="form.location" placeholder="如 北京" />
        </label>
        <label>
          所在组织
          <input v-model="form.organization" placeholder="如 某大学" />
        </label>
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
            placeholder="输入后回车添加（如 数字人文）"
            @keydown.enter.prevent="addField"
            @blur="addField"
          />
        </div>
      </div>

      <p v-if="error" class="error">{{ error }}</p>

      <div class="actions">
        <button class="primary" :disabled="loading" @click="submit">
          {{ loading ? '提交中…' : '保存并进入' }}
        </button>
        <button class="ghost" @click="skip">稍后填写</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.info-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.info-card {
  width: 100%;
  max-width: 560px;
  background: #fff;
  border-radius: 16px;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.08);
  padding: 32px;
}
.info-card h2 {
  margin: 0;
  font-size: 20px;
}
.hint {
  color: var(--text-2);
  font-size: 13px;
  margin: 6px 0 20px;
}
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
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
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  font-size: 14px;
  outline: none;
}
.grid input:focus,
.grid select:focus {
  border-color: var(--red);
}
.fields {
  margin-top: 16px;
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
  min-width: 140px;
  border: none;
  outline: none;
  font-size: 14px;
  padding: 6px;
}
.actions {
  display: flex;
  gap: 12px;
  margin-top: 20px;
}
.actions .primary {
  flex: 1;
}
.actions .ghost {
  width: 110px;
}
</style>
