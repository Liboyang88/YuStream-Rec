<script setup>
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import MathText from '@/components/MathText.vue'
import OnboardingForm from '@/components/OnboardingForm.vue'
import { useAuthStore } from '@/stores/auth'

/**
 * 注册后的「信息填写 + 生成画像」独立页面（/welcome）。
 * 同样是整屏引导页布局，不共享主页面侧边栏。
 */
const auth = useAuthStore()
const router = useRouter()

/** 资料是否已提交：提交后就地展示画像生成结果 */
const submitted = ref(false)

const summary = computed(() => auth.profile.summary || '')
const generating = computed(() => auth.summaryGenerating)

function handleDone() {
  submitted.value = true
}

function goHome() {
  router.replace({ name: 'home' })
}
</script>

<template>
  <div class="welcome">
    <!-- 填写阶段 -->
    <OnboardingForm v-if="!submitted" class="rise-in" @done="handleDone" @skip="goHome" />

    <!-- 提交后就地展示画像生成结果 -->
    <section v-else class="result rise-in">
      <span class="result-icon">
        <AppIcon :name="summary ? 'sparkles' : 'wand'" :size="24" />
      </span>
      <h1 class="result-title">{{ summary ? '你的用户画像已生成' : '资料已保存' }}</h1>
      <p class="result-sub">
        {{
          summary
            ? '推荐将依据该画像实时生成，并随你的浏览与点击持续演进。'
            : '画像正在后台生成，你可以先进入推荐流，稍后在「我的」中查看。'
        }}
      </p>

      <div v-if="generating" class="result-generating">
        <span class="gen-dot" aria-hidden="true" />
        正在生成用户画像…
      </div>

      <div v-else-if="summary" class="result-summary">
        <h2 class="result-summary-title">
          <AppIcon name="target" :size="14" />
          我的用户画像
        </h2>
        <p class="result-summary-text"><MathText :text="summary" /></p>
      </div>

      <div class="result-actions">
        <button type="button" class="btn btn-primary" @click="goHome">
          <AppIcon name="arrowRight" :size="16" />
          进入流式推荐
        </button>
        <RouterLink class="btn btn-quiet" :to="{ name: 'home' }">稍后在「我的」中查看</RouterLink>
      </div>
    </section>
  </div>
</template>

<style scoped>
.welcome {
  width: 100%;
  display: flex;
  justify-content: center;
}

.result {
  width: 100%;
  max-width: 560px;
  margin-top: 24px;
  padding: 30px 26px;
  border-radius: var(--r-xl);
  background: var(--surface);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-md);
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 12px;
}
.result-icon {
  display: grid;
  place-items: center;
  width: 56px;
  height: 56px;
  border-radius: 18px;
  background: var(--brand-gradient);
  color: #fff;
  box-shadow: 0 16px 30px -14px rgba(79, 70, 229, 0.9);
}
.result-title {
  font-size: 20px;
  font-weight: 750;
  letter-spacing: -0.02em;
}
.result-sub {
  font-size: 13.5px;
  color: var(--text-2);
  line-height: 1.7;
  max-width: 420px;
}

.result-generating {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 11px 16px;
  border-radius: var(--r-pill);
  background: var(--brand-soft);
  color: var(--brand-dark);
  font-size: 13px;
  font-weight: 500;
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

.result-summary {
  width: 100%;
  text-align: left;
  padding: 15px 16px;
  border-radius: var(--r-md);
  background: linear-gradient(135deg, #eef0ff 0%, #f7f1ff 100%);
  border: 1px solid #e2e4ff;
}
.result-summary-title {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  font-weight: 700;
  color: var(--brand-dark);
  margin-bottom: 7px;
}
.result-summary-text {
  font-size: 13.5px;
  line-height: 1.75;
  color: var(--text-2);
  white-space: pre-wrap;
  max-height: 220px;
  overflow-y: auto;
}

.result-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: center;
  margin-top: 6px;
}
</style>
