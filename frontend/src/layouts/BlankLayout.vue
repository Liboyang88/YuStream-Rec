<script setup>
import AppIcon from '@/components/AppIcon.vue'

/**
 * 引导页布局（登录&注册 / 信息填写）：整屏独立页面，**不含侧边栏**。
 * 与主页面完全分离，避免引导流程复用主页面的导航外壳。
 */
</script>

<template>
  <div class="blank">
    <span class="blob blob-a" aria-hidden="true" />
    <span class="blob blob-b" aria-hidden="true" />

    <div class="blank-scroll">
      <div class="blank-inner">
        <header class="brand">
          <span class="brand-mark"><AppIcon name="sparkles" :size="18" /></span>
          <span class="brand-text">
            <strong class="brand-name">流式推荐</strong>
            <small class="brand-sub">Streaming Recommender</small>
          </span>
        </header>

        <slot />
      </div>
    </div>
  </div>
</template>

<style scoped>
.blank {
  position: relative;
  height: 100vh;
  overflow: hidden;
}
.blank-scroll {
  position: relative;
  z-index: 1;
  height: 100%;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.blank-inner {
  min-height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 22px;
  padding: 44px 24px 56px;
}

/* 背景装饰 */
.blob {
  position: absolute;
  border-radius: 50%;
  filter: blur(70px);
  pointer-events: none;
}
.blob-a {
  width: 420px;
  height: 420px;
  top: -140px;
  left: -100px;
  background: rgba(99, 102, 241, 0.22);
}
.blob-b {
  width: 380px;
  height: 380px;
  bottom: -160px;
  right: -90px;
  background: rgba(168, 85, 247, 0.18);
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
}
.brand-mark {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 11px;
  background: var(--brand-gradient);
  color: #fff;
  box-shadow: 0 8px 18px -8px rgba(79, 70, 229, 0.9);
}
.brand-text {
  display: flex;
  flex-direction: column;
}
.brand-name {
  font-size: 14.5px;
  font-weight: 700;
  letter-spacing: -0.01em;
  line-height: 1.25;
}
.brand-sub {
  font-size: 10px;
  color: var(--text-3);
  letter-spacing: 0.05em;
  text-transform: uppercase;
}

@media (max-width: 620px) {
  .blank-inner {
    padding: 28px 16px 40px;
    gap: 16px;
  }
}
</style>
