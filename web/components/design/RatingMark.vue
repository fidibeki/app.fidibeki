<script setup lang="ts">
const props = defineProps<{ rating?: number | null; compact?: boolean; stars?: boolean }>()
const { t } = useI18n()

const hasRating = computed(() => props.rating !== null && props.rating !== undefined)
// Clamped, because the stars variant draws a fixed row of five: a value outside
// the range would otherwise render a broken one.
const filled = computed(() => Math.min(5, Math.max(0, Math.round(props.rating ?? 0))))
const label = computed(() => hasRating.value
  ? t('feedback.rating.value', { rating: props.rating })
  : t('feedback.rating.notRated'))
const visibleValue = computed(() => hasRating.value
  ? props.rating
  : props.compact ? '—' : t('feedback.rating.notRated'))
</script>

<template>
  <!-- Stars where the rating is being read as a customer would see it published;
       the numeric badge where it is being compared across a triage list. Both
       carry the same label, so nothing changes for a screen reader. -->
  <span v-if="stars" class="rating-stars" :class="{ 'rating-stars--compact': compact }" :title="label">
    <template v-if="hasRating">
      <span v-for="index in 5" :key="index" :class="index <= filled ? 'is-on' : 'is-off'" aria-hidden="true">★</span>
    </template>
    <span v-else class="rating-stars__empty" aria-hidden="true">—</span>
    <span class="sr-only">{{ label }}</span>
  </span>
  <span v-else class="rating-mark" :class="[hasRating ? `rating-${rating}` : 'rating-mark--empty', { 'rating-mark--compact': compact }]" :title="label">
    <span aria-hidden="true">{{ visibleValue }}</span>
    <span v-if="hasRating && !compact" class="rating-label">/ 5</span>
    <span class="sr-only">{{ label }}</span>
  </span>
</template>

<style scoped>
.rating-mark {
  display: inline-flex;
  min-width: 46px;
  height: 28px;
  align-items: baseline;
  justify-content: center;
  flex: 0 0 auto;
  gap: 2px;
  padding: 0 7px;
  background: var(--surface-soft);
  color: var(--ink);
  border: 1px solid var(--line);
  border-radius: 5px;
  font-weight: 750;
  line-height: 26px;
  white-space: nowrap;
}

.rating-stars {
  display: inline-flex;
  align-items: center;
  flex: 0 0 auto;
  gap: 1px;
  color: var(--amber-ink);
  font-size: 13px;
  line-height: 1;
  white-space: nowrap;
}

.rating-stars--compact { font-size: 11px; }
.rating-stars .is-off { color: var(--line-strong); }
.rating-stars__empty { color: var(--muted); font-size: 10px; font-weight: 700; }

.rating-mark--compact { min-width: 36px; }
.rating-mark--empty { color: var(--muted); font-size: 10px; font-weight: 700; }

.rating-label {
  color: var(--muted);
  font-size: 10px;
}

.rating-1,
.rating-2 {
  background: var(--red-soft);
  color: #8f2732;
  border-color: #efc2c6;
}

.rating-3 {
  background: var(--amber-soft);
  color: #7a4305;
  border-color: #f0d29c;
}

.rating-4,
.rating-5 {
  background: var(--green-soft);
  color: #175f40;
  border-color: #b8dfca;
}
</style>
