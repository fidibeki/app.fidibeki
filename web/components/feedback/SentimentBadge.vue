<script setup lang="ts">
import { classifySentiment } from '~/utils/sentiment'

const props = defineProps<{ score?: number | null }>()
const { t } = useI18n()
const { decimal } = useRegionalFormat()
const classification = computed(() => classifySentiment(props.score))
const label = computed(() => t(`feedback.sentiment.${classification.value}`))
const exactScore = computed(() => classification.value === 'unscored'
  ? t('feedback.sentiment.unscored')
  : decimal(props.score!, { minimumFractionDigits: 2, maximumFractionDigits: 2 }))
const accessibleLabel = computed(() => classification.value === 'unscored'
  ? t('feedback.sentiment.notScored')
  : t('feedback.sentiment.accessible', { label: label.value, score: exactScore.value }))
const detail = computed(() => classification.value === 'unscored'
  ? t('feedback.sentiment.noExactScore')
  : t('feedback.sentiment.exactScore', { score: exactScore.value }))
</script>

<template>
  <span
    class="sentiment-badge"
    :class="`sentiment-${classification}`"
    :aria-label="accessibleLabel"
    :data-detail="detail"
    tabindex="0"
  >{{ label }}</span>
</template>

<style scoped>
.sentiment-badge {
  --sentiment-detail-visible: 0;
  position: relative;
  display: inline-flex;
  min-height: 26px;
  align-items: center;
  gap: 6px;
  padding: 2px 8px;
  background: var(--surface-soft);
  color: var(--ink-soft);
  border: 1px solid var(--line);
  border-radius: 4px;
  font-size: 9px;
  font-weight: 750;
  white-space: nowrap;
}
.sentiment-badge::before { width: 7px; height: 7px; flex: 0 0 7px; background: var(--muted); border-radius: 50%; content: ''; }
.sentiment-badge::after {
  position: absolute;
  right: 0;
  bottom: calc(100% + 6px);
  z-index: 30;
  width: max-content;
  max-width: 190px;
  padding: 6px 8px;
  background: #17342f;
  color: #fff;
  border-radius: 4px;
  box-shadow: var(--shadow-overlay);
  content: attr(data-detail);
  font-size: 9px;
  font-weight: 650;
  opacity: var(--sentiment-detail-visible);
  pointer-events: none;
  transform: translateY(calc((1 - var(--sentiment-detail-visible)) * 2px));
}
.sentiment-badge:hover,
.sentiment-badge:focus { --sentiment-detail-visible: 1; outline: none; }
.sentiment-badge:focus-visible { box-shadow: 0 0 0 2px rgb(8 127 115 / 24%); }
.sentiment-negative { background: var(--red-soft); color: #8f2732; border-color: #efc2c6; }
.sentiment-negative::before { background: var(--red); }
.sentiment-neutral { background: #f1f3f2; color: #53605c; }
.sentiment-positive { background: var(--green-soft); color: #175f40; border-color: #b8dfca; }
.sentiment-positive::before { background: var(--green); }
</style>
