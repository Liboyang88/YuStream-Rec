<script setup>
import { useUiStore } from '@/stores/ui'
import AppIcon from './AppIcon.vue'

const ui = useUiStore()

const ICON_BY_TYPE = { error: 'alert', success: 'check', info: 'info' }
</script>

<template>
  <Teleport to="body">
    <Transition name="toast">
      <div v-if="ui.toast" :key="ui.toast.id" class="toast" :class="`is-${ui.toast.type}`" role="status">
        <AppIcon :name="ICON_BY_TYPE[ui.toast.type] || 'info'" :size="16" />
        <span>{{ ui.toast.message }}</span>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.toast {
  position: fixed;
  left: 50%;
  bottom: 30px;
  z-index: 120;
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 11px 17px;
  border-radius: var(--r-pill);
  background: rgba(21, 24, 36, 0.93);
  color: #fff;
  font-size: 13.5px;
  font-weight: 500;
  box-shadow: var(--shadow-lg);
  backdrop-filter: blur(8px);
  max-width: min(78vw, 480px);
  transform: translate(-50%, 0);
}
.toast.is-error {
  background: rgba(180, 38, 60, 0.95);
}
.toast.is-success {
  background: rgba(12, 122, 61, 0.95);
}

.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.26s var(--ease-soft), transform 0.34s var(--ease-out);
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translate(-50%, 14px) scale(0.96);
}
.toast-enter-to,
.toast-leave-from {
  opacity: 1;
  transform: translate(-50%, 0) scale(1);
}
</style>
