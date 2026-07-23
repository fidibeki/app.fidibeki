<script setup lang="ts">
const props = defineProps<{ open: boolean, initialFocus?: string }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
let restoreFocusTo: HTMLElement | null = null

watch(
  () => props.open,
  async (open) => {
    await nextTick()
    if (open && dialog.value && !dialog.value.open) {
      restoreFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
      dialog.value.showModal()
      await nextTick()
      if (props.initialFocus) dialog.value.querySelector<HTMLElement>(props.initialFocus)?.focus()
    }
    if (!open && dialog.value?.open) {
      dialog.value.close()
      await nextTick()
      restoreFocusTo?.focus()
      restoreFocusTo = null
    }
  },
  { immediate: true, flush: 'post' },
)

function closeFromBackdrop(event: MouseEvent) {
  if (event.target === dialog.value) emit('close')
}
</script>

<template>
  <dialog ref="dialog" class="app-dialog" @cancel.prevent="emit('close')" @click="closeFromBackdrop">
    <slot />
  </dialog>
</template>

<style scoped>
.app-dialog {
  width: 100%;
  max-width: none;
  height: 100%;
  max-height: none;
  margin: 0;
  padding: 20px;
  overflow: hidden;
  background: transparent;
  border: 0;
}

@media (max-width: 600px) {
  .app-dialog { padding: 8px; }
}

.app-dialog[open] {
  display: grid;
  place-items: center;
}

.app-dialog::backdrop {
  background: rgb(10 20 17 / 42%);
}
</style>
