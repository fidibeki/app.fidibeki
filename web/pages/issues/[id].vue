<script setup lang="ts">
import { AlertTriangle, ArrowLeft, AtSign, Eye, EyeOff, GitMerge, MessageSquare, Pencil, RotateCcw, Send } from '@lucide/vue'
import type { FeedbackPage, Issue, IssueActivity, IssueComment, IssueStatus, IssueUpdate, Member, TriagePriority, TriageTag } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatDateTime } from '~/utils/format'

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { can, canWrite } = useAuth()
const issueID = computed(() => String(route.params.id || ''))
const nextStatus = ref<IssueStatus>('open')
const comment = ref('')
const reporterMessage = ref('')
const mergeTarget = ref('')
const pending = ref(false)
const mutationError = ref('')
const detailsEditing = ref(false)
const titleDraft = ref('')
const descriptionDraft = ref('')
const commentBody = ref('')
const mentionedMemberIDs = ref<string[]>([])

const { data, status, error, refresh } = await useAsyncData(
  () => `issue-detail:${issueID.value}`,
  async () => {
    const issueResponse = await $apiFetch<{
      issue: Issue
      updates: IssueUpdate[]
      comments?: IssueComment[]
      activities?: IssueActivity[]
    }>(`/v1/issues/${issueID.value}`)
    const [allIssues, feedbacks, tags, members] = await Promise.all([
      can('issue:read') ? $apiFetch<{ issues: Issue[] }>('/v1/issues') : Promise.resolve({ issues: [] }),
      can('feedback:read') ? $apiFetch<FeedbackPage>(`/v1/feedbacks?q=${encodeURIComponent(issueResponse.issue.public_id)}&limit=20`) : Promise.resolve({ feedbacks: [], pagination: { has_more: false, limit: 20 } }),
      canWrite('issue:update') ? $apiFetch<{ tags: TriageTag[] }>('/v1/tags') : Promise.resolve({ tags: [] }),
      canWrite('issue:update') && can('member:read') ? $apiFetch<{ members: Member[] }>('/v1/members') : Promise.resolve({ members: [] }),
    ])
    return {
      ...issueResponse,
      comments: issueResponse.comments || [],
      activities: issueResponse.activities?.length
        ? issueResponse.activities
        : issueResponse.updates.map(activityFromLegacyUpdate),
      allIssues: allIssues.issues,
      feedbacks: feedbacks.feedbacks,
      tags: tags.tags,
      members: members.members,
    }
  },
  { watch: [issueID] },
)

watch(() => data.value?.issue.status, (value) => { if (value) nextStatus.value = value }, { immediate: true })
const mergeCandidates = computed(() => (data.value?.allIssues || []).filter((candidate) => candidate.id !== issueID.value && candidate.project_id === data.value?.issue.project_id && !candidate.merged_into_id))
const triageTags = computed(() => {
  const options = [...(data.value?.tags || [])]
  for (const tag of data.value?.issue.tags || []) {
    if (!options.some(option => option.id === tag.id)) options.push(tag)
  }
  return options.sort((left, right) => left.name.localeCompare(right.name))
})
const triageMembers = computed(() => {
  const options = [...(data.value?.members || [])]
  const assignee = data.value?.issue.assignee
  if (assignee && !options.some(option => option.id === assignee.id)) {
    options.push({ id: assignee.id, tenant_id: '', email: assignee.email, name: assignee.name || undefined, permissions: [] })
  }
  return options
})

function editDetails() {
  if (!data.value) return
  titleDraft.value = data.value.issue.title
  descriptionDraft.value = data.value.issue.description
  detailsEditing.value = true
}

async function updateIssueMetadata(body: Record<string, unknown>, fallback: string) {
  if (!canWrite('issue:update') || !data.value || pending.value) return false
  pending.value = true
  mutationError.value = ''
  try {
    const response = await $apiFetch<{ issue: Issue }>(`/v1/issues/${issueID.value}`, { method: 'PATCH', body })
    data.value.issue = response.issue
    return true
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, fallback)
    return false
  } finally {
    pending.value = false
  }
}

