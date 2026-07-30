<script setup lang="ts">
import { Monitor, Smartphone } from '@lucide/vue'
import type { FeedbackSource } from '~/utils/feedback-source'

const props = defineProps<{
  source?: FeedbackSource
  /** Icon-only, for dense list rows where the label would repeat down the
   *  column; the accessible name is unchanged. */
  compact?: boolean
}>()

const { t } = useI18n()
const normalizedSource = computed<FeedbackSource>(() => (
  props.source === 'ios' || props.source === 'android' ? props.source : 'web'
))
const sourceIcon = computed(() => normalizedSource.value === 'web' ? Monitor : Smartphone)
</script>

<template>
  <span
    class="feedback-source-badge"
    :data-source="normalizedSource"
    :aria-label="t('feedback.source.accessible', { source: t(`feedback.source.${normalizedSource}`) })"
    :title="compact ? t(`feedback.source.${normalizedSource}`) : undefined"
  >
    <component :is="sourceIcon" :size="11" aria-hidden="true" />
    <template v-if="!compact">{{ t(`feedback.source.${normalizedSource}`) }}</template>
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
</style>
