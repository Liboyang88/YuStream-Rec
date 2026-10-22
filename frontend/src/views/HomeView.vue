<script setup>
import { onActivated, ref } from 'vue'
import FeedStream from '@/components/FeedStream.vue'
import SettingSwitch from '@/components/SettingSwitch.vue'
import { usePreferencesStore } from '@/stores/preferences'
import { useSmoothWheel } from '@/composables/useSmoothWheel'

/**
 * 首页（流式推荐）—— 只负责主页面布局中的「中间内容区」。
 * 登录/注册与信息填写已拆分为独立页面（/auth、/welcome），
 * 因此这里不再承载引导流程。
 */
const prefs = usePreferencesStore()

const scrollerRef = ref(null)

// 中心区域用鼠标滚轮平滑滚动（小红书式手感）
useSmoothWheel(scrollerRef)

// 从其它页面返回首页时回到顶部，避免沿用上次的位置
onActivated(() => {
  if (scrollerRef.value) scrollerRef.value.scrollTop = 0
})
</script>

<template>
  <main class="content">
    <div ref="scrollerRef" class="scroller" data-feed-scroll>
      <div class="scroller-inner">
        <header class="page-head">
          <div class="head-main">
            <h1 class="page-title">为你推荐</h1>
            <p class="page-sub">根据你的用户画像实时生成，滚动到底部会自动加载更多。</p>
          </div>

          <!-- 原先的「N 条推荐 / 推荐来源」chip 已移除，此处改为「中文翻译」开关 -->
          <div class="head-meta">
            <SettingSwitch
              v-model="prefs.translateChinese"
              label="中文翻译"
              hint="卡片标题与摘要切换为中文"
            />
          </div>
        </header>

        <FeedStream />
      </div>
    </div>
  </main>
</template>

<style scoped>
.content {
  flex: 1 1 auto;
  min-width: 0;
  height: 100vh;
  position: relative;
}

.scroller {
  height: 100%;
  overflow-y: auto;
  overflow-x: hidden;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}

.scroller-inner {
  min-height: 100%;
  padding: var(--feed-pad-y) var(--feed-pad-x) 48px;
  display: flex;
  flex-direction: column;
  gap: 22px;
}

.page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 18px;
  flex-wrap: wrap;
  max-width: 1120px;
  width: 100%;
  margin: 0 auto;
}
.page-title {
  font-size: 25px;
  font-weight: 760;
  letter-spacing: -0.025em;
  line-height: 1.25;
}
.page-sub {
  margin-top: 5px;
  font-size: 13.5px;
  color: var(--text-2);
  line-height: 1.6;
}
.head-meta {
  display: flex;
  gap: 8px;
  align-items: center;
  padding-bottom: 4px;
}

@media (max-width: 900px) {
  .scroller-inner {
    padding: 22px 20px 40px;
  }
  .page-title {
    font-size: 21px;
  }
}

@media (max-width: 620px) {
  .scroller-inner {
    padding: 18px 14px 36px;
  }
}
</style>
