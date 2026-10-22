<script setup>
import { computed } from 'vue'
import { buildMathSegments } from '@/utils/mathText'

/**
 * 混排 LaTeX 文本渲染器。
 *
 * 用法：把原来的 {{ text }} 换成 <MathText :text="text" />
 *   <h3 class="card-title"><MathText :text="title" mode="inline" /></h3>
 *
 * 安全：普通文本走 Vue 插值（自动转义），只有 KaTeX 自己生成的 HTML 走 v-html；
 * KaTeX 侧 trust:false，禁用 \href、\includegraphics 等可注入命令。
 */
const props = defineProps({
  text: { type: String, default: '' },
  /**
   * 数学段落模式：
   *  'auto'（默认）按原文定界符决定（$$ / \[ 为块级，$ / \( 为行内）
   *  'inline' 强制行内（卡片标题这类单行场景）
   */
  mode: { type: String, default: 'auto' },
})

const segments = computed(() => buildMathSegments(props.text, props.mode))
</script>

<template>
  <template v-for="(segment, index) in segments" :key="index">
    <span
      v-if="segment.type === 'math' && segment.html"
      class="math-fragment"
      :class="{ 'is-display': segment.display }"
      v-html="segment.html"
    />
    <!-- 渲染失败时退化为公式原文，至少不丢内容 -->
    <template v-else>{{ segment.value }}</template>
  </template>
</template>

<style scoped>
.math-fragment.is-display {
  display: block;
  overflow-x: auto;
  overflow-y: hidden;
}
</style>
