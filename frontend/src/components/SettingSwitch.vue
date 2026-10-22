<script setup>
import { useId } from 'vue'

/**
 * 通用滑块开关（参考给定样式：隐藏原生 checkbox，用 ::before 做位移动画）。
 *
 * 两种用法：
 *  1) 自带文案：<SettingSwitch v-model="on" label="中文翻译" hint="…" />
 *  2) 只出开关（文案交给外层行渲染）：<SettingSwitch v-model="on" aria-label="…" />
 */
const props = defineProps({
  modelValue: { type: Boolean, default: false },
  /** 开关左侧主文案，留空则只渲染开关本体 */
  label: { type: String, default: '' },
  hint: { type: String, default: '' },
  /** 开关本体尺寸：'md'（默认 60×30）/ 'sm'（44×24，用于设置列表行） */
  size: { type: String, default: 'md' },
  /** 无可见文案时提供给读屏的说明 */
  ariaLabel: { type: String, default: '' },
  disabled: { type: Boolean, default: false },
})

const emit = defineEmits(['update:modelValue'])

// Vue 3.5 的 useId()：同一页面多个开关实例之间不会冲突
const inputId = `switch-${useId()}`

function handleChange(event) {
  emit('update:modelValue', event.target.checked)
}
</script>

<template>
  <label class="switch-field" :class="[`size-${size}`, { 'is-on': modelValue, 'is-disabled': disabled }]">
    <span v-if="label || hint" class="switch-text">
      <span v-if="label" class="switch-label">{{ label }}</span>
      <span v-if="hint" class="switch-hint">{{ hint }}</span>
    </span>

    <input
      :id="inputId"
      class="checkbox"
      type="checkbox"
      role="switch"
      :checked="modelValue"
      :disabled="disabled"
      :aria-label="ariaLabel || undefined"
      :aria-checked="modelValue"
      @change="handleChange"
    />
    <span class="slider" aria-hidden="true" />
  </label>
</template>

<style scoped>
.switch-field {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  user-select: none;
}
.switch-field.is-disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.switch-text {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
}
.switch-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
  white-space: nowrap;
}
.switch-hint {
  font-size: 11.5px;
  color: var(--text-3);
}

/* 原生 checkbox 视觉隐藏但保留键盘可达性（display:none 会丢失焦点与读屏） */
.checkbox {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: 0;
  opacity: 0;
  pointer-events: none;
}

.slider {
  flex: 0 0 auto;
  width: 50px;
  height: 20px;
  background-color: lightgray;
  border-radius: 20px;
  overflow: hidden;
  display: flex;
  align-items: center;
  border: 3px solid transparent;
  transition: 0.3s;
  box-shadow: 0 0 10px 0 rgb(0, 0, 0, 0.25) inset;
  cursor: pointer;
}
.slider::before {
  content: '';
  display: block;
  width: 100%;
  height: 100%;
  background-color: #fff;
  transform: translateX(-30px);
  border-radius: 20px;
  transition: 0.3s;
  box-shadow: 0 0 10px 3px rgb(0, 0, 0, 0.25);
}
.checkbox:checked ~ .slider::before {
  transform: translateX(30px);
  box-shadow: 0 0 10px 3px rgb(0, 0, 0, 0.25);
}
.checkbox:checked ~ .slider {
  background-color: #4f46e573;
}
.checkbox:active ~ .slider::before {
  transform: translate(0);
}
.checkbox:focus-visible ~ .slider {
  outline: 2px solid var(--brand);
  outline-offset: 2px;
}

/* 小号：设置列表行内使用，比例与参考样式一致 */
.size-sm .slider {
  width: 47px;
  height: 24px;
  border-width: 3px;
}
.size-sm .slider::before {
  transform: translateX(-22px);
}
.size-sm .checkbox:checked ~ .slider::before {
  transform: translateX(22px);
}
.size-sm .checkbox:checked ~ .slider {
  background-color: #4f46e573;
}

@media (prefers-reduced-motion: reduce) {
  .slider,
  .slider::before {
    transition-duration: 0.001ms;
  }
}
</style>
