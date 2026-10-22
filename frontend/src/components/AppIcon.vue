<script setup>
import { computed } from 'vue'

/**
 * 轻量图标组件（内联 SVG，无第三方依赖）。
 * 统一 24x24 viewBox + stroke 风格，颜色继承 currentColor。
 */
const props = defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 18 },
  strokeWidth: { type: [Number, String], default: 1.9 },
})

const ICONS = {
  home: '<path d="M3 10.2 12 3l9 7.2V20a1 1 0 0 1-1 1h-5.2v-6.4H9.2V21H4a1 1 0 0 1-1-1z"/>',
  user: '<path d="M20 21v-1.8a4.2 4.2 0 0 0-4.2-4.2H8.2A4.2 4.2 0 0 0 4 19.2V21"/><circle cx="12" cy="7.4" r="3.9"/>',
  settings:
    '<circle cx="12" cy="12" r="3.1"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>',
  close: '<path d="M18 6 6 18M6 6l12 12"/>',
  logout: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/>',
  login: '<path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"/><path d="m10 17-5-5 5-5"/><path d="M15 12H5"/>',
  sparkles:
    '<path d="M12 3.2 13.7 8l4.8 1.7-4.8 1.7L12 16.2l-1.7-4.8L5.5 9.7 10.3 8z"/><path d="M18.5 15.5l.8 2.2 2.2.8-2.2.8-.8 2.2-.8-2.2-2.2-.8 2.2-.8z"/>',
  calendar:
    '<rect x="3" y="5" width="18" height="16" rx="2.4"/><path d="M3 10h18M8 3v4M16 3v4"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7.4V12l3.2 1.9"/>',
  book: '<path d="M4 4.5A2.5 2.5 0 0 1 6.5 2H20v16H6.5A2.5 2.5 0 0 0 4 20.5z"/><path d="M4 20.5A2.5 2.5 0 0 1 6.5 18H20v4H6.5A2.5 2.5 0 0 1 4 19.5z"/>',
  tag: '<path d="M20.6 13.4 12.4 21.6a2 2 0 0 1-2.8 0l-7.2-7.2a2 2 0 0 1-.6-1.4V4.6A1.6 1.6 0 0 1 3.4 3h8.4a2 2 0 0 1 1.4.6l7.4 7.4a2 2 0 0 1 0 2.8z"/><circle cx="7.6" cy="7.6" r="1.3"/>',
  layers: '<path d="m12 2.6 9 4.7-9 4.7-9-4.7z"/><path d="m3 12.3 9 4.7 9-4.7"/><path d="m3 17 9 4.7 9-4.7"/>',
  refresh: '<path d="M21 12a9 9 0 1 1-3.2-6.9"/><path d="M21 4.5V10h-5.4"/>',
  check: '<path d="m5 12.5 4.6 4.6L19 7.7"/>',
  plus: '<path d="M12 5.5v13M5.5 12h13"/>',
  arrowRight: '<path d="M5 12h14"/><path d="m13 6 6 6-6 6"/>',
  chevronRight: '<path d="m9 6 6 6-6 6"/>',
  external: '<path d="M14 4h6v6"/><path d="M20 4 11 13"/><path d="M19 14.5V19a1.9 1.9 0 0 1-1.9 1.9H5.1A1.9 1.9 0 0 1 3.2 19V7.1A1.9 1.9 0 0 1 5.1 5.2h4.6"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5.2"/><path d="M12 7.8h.01"/>',
  alert: '<path d="M10.3 3.9 1.9 18a2 2 0 0 0 1.7 3h16.8a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/><path d="M12 9v4.4"/><path d="M12 17h.01"/>',
  wand: '<path d="m5 19 9.4-9.4"/><path d="M14.4 9.6 17 7.2"/><path d="M9.4 4.6 10 6.5l1.9.6-1.9.6-.6 1.9-.6-1.9L6.9 7.1l1.9-.6z"/><path d="M17.6 3.2l.5 1.5 1.5.5-1.5.5-.5 1.5-.5-1.5-1.5-.5 1.5-.5z"/>',
  compass: '<circle cx="12" cy="12" r="9"/><path d="m15.6 8.4-2 5.2-5.2 2 2-5.2z"/>',
  send: '<path d="M21.5 2.5 11 13"/><path d="M21.5 2.5 15 21.5l-4-8.5-8.5-4z"/>',
  grid: '<rect x="3" y="3" width="7.5" height="7.5" rx="1.8"/><rect x="13.5" y="3" width="7.5" height="7.5" rx="1.8"/><rect x="3" y="13.5" width="7.5" height="7.5" rx="1.8"/><rect x="13.5" y="13.5" width="7.5" height="7.5" rx="1.8"/>',
  target:
    '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><circle cx="12" cy="12" r="1.4"/>',
}

const markup = computed(() => ICONS[props.name] || ICONS.info)
</script>

<template>
  <svg
    class="app-icon"
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    :stroke-width="strokeWidth"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    v-html="markup"
  />
</template>

<style scoped>
.app-icon {
  flex: 0 0 auto;
  display: block;
}
</style>
