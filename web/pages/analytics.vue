<script setup lang="ts">
import { AlertTriangle, BarChart3, RotateCcw } from '@lucide/vue'
import type { WidgetAnalyticsBreakdown, WidgetAnalyticsResponse } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatDate, formatWholeNumber } from '~/utils/format'
import { buildWidgetAnalyticsQuery, formatAnalyticsRate, type WidgetAnalyticsFilters } from '~/utils/widget-analytics'

type AnalyticsTab = 'projects' | 'rules' | 'pages'

const route = useRoute()
const router = useRouter()
const { $apiFetch } = useNuxtApp()
const supportedRanges = new Set<WidgetAnalyticsFilters['range']>(['7d', '30d', '90d'])
const requestedRange = typeof route.query.range === 'string' ? route.query.range : '30d'
const range = ref<WidgetAnalyticsFilters['range']>(supportedRanges.has(requestedRange as WidgetAnalyticsFilters['range'])
  ? requestedRange as WidgetAnalyticsFilters['range']
  : '30d')
const projectId = ref(typeof route.query.project_id === 'string' ? route.query.project_id : '')
const ruleId = ref(typeof route.query.rule_id === 'string' ? route.query.rule_id : '')
const pageFilter = ref(typeof route.query.page === 'string' ? route.query.page : '')
const activeTab = ref<AnalyticsTab>('projects')

const filters = computed<WidgetAnalyticsFilters>(() => ({
  range: range.value,
  projectId: projectId.value,
  ruleId: ruleId.value,
  page: pageFilter.value,
}))
const requestPath = computed(() => `/v1/analytics/widget?${buildWidgetAnalyticsQuery(filters.value)}`)
const { data, status, error, refresh } = await useAsyncData<WidgetAnalyticsResponse>(
  'widget-funnel-analytics',
  (_nuxtApp, { signal }) => $apiFetch<WidgetAnalyticsResponse>(requestPath.value, { signal }),
  { watch: [requestPath], lazy: true },
)

const metrics = computed(() => data.value ? [
  { label: 'Impressions', value: formatWholeNumber(data.value.summary.impressions), note: `${formatWholeNumber(data.value.summary.matched)} matched` },
  { label: 'Interaction rate', value: formatAnalyticsRate(data.value.summary.interaction_rate), note: `${formatWholeNumber(data.value.summary.interactions)} interactions` },
  { label: 'Completion rate', value: formatAnalyticsRate(data.value.summary.completion_rate), note: `${formatWholeNumber(data.value.summary.submissions)} submitted` },
  { label: 'Submit rate', value: formatAnalyticsRate(data.value.summary.submit_rate), note: `${formatWholeNumber(data.value.summary.dismissals)} dismissed` },
] : [])
const availableRules = computed(() => data.value?.options.rules.filter(option => !projectId.value || option.project_id === projectId.value) || [])
const activeRows = computed(() => data.value?.[activeTab.value] || [])
const activeMetadata = computed(() => data.value?.metadata[activeTab.value])
const hasActivity = computed(() => Boolean(data.value?.summary.matched))

function setRange(nextRange: WidgetAnalyticsFilters['range']) {
  range.value = nextRange
}

function changeProject(event: Event) {
  projectId.value = (event.target as HTMLSelectElement).value
  ruleId.value = ''
  pageFilter.value = ''
}

function changeRule(event: Event) {
  ruleId.value = (event.target as HTMLSelectElement).value
  pageFilter.value = ''
}

function resetFilters() {
  range.value = '30d'
  projectId.value = ''
  ruleId.value = ''
  pageFilter.value = ''
}

function rowName(row: WidgetAnalyticsBreakdown) {
  return row.rule_name || row.project_name || row.page_url || 'Unknown'
}

function rowContext(row: WidgetAnalyticsBreakdown) {
  if (row.rule_id) return row.deleted ? `${row.project_name} · deleted` : row.project_name
  return ''
}

