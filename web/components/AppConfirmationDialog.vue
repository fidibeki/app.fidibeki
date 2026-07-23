<script setup lang="ts">
import { AlertTriangle, LoaderCircle } from '@lucide/vue'

defineProps<{
  open: boolean
  title: string
  description: string
  confirmLabel: string
  pendingLabel: string
  cancelLabel: string
  pending?: boolean
  error?: string
}>()

const emit = defineEmits<{ cancel: [], confirm: [] }>()
const identifier = useId()

function cancel(pending = false) {
  if (!pending) emit('cancel')
}
</script>

<template>
  <AppDialog
    :open="open"
    initial-focus="[data-confirm-cancel]"
    :aria-labelledby="`${identifier}-title`"
    :aria-describedby="`${identifier}-description`"
    @close="cancel(pending)"
  >
    <section class="app-modal app-confirmation">
      <header>
        <span class="app-confirmation__mark"><AlertTriangle :size="18" /></span>
        <div>
          <p class="app-eyebrow">{{ confirmLabel }}</p>
          <h2 :id="`${identifier}-title`">{{ title }}</h2>
        </div>
      </header>
      <div class="app-modal__body">
        <p :id="`${identifier}-description`">{{ description }}</p>
        <p v-if="error" class="app-confirmation__error" role="alert">{{ error }}</p>
      </div>
      <footer>
        <button
          class="app-button"
          type="button"
          data-confirm-cancel
          :disabled="pending"
          @click="cancel(pending)"
        >
          {{ cancelLabel }}
        </button>
        <button class="app-button app-confirmation__action" type="button" :disabled="pending" @click="emit('confirm')">
          <LoaderCircle v-if="pending" class="app-confirmation__spinner" :size="15" />
          {{ pending ? pendingLabel : confirmLabel }}
        </button>
      </footer>
    </section>
  </AppDialog>
</template>

<style scoped>
.app-confirmation { width: min(500px, calc(100vw - 40px)); }
.app-confirmation > header { justify-content: flex-start; }
.app-confirmation__mark {
  display: grid;
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  place-items: center;
  background: var(--red-soft);
  color: var(--red);
  border-radius: 5px;
}
.app-confirmation .app-modal__body > p:first-child { color: var(--ink-soft); font-size: 11px; line-height: 1.6; }
.app-confirmation__error {
  padding: 9px 10px;
  background: var(--red-soft);
  color: var(--red);
  border-radius: 4px;
  font-size: 10px;
}
.app-confirmation__action { background: var(--red); color: #fff; border-color: var(--red); }
.app-confirmation__action:hover:not(:disabled) { background: #982f3a; border-color: #982f3a; }
.app-confirmation__spinner { animation: app-confirmation-spin 800ms linear infinite; }
@keyframes app-confirmation-spin { to { transform: rotate(360deg); } }

@media (max-width: 600px) {
  .app-confirmation { width: 100%; }
  .app-confirmation > footer { align-items: stretch; flex-direction: column-reverse; }
  .app-confirmation > footer .app-button { width: 100%; }
}

@media (prefers-reduced-motion: reduce) {
  .app-confirmation__spinner { animation: none; }
}
</style>
