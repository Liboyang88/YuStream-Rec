<script setup>
import { computed } from 'vue'
import AppIcon from './AppIcon.vue'
import MathText from './MathText.vue'
import { INTERACTION, recordInteraction } from '@/api/interactions'
import { usePreferencesStore } from '@/stores/preferences'
import {
  displayDescription,
  displayTitle,
  formatPublishDate,
  sourceLabel,
  timeAgo,
} from '@/utils/format'

const props = defineProps({
  item: { type: Object, required: true },
  /** 同排错峰延迟，形成波浪式浮现 */
  revealDelay: { type: Number, default: 0 },
})

const emit = defineEmits(['open'])

const prefs = usePreferencesStore()

/** 标题：中文文献恒为原文 title；非中文文献在中文翻译开启时取 title_chinese（详情页 detail-title 不受影响） */
const title = computed(() => displayTitle(props.item, { translate: prefs.translateChinese }))
/** 摘要：默认优先二次概括 summary；关闭后取原文 abstract（非中文文献且翻译开启时取 abstract_chinese） */
const description = computed(
  () =>
    displayDescription(props.item, 160, {
      preferSummary: prefs.preferSummary,
      translate: prefs.translateChinese,
    }) || '暂无摘要',
)
const fields = computed(() => (Array.isArray(props.item.fields) ? props.item.fields.slice(0, 2) : []))
const dateText = computed(() => formatPublishDate(props.item.publish_date))
const agoText = computed(() => timeAgo(props.item.publish_date))
const source = computed(() => sourceLabel(props.item))
const url = computed(() => (typeof props.item?.url === 'string' ? props.item.url.trim() : ''))

function handleOpen() {
  emit('open', props.item)
}

/** 跳转到文献官网原址：在新标签页打开 item.url，并上报一次 click 互动 */
function handleOriginal() {
  if (!url.value) return
  window.open(url.value, '_blank', 'noopener,noreferrer')
  recordInteraction({ type: INTERACTION.CLICK, target: props.item, detail: 'link' }).catch(() => {})
}
</script>

<template>
  <article
    v-reveal="{ delay: revealDelay }"
    class="card"
    role="button"
    tabindex="0"
    :aria-label="`查看详情：${title}`"
    @click="handleOpen"
    @keydown.enter.prevent="handleOpen"
    @keydown.space.prevent="handleOpen"
  >
    <div class="card-top">
      <span class="source">{{ source }}</span>
      <span class="date">
        <AppIcon name="calendar" :size="13" />
        <span>{{ dateText }}</span>
      </span>
    </div>

    <h3 class="card-title"><MathText :text="title" mode="inline" /></h3>

    <p class="card-desc"><MathText :text="description" /></p>

    <div class="card-foot">
      <div class="tags">
        <span v-for="field in fields" :key="field" class="tag">{{ field }}</span>
        <span v-if="!fields.length" class="tag tag-muted">综合</span>
      </div>
      <div class="foot-actions">
        <button
          v-if="url"
          type="button"
          class="original-btn"
          :aria-label="`打开原文：${title}`"
          @click.stop="handleOriginal"
          @keydown.stop
        >
          <AppIcon name="external" :size="14" />
          原文
        </button>
        <span v-if="agoText" class="ago">{{ agoText }}</span>
      </div>
    </div>

    <span class="card-glow" aria-hidden="true" />
  </article>
</template>

<style scoped>
.card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: var(--card-w);
  min-height: var(--card-min-h);
  padding: 18px 18px 16px;
  border-radius: var(--r-lg);
  background: var(--surface);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-sm);
  cursor: pointer;
  overflow: hidden;
  outline: none;
  transition: transform 0.26s var(--ease-out), box-shadow 0.26s var(--ease-out),
    border-color 0.26s var(--ease-soft);
}
.card:hover {
  transform: translateY(-4px);
  box-shadow: var(--shadow-md);
  border-color: #d9dcf0;
}
.card:focus-visible {
  border-color: var(--brand);
  box-shadow: 0 0 0 3.5px rgba(79, 70, 229, 0.18), var(--shadow-md);
}

/* 悬停时右上角的柔光 */
.card-glow {
  position: absolute;
  top: -60px;
  right: -60px;
  width: 150px;
  height: 150px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(139, 92, 246, 0.18) 0%, rgba(139, 92, 246, 0) 70%);
  opacity: 0;
  transition: opacity 0.3s var(--ease-soft);
  pointer-events: none;
}
.card:hover .card-glow {
  opacity: 1;
}

.card-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.source {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 9px;
  border-radius: var(--r-pill);
  background: var(--brand-soft);
  color: var(--brand-dark);
  font-size: 11.5px;
  font-weight: 700;
  letter-spacing: 0.02em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 46%;
}
.date {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11.5px;
  color: var(--text-3);
  white-space: nowrap;
}

.card-title {
  font-size: 15.5px;
  font-weight: 700;
  line-height: 1.42;
  letter-spacing: -0.01em;
  color: var(--text);
  display: -webkit-box;
  -webkit-line-clamp: 3;
  line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.card-desc {
  flex: 1 1 auto;
  font-size: 13px;
  line-height: 1.62;
  color: var(--text-2);
  display: -webkit-box;
  -webkit-line-clamp: 4;
  line-clamp: 4;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.card-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding-top: 11px;
  border-top: 1px solid var(--border);
  margin-top: auto;
}
.tags {
  display: flex;
  gap: 6px;
  min-width: 0;
  overflow: hidden;
}
.tag {
  display: inline-block;
  padding: 3px 9px;
  border-radius: var(--r-pill);
  background: var(--surface-2);
  color: var(--text-2);
  font-size: 11px;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 110px;
}
.tag-muted {
  color: var(--text-3);
}
.foot-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
}
.original-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 10px;
  border-radius: var(--r-pill);
  border: 1px solid var(--border-strong);
  background: var(--surface);
  color: var(--brand);
  font-size: 11.5px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  transition: all 0.18s var(--ease-soft);
}
.original-btn:hover {
  border-color: var(--brand);
  background: var(--brand-soft);
}
.original-btn:focus-visible {
  outline: 2px solid var(--brand);
  outline-offset: 2px;
}
.ago {
  font-size: 11px;
  color: var(--text-3);
  white-space: nowrap;
  flex: 0 0 auto;
}
</style>
