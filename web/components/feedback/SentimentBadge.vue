<script setup lang="ts">
import { classifySentiment, formatSentimentScore } from '~/utils/sentiment'

const props = defineProps<{ score?: number | null }>()
const classification = computed(() => classifySentiment(props.score))
const label = computed(() => classification.value === 'unscored'
  ? 'Not scored'
  : classification.value.slice(0, 1).toUpperCase() + classification.value.slice(1))
const exactScore = computed(() => formatSentimentScore(props.score))
const accessibleLabel = computed(() => classification.value === 'unscored'
  ? 'Sentiment not scored'
  : `${label.value} sentiment, exact score ${exactScore.value}`)
</script>

<template>
  <span
    class="sentiment-badge"
    :class="`sentiment-${classification}`"
    :aria-label="accessibleLabel"
    :data-detail="classification === 'unscored' ? 'No exact score' : `Exact score ${exactScore}`"
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
