<script setup lang="ts">
import {
  AlertTriangle,
  Archive,
  ArrowLeft,
  Camera,
  CheckCircle2,
  ExternalLink,
  Link2,
  Mic,
  MonitorPlay,
  RotateCcw,
  ShieldAlert,
  Video,
} from '@lucide/vue'
import type { FeedbackDetail, FeedbackStatus } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatDateTime } from '~/utils/format'

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { canWrite } = useAuth()
const feedbackID = computed(() => String(route.params.id || ''))
const tab = ref<'evidence' | 'replay' | 'details'>('evidence')
const mutationPending = ref(false)
const mutationError = ref('')
const notice = ref('')

const { data, status, error, refresh } = await useAsyncData(
  () => `feedback-detail:${feedbackID.value}`,
  () => $apiFetch<{ feedback: FeedbackDetail }>(`/v1/feedbacks/${feedbackID.value}`),
  { watch: [feedbackID] },
)

const feedback = computed(() => data.value?.feedback)
const copy = computed(() => feedback.value?.text_cleaned || feedback.value?.text || 'Feedback without written detail')
const transcript = computed(() => feedback.value?.attachments.find((item) => item.transcription)?.transcription)
const failedAttachments = computed(() => feedback.value?.attachments.filter((item) => item.processing_status === 'failed') || [])
const evidenceIcons = { screenshot: Camera, audio: Mic, video: Video, replay: MonitorPlay }

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

async function reprocessAll() {
  if (!canWrite('feedback:update') || !feedback.value || mutationPending.value) return
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
      </div>
    </header>

    <div v-if="status === 'pending'" class="app-state"><div><span class="app-spinner" /><h2>Loading feedback evidence</h2></div></div>
    <div v-else-if="error" class="app-state" role="alert"><div><AlertTriangle :size="24" /><h2>Feedback unavailable</h2><p>{{ apiErrorMessage(error) }}</p><button class="app-button" @click="refresh()">Try again</button></div></div>

    <template v-else-if="feedback">
      <section class="feedback-detail-heading">
        <div><p class="app-eyebrow">{{ feedback.project_name }} · {{ feedback.page_url }}</p><h1>{{ copy }}</h1><span>{{ feedback.public_id }} · {{ formatDate(feedback.created_at) }}</span></div>
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
            <section class="feedback-evidence-section">
              <header><div><h2>Captured evidence</h2><p>{{ feedback.attachments.length }} attachments</p></div><button v-if="canWrite('feedback:update')" class="app-button" :disabled="mutationPending" @click="reprocessAll"><RotateCcw :size="14" /> Reprocess all</button></header>
              <div v-if="feedback.attachments.length" class="feedback-attachment-list">
                <article v-for="attachmentItem in feedback.attachments" :key="attachmentItem.id">
                  <span class="feedback-evidence-icon"><component :is="evidenceIcons[attachmentItem.kind]" :size="18" /></span>
                  <div><b>{{ attachmentItem.kind }}</b><span>{{ attachmentItem.content_type }} · {{ (attachmentItem.size_bytes / 1024).toFixed(1) }} KB</span><p v-if="attachmentItem.transcription">{{ attachmentItem.transcription }}</p><p v-if="attachmentItem.error_message" class="feedback-attachment-error">{{ attachmentItem.error_message }}</p></div>
                  <span class="status-pill" :class="`status-${attachmentItem.processing_status}`">{{ attachmentItem.processing_status }}</span>
                  <div class="feedback-attachment-actions"><AttachmentPreview :attachment="attachmentItem" /><button v-if="attachmentItem.processing_status === 'failed' && canWrite('feedback:update')" class="app-button" :disabled="mutationPending" @click="retryAttachment(attachmentItem.id)">Retry</button></div>
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
              <header><div><h2>Session replay</h2><p>{{ feedback.session.events_count }} events · rrweb {{ feedback.session.rrweb_version }}</p></div><span class="status-pill" :class="feedback.session.replay_ready ? 'status-completed' : 'status-processing'">{{ feedback.session.replay_ready ? 'ready' : 'processing' }}</span></header>
              <ReplayPlayer v-if="feedback.session.replay_ready && feedback.session.replay_url" :url="feedback.session.replay_url" :viewport-width="feedback.viewport_width" :viewport-height="feedback.viewport_height" />
              <div v-else class="feedback-replay-placeholder"><MonitorPlay :size="34" /><h3>Replay is still processing</h3><p>{{ feedback.session.merge_error || 'The replay will appear after its batches are merged.' }}</p></div>
            </section>
            <section v-else class="feedback-evidence-empty"><MonitorPlay :size="28" /><h2>No session replay</h2><p>This feedback was captured without replay enabled.</p></section>
          </template>

          <section v-else class="feedback-environment">
            <h2>Capture environment</h2>
            <dl><div><dt>Page</dt><dd><a :href="feedback.page_url" target="_blank" rel="noreferrer">{{ feedback.page_url }} <ExternalLink :size="12" /></a></dd></div><div><dt>Referrer</dt><dd>{{ feedback.referrer || 'Direct' }}</dd></div><div><dt>Browser</dt><dd>{{ feedback.browser || 'Unknown' }}</dd></div><div><dt>Operating system</dt><dd>{{ feedback.os || 'Unknown' }}</dd></div><div><dt>Device</dt><dd>{{ feedback.device || 'Unknown' }}</dd></div><div><dt>Viewport</dt><dd>{{ feedback.viewport_width && feedback.viewport_height ? `${feedback.viewport_width} × ${feedback.viewport_height}` : 'Unknown' }}</dd></div><div><dt>Language</dt><dd>{{ feedback.language || 'Unknown' }}</dd></div><div><dt>User agent</dt><dd>{{ feedback.user_agent || 'Unknown' }}</dd></div></dl>
          </section>
        </main>

        <aside class="feedback-case-facts">
          <section><h2>Triage</h2><dl><div><dt>Status</dt><dd><span class="status-pill" :class="`status-${feedback.status}`">{{ feedback.status }}</span></dd></div><div><dt>AI status</dt><dd><span class="status-pill" :class="`status-${feedback.ai_status}`">{{ feedback.ai_status }}</span></dd></div><div><dt>Sentiment</dt><dd>{{ feedback.sentiment_score?.toFixed(2) || 'Not scored' }}</dd></div><div><dt>Moderation</dt><dd>{{ feedback.moderation_flags.join(', ') || 'Clear' }}</dd></div><div><dt>Issue</dt><dd><NuxtLink v-if="feedback.issue_id" :to="`/issues/${feedback.issue_id}`">{{ feedback.issue_public_id }}</NuxtLink><span v-else>Not linked</span></dd></div></dl></section>
          <section><h2>Reporter</h2><dl><div><dt>Email</dt><dd>{{ feedback.reporter_email || 'Anonymous' }}</dd></div><div><dt>Updates</dt><dd>{{ feedback.notify_reporter ? 'Subscribed' : 'Not subscribed' }}</dd></div></dl></section>
          <section v-if="failedAttachments.length"><h2>AI failures</h2><p>{{ failedAttachments.length }} attachment{{ failedAttachments.length === 1 ? '' : 's' }} can be retried.</p></section>
        </aside>
      </div>
    </template>
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
  .feedback-detail-tabs { padding: 0 8px; }
  .feedback-detail-grid { padding: 12px 12px 36px; }
  .feedback-case-facts { grid-row: auto; display: flex; }
  .feedback-attachment-list article { grid-template-columns: 36px minmax(0, 1fr) auto; }
  .feedback-attachment-list article > .app-button { grid-column: 2 / -1; }
}
</style>