function rowKey(tab: AnalyticsTab, row: WidgetAnalyticsBreakdown) {
  if (tab === 'projects') return row.project_id || row.project_name || 'unknown-project'
  if (tab === 'rules') return row.rule_id || `${row.project_id}:${row.rule_name}`
  return row.page_url || 'unknown-page'
}

watch(filters, (next) => {
  const query: Record<string, string> = { range: next.range }
  if (next.projectId) query.project_id = next.projectId
  if (next.ruleId) query.rule_id = next.ruleId
  if (next.page) query.page = next.page
  void router.replace({ query })
})

useHead({ title: 'Analytics · Fidibeki' })
</script>

<template>
  <div class="app-page analytics-page">
    <header class="app-page-head analytics-heading">
      <div><p class="app-eyebrow">Widget funnel</p><h1>Analytics</h1><span v-if="data">{{ formatDate(data.period.start_date) }} – {{ formatDate(data.period.end_date) }} · UTC</span></div>
    </header>

    <section class="analytics-toolbar" aria-label="Analytics filters">
      <div class="analytics-range" aria-label="Period">
        <button v-for="option in (['7d', '30d', '90d'] as const)" :key="option" type="button" :aria-pressed="range === option" @click="setRange(option)">{{ option }}</button>
      </div>
      <label><span>Project</span><select class="app-select" :value="projectId" @change="changeProject"><option value="">All projects</option><option v-for="option in data?.options.projects || []" :key="option.id" :value="option.id">{{ option.name }}</option></select></label>
      <label><span>Rule</span><select class="app-select" :value="ruleId" @change="changeRule"><option value="">All rules</option><option v-for="option in availableRules" :key="option.id" :value="option.id">{{ option.name }}{{ option.deleted ? ' (deleted)' : '' }}</option></select></label>
      <label class="analytics-page-filter"><span>Page</span><select v-model="pageFilter" class="app-select"><option value="">All pages</option><option v-for="option in data?.options.pages || []" :key="option.url" :value="option.url">{{ option.url }}</option></select></label>
      <button class="app-icon-button analytics-reset" type="button" title="Reset filters" :disabled="range === '30d' && !projectId && !ruleId && !pageFilter" @click="resetFilters"><RotateCcw :size="16" /><span class="sr-only">Reset filters</span></button>
    </section>

    <section v-if="status === 'pending' && !data" class="app-surface app-state" aria-live="polite"><div><span class="app-spinner" /><h2>Loading analytics</h2></div></section>
    <section v-else-if="error" class="app-surface app-state" role="alert"><div><AlertTriangle :size="24" /><h2>Analytics unavailable</h2><p>{{ apiErrorMessage(error) }}</p><button class="app-button" @click="refresh()">Try again</button></div></section>
    <template v-else-if="data">
      <section class="analytics-ledger" :aria-busy="status === 'pending'">
        <article v-for="metric in metrics" :key="metric.label"><span>{{ metric.label }}</span><b>{{ metric.value }}</b><small>{{ metric.note }}</small></article>
      </section>

      <section class="analytics-diagnostics" aria-label="Funnel diagnostics">
        <span><b>{{ formatWholeNumber(data.summary.matched) }}</b> matched</span>
        <span><b>{{ formatAnalyticsRate(data.summary.impression_rate) }}</b> impression rate</span>
        <span><b>{{ formatWholeNumber(data.summary.dismissals) }}</b> dismissed</span>
      </section>

      <section class="analytics-trend">
        <header><div><p class="app-eyebrow">Daily movement</p><h2>Funnel trend</h2></div><span>Peak impression volume and downstream completion</span></header>
        <WidgetFunnelTrend :points="data.trend" />
      </section>

      <section class="analytics-breakdown app-surface">
        <header>
          <div class="analytics-tabs" role="tablist" aria-label="Analytics breakdown">
            <button v-for="tab in (['projects', 'rules', 'pages'] as const)" :id="`analytics-tab-${tab}`" :key="tab" type="button" role="tab" :aria-selected="activeTab === tab" :aria-controls="`analytics-panel-${tab}`" @click="activeTab = tab">{{ tab[0]?.toUpperCase() + tab.slice(1) }}</button>
          </div>
          <span>{{ activeMetadata?.truncated ? `${activeMetadata.returned} of ${activeMetadata.total}` : activeRows.length }} {{ activeTab }}</span>
        </header>
        <div :id="`analytics-panel-${activeTab}`" class="analytics-table-scroll" role="tabpanel" :aria-labelledby="`analytics-tab-${activeTab}`">
          <table>
            <thead><tr><th>Source</th><th>Impressions</th><th>Interactions</th><th>Submissions</th><th>Interaction</th><th>Completion</th><th>Submit rate</th></tr></thead>
            <tbody>
              <tr v-for="row in activeRows" :key="rowKey(activeTab, row)" :data-row-key="rowKey(activeTab, row)">
                <th scope="row"><strong>{{ rowName(row) }}</strong><span v-if="rowContext(row)">{{ rowContext(row) }}</span></th>
                <td>{{ formatWholeNumber(row.impressions) }}</td><td>{{ formatWholeNumber(row.interactions) }}</td><td>{{ formatWholeNumber(row.submissions) }}</td><td>{{ formatAnalyticsRate(row.interaction_rate) }}</td><td>{{ formatAnalyticsRate(row.completion_rate) }}</td><td>{{ formatAnalyticsRate(row.submit_rate) }}</td>
              </tr>
              <tr v-if="!activeRows.length" class="analytics-empty-row"><td colspan="7"><BarChart3 :size="20" /><b>{{ hasActivity ? `No ${activeTab} match these filters` : 'No widget activity in this period' }}</b><span>Rates remain unavailable until their denominator has activity.</span></td></tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.analytics-page { max-width: 1540px; margin: 0 auto; }
