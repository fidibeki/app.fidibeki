<script setup lang="ts">
import { AlertTriangle, ChevronRight, CircleDot, LayoutList, Search } from '@lucide/vue'
import type { Issue, IssueStatus, Project } from '~/types/api'
import { issueStatusKey, triagePriorityKey } from '~/utils/issue-activity'

const { $apiFetch } = useNuxtApp()
const { can, canWrite } = useAuth()
const { t } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { integer, list, plural } = useRegionalFormat()
const issueStatuses: IssueStatus[] = ['open', 'in_progress', 'needs_info', 'resolved', 'closed']
const search = ref('')
const statusFilter = ref('')
const projectFilter = ref('')
const mutationError = ref('')
const updatingID = ref('')

const { data, status, error, refresh } = await useAsyncData('issues-list', async () => {
  const [issues, projects] = await Promise.all([
    $apiFetch<{ issues: Issue[] }>('/v1/issues'),
    can('project:read')
      ? $apiFetch<{ projects: Project[] }>('/v1/projects')
      : Promise.resolve({ projects: [] as Project[] }),
  ])
  return { issues: issues.issues, projects: projects.projects }
})

const projectNames = computed(() => new Map((data.value?.projects || []).map((project) => [project.id, project.name])))
const filteredIssues = computed(() => {
  const term = search.value.trim().toLowerCase()
  return (data.value?.issues || []).filter((issue) => {
    if (statusFilter.value && issue.status !== statusFilter.value) return false
    if (projectFilter.value && issue.project_id !== projectFilter.value) return false
    return !term || issue.public_id.toLowerCase().includes(term) || issue.title.toLowerCase().includes(term)
  })
})

