<script setup lang="ts">
import { AlertTriangle, ArrowLeft, GitMerge, RotateCcw } from '@lucide/vue'
import type { FeedbackPage, Issue, IssueStatus, IssueUpdate } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatDateTime } from '~/utils/format'

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { can, canWrite } = useAuth()
const issueID = computed(() => String(route.params.id || ''))
const nextStatus = ref<IssueStatus>('open')
const comment = ref('')
const mergeTarget = ref('')
const pending = ref(false)
const mutationError = ref('')

const { data, status, error, refresh } = await useAsyncData(
  () => `issue-detail:${issueID.value}`,
  async () => {
    const issueResponse = await $apiFetch<{ issue: Issue; updates: IssueUpdate[] }>(`/v1/issues/${issueID.value}`)
    const [allIssues, feedbacks] = await Promise.all([
      can('issue:read') ? $apiFetch<{ issues: Issue[] }>('/v1/issues') : Promise.resolve({ issues: [] }),
      can('feedback:read') ? $apiFetch<FeedbackPage>(`/v1/feedbacks?q=${encodeURIComponent(issueResponse.issue.public_id)}&limit=20`) : Promise.resolve({ feedbacks: [], pagination: { has_more: false, limit: 20 } }),
    ])
    return { ...issueResponse, allIssues: allIssues.issues, feedbacks: feedbacks.feedbacks }
  },
  { watch: [issueID] },
)

watch(() => data.value?.issue.status, (value) => { if (value) nextStatus.value = value }, { immediate: true })
const mergeCandidates = computed(() => (data.value?.allIssues || []).filter((candidate) => candidate.id !== issueID.value && candidate.project_id === data.value?.issue.project_id && !candidate.merged_into_id))

async function updateIssue() {
  if (!canWrite('issue:update') || !data.value || pending.value || nextStatus.value === data.value.issue.status) return
  pending.value = true
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/issues/${issueID.value}`, { method: 'PATCH', body: { status: nextStatus.value, comment: comment.value.trim() || null } })
    comment.value = ''
    await refresh()
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, 'Issue could not be updated.')
  } finally { pending.value = false }
}

async function mergeIssue() {
  if (!canWrite('issue:merge') || !mergeTarget.value || pending.value) return
  pending.value = true
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/issues/${issueID.value}/merge`, { method: 'POST', body: { target_issue_id: mergeTarget.value } })
    mergeTarget.value = ''
    await refresh()
  } catch (mergeError) {
    mutationError.value = apiErrorMessage(mergeError, 'Issues could not be merged.')
  } finally { pending.value = false }
}

