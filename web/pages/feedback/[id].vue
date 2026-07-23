<script setup lang="ts">
import {
  AlertTriangle,
  Archive,
  ArrowLeft,
  ArrowUpRight,
  Camera,
  CheckCircle2,
  ExternalLink,
  Link2,
  Mic,
  MonitorPlay,
  Pencil,
  Plus,
  RotateCcw,
  ShieldAlert,
  Video,
  X,
} from '@lucide/vue'
import type { FeedbackDetail, FeedbackStatus, TenantConfiguration } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatDateTime } from '~/utils/format'

const route = useRoute()
const router = useRouter()
const { $apiFetch } = useNuxtApp()
const { canWrite } = useAuth()
const feedbackID = computed(() => String(route.params.id || ''))
const tab = ref<'evidence' | 'replay' | 'details'>('evidence')
const mutationPending = ref(false)
const mutationError = ref('')
const notice = ref('')
const titleEditing = ref(false)
const titleDraft = ref('')
const createIssueOpen = ref(false)
const issueTitle = ref('')
const issueDescription = ref('')

const { data, status, error, refresh } = await useAsyncData(
  () => `feedback-detail:${feedbackID.value}`,
  async () => {
    const [feedbackResponse, configuration] = await Promise.all([
      $apiFetch<{ feedback: FeedbackDetail }>(`/v1/feedbacks/${feedbackID.value}`),
      $apiFetch<TenantConfiguration>('/v1/config'),
    ])
    return { feedback: feedbackResponse.feedback, configuration }
  },
  { watch: [feedbackID] },
)

const feedback = computed(() => data.value?.feedback)
const configuration = computed(() => data.value?.configuration)
const description = computed(() => feedback.value?.description || feedback.value?.text || '')
const transcript = computed(() => feedback.value?.attachments.find((item) => item.transcription)?.transcription)
const retryableAttachments = computed(() => feedback.value?.attachments.filter((item) => (
  item.processing_status === 'failed' && Boolean(item.completed_at)
)) || [])
const replayStatus = computed(() => feedback.value?.session?.status || 'processing')
const aiEnabled = computed(() => configuration.value?.plan.features.ai === true)
const hasReprocessableEvidence = computed(() => feedback.value?.attachments.some((item) => (
  item.kind !== 'replay' && Boolean(item.completed_at)
)) === true)
const canReprocessAll = computed(() => canWrite('feedback:update') && aiEnabled.value && hasReprocessableEvidence.value)
const evidenceIcons = { screenshot: Camera, audio: Mic, video: Video, replay: MonitorPlay }
const structuredResponses = computed(() => Object.entries(feedback.value?.responses || {}))

function responseLabel(key: string) {
  return key.replace(/_/g, ' ').replace(/^./, (character) => character.toUpperCase())
}

const safeDOMSnapshot = computed(() => {
  if (!feedback.value?.dom_snapshot_html) return ''
  const content = feedback.value.dom_snapshot_html
    .replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi, '')
    .replace(/\son\w+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, '')
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'"><style>html,body{margin:0;padding:14px;font-family:system-ui;color:#17211f;background:#fff}*{box-sizing:border-box;max-width:100%}</style></head><body>${content}</body></html>`
})

async function updateStatus(nextStatus: FeedbackStatus) {
  if (!canWrite('feedback:update') || !feedback.value || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/feedbacks/${feedback.value.id}`, { method: 'PATCH', body: { status: nextStatus } })
    notice.value = `Feedback marked ${nextStatus}.`
    await refresh()
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, 'Feedback could not be updated.')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

function editTitle() {
  if (!feedback.value) return
  titleDraft.value = feedback.value.title
  titleEditing.value = true
}

async function saveTitle() {
  const title = titleDraft.value.trim()
  if (!canWrite('feedback:update') || !feedback.value || mutationPending.value || !title) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/feedbacks/${feedback.value.id}`, { method: 'PATCH', body: { title } })
    titleEditing.value = false
    notice.value = 'Feedback title updated.'
    await refresh()
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, 'Feedback title could not be updated.')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

function openCreateIssue() {
  if (!feedback.value || feedback.value.issue_id) return
  issueTitle.value = feedback.value.ai_suggested_title || feedback.value.title
  issueDescription.value = description.value
  createIssueOpen.value = true
}