async function saveDetails() {
  const title = titleDraft.value.trim()
  if (!title) return
  if (await updateIssueMetadata({ title, description: descriptionDraft.value.trim() }, 'Issue details could not be updated.')) {
    detailsEditing.value = false
  }
}

function setIssuePriority(priority: TriagePriority | null) {
  return updateIssueMetadata({ priority: priority || 'none' }, 'Issue priority could not be updated.')
}

function setIssueAssignee(memberID: string | null) {
  return updateIssueMetadata(memberID ? { assignee_id: memberID } : { unassign: true }, 'Issue assignee could not be updated.')
}

function toggleIssueTag(tagID: string, checked: boolean) {
  return updateIssueMetadata(checked ? { add_tag_ids: [tagID] } : { remove_tag_ids: [tagID] }, 'Issue tags could not be updated.')
}

async function updateIssue() {
  if (!canWrite('issue:update') || !data.value || pending.value || nextStatus.value === data.value.issue.status ||
    (nextStatus.value === 'needs_info' && !reporterMessage.value.trim())) return
  pending.value = true
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/issues/${issueID.value}`, {
      method: 'PATCH',
      body: {
        status: nextStatus.value,
        comment: comment.value.trim() || null,
        reporter_message: nextStatus.value === 'needs_info' ? reporterMessage.value.trim() : null,
      },
    })
    comment.value = ''
    reporterMessage.value = ''
    await refresh()
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, 'Issue could not be updated.')
  } finally { pending.value = false }
}

async function setReporterAccess(enabled: boolean) {
  if (!canWrite('issue:update') || !data.value || pending.value) return
  pending.value = true
  mutationError.value = ''
  try {
    const response = await $apiFetch<{ issue: Issue }>(`/v1/issues/${issueID.value}/reporter-access`, {
      method: 'PATCH', body: { enabled },
    })
    data.value.issue = response.issue
  } catch (accessError) {
    mutationError.value = apiErrorMessage(accessError, 'Reporter access could not be updated.')
  } finally {
    pending.value = false
  }
}

async function createComment() {
  const body = commentBody.value.trim()
  if (!canWrite('issue:update') || !data.value || pending.value || !body) return
  pending.value = true
  mutationError.value = ''
  try {
    const response = await $apiFetch<{ comment: IssueComment, activity: IssueActivity }>(
      `/v1/issues/${issueID.value}/comments`,
      { method: 'POST', body: { body, mention_member_ids: mentionedMemberIDs.value } },
    )
    data.value.comments.push(response.comment)
    data.value.activities.push(response.activity)
    commentBody.value = ''
    mentionedMemberIDs.value = []
  } catch (commentError) {
    mutationError.value = apiErrorMessage(commentError, 'Comment could not be added.')
  } finally {
    pending.value = false
  }
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

function actorLabel(activity: IssueActivity) {
  return activity.actor?.name || activity.actor?.email || 'Fidibeki'
}

function activityFromLegacyUpdate(update: IssueUpdate): IssueActivity {
  return {
    id: `legacy-${update.id}`,
    issue_id: update.issue_id,
    kind: 'issue.status_changed',
    visibility: 'internal',
    data: {
      changes: { status: { from: update.old_status, to: update.new_status } },
      transition_comment: update.comment,
    },
    created_at: update.created_at,
  }
}

function activitySummary(activity: IssueActivity) {
  const actor = actorLabel(activity)
  if (activity.kind === 'issue.created') return `${actor} created the issue`
  if (activity.kind === 'issue.comment_created') return `${actor} added a comment`
  if (activity.kind === 'issue.status_changed') return `${actor} changed the status`
  if (activity.kind === 'issue.updated') return `${actor} updated issue details`
  if (activity.kind === 'feedback.linked') return `${actor} linked feedback`
  if (activity.kind === 'feedback.unlinked') return `${actor} unlinked feedback`
  if (activity.kind === 'issue.merged') return `${actor} merged this issue`
  if (activity.kind === 'issue.merge_received') return `${actor} merged another issue here`
  if (activity.kind === 'issue.unmerged' || activity.kind === 'issue.merge_removed') return `${actor} removed a merge`
  if (activity.kind === 'reporter.access_changed') return `${actor} changed reporter access`
  if (activity.kind === 'reporter.responded') return 'Reporter answered the clarification request'
  if (activity.kind === 'reporter.notified') return 'Reporter notification delivered'
  return activity.kind.replaceAll('_', ' ').replaceAll('.', ' · ')
}

function activityDetail(activity: IssueActivity) {
  const changes = activity.data.changes
  if (changes && typeof changes === 'object' && !Array.isArray(changes)) {
    return Object.entries(changes as Record<string, { from?: unknown, to?: unknown }>).map(([field, value]) => {
      const label = field.replace('_id', '').replaceAll('_', ' ')
      const before = value?.from == null || value.from === '' ? 'none' : String(value.from).replaceAll('_', ' ')
      const after = value?.to == null || value.to === '' ? 'none' : String(value.to).replaceAll('_', ' ')
      return `${label}: ${before} → ${after}`
    }).join(' · ')
  }
  const feedbackIDs = activity.data.feedback_ids
  if (Array.isArray(feedbackIDs)) return `${feedbackIDs.length} feedback ${feedbackIDs.length === 1 ? 'record' : 'records'}`
  return ''
}

function transitionComment(activity: IssueActivity) {
  return typeof activity.data.transition_comment === 'string' ? activity.data.transition_comment : ''
}

useHead(() => ({ title: `${data.value?.issue.public_id || 'Issue'} · Fidibeki` }))
</script>

<template>
  <div class="app-page issue-detail-page">
    <header class="issue-detail-back"><NuxtLink class="app-button" to="/issues"><ArrowLeft :size="15" /> Back to issues</NuxtLink></header>
    <section v-if="status === 'pending'" class="app-surface app-state"><div><span class="app-spinner" /><h2>Loading issue</h2></div></section>
    <section v-else-if="error" class="app-surface app-state"><div><AlertTriangle :size="24" /><h2>Issue unavailable</h2><p>{{ apiErrorMessage(error) }}</p></div></section>
    <template v-else-if="data">
      <header class="issue-detail-heading">
        <div><p class="app-eyebrow">{{ data.issue.public_id }}</p>
          <form v-if="detailsEditing" class="issue-detail-editor" @submit.prevent="saveDetails">
            <label><span>Title</span><input v-model="titleDraft" class="app-input" aria-label="Issue title" required maxlength="80" autofocus /></label>
            <label><span>Description</span><textarea v-model="descriptionDraft" aria-label="Issue description" maxlength="10000" /></label>
            <div><button type="button" class="app-button" :disabled="pending" @click="detailsEditing = false">Cancel</button><button type="submit" class="app-button app-button--primary" :disabled="pending || !titleDraft.trim()">Save details</button></div>
          </form>
          <template v-else>
            <h1>{{ data.issue.title }}<button v-if="canWrite('issue:update')" type="button" class="issue-detail-edit" title="Edit issue details" aria-label="Edit issue details" @click="editDetails"><Pencil :size="14" /></button></h1>
            <p v-if="data.issue.description" class="issue-detail-description">{{ data.issue.description }}</p>
          </template>
          <span>{{ data.issue.feedback_count }} feedback {{ data.issue.feedback_count === 1 ? 'record' : 'records' }} · created {{ formatDate(data.issue.created_at) }}</span>
        </div>
        <span class="status-pill" :class="`status-${data.issue.status}`">{{ data.issue.status.replace('_', ' ') }}</span>
      </header>
      <p v-if="mutationError" class="issues-error" role="alert"><AlertTriangle :size="15" /> {{ mutationError }}</p>
      <div class="issue-detail-grid">
        <main>
          <section class="app-surface issue-update-panel"><header><h2>Update status</h2><p>Status changes are recorded in the issue history.</p></header><div><select v-model="nextStatus" class="app-select" :disabled="!canWrite('issue:update')"><option value="open">Open</option><option value="in_progress">In progress</option><option value="needs_info">Needs info</option><option value="resolved">Resolved</option><option value="closed">Closed</option></select><textarea v-model="comment" placeholder="Internal context for this change (optional)" maxlength="2000" :disabled="!canWrite('issue:update')" /><textarea v-if="nextStatus === 'needs_info'" v-model="reporterMessage" class="issue-reporter-message" aria-label="Message for reporter" placeholder="Question for the reporter" maxlength="2000" required :disabled="!canWrite('issue:update')" /><button class="app-button app-button--primary" :disabled="pending || !canWrite('issue:update') || nextStatus === data.issue.status || (nextStatus === 'needs_info' && !reporterMessage.trim())" @click="updateIssue">Update issue</button></div></section>
          <section class="app-surface issue-comments">
            <header><div><h2>Internal comments</h2><p>Collaborate with workspace members without changing status.</p></div><span>{{ data.comments.length }}</span></header>
            <div v-if="data.comments.length" class="issue-comment-list">
              <article v-for="entry in [...data.comments].reverse()" :key="entry.id">
                <span>{{ (entry.author?.name || entry.author?.email || 'F').slice(0, 1).toUpperCase() }}</span>
                <div><header><b>{{ entry.author?.name || entry.author?.email || 'Former member' }}</b><time>{{ formatDate(entry.created_at) }}</time></header><p>{{ entry.body }}</p><small v-if="entry.mentions.length"><AtSign :size="12" /> {{ entry.mentions.map(member => member.name || member.email).join(', ') }}</small></div>
              </article>
            </div>
            <div v-else class="issue-empty"><MessageSquare :size="20" /> No internal comments yet.</div>
            <form v-if="canWrite('issue:update')" class="issue-comment-form" @submit.prevent="createComment">
              <textarea v-model="commentBody" aria-label="Internal comment" placeholder="Add internal context" maxlength="5000" required />
              <fieldset v-if="triageMembers.length"><legend><AtSign :size="12" /> Notify members</legend><label v-for="member in triageMembers" :key="member.id"><input v-model="mentionedMemberIDs" type="checkbox" :value="member.id" /><span>{{ member.name || member.email }}</span></label></fieldset>
              <button class="app-button app-button--primary" type="submit" :disabled="pending || !commentBody.trim()"><Send :size="14" /> Add comment</button>
            </form>
          </section>
          <section class="app-surface issue-feedback"><header><h2>Linked feedback</h2><span>{{ data.feedbacks.length }} {{ data.feedbacks.length === 1 ? 'record' : 'records' }}</span></header><div v-if="!data.feedbacks.length" class="issue-empty">No readable feedback is linked.</div><NuxtLink v-for="feedback in data.feedbacks" :key="feedback.id" :to="`/feedback/${feedback.id}`"><RatingMark v-if="feedback.rating" :rating="feedback.rating" compact /><span v-else>-</span><p><b>{{ feedback.title || 'Feedback without written detail' }}</b><small>{{ feedback.public_id }} · {{ feedback.page_url }}</small></p><span class="status-pill" :class="`status-${feedback.ai_status}`">{{ feedback.ai_status }}</span></NuxtLink></section>
          <section class="app-surface issue-history"><header><h2>Activity</h2><span>{{ data.activities.length }}</span></header><ol><li v-for="activity in [...data.activities].reverse()" :key="activity.id"><span /><div><p><b>{{ activitySummary(activity) }}</b></p><small v-if="activityDetail(activity)">{{ activityDetail(activity) }}</small><blockquote v-if="transitionComment(activity)">{{ transitionComment(activity) }}</blockquote><time>{{ formatDate(activity.created_at) }}</time></div></li><li v-if="!data.activities.length" class="issue-created"><span /><div><p>Issue created</p><time>{{ formatDate(data.issue.created_at) }}</time></div></li></ol></section>
        </main>
        <aside>
          <OperationalTriageControls
            class="app-surface issue-operational-triage"
            :priority="data.issue.priority"
            :assignee="data.issue.assignee"
            :tags="data.issue.tags || []"
            :tag-options="triageTags"
            :members="triageMembers"
            :editable="canWrite('issue:update')"
            :pending="pending"
            @set-priority="setIssuePriority"
            @set-assignee="setIssueAssignee"
            @toggle-tag="toggleIssueTag"
          />
          <section class="app-surface issue-facts"><h2>Issue facts</h2><dl><div><dt>Public ID</dt><dd>{{ data.issue.public_id }}</dd></div><div><dt>Reporter</dt><dd>{{ data.issue.reporter_email || 'Anonymous' }}</dd></div><div><dt>Reporter updates</dt><dd>{{ data.issue.notify_reporter ? 'Enabled' : 'Disabled' }}</dd></div><div><dt>Progress access</dt><dd>{{ data.issue.reporter_access_revoked_at ? 'Revoked' : 'Enabled' }}</dd></div><div><dt>Resolved</dt><dd>{{ data.issue.resolved_at ? formatDate(data.issue.resolved_at) : 'Not resolved' }}</dd></div></dl><button v-if="data.issue.reporter_email && data.issue.notify_reporter && canWrite('issue:update')" class="app-button issue-reporter-access" :disabled="pending" @click="setReporterAccess(Boolean(data.issue.reporter_access_revoked_at))"><Eye v-if="data.issue.reporter_access_revoked_at" :size="14" /><EyeOff v-else :size="14" />{{ data.issue.reporter_access_revoked_at ? 'Enable progress access' : 'Revoke progress access' }}</button></section>
          <section class="app-surface issue-merge"><h2>Merge issue</h2><template v-if="data.issue.merged_into_id"><p>This issue is merged into another issue.</p><button class="app-button" :disabled="pending || !canWrite('issue:merge')" @click="undoMerge"><RotateCcw :size="14" /> Undo merge</button></template><template v-else><p>Move this issue under another issue in the same project.</p><select v-model="mergeTarget" class="app-select" :disabled="!canWrite('issue:merge')"><option value="">Choose target issue</option><option v-for="candidate in mergeCandidates" :key="candidate.id" :value="candidate.id">{{ candidate.public_id }} · {{ candidate.title }}</option></select><button class="app-button" :disabled="pending || !mergeTarget || !canWrite('issue:merge')" @click="mergeIssue"><GitMerge :size="14" /> Merge</button></template></section>
        </aside>
      </div>
    </template>
  </div>
</template>

<style scoped>
.issue-detail-back { margin-bottom: 14px; }
.issue-detail-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.issue-detail-heading h1 { display: flex; align-items: center; gap: 8px; margin: 5px 0 7px; font-size: 22px; }
.issue-detail-description { max-width: 760px; margin: 0 0 9px; color: var(--ink-soft); font-size: 12px; line-height: 1.55; white-space: pre-wrap; }
.issue-detail-heading > div > span { color: var(--muted); font-size: 10px; }
.issue-detail-edit { display: inline-grid; width: 28px; height: 28px; flex: 0 0 auto; place-items: center; padding: 0; background: transparent; color: var(--muted); border: 1px solid transparent; border-radius: 4px; }
.issue-detail-edit:hover { color: var(--teal); border-color: var(--line); }
.issue-detail-editor { display: grid; width: min(760px, calc(100vw - 80px)); gap: 10px; margin: 8px 0 10px; }
.issue-detail-editor label { display: grid; gap: 5px; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.issue-detail-editor textarea { min-height: 100px; padding: 9px; border: 1px solid var(--line-strong); border-radius: 5px; resize: vertical; font: inherit; font-size: 11px; line-height: 1.5; }
.issue-detail-editor > div { display: flex; justify-content: flex-end; gap: 7px; }
.issues-error { display: flex; align-items: center; gap: 7px; margin: 0 0 12px; padding: 10px 12px; background: var(--red-soft); color: var(--red); border: 1px solid #efc2c6; border-radius: 5px; font-size: 11px; }
.issue-detail-grid { display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 16px; }
.issue-detail-grid main,
.issue-detail-grid aside { display: flex; min-width: 0; flex-direction: column; gap: 14px; }
.issue-detail-grid section > header { display: flex; min-height: 60px; align-items: center; justify-content: space-between; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.issue-detail-grid h2 { margin: 0; font-size: 13px; }
.issue-detail-grid header p { margin: 4px 0 0; color: var(--muted); font-size: 9px; }
.issue-update-panel > div { display: grid; grid-template-columns: 150px minmax(0, 1fr) auto; align-items: start; gap: 9px; padding: 14px; }
.issue-update-panel textarea { min-height: 72px; padding: 9px; border: 1px solid var(--line-strong); border-radius: 5px; resize: vertical; font: inherit; font-size: 11px; }
.issue-update-panel .issue-reporter-message { grid-column: 2 / 3; border-color: var(--amber); }
.issue-comments > header > span,
.issue-history > header > span { color: var(--muted); font-size: 9px; }
.issue-comment-list article { display: grid; grid-template-columns: 30px minmax(0, 1fr); gap: 10px; padding: 12px 14px; border-bottom: 1px solid var(--line); }
.issue-comment-list article > span { display: grid; width: 28px; height: 28px; place-items: center; background: var(--surface-soft); color: var(--teal); border: 1px solid var(--line); border-radius: 50%; font-size: 10px; font-weight: 800; }
.issue-comment-list article header { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.issue-comment-list article b { font-size: 10px; }
.issue-comment-list article time { color: var(--muted); font-size: 9px; }
.issue-comment-list article p { margin: 6px 0; color: var(--ink-soft); font-size: 11px; line-height: 1.55; white-space: pre-wrap; }
.issue-comment-list article small { display: flex; align-items: center; gap: 4px; color: var(--muted); font-size: 9px; }
.issue-comment-form { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: end; gap: 9px; padding: 14px; background: var(--surface-soft); border-top: 1px solid var(--line); }
.issue-comment-form > textarea { min-height: 78px; grid-column: 1 / -1; padding: 9px; border: 1px solid var(--line-strong); border-radius: 5px; resize: vertical; font: inherit; font-size: 11px; }
.issue-comment-form fieldset { display: flex; min-width: 0; flex-wrap: wrap; gap: 6px; margin: 0; padding: 0; border: 0; }
.issue-comment-form legend { display: flex; align-items: center; gap: 4px; margin-bottom: 6px; color: var(--muted); font-size: 9px; font-weight: 750; }
.issue-comment-form label { display: inline-flex; min-height: 28px; align-items: center; gap: 5px; padding: 0 7px; background: #fff; border: 1px solid var(--line); border-radius: 4px; color: var(--ink-soft); font-size: 9px; }
.issue-comment-form label:has(input:checked) { color: var(--teal); border-color: var(--teal); }
.issue-comment-form button { align-self: end; }
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
.issue-history small { display: block; margin: 0 0 5px; color: var(--muted); font-size: 9px; line-height: 1.5; }
.issue-history blockquote { padding: 7px 9px; background: var(--surface-soft); border-left: 2px solid var(--line-strong); }
.issue-history time { color: var(--muted); font-size: 9px; }
.issue-facts,
.issue-merge { padding: 15px; }
.issue-operational-triage { padding: 15px; }
.issue-facts dl { margin: 12px 0 0; }
.issue-facts dl div { display: flex; min-height: 39px; align-items: center; justify-content: space-between; gap: 12px; border-bottom: 1px solid var(--line); }
.issue-facts dt { color: var(--muted); font-size: 9px; }
.issue-facts dd { margin: 0; max-width: 170px; overflow-wrap: anywhere; font-size: 9px; text-align: right; }
.issue-reporter-access { width: 100%; margin-top: 12px; }
.issue-merge p { color: var(--muted); font-size: 10px; line-height: 1.5; }
.issue-merge select { width: 100%; margin-bottom: 8px; font-size: 9px; }
.issue-merge button { width: 100%; }
@media (max-width: 900px) { .issue-detail-grid { grid-template-columns: 1fr; } .issue-detail-grid aside { grid-row: 1; display: grid; grid-template-columns: 1fr 1fr; } }
@media (max-width: 700px) { .issue-detail-grid aside { display: flex; } .issue-update-panel > div, .issue-comment-form { grid-template-columns: 1fr; } .issue-update-panel .issue-reporter-message, .issue-comment-form > textarea { grid-column: auto; } .issue-detail-heading { flex-direction: column; } }
</style>
