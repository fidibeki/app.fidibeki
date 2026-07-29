<script setup lang="ts">
import { Mail, Monitor, Smartphone } from '@lucide/vue'
import type { FeedbackSource } from '~/utils/feedback-source'

const props = defineProps<{
  source?: FeedbackSource
}>()

const { t } = useI18n()
const normalizedSource = computed<FeedbackSource>(() => (
  props.source === 'ios' || props.source === 'android' || props.source === 'email'
    ? props.source
    : 'web'
))
const sourceIcon = computed(() => {
  if (normalizedSource.value === 'web') return Monitor
  if (normalizedSource.value === 'email') return Mail
  return Smartphone
})
</script>

<template>
  <span
    class="feedback-source-badge"
    :data-source="normalizedSource"
    :aria-label="t('feedback.source.accessible', { source: t(`feedback.source.${normalizedSource}`) })"
  >
    <component :is="sourceIcon" :size="11" aria-hidden="true" />
    {{ t(`feedback.source.${normalizedSource}`) }}
  </span>
</template>

<style scoped>
.feedback-source-badge {
  display: inline-flex;
  min-height: 20px;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
  padding: 2px 6px;
  background: var(--surface-soft);
  color: var(--ink-soft);
  border: 1px solid var(--line);
  border-radius: 4px;
  font-size: 9px;
  font-weight: 750;
  line-height: 1;
  white-space: nowrap;
}

.feedback-source-badge[data-source='ios'] {
  background: var(--teal-soft);
  color: var(--teal);
  border-color: var(--teal-line);
}

.feedback-source-badge[data-source='android'] {
  background: var(--green-soft);
  color: var(--green);
  border-color: var(--green-line);
}

.feedback-source-badge[data-source='email'] {
  background: var(--blue-soft);
  color: var(--blue);
  border-color: color-mix(in srgb, var(--blue) 35%, var(--line));
}
</style>
