<script setup lang="ts">
import { Activity, AlertTriangle, RefreshCw, ShieldCheck } from '@lucide/vue'
import type {
  Project,
  SubmissionHealthCategory,
  SubmissionHealthResponse,
} from '~/types/api'
import {
  buildSubmissionHealthTrend,
  submissionHealthCategories,
} from '~/utils/submission-health'

defineProps<{ projects: Project[] }>()

const { $apiFetch } = useNuxtApp()
const { can } = useAuth()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { integer } = useRegionalFormat()
const permitted = computed(() => can('tenant:read'))
const canFilterProjects = computed(() => can('project:read'))
const range = ref<'24h' | '7d' | '30d'>('7d')
const projectID = ref('')
const health = ref<SubmissionHealthResponse | null>(null)
const loadState = ref<'idle' | 'loading' | 'ready' | 'error' | 'forbidden'>('idle')
const failure = ref('')
let generation = 0

const categoryRows = computed(() => {
  const totals = new Map(
    (health.value?.totals ?? []).map(total => [total.category, Math.max(0, total.count)]),
  )
  return submissionHealthCategories.map(category => ({
    ...category,
    count: totals.get(category.id) ?? 0,
  }))
})

const trend = computed(() => health.value
  ? buildSubmissionHealthTrend(
      health.value.trend,
      health.value.period.start_at,
      health.value.period.end_at,
    )
  : [])

const reasonRows = computed(() => {
  const reasons = new Map<string, { category: SubmissionHealthCategory, count: number }>()
  for (const point of health.value?.trend ?? []) {
    const key = `${point.category}:${point.reason}`
    const current = reasons.get(key)
    reasons.set(key, {
      category: point.category,
      count: (current?.count ?? 0) + Math.max(0, point.count),
    })
  }
  return Array.from(reasons, ([key, value]) => ({
    reason: key.slice(key.indexOf(':') + 1),
    ...value,
  }))
    .sort((left, right) => right.count - left.count || left.reason.localeCompare(right.reason))
    .slice(0, 6)
})

const knownReasons = new Set([
  'submission_rate_limited',
  'upload_rate_limited',
  'replay_rate_limited',
  'spam_rejected',
  'signature_mismatch',
  'ai_daily_budget_exhausted',
  'provider_timeout',
])

const categoryLabel = (category: SubmissionHealthCategory) =>
  t(`admin.health.categories.${category}.label`)

const categoryDescription = (category: SubmissionHealthCategory) =>
  t(`admin.health.categories.${category}.description`)

const reasonLabel = (reason: string) =>
  t(`admin.health.reasons.${knownReasons.has(reason) ? reason : 'unclassified'}`)

async function loadHealth() {
  const requestGeneration = ++generation
  if (!permitted.value) {
    health.value = null
    loadState.value = 'forbidden'
    return
  }
  loadState.value = 'loading'
  failure.value = ''
  try {
    const query = new URLSearchParams({ range: range.value })
    if (projectID.value) query.set('project_id', projectID.value)
    const response = await $apiFetch<SubmissionHealthResponse>(
      `/v1/abuse/diagnostics?${query.toString()}`,
    )
    if (requestGeneration !== generation) return
    health.value = response
    loadState.value = 'ready'
  } catch (loadError) {
    if (requestGeneration !== generation) return
    health.value = null
    failure.value = localizedError.message(loadError, 'admin.health.error')
    loadState.value = 'error'
  }
}

watch([permitted, range, projectID], () => { void loadHealth() }, { immediate: true })
</script>

