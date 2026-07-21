<script setup lang="ts">
import { AlertTriangle, Eye, LoaderCircle, X } from '@lucide/vue'
import type { FeedbackAttachment } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'

const props = defineProps<{ attachment: FeedbackAttachment }>()
const { $apiFetch } = useNuxtApp()
const state = ref<'idle' | 'loading' | 'ready' | 'error'>('idle')
const objectURL = ref('')
const errorMessage = ref('')

async function load() {
  if (!props.attachment.content_url || state.value === 'loading') return
  state.value = 'loading'
  errorMessage.value = ''
  try {
    const blob = await $apiFetch<Blob>(props.attachment.content_url, { responseType: 'blob' })
    if (objectURL.value) URL.revokeObjectURL(objectURL.value)
    objectURL.value = URL.createObjectURL(blob)
    state.value = 'ready'
  } catch (error) {
    errorMessage.value = apiErrorMessage(error, 'Evidence could not be loaded.')
    state.value = 'error'
  }
}

function close() {
  if (objectURL.value) URL.revokeObjectURL(objectURL.value)
  objectURL.value = ''
  state.value = 'idle'
}

onBeforeUnmount(() => {
  if (objectURL.value) URL.revokeObjectURL(objectURL.value)
})
</script>

<template>
  <button v-if="attachment.content_url && state === 'idle'" class="attachment-preview-button app-button" type="button" @click="load"><Eye :size="14" /> View</button>
  <span v-else-if="state === 'loading'" class="attachment-preview-loading"><LoaderCircle :size="14" /> Loading</span>
  <div v-else-if="state === 'ready'" class="attachment-preview-backdrop" @click.self="close">
    <section class="attachment-preview-dialog" role="dialog" aria-modal="true" aria-label="Attachment evidence">
      <header><b>{{ attachment.kind }} evidence</b><button class="app-icon-button" type="button" title="Close evidence" @click="close"><X :size="17" /></button></header>
      <div class="attachment-preview-media" :data-kind="attachment.kind">
        <img v-if="attachment.kind === 'screenshot'" :src="objectURL" alt="Captured feedback screenshot" />
        <audio v-else-if="attachment.kind === 'audio'" :src="objectURL" controls preload="metadata" />
        <video v-else-if="attachment.kind === 'video'" :src="objectURL" controls preload="metadata" />
        <a v-else :href="objectURL" target="_blank" rel="noreferrer">Open evidence</a>
      </div>
    </section>
  </div>
  <button v-else-if="state === 'error'" class="attachment-preview-error" type="button" :title="errorMessage" @click="load"><AlertTriangle :size="14" /> Retry view</button>
</template>

<style scoped>
.attachment-preview-button { min-height: 32px; font-size: 10px; }
.attachment-preview-loading { display: inline-flex; align-items: center; gap: 5px; color: var(--muted); font-size: 9px; }
.attachment-preview-loading svg { animation: attachment-spin 700ms linear infinite; }
.attachment-preview-backdrop { position: fixed; inset: 0; z-index: 130; display: grid; place-items: center; padding: 20px; background: rgb(10 20 17 / 52%); }
.attachment-preview-dialog { width: min(900px, 100%); max-height: 90vh; overflow: hidden; background: #fff; border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.attachment-preview-dialog > header { display: flex; min-height: 50px; align-items: center; justify-content: space-between; padding: 6px 8px 6px 14px; border-bottom: 1px solid var(--line); text-transform: capitalize; }
.attachment-preview-dialog > header b { font-size: 11px; }
.attachment-preview-media { display: grid; width: 100%; min-height: 180px; max-height: calc(90vh - 51px); place-items: center; padding: 14px; overflow: auto; background: #eef2f0; }
.attachment-preview-media img, .attachment-preview-media video { display: block; width: min(100%, 860px); max-height: calc(90vh - 80px); object-fit: contain; background: #fff; border: 1px solid var(--line); border-radius: 4px; }
.attachment-preview-media audio { width: min(100%, 520px); height: 38px; }
.attachment-preview-media a { color: var(--teal); font-size: 10px; font-weight: 700; }
.attachment-preview-error { display: inline-flex; align-items: center; gap: 5px; padding: 0; background: transparent; color: var(--red); border: 0; font-size: 9px; }
@keyframes attachment-spin { to { transform: rotate(360deg); } }
</style>
