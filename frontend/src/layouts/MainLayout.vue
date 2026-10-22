<script setup>
import SideBar from '@/components/SideBar.vue'

/**
 * 主页面布局：左侧导航 + 中间内容区。
 * 仅「首页（流式推荐）」使用该布局。
 */
</script>

<template>
  <div class="shell">
    <SideBar />
    <slot />
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  height: 100vh;
  overflow: hidden;
}
</style>