<template>
  <section class="app-surface submission-health">
    <header class="submission-health-head">
      <div>
        <p class="app-eyebrow">{{ t('admin.health.eyebrow') }}</p>
        <h2>{{ t('admin.health.title') }}</h2>
        <span>{{ t('admin.health.description') }}</span>
      </div>
      <div v-if="permitted" class="submission-health-actions">
        <label>
          <span>{{ t('admin.health.period') }}</span>
          <select v-model="range" class="app-select" :aria-label="t('admin.health.periodLabel')">
            <option value="24h">{{ t('admin.health.last24Hours') }}</option>
            <option value="7d">{{ t('admin.health.last7Days') }}</option>
            <option value="30d">{{ t('admin.health.last30Days') }}</option>
          </select>
        </label>
        <label v-if="canFilterProjects">
          <span>{{ t('admin.health.project') }}</span>
          <select v-model="projectID" class="app-select" :aria-label="t('admin.health.projectLabel')">
            <option value="">{{ t('admin.health.allProjects') }}</option>
            <option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</option>
          </select>
        </label>
        <button
          class="app-icon-button"
          type="button"
          :title="t('admin.health.refresh')"
          :aria-label="t('admin.health.refresh')"
          :disabled="loadState === 'loading'"
          @click="loadHealth"
        >
          <RefreshCw :size="16" :class="{ 'submission-health-spin': loadState === 'loading' }" />
        </button>
      </div>
    </header>

    <div v-if="loadState === 'forbidden'" class="submission-health-state">
      <ShieldCheck :size="24" />
      <h3>{{ t('admin.health.accessRequired') }}</h3>
      <p>{{ t('admin.health.accessDescription') }}</p>
    </div>
    <div v-else-if="loadState === 'loading' && !health" class="submission-health-state" role="status">
      <span class="app-spinner" />
      <h3>{{ t('admin.health.loading') }}</h3>
    </div>
    <div v-else-if="loadState === 'error'" class="submission-health-state">
      <AlertTriangle :size="24" />
      <h3>{{ t('admin.health.unavailable') }}</h3>
      <p>{{ failure }}</p>
      <button class="app-button" type="button" @click="loadHealth">{{ t('admin.common.tryAgain') }}</button>
    </div>
    <template v-else-if="health">
      <div class="submission-health-summary" :data-empty="health.total === 0">
        <div>
          <span>{{ t('admin.health.recordedSignals') }}</span>
          <strong>{{ integer(health.total) }}</strong>
        </div>
        <p v-if="health.total === 0"><ShieldCheck :size="17" /> {{ t('admin.health.noPressure') }}</p>
        <p v-else><Activity :size="17" /> {{ t('admin.health.aggregateDescription') }}</p>
      </div>

      <div class="submission-health-categories" :aria-label="t('admin.health.categoriesLabel')">
        <article
          v-for="category in categoryRows"
          :key="category.id"
          :data-tone="category.tone"
          :data-state="category.count > 0 ? 'attention' : 'quiet'"
        >
          <i />
          <div><h3>{{ categoryLabel(category.id) }}</h3><p>{{ categoryDescription(category.id) }}</p></div>
          <strong>{{ integer(category.count) }}</strong>
        </article>
      </div>

      <div class="submission-health-trend">
        <header>
          <div><span>{{ t('admin.health.activity') }}</span><strong>{{ range === '24h' ? t('admin.health.hourly') : t('admin.health.selectedPeriod') }}</strong></div>
          <small>UTC</small>
        </header>
        <div
          v-if="health.total > 0"
          class="submission-health-bars"
          role="img"
          :aria-label="t('admin.health.chartLabel', { count: integer(health.total), buckets: integer(trend.length) })"
        >
          <span
            v-for="bucket in trend"
            :key="bucket.startAt"
            :aria-label="t('admin.health.bucketLabel', { label: bucket.label, count: integer(bucket.count) })"
            :title="t('admin.health.bucketLabel', { label: bucket.label, count: integer(bucket.count) })"
          >
            <i :style="{ height: `${bucket.count > 0 ? Math.max(7, bucket.heightPercent) : 2}%` }" />
          </span>
        </div>
        <div v-else class="submission-health-no-trend">{{ t('admin.health.noActivity') }}</div>
        <div v-if="trend.length" class="submission-health-trend-labels">
          <span>{{ trend[0]?.label }}</span>
          <span>{{ trend[Math.floor(trend.length / 2)]?.label }}</span>
          <span>{{ trend[trend.length - 1]?.label }}</span>
        </div>
      </div>

      <div v-if="reasonRows.length" class="submission-health-reasons">
        <header><span>{{ t('admin.health.topReasons') }}</span><span>{{ t('admin.health.events') }}</span></header>
        <div v-for="row in reasonRows" :key="`${row.category}:${row.reason}`">
          <span><strong>{{ reasonLabel(row.reason) }}</strong><small>{{ categoryLabel(row.category) }}</small></span>
          <b>{{ integer(row.count) }}</b>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.submission-health { overflow: hidden; }