.analytics-heading { margin-bottom: 16px; }
.analytics-toolbar { display: grid; grid-template-columns: auto minmax(150px, 0.8fr) minmax(150px, 0.8fr) minmax(220px, 1.4fr) 38px; align-items: end; gap: 10px; margin-bottom: 18px; padding: 12px; background: #eef2f0; border: 1px solid var(--line); border-radius: 6px; }
.analytics-toolbar label { display: flex; min-width: 0; flex-direction: column; gap: 5px; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.analytics-toolbar .app-select { width: 100%; min-width: 0; font-size: 11px; text-transform: none; }
.analytics-range { display: grid; height: 38px; grid-template-columns: repeat(3, 44px); padding: 3px; background: #fff; border: 1px solid var(--line-strong); border-radius: 5px; }
.analytics-range button { padding: 0; background: transparent; border: 0; border-radius: 3px; color: var(--muted); font-size: 10px; font-weight: 750; }
.analytics-range button[aria-pressed='true'] { background: #dfeae7; color: var(--teal); }
.analytics-reset { border-color: var(--line-strong); background: #fff; }
.analytics-reset:disabled { cursor: default; opacity: 0.45; }
.analytics-ledger { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border-top: 1px solid var(--line-strong); border-bottom: 1px solid var(--line-strong); }
.analytics-ledger article { display: grid; min-height: 118px; align-content: center; padding: 16px 20px; border-right: 1px solid var(--line); }
.analytics-ledger article:last-child { border-right: 0; }
.analytics-ledger span { color: var(--muted); font-size: 10px; font-weight: 750; text-transform: uppercase; }
.analytics-ledger b { margin: 7px 0 5px; font-size: 28px; font-variant-numeric: tabular-nums; line-height: 1; }
.analytics-ledger small { color: var(--muted); font-size: 10px; }
.analytics-diagnostics { display: flex; min-height: 42px; align-items: center; gap: 22px; padding: 0 2px; color: var(--muted); font-size: 10px; }
.analytics-diagnostics b { color: var(--ink-soft); font-variant-numeric: tabular-nums; }
.analytics-trend { padding: 24px 0 28px; border-bottom: 1px solid var(--line); }
.analytics-trend > header { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
.analytics-trend h2 { margin-top: 3px; font-size: 15px; }
.analytics-trend > header > span { color: var(--muted); font-size: 10px; }
.analytics-breakdown { margin-top: 24px; overflow: hidden; }
.analytics-breakdown > header { display: flex; min-height: 50px; align-items: center; justify-content: space-between; gap: 12px; padding: 0 12px; border-bottom: 1px solid var(--line); }
.analytics-breakdown > header > span { color: var(--muted); font-size: 9px; font-weight: 700; }
.analytics-tabs { display: flex; align-self: stretch; gap: 4px; }
.analytics-tabs button { position: relative; min-width: 76px; padding: 0 10px; background: transparent; border: 0; color: var(--muted); font-size: 11px; font-weight: 700; }
.analytics-tabs button[aria-selected='true'] { color: var(--ink); }
.analytics-tabs button[aria-selected='true']::after { position: absolute; right: 8px; bottom: -1px; left: 8px; height: 2px; background: var(--teal); content: ''; }
.analytics-table-scroll { max-width: 100%; overflow-x: auto; }
table { width: 100%; min-width: 820px; border-collapse: collapse; table-layout: fixed; }
th, td { height: 51px; padding: 8px 12px; border-bottom: 1px solid var(--line); font-size: 10px; text-align: right; font-variant-numeric: tabular-nums; }
thead th { height: 38px; background: #f8faf9; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
thead th:first-child, tbody th { width: 30%; text-align: left; }
tbody th { overflow: hidden; }
tbody th strong, tbody th span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
tbody th strong { color: var(--ink-soft); font-size: 10px; }
tbody th span { margin-top: 2px; color: var(--muted); font-size: 9px; font-weight: 500; }
tbody tr:last-child th, tbody tr:last-child td { border-bottom: 0; }
.analytics-empty-row td { height: 180px; color: var(--muted); text-align: center; }
.analytics-empty-row svg { margin: 0 auto 8px; }
.analytics-empty-row b, .analytics-empty-row span { display: block; }
.analytics-empty-row b { color: var(--ink-soft); font-size: 12px; }
.analytics-empty-row span { margin-top: 4px; font-size: 10px; }
@media (max-width: 980px) { .analytics-toolbar { grid-template-columns: auto repeat(2, minmax(150px, 1fr)) 38px; } .analytics-page-filter { grid-column: 2 / 4; } }
@media (max-width: 700px) {
  .analytics-toolbar { grid-template-columns: minmax(0, 1fr) 38px; }
  .analytics-range { width: max-content; grid-column: 1; }
  .analytics-toolbar label, .analytics-page-filter { grid-column: 1 / -1; }
  .analytics-reset { grid-column: 2; grid-row: 1; }
  .analytics-ledger { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .analytics-ledger article { min-height: 102px; padding: 13px; border-bottom: 1px solid var(--line); }
  .analytics-ledger article:nth-child(2) { border-right: 0; }
  .analytics-ledger article:nth-last-child(-n + 2) { border-bottom: 0; }
  .analytics-ledger b { font-size: 23px; }
  .analytics-diagnostics { align-items: flex-start; flex-direction: column; gap: 3px; padding: 10px 2px; }
  .analytics-trend > header { align-items: flex-start; flex-direction: column; }
  .analytics-breakdown > header { align-items: flex-start; flex-direction: column; padding: 0 8px; }
  .analytics-tabs { width: 100%; height: 48px; }
  .analytics-tabs button { flex: 1; min-width: 0; }
  .analytics-breakdown > header > span { padding: 0 8px 10px; }
}
</style>
