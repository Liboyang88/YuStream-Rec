<script setup>
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import AppIcon from './AppIcon.vue'

/**
 * 通用「固定浮动窗口」。
 * - Teleport 到 body，脱离中间内容区的滚动容器，滚轮不会穿透到底层卡片流。
 * - 打开时锁定 body 滚动、支持 Esc / 点击遮罩关闭。
 */
const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: '' },
  subtitle: { type: String, default: '' },
  icon: { type: String, default: '' },
  size: { type: String, default: 'md' }, // sm | md | lg
  closeOnBackdrop: { type: Boolean, default: true },
  bare: { type: Boolean, default: false },
})

const emit = defineEmits(['close'])

const panelRef = ref(null)
let previousBodyOverflow = ''

function requestClose() {
  emit('close')
}

function onKeydown(event) {
  if (event.key === 'Escape') {
    event.stopPropagation()
    requestClose()
  }
}

function onBackdropClick() {
  if (props.closeOnBackdrop) requestClose()
}

watch(
  () => props.open,
  async (isOpen) => {
    if (typeof document === 'undefined') return

    if (isOpen) {
      previousBodyOverflow = document.body.style.overflow
      document.body.style.overflow = 'hidden'
      window.addEventListener('keydown', onKeydown)
      await nextTick()
      // 让浮窗自身获得焦点，键盘用户可直接 Tab 进入内容
      panelRef.value?.focus?.({ preventScroll: true })
    } else {
      document.body.style.overflow = previousBodyOverflow || ''
      window.removeEventListener('keydown', onKeydown)
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (typeof document === 'undefined') return
  document.body.style.overflow = previousBodyOverflow || ''
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="open" class="modal-root" role="presentation">
        <div class="modal-backdrop" @click="onBackdropClick" />

        <div
          ref="panelRef"
          class="modal-panel"
          :class="[`size-${size}`, { 'is-bare': bare }]"
          role="dialog"
          aria-modal="true"
          :aria-label="title || '弹窗'"
          tabindex="-1"
        >
          <header v-if="!bare && (title || $slots.header)" class="modal-header">
            <div class="modal-heading">
              <span v-if="icon" class="modal-icon"><AppIcon :name="icon" :size="17" /></span>
              <div class="modal-titles">
                <h2 class="modal-title">{{ title }}</h2>
                <p v-if="subtitle" class="modal-subtitle">{{ subtitle }}</p>
              </div>
            </div>
            <button type="button" class="modal-close" aria-label="关闭" @click="requestClose">
              <AppIcon name="close" :size="17" />
            </button>
          </header>

          <div class="modal-body">
            <slot />
          </div>

          <footer v-if="$slots.footer" class="modal-footer">
            <slot name="footer" />
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.modal-root {
  position: fixed;
  inset: 0;
  z-index: 90;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 28px 22px;
}

.modal-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(20, 22, 34, 0.34);
  backdrop-filter: blur(7px) saturate(120%);
  -webkit-backdrop-filter: blur(7px) saturate(120%);
}

.modal-panel {
  position: relative;
  display: flex;
  flex-direction: column;
  width: 100%;
  max-height: min(86vh, 860px);
  background: var(--surface);
  border-radius: var(--r-xl);
  box-shadow: var(--shadow-lg);
  border: 1px solid rgba(255, 255, 255, 0.7);
  outline: none;
  overflow: hidden;
}
.size-sm {
  max-width: 420px;
}
.size-md {
  max-width: 560px;
}
.size-lg {
  max-width: 760px;
}
.is-bare {
  background: transparent;
  border: none;
  box-shadow: none;
  max-height: none;
}
.is-bare .modal-body {
  padding: 0;
  overflow: visible;
}

.modal-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 20px 20px 16px 22px;
  border-bottom: 1px solid var(--border);
  background: linear-gradient(180deg, #fbfbfe 0%, #ffffff 100%);
}
.modal-heading {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  min-width: 0;
}
.modal-icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand);
  flex: 0 0 auto;
  margin-top: 1px;
}
.modal-titles {
  min-width: 0;
}
.modal-title {
  font-size: 16.5px;
  font-weight: 700;
  letter-spacing: -0.01em;
}
.modal-subtitle {
  margin-top: 2px;
  font-size: 12.5px;
  color: var(--text-3);
}
.modal-close {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: var(--r-sm);
  color: var(--text-3);
  flex: 0 0 auto;
  transition: background-color 0.16s var(--ease-soft), color 0.16s var(--ease-soft);
}
.modal-close:hover {
  background: var(--surface-2);
  color: var(--text);
}

.modal-body {
  padding: 20px 22px 22px;
  overflow-y: auto;
  flex: 1 1 auto;
}

.modal-footer {
  padding: 14px 22px;
  border-top: 1px solid var(--border);
  background: #fcfcfe;
  display: flex;
  gap: 10px;
  justify-content: flex-end;
}

/* 过渡动画 */
.modal-enter-active .modal-panel,
.modal-leave-active .modal-panel {
  transition: opacity 0.24s var(--ease-out), transform 0.32s var(--ease-out);
}
.modal-enter-from .modal-panel,
.modal-leave-to .modal-panel {
  opacity: 0;
  transform: translateY(16px) scale(0.975);
}
.modal-enter-active .modal-backdrop,
.modal-leave-active .modal-backdrop {
  transition: opacity 0.24s var(--ease-soft);
}
.modal-enter-from .modal-backdrop,
.modal-leave-to .modal-backdrop {
  opacity: 0;
}

@media (max-width: 640px) {
  .modal-root {
    padding: 12px;
    align-items: flex-end;
  }
  .modal-panel {
    max-height: 92vh;
    border-radius: var(--r-lg);
  }
}
</style>
