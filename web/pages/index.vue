<script setup lang="ts">
import { AlertTriangle, ArrowDownRight, ArrowUpRight, ChevronRight, Inbox } from '@lucide/vue'
import type { DashboardSummary, FeedbackPage, Project } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatWholeNumber } from '~/utils/format'

const { $apiFetch } = useNuxtApp()
const { can, member } = useAuth()
const range = ref('30d')

const { data, status, error, refresh } = await useAsyncData(
  'production-dashboard',
  async () => {
    const dashboardPromise = $apiFetch<{ dashboard: DashboardSummary }>(`/v1/dashboard?range=${range.value}`)
    const feedbackPromise = $apiFetch<FeedbackPage>('/v1/feedbacks?status=open&limit=6')
    const projectsPromise = can('project:read')
      ? $apiFetch<{ projects: Project[] }>('/v1/projects')
      : Promise.resolve({ projects: [] as Project[] })
    const [dashboard, feedback, projects] = await Promise.all([
      dashboardPromise,
      feedbackPromise,
      projectsPromise,
    ])
    return {
      dashboard: dashboard.dashboard,
      feedbacks: feedback.feedbacks,
      projects: projects.projects,
    }
  },
  { watch: [range] },
)

const chartPoints = computed(() => {
  const points = data.value?.dashboard.trend || []
  if (points.length === 0) return ''
  const max = Math.max(1, ...points.map((point) => point.feedback_count))
  return points
    .map((point, index) => {
      const x = points.length === 1 ? 0 : (index / (points.length - 1)) * 600
      const y = 112 - (point.feedback_count / max) * 96
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
})

const firstName = computed(() => member.value?.name?.split(' ')[0] || member.value?.email.split('@')[0] || 'there')

function feedbackCopy(item: FeedbackPage['feedbacks'][number]) {
  return item.text_cleaned || item.text || 'Feedback without written detail'
}

function signed(value?: number) {
  if (value === undefined) return 'No comparison'
  return `${value > 0 ? '+' : ''}${value.toFixed(1)}% vs previous`
}

function formatAge(value: string) {
  const diff = Math.max(0, Date.now() - new Date(value).getTime())
  const hours = Math.floor(diff / 3_600_000)
  if (hours < 1) return 'Now'
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}

useHead({ title: 'Overview · Fidibeki' })
</script>

<template>
  <div class="app-page dashboard-page">
    <header class="app-page-head">
      <div>
        <p class="app-eyebrow">Operations</p>
        <h1>Good afternoon, {{ firstName }}</h1>
        <span>{{ data?.dashboard.attention_count || 0 }} feedback items need attention.</span>
      </div>
      <select v-model="range" class="app-select" aria-label="Dashboard date range">
        <option value="7d">Last 7 days</option>
        <option value="30d">Last 30 days</option>
        <option value="90d">Last 90 days</option>
      </select>
    </header>

    <section v-if="status === 'pending'" class="app-surface app-state" aria-live="polite">
      <div><span class="app-spinner" /><h2>Loading workspace signals</h2></div>
    </section>

    <section v-else-if="error" class="app-surface app-state" role="alert">
      <div>
        <AlertTriangle :size="24" />
        <h2>Dashboard unavailable</h2>
        <p>{{ apiErrorMessage(error, 'The workspace data could not be loaded.') }}</p>
        <button class="app-button" type="button" @click="refresh()">Try again</button>
      </div>
    </section>

    <template v-else-if="data">
      <section class="dashboard-ledger" aria-label="Workspace metrics">
        <article>
          <span>Feedback received</span>
          <strong>{{ formatWholeNumber(data.dashboard.feedback_count) }}</strong>
          <small :data-positive="(data.dashboard.feedback_delta_percent || 0) >= 0">
            <component :is="(data.dashboard.feedback_delta_percent || 0) >= 0 ? ArrowUpRight : ArrowDownRight" :size="13" />
            {{ signed(data.dashboard.feedback_delta_percent) }}
          </small>
        </article>
        <article>
          <span>Open issues</span>
          <strong>{{ formatWholeNumber(data.dashboard.open_issue_count) }}</strong>
          <small>Active product work</small>
        </article>
        <article>
          <span>Satisfaction</span>
          <strong>{{ data.dashboard.satisfaction?.toFixed(1) || '—' }}</strong>
          <small :data-positive="(data.dashboard.satisfaction_delta || 0) >= 0">
            <component :is="(data.dashboard.satisfaction_delta || 0) >= 0 ? ArrowUpRight : ArrowDownRight" :size="13" />
            {{ data.dashboard.satisfaction_delta === undefined ? 'No comparison' : `${data.dashboard.satisfaction_delta > 0 ? '+' : ''}${data.dashboard.satisfaction_delta.toFixed(1)} vs previous` }}
          </small>
        </article>
        <article class="dashboard-chart-cell">
          <span>Feedback volume</span>
          <svg viewBox="0 0 600 120" role="img" :aria-label="`Feedback volume over ${data.dashboard.range_days} days`" preserveAspectRatio="none">
            <line x1="0" y1="112" x2="600" y2="112" />
            <polyline v-if="chartPoints" :points="chartPoints" />
          </svg>
        </article>
      </section>

      <section class="dashboard-worklist app-surface">
        <header>
          <div><h2>Work requiring attention</h2><p>Open feedback ordered by recency</p></div>
          <NuxtLink to="/feedback?status=open">View queue <ChevronRight :size="15" /></NuxtLink>
        </header>
        <div v-if="data.feedbacks.length === 0" class="dashboard-empty">
          <Inbox :size="22" /><p>No open feedback is waiting.</p>
        </div>
        <NuxtLink v-for="item in data.feedbacks" :key="item.id" :to="`/feedback/${item.id}`" class="dashboard-work-row">
          <span class="dashboard-priority" :data-tone="item.moderation_flags.length ? 'critical' : item.ai_status" />
          <RatingMark :rating="item.rating" compact />
          <span class="dashboard-work-copy"><b>{{ feedbackCopy(item) }}</b><small>{{ item.public_id }} · {{ item.project_name }} · {{ item.page_url }}</small></span>
          <AttachmentIcons :attachments="item.attachment_kinds" />
          <span v-if="item.issue_public_id" class="issue-label">{{ item.issue_public_id }}</span><span v-else class="muted">Unlinked</span>
          <time>{{ formatAge(item.created_at) }}</time>
          <ChevronRight :size="16" />
        </NuxtLink>
      </section>

      <section v-if="can('project:read')" class="dashboard-projects app-surface">
        <header><div><h2>Projects</h2><p>Collection endpoints in this workspace</p></div><NuxtLink to="/projects">Manage projects</NuxtLink></header>
        <div v-if="data.projects.length === 0" class="dashboard-empty"><p>No projects have been created.</p></div>
        <NuxtLink v-for="project in data.projects" :key="project.id" :to="`/projects/${project.id}`">
          <span class="dashboard-project-mark">{{ project.name.slice(0, 1).toUpperCase() }}</span>
          <p><b>{{ project.name }}</b><span>{{ project.allowed_domains.join(', ') || 'No allowed domains' }}</span></p>
          <code>{{ project.public_key }}</code>
          <ChevronRight :size="16" />
        </NuxtLink>
      </section>
    </template>
  </div>
</template>

<style scoped>
.dashboard-ledger {
  display: grid;
  grid-template-columns: repeat(3, minmax(150px, 0.65fr)) minmax(260px, 1.45fr);
  margin-bottom: 18px;
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 6px;
}

.dashboard-ledger article {
  display: flex;
  min-width: 0;
  min-height: 124px;
  flex-direction: column;
  justify-content: center;
  padding: 18px 20px;
  border-right: 1px solid var(--line);
}

.dashboard-ledger article:last-child { border-right: 0; }
.dashboard-ledger span { color: var(--muted); font-size: 11px; font-weight: 700; }
.dashboard-ledger strong { margin: 6px 0; font-size: 28px; font-variant-numeric: tabular-nums; }
.dashboard-ledger small { display: flex; align-items: center; gap: 3px; color: var(--muted); font-size: 10px; }
.dashboard-ledger small[data-positive='true'] { color: var(--green); }
.dashboard-chart-cell { padding-bottom: 10px !important; }
.dashboard-chart-cell svg { width: 100%; height: 70px; margin-top: 5px; overflow: visible; }
.dashboard-chart-cell line { stroke: var(--line); stroke-width: 1; }
.dashboard-chart-cell polyline { fill: none; stroke: var(--teal); stroke-width: 3; vector-effect: non-scaling-stroke; }

.dashboard-worklist,
.dashboard-projects { margin-bottom: 18px; overflow: hidden; }
.dashboard-worklist > header,
.dashboard-projects > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; gap: 16px; padding: 12px 18px; border-bottom: 1px solid var(--line); }
.dashboard-worklist h2,
.dashboard-projects h2 { margin: 0; font-size: 14px; }
.dashboard-worklist header p,
.dashboard-projects header p { margin: 4px 0 0; color: var(--muted); font-size: 10px; }
.dashboard-worklist header a,
.dashboard-projects header a { display: flex; align-items: center; gap: 4px; color: var(--teal); font-size: 11px; font-weight: 750; text-decoration: none; }
.dashboard-work-row { display: grid; min-height: 58px; grid-template-columns: 5px 38px minmax(220px, 1fr) 94px 110px 44px 18px; align-items: center; gap: 10px; padding: 0 14px 0 0; color: var(--ink-soft); border-bottom: 1px solid var(--line); text-decoration: none; }
.dashboard-work-row:last-child { border-bottom: 0; }
.dashboard-work-row:hover { background: #f7faf9; }
.dashboard-priority { align-self: stretch; background: var(--line); }
.dashboard-priority[data-tone='critical'],
.dashboard-priority[data-tone='failed'] { background: var(--red); }
.dashboard-priority[data-tone='processing'],
.dashboard-priority[data-tone='pending'] { background: var(--amber); }
.dashboard-work-copy { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.dashboard-work-copy b,
.dashboard-work-copy small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.dashboard-work-copy b { font-size: 12px; }
.dashboard-work-copy small { color: var(--muted); font-size: 10px; }
.dashboard-work-row time { color: var(--muted); font-size: 10px; font-variant-numeric: tabular-nums; }
.dashboard-empty { display: flex; min-height: 100px; align-items: center; justify-content: center; gap: 10px; color: var(--muted); font-size: 12px; }

.dashboard-projects > a { display: grid; min-height: 62px; grid-template-columns: 34px minmax(180px, 1fr) minmax(190px, auto) 18px; align-items: center; gap: 12px; padding: 0 18px; color: var(--ink-soft); border-bottom: 1px solid var(--line); text-decoration: none; }
.dashboard-projects > a:last-child { border-bottom: 0; }
.dashboard-projects > a:hover { background: #f7faf9; }
.dashboard-project-mark { display: grid; width: 30px; height: 30px; place-items: center; background: var(--teal-soft); color: var(--teal); border-radius: 5px; font-size: 11px; font-weight: 800; }
.dashboard-projects a p { display: flex; min-width: 0; flex-direction: column; gap: 3px; margin: 0; }
.dashboard-projects a p b { font-size: 12px; }
.dashboard-projects a p span { overflow: hidden; color: var(--muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.dashboard-projects code { overflow: hidden; color: var(--muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }

@media (max-width: 1180px) {
  .dashboard-ledger { grid-template-columns: repeat(3, 1fr); }
  .dashboard-chart-cell { grid-column: 1 / -1; border-top: 1px solid var(--line); }
  .dashboard-ledger article:nth-child(3) { border-right: 0; }
  .dashboard-work-row { grid-template-columns: 5px 38px minmax(180px, 1fr) 80px 42px 18px; }
  .dashboard-work-row :deep(.attachment-icons) { display: none; }
  .dashboard-work-row .issue-label,
  .dashboard-work-row > .muted { display: none; }
}

@media (max-width: 700px) {
  .dashboard-ledger { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .dashboard-ledger article { min-height: 96px; padding: 12px; }
  .dashboard-ledger strong { font-size: 21px; }
  .dashboard-ledger small { display: none; }
  .dashboard-work-row { grid-template-columns: 4px 34px minmax(0, 1fr) 35px 16px; gap: 7px; padding-right: 10px; }
  .dashboard-work-row time { text-align: right; }
  .dashboard-projects > a { grid-template-columns: 32px minmax(0, 1fr) 18px; padding: 0 12px; }
  .dashboard-projects code { display: none; }
}
</style>
