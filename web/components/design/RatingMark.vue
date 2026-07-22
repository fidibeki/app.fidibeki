<script setup lang="ts">
const props = defineProps<{ rating?: number | null; compact?: boolean }>()

const hasRating = computed(() => props.rating !== null && props.rating !== undefined)
const label = computed(() => hasRating.value ? `${props.rating} out of 5` : 'Not rated')
const visibleValue = computed(() => hasRating.value ? props.rating : props.compact ? '—' : 'Not rated')
</script>

<template>
  <span class="rating-mark" :class="[hasRating ? `rating-${rating}` : null, { 'rating-mark--compact': compact }]" :title="label">
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

.rating-mark--compact { min-width: 36px; }

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