async function changeStatus(issue: Issue, nextStatus: IssueStatus) {
  if (!canWrite('issue:update') || issue.status === nextStatus || updatingID.value) return
  updatingID.value = issue.id
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/issues/${issue.id}`, { method: 'PATCH', body: { status: nextStatus } })
    await refresh()
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, 'issues.statusUpdateFailed')
  } finally {
    updatingID.value = ''
  }
}

function formatAge(value: string) {
  const timestamp = new Date(value).getTime()
  if (Number.isNaN(timestamp)) return '—'
  const days = Math.floor(Math.max(0, Date.now() - timestamp) / 86_400_000)
  return days < 1 ? t('issues.ageToday') : plural('issues.ageDays', days)
}

function statusOptions(issue: Issue) {
  return issueStatuses.filter(value => value !== 'needs_info' || issue.status === 'needs_info')
}

function statusLabel(status: IssueStatus) {
  const key = issueStatusKey(status)
  return key ? t(key) : status
}

function priorityLabel(issue: Issue) {
  const key = triagePriorityKey(issue.priority)
  return key ? t(key) : issue.priority || ''
}

useHead(() => ({ title: t('issues.pageTitle') }))
</script>

<template>
  <div class="app-page issues-page">
    <header class="app-page-head"><div><p class="app-eyebrow">{{ t('issues.eyebrow') }}</p><h1>{{ t('issues.title') }}</h1><span>{{ t('issues.subtitle') }}</span></div></header>
    <section class="issues-toolbar app-surface">
      <label><Search :size="15" /><input v-model="search" :placeholder="t('issues.search')" :aria-label="t('issues.search')" /></label>
      <select v-model="statusFilter" class="app-select" :aria-label="t('issues.statusFilter')"><option value="">{{ t('issues.allStatuses') }}</option><option v-for="value in issueStatuses" :key="value" :value="value">{{ statusLabel(value) }}</option></select>
      <select v-model="projectFilter" class="app-select" :aria-label="t('issues.projectFilter')"><option value="">{{ t('issues.allProjects') }}</option><option v-for="project in data?.projects" :key="project.id" :value="project.id">{{ project.name }}</option></select>
      <span>{{ plural('issues.count', filteredIssues.length) }}</span>
    </section>
    <p v-if="mutationError" class="issues-error" role="alert"><AlertTriangle :size="15" />{{ mutationError }}</p>
    <section v-if="status === 'pending'" class="app-surface app-state" aria-live="polite"><div><span class="app-spinner" /><h2>{{ t('issues.loading') }}</h2></div></section>
    <section v-else-if="error" class="app-surface app-state" role="alert"><div><AlertTriangle :size="24" /><h2>{{ t('issues.unavailable') }}</h2><p>{{ apiErrorMessage(error, 'issues.loadFailed') }}</p><button class="app-button" type="button" @click="refresh()">{{ t('issues.tryAgain') }}</button></div></section>
    <section v-else-if="filteredIssues.length === 0" class="app-surface app-state"><div><LayoutList :size="25" /><h2>{{ t('issues.emptyTitle') }}</h2><p>{{ t('issues.emptyDescription') }}</p></div></section>
    <section v-else class="issues-table app-surface">
      <header><span>{{ t('issues.columns.issue') }}</span><span>{{ t('issues.columns.project') }}</span><span>{{ t('issues.columns.triage') }}</span><span>{{ t('issues.columns.feedback') }}</span><span>{{ t('issues.columns.status') }}</span><span>{{ t('issues.columns.age') }}</span><span /></header>
      <div v-for="issue in filteredIssues" :key="issue.id" class="issues-row">
        <NuxtLink class="issues-row-link" :to="`/issues/${issue.id}`" :aria-label="t('issues.open', { publicId: issue.public_id, title: issue.title })" />
        <span class="issues-title"><CircleDot :size="15" /><span><b>{{ issue.public_id }}</b><small>{{ issue.title }}</small></span></span>
        <span class="issues-project">{{ projectNames.get(issue.project_id) || t('issues.projectFallback') }}</span>
        <span class="issues-triage">
          <span v-if="issue.priority" class="issue-priority-mark" :data-priority="issue.priority" :title="t('triage.priorityOption', { priority: priorityLabel(issue) })">{{ priorityLabel(issue) }}</span>
          <span v-if="issue.assignee" class="issue-assignee-mark" :title="issue.assignee.name || issue.assignee.email"><MemberAvatar :member-id="issue.assignee.id" :name="issue.assignee.name" :email="issue.assignee.email" :avatar-version="issue.assignee.avatar_version" /></span>
          <span v-if="issue.tags?.length" class="issue-tag-marks" :title="t('issues.tagList', { tags: list(issue.tags.map(tag => tag.name)) })"><i v-for="tag in issue.tags.slice(0, 3)" :key="tag.id" :style="{ backgroundColor: tag.color }" /></span>
          <span v-if="!issue.priority && !issue.assignee && !issue.tags?.length" class="issue-untriaged">{{ t('issues.untriaged') }}</span>
        </span>
        <span class="issues-feedback" :title="plural('issues.feedbackCount', issue.feedback_count)">{{ integer(issue.feedback_count) }}</span>
        <select :value="issue.status" :disabled="updatingID === issue.id || !canWrite('issue:update')" :aria-label="t('issues.statusFor', { publicId: issue.public_id })" @change="changeStatus(issue, ($event.target as HTMLSelectElement).value as IssueStatus)"><option v-for="value in statusOptions(issue)" :key="value" :value="value">{{ statusLabel(value) }}</option></select>
        <time :datetime="issue.created_at">{{ formatAge(issue.created_at) }}</time>
        <ChevronRight :size="16" />
      </div>
    </section>
  </div>
</template>

<style scoped>
.issues-toolbar { display: flex; min-height: 58px; align-items: center; gap: 8px; margin-bottom: 14px; padding: 9px 12px; }
.issues-toolbar label { display: flex; width: min(280px, 32vw); height: 38px; align-items: center; gap: 7px; padding: 0 9px; border: 1px solid var(--line-strong); border-radius: 5px; color: var(--muted); }
.issues-toolbar label input { width: 100%; border: 0; outline: 0; }
.issues-toolbar > span { margin-left: auto; color: var(--muted); font-size: 10px; }
.issues-error { display: flex; align-items: center; gap: 7px; margin: 0 0 12px; padding: 10px 12px; background: var(--red-soft); color: var(--red); border: 1px solid var(--red-line); border-radius: 5px; font-size: 11px; }
.issues-table { overflow: hidden; }
.issues-table > header,
.issues-row { display: grid; min-height: 58px; grid-template-columns: minmax(240px, 1.4fr) minmax(110px, .65fr) 120px 70px 130px 70px 18px; position: relative; align-items: center; gap: 14px; padding: 0 16px; border-bottom: 1px solid var(--line); }
.issues-table > header { min-height: 38px; background: var(--surface-subtle); color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.issues-row { color: var(--ink-soft); font-size: 10px; }
.issues-row:last-child { border-bottom: 0; }
.issues-row:hover { background: var(--surface-subtle); }
.issues-row-link { position: absolute; z-index: 1; inset: 0; }
.issues-row-link:focus-visible { outline: 2px solid var(--teal); outline-offset: -2px; }
.issues-row > :not(.issues-row-link) { position: relative; z-index: 2; pointer-events: none; }
.issues-row select { pointer-events: auto; }
.issues-title { display: flex; min-width: 0; align-items: center; gap: 10px; }
.issues-title > svg { flex: 0 0 auto; color: var(--teal); }
.issues-title > span { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.issues-title b,
.issues-title small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.issues-title b { font-size: 11px; }
.issues-title small { color: var(--muted); font-size: 9px; }
.issues-triage { display: flex; min-width: 0; align-items: center; gap: 5px; overflow: hidden; }
.issue-priority-mark { flex: 0 0 auto; padding: 3px 5px; color: var(--ink-soft); background: var(--teal-soft); border-radius: 3px; font-size: 8px; font-weight: 800; text-transform: uppercase; }
.issue-priority-mark[data-priority='high'] { color: var(--amber); background: var(--amber-soft); }
.issue-priority-mark[data-priority='urgent'] { color: var(--red); background: var(--red-soft); }
.issue-assignee-mark { display: block; width: 23px; height: 23px; flex: 0 0 23px; overflow: hidden; border: 1px solid var(--teal-line); border-radius: 50%; font-size: 8px; }
.issue-tag-marks { display: flex; min-width: 8px; gap: 2px; }
.issue-tag-marks i { width: 6px; height: 18px; flex: 0 0 6px; border-radius: 2px; }
.issue-untriaged { overflow: hidden; color: var(--muted); font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }
.issues-table select { width: 100%; height: 32px; padding: 0 6px; background: var(--surface); border: 1px solid var(--line); border-radius: 4px; font-size: 9px; }
.issues-table time { color: var(--muted); font-size: 9px; }
@media (max-width: 850px) {
  .issues-table > header { display: none; }
  .issues-row { grid-template-columns: minmax(0, 1fr) 110px 50px 18px; gap: 8px; padding: 8px 12px; }
  .issues-project,
  .issues-triage,
  .issues-feedback { display: none; }
  .issues-toolbar { align-items: stretch; flex-wrap: wrap; }
  .issues-toolbar label { width: 100%; }
  .issues-toolbar select { flex: 1; }
}
</style>
