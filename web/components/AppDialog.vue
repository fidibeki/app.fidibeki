<script setup lang="ts">
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)

watch(
  () => props.open,
  async (open) => {
    await nextTick()
    if (open && dialog.value && !dialog.value.open) dialog.value.showModal()
    if (!open && dialog.value?.open) dialog.value.close()
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
  background: transparent;
  border: 0;
}

.app-dialog[open] {
  display: grid;
  place-items: center;
}

.app-dialog::backdrop {
  background: rgb(10 20 17 / 42%);
}
</style>