.submission-health-head { display: flex; min-height: 78px; align-items: center; justify-content: space-between; gap: 20px; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.submission-health-head h2 { margin: 3px 0 2px; font-size: 14px; }
.submission-health-head > div:first-child > span { color: var(--muted); font-size: 9px; }
.submission-health-actions { display: flex; align-items: flex-end; gap: 8px; }
.submission-health-actions label { display: grid; gap: 5px; }
.submission-health-actions label > span { color: var(--muted); font-size: 8px; font-weight: 750; text-transform: uppercase; }
.submission-health-actions .app-select { min-width: 148px; min-height: 34px; padding-top: 0; padding-bottom: 0; font-size: 10px; }
.submission-health-actions .app-icon-button { margin-bottom: 1px; }
.submission-health-state { display: grid; min-height: 300px; place-items: center; align-content: center; gap: 8px; padding: 24px; color: var(--muted); text-align: center; }
.submission-health-state h3 { margin: 0; color: var(--ink-soft); font-size: 12px; }
.submission-health-state p { max-width: 420px; margin: 0; font-size: 10px; line-height: 1.55; }
.submission-health-summary { display: flex; min-height: 90px; align-items: center; justify-content: space-between; gap: 22px; padding: 14px 18px; border-bottom: 1px solid var(--line); }
.submission-health-summary > div { display: flex; flex-direction: column; gap: 4px; }
.submission-health-summary > div span { color: var(--muted); font-size: 8px; font-weight: 750; text-transform: uppercase; }
.submission-health-summary > div strong { font-size: 28px; font-variant-numeric: tabular-nums; }
.submission-health-summary p { display: flex; max-width: 380px; align-items: center; gap: 8px; margin: 0; color: var(--muted); font-size: 9px; line-height: 1.5; }
.submission-health-summary[data-empty='true'] p { color: var(--green); }
.submission-health-categories { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
.submission-health-categories article { display: grid; min-height: 84px; grid-template-columns: 4px minmax(0, 1fr) auto; align-items: center; gap: 12px; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.submission-health-categories article:nth-child(odd) { border-right: 1px solid var(--line); }
.submission-health-categories article > i { width: 4px; height: 34px; background: var(--green); border-radius: 2px; }
.submission-health-categories article[data-state='quiet'] > i { background: #aab5b1; }
.submission-health-categories article[data-tone='red'][data-state='attention'] > i { background: var(--red); }
.submission-health-categories article[data-tone='amber'][data-state='attention'] > i { background: var(--amber); }
.submission-health-categories article[data-tone='blue'][data-state='attention'] > i { background: #3975a8; }
.submission-health-categories article[data-tone='violet'][data-state='attention'] > i { background: #7357a6; }
.submission-health-categories article[data-tone='teal'][data-state='attention'] > i { background: var(--teal); }
.submission-health-categories article[data-tone='rose'][data-state='attention'] > i { background: #b85a73; }
.submission-health-categories article div { min-width: 0; }
.submission-health-categories h3 { margin: 0; color: var(--ink-soft); font-size: 11px; }
.submission-health-categories p { margin: 4px 0 0; color: var(--muted); font-size: 8px; line-height: 1.45; }
.submission-health-categories strong { font-size: 16px; font-variant-numeric: tabular-nums; }
.submission-health-trend { padding: 16px 18px; border-bottom: 1px solid var(--line); }
.submission-health-trend > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.submission-health-trend > header div { display: flex; flex-direction: column; gap: 3px; }
.submission-health-trend > header span { color: var(--muted); font-size: 8px; font-weight: 750; text-transform: uppercase; }
.submission-health-trend > header strong { font-size: 11px; }
.submission-health-trend > header small { color: var(--muted); font-size: 8px; }
.submission-health-bars { display: grid; height: 96px; grid-template-columns: repeat(12, minmax(3px, 1fr)); align-items: end; gap: 5px; margin-top: 15px; border-bottom: 1px solid var(--line); }
.submission-health-bars > span { display: flex; height: 100%; align-items: flex-end; }
.submission-health-bars i { width: 100%; min-height: 2px; background: var(--teal); border-radius: 2px 2px 0 0; }
.submission-health-no-trend { display: grid; height: 96px; place-items: center; margin-top: 15px; border-bottom: 1px solid var(--line); color: var(--muted); font-size: 9px; }
.submission-health-trend-labels { display: flex; justify-content: space-between; gap: 10px; margin-top: 7px; color: var(--muted); font-size: 8px; font-variant-numeric: tabular-nums; }
.submission-health-reasons > header, .submission-health-reasons > div { display: grid; grid-template-columns: minmax(0, 1fr) 72px; align-items: center; gap: 12px; padding: 0 18px; border-bottom: 1px solid var(--line); }
.submission-health-reasons > header { min-height: 34px; background: #f8faf9; color: var(--muted); font-size: 8px; font-weight: 750; text-transform: uppercase; }
.submission-health-reasons > header span:last-child { text-align: right; }
.submission-health-reasons > div { min-height: 52px; }
.submission-health-reasons > div:last-child { border-bottom: 0; }
.submission-health-reasons > div > span { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.submission-health-reasons strong { overflow: hidden; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.submission-health-reasons small { color: var(--muted); font-size: 8px; }
.submission-health-reasons b { text-align: right; font-size: 11px; font-variant-numeric: tabular-nums; }
.submission-health-spin { animation: submission-health-spin .8s linear infinite; }
@keyframes submission-health-spin { to { transform: rotate(360deg); } }
@media (max-width: 720px) {
  .submission-health-head { align-items: stretch; flex-direction: column; }
  .submission-health-actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)) 34px; }
  .submission-health-actions .app-select { width: 100%; min-width: 0; }
  .submission-health-summary { align-items: flex-start; flex-direction: column; gap: 8px; }
  .submission-health-categories { grid-template-columns: 1fr; }
  .submission-health-categories article:nth-child(odd) { border-right: 0; }
}
@media (max-width: 430px) {
  .submission-health-actions { grid-template-columns: minmax(0, 1fr) 34px; }
  .submission-health-actions label:nth-child(2) { grid-column: 1 / -1; grid-row: 2; }
  .submission-health-actions .app-icon-button { grid-column: 2; grid-row: 1; }
  .submission-health-categories article { min-height: 78px; padding-right: 13px; padding-left: 13px; }
  .submission-health-bars { gap: 3px; }
}
</style>
