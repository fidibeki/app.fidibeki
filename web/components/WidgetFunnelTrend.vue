<script setup lang="ts">
import type { WidgetAnalyticsTrendPoint } from '~/types/api'

const props = defineProps<{ points: WidgetAnalyticsTrendPoint[] }>()

const width = 1000
const height = 210
const top = 12
const bottom = 10
const metrics = ['impressions', 'interactions', 'submissions'] as const
const maximum = computed(() => Math.max(1, ...props.points.flatMap(point => metrics.map(metric => point[metric]))))
const gridLines = [0, 0.25, 0.5, 0.75, 1]

function linePoints(metric: typeof metrics[number]) {
  return props.points.map((point, index) => {
    const x = props.points.length <= 1 ? width / 2 : (index / (props.points.length - 1)) * width
    const usableHeight = height - top - bottom
    const y = top + usableHeight - (point[metric] / maximum.value) * usableHeight
    return `${x.toFixed(2)},${y.toFixed(2)}`
  }).join(' ')
}

function shortDate(value?: string) {
  if (!value) return ''
  return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' })
    .format(new Date(`${value}T00:00:00Z`))
}

const dateLabels = computed(() => {
  const points = props.points
  if (!points.length) return ['', '', '']
  return [shortDate(points[0]?.date), shortDate(points[Math.floor((points.length - 1) / 2)]?.date), shortDate(points.at(-1)?.date)]
})
</script>

<template>
  <div class="funnel-trend">
    <div class="funnel-trend-legend" aria-label="Trend series">
      <span data-series="impressions"><i />Impressions</span>
      <span data-series="interactions"><i />Interactions</span>
      <span data-series="submissions"><i />Submissions</span>
    </div>
    <svg :viewBox="`0 0 ${width} ${height}`" preserveAspectRatio="none" role="img" aria-label="Daily widget funnel trend">
      <line
        v-for="line in gridLines"
        :key="line"
        x1="0"
        :y1="top + (height - top - bottom) * line"
        :x2="width"
        :y2="top + (height - top - bottom) * line"
        class="funnel-trend-grid"
      />
      <polyline class="funnel-trend-line funnel-trend-line--impressions" :points="linePoints('impressions')" />
      <polyline class="funnel-trend-line funnel-trend-line--interactions" :points="linePoints('interactions')" />
      <polyline class="funnel-trend-line funnel-trend-line--submissions" :points="linePoints('submissions')" />
    </svg>
    <div class="funnel-trend-dates" aria-hidden="true">
      <span>{{ dateLabels[0] }}</span><span>{{ dateLabels[1] }}</span><span>{{ dateLabels[2] }}</span>
    </div>
  </div>
</template>

<style scoped>
.funnel-trend { min-width: 0; }
.funnel-trend-legend { display: flex; flex-wrap: wrap; gap: 16px; justify-content: flex-end; margin-bottom: 10px; color: var(--muted); font-size: 10px; font-weight: 700; }
.funnel-trend-legend span { display: inline-flex; align-items: center; gap: 6px; }
.funnel-trend-legend i { width: 16px; height: 2px; background: var(--teal); }
.funnel-trend-legend [data-series='interactions'] i { background: var(--blue); }
.funnel-trend-legend [data-series='submissions'] i { background: var(--amber); }
svg { display: block; width: 100%; height: 210px; overflow: visible; }
.funnel-trend-grid { stroke: var(--line); stroke-width: 1; vector-effect: non-scaling-stroke; }
.funnel-trend-line { fill: none; stroke-linecap: round; stroke-linejoin: round; stroke-width: 2; vector-effect: non-scaling-stroke; }
.funnel-trend-line--impressions { stroke: var(--teal); }
.funnel-trend-line--interactions { stroke: var(--blue); }
.funnel-trend-line--submissions { stroke: var(--amber); }
.funnel-trend-dates { display: flex; justify-content: space-between; margin-top: 8px; color: var(--muted); font-size: 9px; font-variant-numeric: tabular-nums; }
@media (max-width: 700px) { svg { height: 170px; } .funnel-trend-legend { justify-content: flex-start; gap: 12px; } }
</style>
