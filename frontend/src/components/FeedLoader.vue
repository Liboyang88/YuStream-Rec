<script setup>
import AppIcon from './AppIcon.vue'

defineProps({
  loading: { type: Boolean, default: false },
  /** 上一轮没有带回新内容（不是“到底”，继续下滑仍会再发请求） */
  noNewContent: { type: Boolean, default: false },
  error: { type: String, default: '' },
  count: { type: Number, default: 0 },
})

defineEmits(['retry'])
</script>

<template>
  <div class="feed-loader">
    <!-- 加载中：底部加载动画 -->
    <div v-if="loading" class="loader-state">
      <span class="dots" aria-hidden="true">
        <i /><i /><i />
      </span>
      <span class="loader-text">正在为你加载更多推荐…</span>
    </div>

    <!-- 出错：允许重试 -->
    <div v-else-if="error" class="loader-state is-error">
      <AppIcon name="alert" :size="16" />
      <span class="loader-text">{{ error }}</span>
      <button type="button" class="btn btn-ghost retry-btn" @click="$emit('retry')">
        <AppIcon name="refresh" :size="15" />
        重试
      </button>
    </div>

    <!-- 本轮没有新内容：不是终点，继续下滑会再次请求 -->
    <div v-else-if="noNewContent && count > 0" class="loader-state is-soft-end">
      <span class="end-line" aria-hidden="true" />
      <span class="loader-text">暂时没有更多新内容了 · 继续下滑会再次获取</span>
      <span class="end-line" aria-hidden="true" />
    </div>
  </div>
</template>

<style scoped>
.feed-loader {
  display: flex;
  justify-content: center;
  padding: 34px 0 12px;
  min-height: 68px;
}

.loader-state {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
  color: var(--text-3);
}
.loader-state.is-error {
  color: var(--danger);
  flex-wrap: wrap;
  justify-content: center;
}
.loader-state.is-soft-end {
  width: 100%;
  max-width: 520px;
  justify-content: center;
  color: var(--text-3);
}

/* 三点弹跳动画 */
.dots {
  display: inline-flex;
  align-items: flex-end;
  gap: 4px;
  height: 14px;
}
.dots i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--brand);
  opacity: 0.45;
  animation: dotBounce 1.1s var(--ease-soft) infinite;
}
.dots i:nth-child(2) {
  animation-delay: 0.16s;
}
.dots i:nth-child(3) {
  animation-delay: 0.32s;
}
@keyframes dotBounce {
  0%,
  70%,
  100% {
    transform: translateY(0);
    opacity: 0.4;
  }
  35% {
    transform: translateY(-6px);
    opacity: 1;
  }
}

.retry-btn {
  height: 32px;
  padding: 0 12px;
  font-size: 12.5px;
  color: var(--text);
}

.end-line {
  flex: 1 1 auto;
  height: 1px;
  background: linear-gradient(90deg, transparent, var(--border-strong), transparent);
}
.loader-text {
  white-space: nowrap;
}
</style>
