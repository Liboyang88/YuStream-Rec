import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

/**
 * 展示偏好（纯前端，落在 localStorage，不经过后端）。
 *
 * - preferSummary：卡片 card-desc 优先展示「二次概括 summary」；
 *   关闭时优先展示论文原文 abstract（原文摘要）。
 * - translateChinese：「中文翻译」开关。仅对**非中文文献**生效：开启后卡片标题取
 *   title_chinese、原文摘要取 abstract_chinese。中文文献（is_chinese === true）
 *   本身就是中文，无论开关如何都展示 title / abstract；详情页 detail-title
 *   恒取原文，不受该开关影响。
 *
 * 两项默认值都等价于改造前的展示行为：
 *   preferSummary = true（原来就是 summary 优先）
 *   translateChinese = false（原来非中文条目本就展示英文原文）
 */
const KEY_PREFER_SUMMARY = 'dcrec.prefer_summary'
const KEY_TRANSLATE_CHINESE = 'dcrec.translate_chinese'

function readFlag(key, fallback) {
  try {
    const raw = window.localStorage.getItem(key)
    if (raw === null || raw === '') return fallback
    return raw === '1'
  } catch {
    return fallback
  }
}

function writeFlag(key, value) {
  try {
    window.localStorage.setItem(key, value ? '1' : '0')
  } catch {
    // 无痕模式等禁用存储的场景：退化为仅内存态，不影响使用
  }
}

export const usePreferencesStore = defineStore('preferences', () => {
  const preferSummary = ref(readFlag(KEY_PREFER_SUMMARY, true))
  const translateChinese = ref(readFlag(KEY_TRANSLATE_CHINESE, false))

  watch(preferSummary, (value) => writeFlag(KEY_PREFER_SUMMARY, value))
  watch(translateChinese, (value) => writeFlag(KEY_TRANSLATE_CHINESE, value))

  function setPreferSummary(value) {
    preferSummary.value = !!value
  }

  function setTranslateChinese(value) {
    translateChinese.value = !!value
  }

  function toggleTranslateChinese() {
    translateChinese.value = !translateChinese.value
  }

  return {
    preferSummary,
    translateChinese,
    setPreferSummary,
    setTranslateChinese,
    toggleTranslateChinese,
  }
})
