<script setup lang="ts">
import { AlertTriangle, CheckCircle2, X } from '@lucide/vue'

const { t } = useI18n()
const { toast, dismiss } = useAppToast()
</script>

<template>
  <Transition name="app-toast">
    <aside
      v-if="toast"
      class="app-toast"
      :data-tone="toast.tone"
      :role="toast.tone === 'error' ? 'alert' : 'status'"
      :aria-live="toast.tone === 'error' ? 'assertive' : 'polite'"
    >
      <component :is="toast.tone === 'error' ? AlertTriangle : CheckCircle2" :size="17" />
      <span>{{ toast.message }}</span>
      <button type="button" :title="t('admin.common.close')" :aria-label="t('admin.common.close')" @click="dismiss(toast.id)">
        <X :size="15" />
      </button>
    </aside>
  </Transition>
</template>

<style scoped>
.app-toast {
  position: fixed;
  right: 20px;
  bottom: 20px;
  z-index: 120;
  display: grid;
  width: min(380px, calc(100vw - 40px));
  min-height: 48px;
  grid-template-columns: auto minmax(0, 1fr) 28px;
  align-items: center;
  gap: 9px;
  padding: 9px 9px 9px 13px;
  background: #17342f;
  color: #fff;
  border: 1px solid #2c524a;
  border-radius: 6px;
  box-shadow: var(--shadow-overlay);
  font-size: 11px;
  font-weight: 650;
}

.app-toast[data-tone='error'] {
  background: #7f2530;
  border-color: #a13a46;
}

.app-toast button {
  display: grid;
  width: 28px;
  height: 28px;
  padding: 0;
  place-items: center;
  background: transparent;
  color: inherit;
  border: 0;
  border-radius: 4px;
}

.app-toast button:hover { background: rgb(255 255 255 / 12%); }
.app-toast-enter-active, .app-toast-leave-active { transition: opacity 150ms ease, transform 150ms ease; }
.app-toast-enter-from, .app-toast-leave-to { opacity: 0; transform: translateY(6px); }

@media (max-width: 600px) {
  .app-toast {
    right: 12px;
    bottom: 12px;
    left: 12px;
    width: auto;
  }
}

@media (prefers-reduced-motion: reduce) {
  .app-toast-enter-active, .app-toast-leave-active { transition: none; }
}
</style>