async function createIssue() {
  if (!feedback.value || feedback.value.issue_id || mutationPending.value || !issueTitle.value.trim()) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    const response = await $apiFetch<{ issue: { id: string } }>(`/v1/feedbacks/${feedback.value.id}/issue`, {
      method: 'POST',
      body: { title: issueTitle.value.trim(), description: issueDescription.value.trim() },
    })
    createIssueOpen.value = false
    await router.push(`/issues/${response.issue.id}`)
  } catch (createError) {
    mutationError.value = apiErrorMessage(createError, 'Issue could not be created.')
  } finally {
    mutationPending.value = false
  }
}

async function reprocessAll() {
  if (!canReprocessAll.value || !feedback.value || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/feedbacks/${feedback.value.id}/reprocess`, { method: 'POST' })
    notice.value = 'AI reprocessing queued.'
    await refresh()
  } catch (reprocessError) {
    mutationError.value = apiErrorMessage(reprocessError, 'AI reprocessing could not be queued.')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

async function retryAttachment(id: string) {
  if (!canWrite('feedback:update') || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/attachments/${id}/retry`, { method: 'POST' })
    notice.value = 'Attachment retry queued.'
    await refresh()
  } catch (retryError) {
    mutationError.value = apiErrorMessage(retryError, 'Attachment retry could not be queued.')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

function formatDate(value?: string) {
  if (!value) return 'Not available'
  return formatDateTime(value)
}

useHead(() => ({ title: `${feedback.value?.public_id || 'Feedback'} · Fidibeki` }))
</script>

<template>
  <div class="feedback-detail-page">
    <header class="feedback-detail-toolbar">
      <NuxtLink class="app-button" to="/feedback"><ArrowLeft :size="15" /> Back to queue</NuxtLink>
      <div v-if="feedback">
        <template v-if="canWrite('feedback:update')">
          <button v-if="feedback.status === 'open'" class="app-button" :disabled="mutationPending" @click="updateStatus('archived')"><Archive :size="15" /> Archive</button>
          <button v-else class="app-button" :disabled="mutationPending" @click="updateStatus('open')">Reopen</button>
          <button class="app-button" :disabled="mutationPending" @click="updateStatus('spam')"><ShieldAlert :size="15" /> Spam</button>
        </template>
        <NuxtLink v-if="feedback.issue_id" class="app-button app-button--primary" :to="`/issues/${feedback.issue_id}`"><Link2 :size="15" /> Open issue</NuxtLink>
        <button v-else-if="canWrite('feedback:update') && canWrite('issue:update')" class="app-button app-button--primary" :disabled="mutationPending" @click="openCreateIssue"><Plus :size="15" /> Create issue</button>
      </div>
    </header>

    <div v-if="status === 'pending'" class="app-state"><div><span class="app-spinner" /><h2>Loading feedback evidence</h2></div></div>
    <div v-else-if="error" class="app-state" role="alert"><div><AlertTriangle :size="24" /><h2>Feedback unavailable</h2><p>{{ apiErrorMessage(error) }}</p><button class="app-button" @click="refresh()">Try again</button></div></div>

    <template v-else-if="feedback">
      <section class="feedback-detail-heading">
        <div><p class="app-eyebrow">{{ feedback.project_name }} · {{ feedback.page_url }}</p><div v-if="titleEditing" class="feedback-title-editor"><input v-model="titleDraft" class="app-input" maxlength="80" aria-label="Feedback title" autofocus /><button class="app-button app-button--primary" :disabled="mutationPending || !titleDraft.trim()" @click="saveTitle">Save</button><button class="app-button" :disabled="mutationPending" @click="titleEditing = false">Cancel</button></div><h1 v-else>{{ feedback.title }}<button v-if="canWrite('feedback:update')" class="feedback-title-edit" title="Edit feedback title" aria-label="Edit feedback title" @click="editTitle"><Pencil :size="14" /></button></h1><p v-if="description" class="feedback-description">{{ description }}</p><span>{{ feedback.public_id }} · {{ formatDate(feedback.created_at) }}</span></div>
        <RatingMark v-if="feedback.rating" :rating="feedback.rating" /><span v-else class="feedback-detail-no-rating">No rating</span>
      </section>

      <p v-if="mutationError" class="feedback-detail-error" role="alert"><AlertTriangle :size="15" /> {{ mutationError }}</p>

      <nav class="feedback-detail-tabs" aria-label="Feedback detail sections">
        <button :data-active="tab === 'evidence'" @click="tab = 'evidence'">Evidence</button>
        <button :data-active="tab === 'replay'" @click="tab = 'replay'">Replay</button>
        <button :data-active="tab === 'details'" @click="tab = 'details'">Environment</button>
      </nav>

      <div class="feedback-detail-grid">
        <main class="feedback-detail-canvas">
          <template v-if="tab === 'evidence'">
            <section v-if="structuredResponses.length || feedback.comment" class="feedback-response-section">
              <header><div><h2>Submitted response</h2><p v-if="feedback.widget_definition_version">Widget definition v{{ feedback.widget_definition_version }}</p></div></header>
              <dl v-if="structuredResponses.length"><div v-for="[key, value] in structuredResponses" :key="key"><dt>{{ responseLabel(key) }}</dt><dd>{{ value }}</dd></div></dl>
              <div v-if="feedback.comment" class="feedback-response-comment"><b>Additional context</b><p>{{ feedback.comment }}</p></div>
            </section>
            <section id="attachments" class="feedback-evidence-section">
              <header><div><h2>Captured evidence</h2><p>{{ feedback.attachments.length }} attachments</p></div><NuxtLink v-if="canWrite('feedback:update') && configuration && !aiEnabled" class="app-button" to="/billing">Upgrade for AI <ArrowUpRight :size="14" /></NuxtLink><button v-else-if="canReprocessAll" class="app-button" :disabled="mutationPending" @click="reprocessAll"><RotateCcw :size="14" /> Reprocess all</button></header>
              <div v-if="feedback.attachments.length" class="feedback-attachment-list">
                <article v-for="attachmentItem in feedback.attachments" :key="attachmentItem.id">
                  <span class="feedback-evidence-icon"><component :is="evidenceIcons[attachmentItem.kind]" :size="18" /></span>
                  <div><b>{{ attachmentItem.kind }}</b><span>{{ attachmentItem.content_type }} · {{ (attachmentItem.size_bytes / 1024).toFixed(1) }} KB</span><p v-if="attachmentItem.transcription">{{ attachmentItem.transcription }}</p><p v-if="attachmentItem.error_message" class="feedback-attachment-error">{{ attachmentItem.error_message }}</p></div>
                  <span class="status-pill" :class="`status-${attachmentItem.processing_status}`">{{ attachmentItem.processing_status }}</span>
                  <div class="feedback-attachment-actions"><AttachmentPreview :attachment="attachmentItem" /><button v-if="attachmentItem.processing_status === 'failed' && attachmentItem.completed_at && canWrite('feedback:update') && aiEnabled" class="app-button" :disabled="mutationPending" @click="retryAttachment(attachmentItem.id)">Retry</button></div>
                </article>
              </div>
              <div v-else class="feedback-evidence-empty"><Camera :size="24" /><p>No media was attached to this feedback.</p></div>
            </section>

            <section v-if="safeDOMSnapshot" class="feedback-dom-capture">
              <header><div><h2>Attached element</h2><p>Rendered in a script-free sandbox</p></div><ExternalLink :size="15" /></header>
              <iframe title="Captured page element" sandbox="" :srcdoc="safeDOMSnapshot" />
            </section>

            <section v-if="transcript" class="feedback-transcript"><h2>Transcript</h2><p>{{ transcript }}</p></section>
          </template>

          <template v-else-if="tab === 'replay'">
            <section v-if="feedback.session" class="feedback-replay-surface">
              <header><div><h2>Session replay</h2><p>{{ feedback.session.events_count }} events · rrweb {{ feedback.session.rrweb_version }}</p></div><span class="status-pill" :class="replayStatus === 'ready' ? 'status-completed' : `status-${replayStatus === 'recording' ? 'processing' : replayStatus}`">{{ replayStatus }}</span></header>
              <ReplayPlayer v-if="feedback.session.replay_ready && feedback.session.replay_url" :url="feedback.session.replay_url" :viewport-width="feedback.viewport_width" :viewport-height="feedback.viewport_height" />
              <div v-else class="feedback-replay-placeholder"><MonitorPlay :size="34" /><h3>{{ replayStatus === 'failed' ? 'Session replay unavailable' : 'Replay is still processing' }}</h3><p>{{ feedback.session.merge_error || 'The replay will appear after its batches are merged.' }}</p></div>
            </section>
            <section v-else class="feedback-evidence-empty"><MonitorPlay :size="28" /><h2>No session replay</h2><p>This feedback was captured without replay enabled.</p></section>
          </template>

          <section v-else class="feedback-environment">
            <h2>Capture environment</h2>
            <dl><div><dt>Page</dt><dd><a :href="feedback.page_url" target="_blank" rel="noreferrer">{{ feedback.page_url }} <ExternalLink :size="12" /></a></dd></div><div><dt>Referrer</dt><dd>{{ feedback.referrer || 'Direct' }}</dd></div><div><dt>Browser</dt><dd>{{ feedback.browser || 'Unknown' }}</dd></div><div><dt>Operating system</dt><dd>{{ feedback.os || 'Unknown' }}</dd></div><div><dt>Device</dt><dd>{{ feedback.device || 'Unknown' }}</dd></div><div><dt>Viewport</dt><dd>{{ feedback.viewport_width && feedback.viewport_height ? `${feedback.viewport_width} × ${feedback.viewport_height}` : 'Unknown' }}</dd></div><div><dt>Language</dt><dd>{{ feedback.language || 'Unknown' }}</dd></div><div><dt>User agent</dt><dd>{{ feedback.user_agent || 'Unknown' }}</dd></div></dl>
          </section>
        </main>

        <aside class="feedback-case-facts">
          <section><h2>Triage</h2><dl><div><dt>Status</dt><dd><span class="status-pill" :class="`status-${feedback.status}`">{{ feedback.status }}</span></dd></div><div><dt>AI status</dt><dd><span class="status-pill" :class="`status-${feedback.ai_status}`">{{ feedback.ai_status }}</span></dd></div><div v-if="feedback.ai_actionability"><dt>Actionability</dt><dd>{{ feedback.ai_actionability.replace('_', ' ') }}</dd></div><div v-if="feedback.ai_category"><dt>Category</dt><dd>{{ feedback.ai_category }}</dd></div><div v-if="feedback.ai_priority"><dt>Suggested priority</dt><dd>{{ feedback.ai_priority }}</dd></div><div><dt>Sentiment</dt><dd>{{ feedback.sentiment_score?.toFixed(2) || 'Not scored' }}</dd></div><div><dt>Moderation</dt><dd>{{ feedback.moderation_flags.join(', ') || 'Clear' }}</dd></div><div><dt>Issue</dt><dd><NuxtLink v-if="feedback.issue_id" :to="`/issues/${feedback.issue_id}`">{{ feedback.issue_public_id }}</NuxtLink><span v-else>Not linked</span></dd></div></dl></section>
          <section><h2>Reporter</h2><dl><div><dt>Email</dt><dd>{{ feedback.reporter_email || 'Anonymous' }}</dd></div><div><dt>Updates</dt><dd>{{ feedback.notify_reporter ? 'Subscribed' : 'Not subscribed' }}</dd></div></dl></section>
          <section v-if="retryableAttachments.length"><h2>AI failures</h2><p>{{ retryableAttachments.length }} attachment{{ retryableAttachments.length === 1 ? '' : 's' }} can be retried.</p></section>
        </aside>
      </div>
    </template>
    <AppDialog :open="createIssueOpen" aria-labelledby="create-issue-title" @close="createIssueOpen = false">
      <form class="feedback-issue-modal app-modal" @submit.prevent="createIssue">
        <header><div><p class="app-eyebrow">Create actionable work</p><h2 id="create-issue-title">Create issue from feedback</h2></div><button type="button" class="app-icon-button" title="Close" aria-label="Close" @click="createIssueOpen = false"><X :size="16" /></button></header>
        <label><span>Title</span><input v-model="issueTitle" class="app-input" required maxlength="80" autofocus /></label>
        <label><span>Description</span><textarea v-model="issueDescription" class="app-input" rows="6" maxlength="10000" /></label>
        <p v-if="feedback?.ai_suggested_title && feedback.ai_suggested_title !== feedback.title" class="feedback-ai-suggestion">AI suggestion: {{ feedback.ai_suggested_title }}</p>
        <footer><button type="button" class="app-button" :disabled="mutationPending" @click="createIssueOpen = false">Cancel</button><button class="app-button app-button--primary" :disabled="mutationPending || !issueTitle.trim()" type="submit"><Plus :size="14" /> {{ mutationPending ? 'Creating...' : 'Create issue' }}</button></footer>
      </form>
    </AppDialog>
    <p v-if="notice" class="feedback-toast" role="status"><CheckCircle2 :size="14" /> {{ notice }}</p>
  </div>
</template>

<style scoped>
.feedback-detail-page { min-height: calc(100vh - 56px); background: #f7f9f8; }
.feedback-detail-toolbar { display: flex; min-height: 62px; align-items: center; justify-content: space-between; gap: 12px; padding: 10px clamp(16px, 3vw, 38px); background: #fff; border-bottom: 1px solid var(--line); }
.feedback-detail-toolbar > div { display: flex; gap: 7px; }
.feedback-detail-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 30px; padding: 26px clamp(16px, 3vw, 38px) 22px; background: #fff; border-bottom: 1px solid var(--line); }
.feedback-detail-heading > div { max-width: 900px; }
.feedback-detail-heading h1 { margin: 7px 0 8px; font-size: 21px; font-weight: 650; line-height: 1.4; }
.feedback-detail-heading h1 { display: flex; align-items: center; gap: 8px; }
.feedback-title-edit { display: inline-grid; width: 28px; height: 28px; flex: 0 0 auto; place-items: center; padding: 0; background: transparent; color: var(--muted); border: 1px solid transparent; border-radius: 4px; }
.feedback-title-edit:hover { color: var(--teal); border-color: var(--line); }
.feedback-title-editor { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; gap: 7px; margin: 7px 0 8px; }
.feedback-description { max-width: 760px; margin: 0 0 9px; color: var(--ink-soft); font-size: 12px; line-height: 1.55; white-space: pre-wrap; }
.feedback-detail-heading > div > span { color: var(--muted); font-size: 10px; }
.feedback-detail-no-rating { padding: 7px 9px; color: var(--muted); border: 1px solid var(--line); border-radius: 5px; font-size: 10px; }
.feedback-detail-error { display: flex; align-items: center; gap: 7px; margin: 0; padding: 9px clamp(16px, 3vw, 38px); background: var(--red-soft); color: var(--red); border-bottom: 1px solid #efc2c6; font-size: 11px; }
.feedback-detail-tabs { display: flex; min-height: 46px; padding: 0 clamp(16px, 3vw, 38px); background: #fff; border-bottom: 1px solid var(--line); }
.feedback-detail-tabs button { padding: 0 14px; background: transparent; color: var(--muted); border: 0; border-bottom: 2px solid transparent; font-size: 11px; font-weight: 750; }
.feedback-detail-tabs button[data-active='true'] { color: var(--teal); border-bottom-color: var(--teal); }
.feedback-detail-grid { display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 18px; padding: 18px clamp(16px, 3vw, 38px) 48px; }
.feedback-detail-canvas { min-width: 0; }
.feedback-detail-canvas > section { margin-bottom: 18px; background: #fff; border: 1px solid var(--line); border-radius: 6px; }
.feedback-evidence-section > header,
.feedback-dom-capture > header,
.feedback-replay-surface > header { display: flex; min-height: 64px; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.feedback-detail-canvas h2,
.feedback-case-facts h2 { margin: 0; font-size: 13px; }
.feedback-detail-canvas header p { margin: 4px 0 0; color: var(--muted); font-size: 9px; }
.feedback-response-section > header { min-height: 58px; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.feedback-response-section dl { margin: 0; padding: 4px 16px; }
.feedback-response-section dl div { display: grid; min-height: 42px; grid-template-columns: 150px minmax(0, 1fr); align-items: center; gap: 14px; border-bottom: 1px solid var(--line); }
.feedback-response-section dl div:last-child { border-bottom: 0; }
.feedback-response-section dt { color: var(--muted); font-size: 9px; font-weight: 700; }
.feedback-response-section dd { margin: 0; overflow-wrap: anywhere; font-size: 11px; }
.feedback-response-comment { padding: 14px 16px; border-top: 1px solid var(--line); }
.feedback-response-comment b { font-size: 9px; }
.feedback-response-comment p { margin: 6px 0 0; font-size: 11px; line-height: 1.55; white-space: pre-wrap; }
.feedback-attachment-list article { display: grid; min-height: 72px; grid-template-columns: 38px minmax(0, 1fr) auto auto; align-items: center; gap: 12px; padding: 10px 16px; border-bottom: 1px solid var(--line); }
.feedback-attachment-list article:last-child { border-bottom: 0; }
.feedback-evidence-icon { display: grid; width: 36px; height: 36px; place-items: center; background: var(--surface-soft); color: var(--teal); border: 1px solid var(--line); border-radius: 5px; }
.feedback-attachment-list article > div { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.feedback-attachment-list article > .feedback-attachment-actions { align-items: flex-end; flex-direction: row; }
.feedback-attachment-list b { font-size: 11px; text-transform: capitalize; }
.feedback-attachment-list span,
.feedback-attachment-list p { margin: 0; color: var(--muted); font-size: 9px; line-height: 1.45; }
.feedback-attachment-list .feedback-attachment-error { color: var(--red); }
.feedback-evidence-empty { display: grid; min-height: 240px; place-items: center; align-content: center; gap: 10px; padding: 30px; color: var(--muted); text-align: center; }
.feedback-evidence-empty h2,
.feedback-evidence-empty p { margin: 0; }
.feedback-evidence-empty p { font-size: 11px; }
.feedback-dom-capture iframe { display: block; width: 100%; height: 360px; border: 0; }
.feedback-transcript { padding: 18px; }
.feedback-transcript p { margin: 10px 0 0; font-size: 13px; line-height: 1.65; }
.feedback-replay-placeholder { display: grid; min-height: 420px; place-items: center; align-content: center; gap: 10px; padding: 30px; background: #edf2f0; color: var(--muted); text-align: center; }
.feedback-replay-placeholder h3,
.feedback-replay-placeholder p { margin: 0; }
.feedback-replay-placeholder h3 { color: var(--ink); font-size: 14px; }
.feedback-replay-placeholder p { max-width: 520px; font-size: 10px; line-height: 1.55; }
.feedback-environment { padding: 18px; }
.feedback-environment dl { margin: 14px 0 0; }
.feedback-environment dl div { display: grid; min-height: 42px; grid-template-columns: 150px minmax(0, 1fr); align-items: center; gap: 18px; border-bottom: 1px solid var(--line); }
.feedback-environment dt { color: var(--muted); font-size: 10px; }
.feedback-environment dd { min-width: 0; margin: 0; overflow-wrap: anywhere; font-size: 10px; }
.feedback-environment a { display: inline-flex; align-items: center; gap: 4px; color: var(--teal); }
.feedback-case-facts { display: flex; flex-direction: column; gap: 12px; }
.feedback-case-facts section { padding: 15px; background: #fff; border: 1px solid var(--line); border-radius: 6px; }
.feedback-case-facts dl { margin: 12px 0 0; }
.feedback-case-facts dl div { display: flex; min-height: 38px; align-items: center; justify-content: space-between; gap: 14px; border-bottom: 1px solid var(--line); }
.feedback-case-facts dl div:last-child { border-bottom: 0; }
.feedback-case-facts dt { color: var(--muted); font-size: 9px; }
.feedback-case-facts dd { margin: 0; max-width: 170px; overflow-wrap: anywhere; font-size: 9px; text-align: right; }
.feedback-case-facts a { color: var(--teal); font-weight: 700; }
.feedback-case-facts section > p { margin: 8px 0 0; color: var(--muted); font-size: 10px; line-height: 1.5; }
.feedback-toast { position: fixed; right: 20px; bottom: 20px; z-index: 90; display: flex; align-items: center; gap: 7px; margin: 0; padding: 10px 13px; background: #17342f; color: #fff; border-radius: 5px; box-shadow: var(--shadow-overlay); font-size: 11px; }
.feedback-issue-modal { width: min(560px, calc(100vw - 32px)); }
.feedback-issue-modal label { display: grid; gap: 6px; margin-top: 12px; }
.feedback-issue-modal label span { color: var(--muted); font-size: 10px; font-weight: 700; }
.feedback-issue-modal textarea { min-height: 130px; padding: 10px; resize: vertical; font: inherit; font-size: 11px; line-height: 1.5; }
.feedback-issue-modal footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
.feedback-ai-suggestion { margin: 10px 0 0; color: var(--muted); font-size: 9px; }

@media (max-width: 900px) {
  .feedback-detail-grid { grid-template-columns: 1fr; }
  .feedback-case-facts { grid-row: 1; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 700px) {
  .feedback-detail-toolbar { align-items: stretch; flex-direction: column; }
  .feedback-detail-toolbar > div { display: grid; grid-template-columns: repeat(2, 1fr); }
  .feedback-detail-toolbar > div .app-button--primary { grid-column: 1 / -1; }
  .feedback-detail-heading { padding: 18px 14px; }
  .feedback-detail-heading h1 { font-size: 17px; }
  .feedback-title-editor { grid-template-columns: 1fr 1fr; }
  .feedback-title-editor input { grid-column: 1 / -1; }
  .feedback-detail-tabs { padding: 0 8px; }
  .feedback-detail-grid { padding: 12px 12px 36px; }
  .feedback-case-facts { grid-row: auto; display: flex; }
  .feedback-attachment-list article { grid-template-columns: 36px minmax(0, 1fr) auto; }
  .feedback-attachment-list article > .app-button { grid-column: 2 / -1; }
}
</style>
