<script setup>
import { computed, ref } from 'vue'
import AppIcon from './AppIcon.vue'
import MathText from './MathText.vue'
import { INTERACTION, recordInteraction } from '@/api/interactions'
import { usePreferencesStore } from '@/stores/preferences'
import {
  displayAbstract,
  displayDescription,
  displaySummary,
  displayTitle,
  formatPublishDate,
  readingMinutes,
  sourceLabel,
} from '@/utils/format'

const props = defineProps({
  item: { type: Object, required: true },
})

const prefs = usePreferencesStore()

const liked = ref(false)

/** 详情标题不跟随「中文翻译」开关：非中文文献取原文 title，中文文献同样取 title */
const title = computed(() => displayTitle(props.item))
/**
 * 原文标题对照：仅在「原文标题 vs 中文译名」确有区别时展示。
 * 中文文献的 detail-title 本身就是原文，不再重复一行。
 */
const originalTitle = computed(() => {
  const { is_chinese: isChinese, title: rawTitle, title_chinese: chineseTitle } = props.item
  if (isChinese) return ''
  if (chineseTitle && chineseTitle !== rawTitle) return chineseTitle
  return ''
})

/** 「摘要」块：非中文文献在中文翻译开启时取 abstract_chinese；中文文献恒取 abstract */
const abstract = computed(() => displayAbstract(props.item, { translate: prefs.translateChinese }))
const summary = computed(() => displaySummary(props.item))
/** 摘要缺失时的兜底简介，跟随卡片摘要偏好 */
const description = computed(() =>
  displayDescription(props.item, 0, {
    preferSummary: prefs.preferSummary,
    translate: prefs.translateChinese,
  }),
)
const authors = computed(() => (Array.isArray(props.item.authors) ? props.item.authors : []))
const fields = computed(() => (Array.isArray(props.item.fields) ? props.item.fields : []))
const keywords = computed(() => (Array.isArray(props.item.keywords) ? props.item.keywords : []))
const source = computed(() => sourceLabel(props.item))
const dateText = computed(() => formatPublishDate(props.item.publish_date))
const readMinutes = computed(() => readingMinutes(abstract.value || description.value))

function toggleLike() {
  liked.value = !liked.value
  if (liked.value) {
    recordInteraction({ type: INTERACTION.LIKE, target: props.item }).catch(() => {})
  }
}
</script>

<template>
  <article class="detail">
    <div class="detail-banner">
      <span class="detail-source">{{ source }}</span>
      <span class="detail-dot" aria-hidden="true">·</span>
      <span class="detail-date">{{ dateText }}</span>
      <span class="detail-dot" aria-hidden="true">·</span>
      <span class="detail-date">约 {{ readMinutes }} 分钟</span>
    </div>

    <h1 class="detail-title"><MathText :text="title" mode="inline" /></h1>
    <p v-if="originalTitle" class="detail-subtitle"><MathText :text="originalTitle" /></p>

    <div v-if="authors.length" class="detail-authors">
      <AppIcon name="user" :size="14" />
      <span class="authors-text">{{ authors.join('、') }}</span>
    </div>

    <div v-if="fields.length || keywords.length" class="detail-tags">
      <span v-for="field in fields" :key="`f-${field}`" class="tag tag-brand">{{ field }}</span>
      <span v-for="kw in keywords.slice(0, 8)" :key="`k-${kw}`" class="tag">{{ kw }}</span>
    </div>

    <!-- 模型个性化解读 -->
    <section v-if="summary" class="block block-highlight">
      <h2 class="block-title">
        <AppIcon name="sparkles" :size="15" />
        推荐理由
      </h2>
      <p class="block-text"><MathText :text="summary" /></p>
    </section>

    <section v-if="abstract" class="block">
      <h2 class="block-title">
        <AppIcon name="book" :size="15" />
        摘要
      </h2>
      <p class="block-text block-abstract"><MathText :text="abstract" /></p>
    </section>

    <section v-else-if="description" class="block">
      <h2 class="block-title">
        <AppIcon name="book" :size="15" />
        简介
      </h2>
      <p class="block-text"><MathText :text="description" /></p>
    </section>

    <footer class="detail-foot">
      <button type="button" class="like-btn" :class="{ 'is-liked': liked }" @click="toggleLike">
        <AppIcon name="sparkles" :size="15" />
        {{ liked ? '已收藏' : '感兴趣' }}
      </button>
      <span class="foot-note">推荐内容来源于 {{ source }}</span>
    </footer>
  </article>
</template>

<style scoped>
.detail {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.detail-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-3);
  flex-wrap: wrap;
}
.detail-source {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 9px;
  border-radius: var(--r-pill);
  background: var(--brand-soft);
  color: var(--brand-dark);
  font-weight: 700;
  font-size: 11.5px;
}
.detail-dot {
  color: var(--border-strong);
}

.detail-title {
  font-size: 20px;
  font-weight: 750;
  line-height: 1.38;
  letter-spacing: -0.015em;
}
.detail-subtitle {
  font-size: 13px;
  color: var(--text-3);
  line-height: 1.5;
  margin-top: -6px;
}

.detail-authors {
  display: flex;
  gap: 7px;
  align-items: flex-start;
  font-size: 12.5px;
  color: var(--text-2);
  padding: 10px 12px;
  border-radius: var(--r-sm);
  background: var(--surface-2);
}
.authors-text {
  line-height: 1.55;
}

.detail-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}
.tag {
  display: inline-block;
  padding: 4px 10px;
  border-radius: var(--r-pill);
  background: var(--surface-2);
  color: var(--text-2);
  font-size: 11.5px;
  font-weight: 500;
}
.tag-brand {
  background: var(--brand-soft);
  color: var(--brand-dark);
  font-weight: 600;
}

.block {
  padding: 15px 16px;
  border-radius: var(--r-md);
  background: var(--surface-2);
}
.block-highlight {
  background: linear-gradient(135deg, #eef0ff 0%, #f6f0ff 100%);
  border: 1px solid #e2e4ff;
}
.block-title {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  font-weight: 700;
  color: var(--text);
  margin-bottom: 8px;
}
.block-highlight .block-title {
  color: var(--brand-dark);
}
.block-text {
  font-size: 13.5px;
  line-height: 1.75;
  color: var(--text-2);
  white-space: pre-wrap;
  word-break: break-word;
}
.block-abstract {
  max-height: 320px;
  overflow-y: auto;
}

.detail-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-top: 6px;
  border-top: 1px solid var(--border);
  margin-top: 2px;
  flex-wrap: wrap;
}
.like-btn {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 36px;
  padding: 0 14px;
  border-radius: var(--r-pill);
  border: 1px solid var(--border-strong);
  background: var(--surface);
  font-size: 13px;
  font-weight: 600;
  color: var(--text-2);
  transition: all 0.2s var(--ease-soft);
}
.like-btn:hover {
  border-color: var(--brand);
  color: var(--brand);
}
.like-btn.is-liked {
  background: var(--brand-gradient);
  border-color: transparent;
  color: #fff;
  box-shadow: 0 8px 18px -8px rgba(79, 70, 229, 0.7);
}
.foot-note {
  font-size: 11.5px;
  color: var(--text-3);
}
</style>
