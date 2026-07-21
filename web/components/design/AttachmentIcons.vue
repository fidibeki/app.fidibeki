<script setup lang="ts">
import { Camera, Mic, MonitorPlay, Video } from '@lucide/vue'
import type { AttachmentKind } from '~/types/design'

defineProps<{ attachments: AttachmentKind[] }>()

const icons = { screenshot: Camera, audio: Mic, video: Video, replay: MonitorPlay }
</script>

<template>
  <span v-if="attachments.length" class="attachment-icons" aria-label="Attached evidence">
    <span v-for="kind in attachments" :key="kind" :title="kind">
      <component :is="icons[kind]" :size="14" aria-hidden="true" />
      <span class="sr-only">{{ kind }}</span>
    </span>
  </span>
  <span v-else class="muted">None</span>
</template>

<style scoped>
.attachment-icons {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.attachment-icons > span {
  display: inline-grid;
  width: 25px;
  height: 25px;
  place-items: center;
  background: var(--surface-soft);
  color: var(--ink-soft);
  border: 1px solid var(--line);
  border-radius: 4px;
}
</style>