async function undoMerge() {
  if (!canWrite('issue:merge') || pending.value) return
  pending.value = true
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/issues/${issueID.value}/merge`, { method: 'DELETE' })
    await refresh()
  } catch (mergeError) {
    mutationError.value = apiErrorMessage(mergeError, 'The merge could not be undone.')
  } finally { pending.value = false }
}

function formatDate(value: string) {
  return formatDateTime(value)
}

useHead(() => ({ title: `${data.value?.issue.public_id || 'Issue'} · Fidibeki` }))
</script>

<template>
  <div class="app-page issue-detail-page">
    <header class="issue-detail-back"><NuxtLink class="app-button" to="/issues"><ArrowLeft :size="15" /> Back to issues</NuxtLink></header>
    <section v-if="status === 'pending'" class="app-surface app-state"><div><span class="app-spinner" /><h2>Loading issue</h2></div></section>
    <section v-else-if="error" class="app-surface app-state"><div><AlertTriangle :size="24" /><h2>Issue unavailable</h2><p>{{ apiErrorMessage(error) }}</p></div></section>
    <template v-else-if="data">
      <header class="issue-detail-heading"><div><p class="app-eyebrow">{{ data.issue.public_id }}</p><h1>{{ data.issue.title }}</h1><span>{{ data.issue.feedback_count }} feedback records · created {{ formatDate(data.issue.created_at) }}</span></div><span class="status-pill" :class="`status-${data.issue.status}`">{{ data.issue.status.replace('_', ' ') }}</span></header>
      <p v-if="mutationError" class="issues-error" role="alert"><AlertTriangle :size="15" /> {{ mutationError }}</p>
      <div class="issue-detail-grid">
        <main>
          <section class="app-surface issue-update-panel"><header><h2>Update status</h2><p>Status changes are recorded in the issue history.</p></header><div><select v-model="nextStatus" class="app-select" :disabled="!canWrite('issue:update')"><option value="open">Open</option><option value="in_progress">In progress</option><option value="resolved">Resolved</option><option value="closed">Closed</option></select><textarea v-model="comment" placeholder="Internal context for this change (optional)" maxlength="2000" :disabled="!canWrite('issue:update')" /><button class="app-button app-button--primary" :disabled="pending || !canWrite('issue:update') || nextStatus === data.issue.status" @click="updateIssue">Update issue</button></div></section>
          <section class="app-surface issue-feedback"><header><h2>Linked feedback</h2><span>{{ data.feedbacks.length }} records</span></header><div v-if="!data.feedbacks.length" class="issue-empty">No readable feedback is linked.</div><NuxtLink v-for="feedback in data.feedbacks" :key="feedback.id" :to="`/feedback/${feedback.id}`"><RatingMark v-if="feedback.rating" :rating="feedback.rating" compact /><span v-else>—</span><p><b>{{ feedback.text_cleaned || feedback.text || 'No written feedback' }}</b><small>{{ feedback.public_id }} · {{ feedback.page_url }}</small></p><span class="status-pill" :class="`status-${feedback.ai_status}`">{{ feedback.ai_status }}</span></NuxtLink></section>
          <section class="app-surface issue-history"><header><h2>History</h2></header><ol><li v-for="update in [...data.updates].reverse()" :key="update.id"><span /><div><p><b>{{ update.old_status || 'Created' }}</b> → <b>{{ update.new_status }}</b></p><blockquote v-if="update.comment">{{ update.comment }}</blockquote><time>{{ formatDate(update.created_at) }}</time></div></li><li class="issue-created"><span /><div><p>Issue created from feedback</p><time>{{ formatDate(data.issue.created_at) }}</time></div></li></ol></section>
        </main>
        <aside>
          <section class="app-surface issue-facts"><h2>Issue facts</h2><dl><div><dt>Public ID</dt><dd>{{ data.issue.public_id }}</dd></div><div><dt>Reporter</dt><dd>{{ data.issue.reporter_email || 'Anonymous' }}</dd></div><div><dt>Reporter updates</dt><dd>{{ data.issue.notify_reporter ? 'Enabled' : 'Disabled' }}</dd></div><div><dt>Resolved</dt><dd>{{ data.issue.resolved_at ? formatDate(data.issue.resolved_at) : 'Not resolved' }}</dd></div></dl></section>
          <section class="app-surface issue-merge"><h2>Merge issue</h2><template v-if="data.issue.merged_into_id"><p>This issue is merged into another issue.</p><button class="app-button" :disabled="pending || !canWrite('issue:merge')" @click="undoMerge"><RotateCcw :size="14" /> Undo merge</button></template><template v-else><p>Move this issue under another issue in the same project.</p><select v-model="mergeTarget" class="app-select" :disabled="!canWrite('issue:merge')"><option value="">Choose target issue</option><option v-for="candidate in mergeCandidates" :key="candidate.id" :value="candidate.id">{{ candidate.public_id }} · {{ candidate.title }}</option></select><button class="app-button" :disabled="pending || !mergeTarget || !canWrite('issue:merge')" @click="mergeIssue"><GitMerge :size="14" /> Merge</button></template></section>
        </aside>
      </div>
    </template>
  </div>
</template>

<style scoped>
.issue-detail-back { margin-bottom: 14px; }
.issue-detail-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.issue-detail-heading h1 { margin: 5px 0 7px; font-size: 22px; }
.issue-detail-heading > div > span { color: var(--muted); font-size: 10px; }
.issues-error { display: flex; align-items: center; gap: 7px; margin: 0 0 12px; padding: 10px 12px; background: var(--red-soft); color: var(--red); border: 1px solid #efc2c6; border-radius: 5px; font-size: 11px; }
.issue-detail-grid { display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 16px; }
.issue-detail-grid main,
.issue-detail-grid aside { display: flex; min-width: 0; flex-direction: column; gap: 14px; }
.issue-detail-grid section > header { display: flex; min-height: 60px; align-items: center; justify-content: space-between; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.issue-detail-grid h2 { margin: 0; font-size: 13px; }
.issue-detail-grid header p { margin: 4px 0 0; color: var(--muted); font-size: 9px; }
.issue-update-panel > div { display: grid; grid-template-columns: 150px minmax(0, 1fr) auto; align-items: start; gap: 9px; padding: 14px; }
.issue-update-panel textarea { min-height: 72px; padding: 9px; border: 1px solid var(--line-strong); border-radius: 5px; resize: vertical; font: inherit; font-size: 11px; }
.issue-feedback > header > span { color: var(--muted); font-size: 9px; }
.issue-feedback > a { display: grid; min-height: 62px; grid-template-columns: 38px minmax(0, 1fr) auto; align-items: center; gap: 11px; padding: 8px 14px; color: var(--ink-soft); border-bottom: 1px solid var(--line); text-decoration: none; }
.issue-feedback > a:last-child { border-bottom: 0; }
.issue-feedback a p { display: flex; min-width: 0; flex-direction: column; gap: 4px; margin: 0; }
.issue-feedback a b,
.issue-feedback a small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.issue-feedback a b { font-size: 11px; }
.issue-feedback a small { color: var(--muted); font-size: 9px; }
.issue-empty { padding: 30px; color: var(--muted); font-size: 11px; text-align: center; }
.issue-history ol { margin: 0; padding: 14px 18px; list-style: none; }
.issue-history li { display: grid; min-height: 66px; grid-template-columns: 14px minmax(0, 1fr); gap: 10px; }
.issue-history li > span { position: relative; width: 8px; height: 8px; margin-top: 4px; background: var(--teal); border: 2px solid #fff; border-radius: 50%; box-shadow: 0 0 0 1px var(--teal); }
.issue-history li > span::after { position: absolute; top: 10px; left: 2px; width: 1px; height: 51px; background: var(--line); content: ''; }
.issue-history li:last-child > span::after { display: none; }
.issue-history p,
.issue-history blockquote { margin: 0 0 5px; font-size: 10px; }
.issue-history blockquote { padding: 7px 9px; background: var(--surface-soft); border-left: 2px solid var(--line-strong); }
.issue-history time { color: var(--muted); font-size: 9px; }
.issue-facts,
.issue-merge { padding: 15px; }
.issue-facts dl { margin: 12px 0 0; }
.issue-facts dl div { display: flex; min-height: 39px; align-items: center; justify-content: space-between; gap: 12px; border-bottom: 1px solid var(--line); }
.issue-facts dt { color: var(--muted); font-size: 9px; }
.issue-facts dd { margin: 0; max-width: 170px; overflow-wrap: anywhere; font-size: 9px; text-align: right; }
.issue-merge p { color: var(--muted); font-size: 10px; line-height: 1.5; }
.issue-merge select { width: 100%; margin-bottom: 8px; font-size: 9px; }
.issue-merge button { width: 100%; }
@media (max-width: 900px) { .issue-detail-grid { grid-template-columns: 1fr; } .issue-detail-grid aside { grid-row: 1; display: grid; grid-template-columns: 1fr 1fr; } }
@media (max-width: 700px) { .issue-detail-grid aside { display: flex; } .issue-update-panel > div { grid-template-columns: 1fr; } .issue-detail-heading { flex-direction: column; } }
</style>
