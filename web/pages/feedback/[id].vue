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
import type { FeedbackAttachment, FeedbackDetail, FeedbackStatus, TenantConfiguration } from '~/types/api'
import {
  feedbackContextLabel,
  feedbackContextURL,
  feedbackSource,
  isMobileFeedbackSource,
} from '~/utils/feedback-source'

const route = useRoute()
const router = useRouter()
const { $apiFetch } = useNuxtApp()
const { can, canWrite } = useAuth()
const { t, te } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { dateTime, decimal, integer, plural } = useRegionalFormat()
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
  item.safety_status === 'safe' && item.processing_status === 'failed' && Boolean(item.completed_at)
)) || [])
const replayStatus = computed(() => feedback.value?.session?.status || 'processing')
const replayFailure = computed(() => feedback.value?.session?.failure)
const replayFailureKeys: Record<string, string> = {
  contract_violation: 'contractViolation',
  unsafe_payload: 'unsafePayload',
  upload_expired: 'uploadExpired',
  processing_failed: 'processingFailed',
  unavailable: 'unavailable',
}
const replayFailureMessage = computed(() => {
  const key = replayFailureKeys[replayFailure.value?.code || '']
  return key
    ? t(`feedback.detail.replayFailures.${key}`)
    : t('feedback.detail.replayFailureFallback')
})
const replayPlaceholderTitle = computed(() => {
  if (replayStatus.value === 'failed') {
    return replayFailure.value?.code === 'processing_failed'
      ? t('feedback.detail.replayProcessingFailed')
      : t('feedback.detail.replayUnavailable')
  }
  return replayStatus.value === 'expired'
    ? t('feedback.detail.replayExpired')
    : t('feedback.detail.replayProcessing')
})
const replayPlaceholderMessage = computed(() => {
  if (replayStatus.value === 'failed') return replayFailureMessage.value
  return replayStatus.value === 'expired'
    ? t('feedback.detail.replayExpiredDescription')
    : t('feedback.detail.replayPending')
})
const canRetryReplay = computed(() => (
  replayStatus.value === 'failed'
  && replayFailure.value?.retryable === true
  && canWrite('feedback:update')
  && can('session:read')
))
const aiEnabled = computed(() => configuration.value?.plan.features.ai === true)
const hasReprocessableEvidence = computed(() => feedback.value?.attachments.some((item) => (
  item.kind !== 'replay' && item.safety_status === 'safe' && Boolean(item.completed_at)
)) === true)
const canReprocessAll = computed(() => canWrite('feedback:update') && aiEnabled.value && hasReprocessableEvidence.value)
const evidenceIcons = { screenshot: Camera, audio: Mic, video: Video, replay: MonitorPlay }
const structuredResponses = computed(() => Object.entries(feedback.value?.responses || {}))
const source = computed(() => feedbackSource(feedback.value))
const isMobileSource = computed(() => isMobileFeedbackSource(source.value))
const contextLabel = computed(() => feedbackContextLabel(feedback.value) || t('feedback.common.notAvailable'))
const contextURL = computed(() => feedbackContextURL(feedback.value))

function responseLabel(key: string) {
  return key.replace(/_/g, ' ').replace(/^./, (character) => character.toUpperCase())
}

function attachmentStatus(attachment: FeedbackAttachment) {
  return attachment.safety_status === 'safe' ? attachment.processing_status : attachment.safety_status
}

// A quarantined upload carries the specific reason it was refused; showing it is what makes a
// rejection actionable instead of leaving every failure as the same opaque sentence.
const attachmentRejectionKeys: Record<string, string> = {
  empty_upload: 'emptyUpload',
  size_mismatch: 'sizeMismatch',
  unknown_signature: 'unknownSignature',
  signature_mismatch: 'signatureMismatch',
  kind_mismatch: 'kindMismatch',
  invalid_structure: 'invalidStructure',
  invalid_json: 'invalidJson',
}

function attachmentError(attachment: FeedbackAttachment) {
  if (!attachment.error_message) return ''
  const key = attachmentRejectionKeys[attachment.safety_error_reason || '']
  return key ? t(`feedback.detail.attachmentRejected.${key}`) : t('feedback.detail.attachmentFailed')
}

const safeDOMSnapshot = computed(() => {
  if (!feedback.value?.dom_snapshot_html) return ''
  const content = feedback.value.dom_snapshot_html
    .replace(/<script\b[^>]*>[\s\S]*?<\/script>/gi, '')
    .replace(/\son\w+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, '')
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"><style>html,body{margin:0;padding:10px;color:#147d64;background:#fff}body *{display:block;box-sizing:border-box;min-height:18px;margin:5px;padding:5px;border:1px solid currentColor;background:rgba(20,125,100,.04)}table{display:table}thead{display:table-header-group}tbody{display:table-row-group}tfoot{display:table-footer-group}tr{display:table-row}td,th{display:table-cell}</style></head><body>${content}</body></html>`
})

async function updateStatus(nextStatus: FeedbackStatus) {
  if (!canWrite('feedback:update') || !feedback.value || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/feedbacks/${feedback.value.id}`, { method: 'PATCH', body: { status: nextStatus } })
    notice.value = t('feedback.notices.marked', { status: statusLabel(nextStatus).toLocaleLowerCase() })
    await refresh()
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, 'feedback.errors.update')
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
    notice.value = t('feedback.notices.titleUpdated')
    await refresh()
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, 'feedback.errors.title')
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
    mutationError.value = apiErrorMessage(createError, 'feedback.errors.createIssue')
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
    notice.value = t('feedback.notices.aiReprocessing')
    await refresh()
  } catch (reprocessError) {
    mutationError.value = apiErrorMessage(reprocessError, 'feedback.errors.aiReprocess')
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
    notice.value = t('feedback.notices.attachmentRetry')
    await refresh()
  } catch (retryError) {
    mutationError.value = apiErrorMessage(retryError, 'feedback.errors.attachmentRetry')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

async function retryReplay() {
  const session = feedback.value?.session
  if (!session || !canRetryReplay.value || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/sessions/${session.id}/retry`, { method: 'POST' })
    notice.value = t('feedback.notices.replayRetry')
    await refresh()
  } catch (retryError) {
    mutationError.value = apiErrorMessage(retryError, 'feedback.errors.replayRetry')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

function formatDate(value?: string) {
  if (!value) return t('feedback.common.notAvailable')
  return dateTime(value, { dateStyle: 'medium', timeStyle: 'short' })
}

function statusLabel(value: string) {
  return t(`feedback.status.${value}`)
}

function priorityLabel(value: string) {
  return t(`feedback.priority.${value}`)
}

function actionabilityLabel(value: string) {
  return t(`feedback.actionability.${value}`)
}

function moderationLabel(value: string) {
  const key = `feedback.moderationFlags.${value}`
  return te(key) ? t(key) : value
}

function evidenceKindLabel(value: string) {
  return t(`feedback.evidenceKinds.${value}`)
}

function attachmentSize(sizeBytes: number) {
  return decimal(sizeBytes / 1024, { minimumFractionDigits: 1, maximumFractionDigits: 1 })
}

useHead(() => ({
  title: t('feedback.detailPageTitle', { feedback: feedback.value?.public_id || t('feedback.title') }),
}))
</script>

<template>
  <div class="feedback-detail-page">
    <header class="feedback-detail-toolbar">
      <NuxtLink class="app-button" to="/feedback"><ArrowLeft :size="15" /> {{ t('feedback.detail.back') }}</NuxtLink>
      <div v-if="feedback">
        <template v-if="canWrite('feedback:update')">
          <button v-if="feedback.status === 'open'" class="app-button" :disabled="mutationPending" @click="updateStatus('archived')"><Archive :size="15" /> {{ t('feedback.actions.archive') }}</button>
          <button v-else class="app-button" :disabled="mutationPending" @click="updateStatus('open')">{{ t('feedback.actions.reopen') }}</button>
          <button class="app-button" :disabled="mutationPending" @click="updateStatus('spam')"><ShieldAlert :size="15" /> {{ t('feedback.actions.spam') }}</button>
        </template>
        <NuxtLink v-if="feedback.issue_id" class="app-button app-button--primary" :to="`/issues/${feedback.issue_id}`"><Link2 :size="15" /> {{ t('feedback.actions.openIssue') }}</NuxtLink>
        <button v-else-if="canWrite('feedback:update') && canWrite('issue:update')" class="app-button app-button--primary" :disabled="mutationPending" @click="openCreateIssue"><Plus :size="15" /> {{ t('feedback.actions.createIssue') }}</button>
      </div>
    </header>

    <div v-if="status === 'pending'" class="app-state"><div><span class="app-spinner" /><h2>{{ t('feedback.detail.loading') }}</h2></div></div>
    <div v-else-if="error" class="app-state" role="alert"><div><AlertTriangle :size="24" /><h2>{{ t('feedback.states.unavailable') }}</h2><p>{{ apiErrorMessage(error, 'feedback.errors.load') }}</p><button class="app-button" @click="refresh()">{{ t('feedback.actions.tryAgain') }}</button></div></div>

    <template v-else-if="feedback">
      <section class="feedback-detail-heading">
        <div>
          <div class="feedback-detail-origin">
            <span class="app-eyebrow">{{ feedback.project_name }}</span>
            <FeedbackSourceBadge :source="source" />
            <a v-if="contextURL" :href="contextURL" target="_blank" rel="noreferrer">{{ contextLabel }} <ExternalLink :size="11" /></a>
            <span v-else>{{ contextLabel }}</span>
          </div>
          <div v-if="titleEditing" class="feedback-title-editor"><input v-model="titleDraft" class="app-input" maxlength="80" :aria-label="t('feedback.detail.titleLabel')" autofocus /><button class="app-button app-button--primary" :disabled="mutationPending || !titleDraft.trim()" @click="saveTitle">{{ t('feedback.actions.save') }}</button><button class="app-button" :disabled="mutationPending" @click="titleEditing = false">{{ t('feedback.actions.cancel') }}</button></div><h1 v-else>{{ feedback.title }}<button v-if="canWrite('feedback:update')" class="feedback-title-edit" :title="t('feedback.actions.editTitle')" :aria-label="t('feedback.actions.editTitle')" @click="editTitle"><Pencil :size="14" /></button></h1><p v-if="description" class="feedback-description">{{ description }}</p><span>{{ feedback.public_id }} · {{ formatDate(feedback.created_at) }}</span>
        </div>
        <RatingMark :rating="feedback.rating" />
      </section>

      <p v-if="mutationError" class="feedback-detail-error" role="alert"><AlertTriangle :size="15" /> {{ mutationError }}</p>

      <nav class="feedback-detail-tabs" :aria-label="t('feedback.inspector.sections')">
        <button :data-active="tab === 'evidence'" @click="tab = 'evidence'">{{ t('feedback.detail.tabs.evidence') }}</button>
        <button :data-active="tab === 'replay'" @click="tab = 'replay'">{{ t('feedback.detail.tabs.replay') }}</button>
        <button :data-active="tab === 'details'" @click="tab = 'details'">{{ t('feedback.detail.tabs.environment') }}</button>
      </nav>

      <div class="feedback-detail-grid">
        <main class="feedback-detail-canvas">
          <template v-if="tab === 'evidence'">
            <section v-if="structuredResponses.length || feedback.comment" class="feedback-response-section">
              <header><div><h2>{{ t('feedback.detail.submittedResponse') }}</h2><p v-if="feedback.widget_definition_version">{{ t('feedback.detail.widgetVersion', { version: integer(feedback.widget_definition_version) }) }}</p></div></header>
              <dl v-if="structuredResponses.length"><div v-for="[key, value] in structuredResponses" :key="key"><dt>{{ responseLabel(key) }}</dt><dd data-fidibeki-mask>{{ value }}</dd></div></dl>
              <div v-if="feedback.comment" class="feedback-response-comment"><b>{{ t('feedback.detail.additionalContext') }}</b><p data-fidibeki-mask>{{ feedback.comment }}</p></div>
            </section>
            <section id="attachments" class="feedback-evidence-section">
              <header><div><h2>{{ t('feedback.detail.capturedEvidence') }}</h2><p>{{ plural('feedback.detail.attachments', feedback.attachments.length, { count: integer(feedback.attachments.length) }) }}</p></div><NuxtLink v-if="canWrite('feedback:update') && configuration && !aiEnabled" class="app-button" to="/billing">{{ t('feedback.actions.upgradeAI') }} <ArrowUpRight :size="14" /></NuxtLink><button v-else-if="canReprocessAll" class="app-button" :disabled="mutationPending" @click="reprocessAll"><RotateCcw :size="14" /> {{ t('feedback.actions.reprocessAll') }}</button></header>
              <div v-if="feedback.attachments.length" class="feedback-attachment-list">
                <article v-for="attachmentItem in feedback.attachments" :key="attachmentItem.id">
                  <span class="feedback-evidence-icon"><component :is="evidenceIcons[attachmentItem.kind]" :size="18" /></span>
                  <div><b>{{ evidenceKindLabel(attachmentItem.kind) }}</b><span>{{ t('feedback.detail.attachmentSize', { contentType: attachmentItem.content_type, size: attachmentSize(attachmentItem.size_bytes) }) }}</span><p v-if="attachmentItem.transcription">{{ attachmentItem.transcription }}</p><p v-if="attachmentItem.error_message" class="feedback-attachment-error">{{ attachmentError(attachmentItem) }}</p></div>
                  <span class="status-pill" :class="`status-${attachmentStatus(attachmentItem)}`">{{ statusLabel(attachmentStatus(attachmentItem)) }}</span>
                  <div class="feedback-attachment-actions"><AttachmentPreview :attachment="attachmentItem" /><button v-if="attachmentItem.safety_status === 'safe' && attachmentItem.processing_status === 'failed' && attachmentItem.completed_at && canWrite('feedback:update') && aiEnabled" class="app-button" :disabled="mutationPending" @click="retryAttachment(attachmentItem.id)">{{ t('feedback.actions.retry') }}</button></div>
                </article>
              </div>
              <div v-else class="feedback-evidence-empty"><Camera :size="24" /><p>{{ t('feedback.detail.noMedia') }}</p></div>
            </section>

            <section v-if="safeDOMSnapshot" class="feedback-dom-capture">
              <header><div><h2>{{ t('feedback.detail.attachedElement') }}</h2><p>{{ t('feedback.detail.sandboxed') }}</p></div><ExternalLink :size="15" /></header>
              <iframe :title="t('feedback.detail.capturedElement')" sandbox="" :srcdoc="safeDOMSnapshot" />
            </section>

            <section v-if="transcript" class="feedback-transcript"><h2>{{ t('feedback.detail.transcript') }}</h2><p>{{ transcript }}</p></section>
          </template>

          <template v-else-if="tab === 'replay'">
            <section v-if="feedback.session" class="feedback-replay-surface">
              <header><div><h2>{{ t('feedback.detail.sessionReplay') }}</h2><p>{{ t('feedback.detail.replayVersion', { events: plural('feedback.replay.events', feedback.session.events_count, { count: integer(feedback.session.events_count) }), version: feedback.session.rrweb_version }) }}</p></div><span class="status-pill" :class="replayStatus === 'ready' ? 'status-completed' : `status-${replayStatus === 'recording' ? 'processing' : replayStatus}`">{{ statusLabel(replayStatus) }}</span></header>
              <ReplayPlayer v-if="feedback.session.replay_ready && feedback.session.replay_url" :url="feedback.session.replay_url" :viewport-width="feedback.viewport_width" :viewport-height="feedback.viewport_height" />
              <div v-else class="feedback-replay-placeholder" :data-failed="replayStatus === 'failed' || replayStatus === 'expired'" aria-live="polite"><AlertTriangle v-if="replayStatus === 'failed' || replayStatus === 'expired'" :size="34" /><MonitorPlay v-else :size="34" /><h3>{{ replayPlaceholderTitle }}</h3><p>{{ replayPlaceholderMessage }}</p><button v-if="canRetryReplay" type="button" class="app-button app-button--primary" :disabled="mutationPending" @click="retryReplay"><RotateCcw :size="14" /> {{ mutationPending ? t('feedback.actions.retryingProcessing') : t('feedback.actions.retryProcessing') }}</button></div>
            </section>
            <section v-else class="feedback-evidence-empty"><MonitorPlay :size="28" /><h2>{{ t('feedback.detail.noReplay') }}</h2><p>{{ t('feedback.detail.noReplayDescription') }}</p></section>
          </template>

          <section v-else class="feedback-environment">
            <h2>{{ t('feedback.detail.environment') }}</h2>
            <dl><div><dt>{{ t('feedback.detail.source') }}</dt><dd><FeedbackSourceBadge :source="source" /></dd></div><div v-if="isMobileSource"><dt>{{ t('feedback.detail.mobileScreen') }}</dt><dd>{{ feedback.mobile_screen || t('feedback.common.notAvailable') }}</dd></div><div v-if="isMobileSource"><dt>{{ t('feedback.detail.applicationID') }}</dt><dd>{{ feedback.mobile_application_id || t('feedback.common.notAvailable') }}</dd></div><div><dt>{{ t('feedback.detail.pageContext') }}</dt><dd><a v-if="contextURL" :href="contextURL" target="_blank" rel="noreferrer">{{ feedback.page_url }} <ExternalLink :size="12" /></a><span v-else>{{ feedback.page_url || t('feedback.common.notAvailable') }}</span></dd></div><div v-if="isMobileSource"><dt>{{ t('feedback.detail.appVersion') }}</dt><dd>{{ feedback.mobile_app_version || t('feedback.common.notAvailable') }}</dd></div><div v-if="isMobileSource"><dt>{{ t('feedback.detail.mobileOSVersion') }}</dt><dd>{{ feedback.mobile_os_version || t('feedback.common.notAvailable') }}</dd></div><div><dt>{{ t('feedback.detail.browser') }}</dt><dd>{{ feedback.browser || t('feedback.common.unknown') }}</dd></div><div><dt>{{ t('feedback.detail.os') }}</dt><dd>{{ feedback.os || t('feedback.common.unknown') }}</dd></div><div><dt>{{ t('feedback.detail.device') }}</dt><dd>{{ feedback.device || t('feedback.common.unknown') }}</dd></div><div><dt>{{ t('feedback.detail.viewport') }}</dt><dd>{{ feedback.viewport_width && feedback.viewport_height ? `${integer(feedback.viewport_width)} × ${integer(feedback.viewport_height)}` : t('feedback.common.unknown') }}</dd></div><div><dt>{{ t('feedback.detail.language') }}</dt><dd>{{ feedback.language || t('feedback.common.unknown') }}</dd></div></dl>
          </section>
        </main>

        <aside class="feedback-case-facts">
          <section><h2>{{ t('feedback.detail.triage') }}</h2><dl><div><dt>{{ t('feedback.common.status') }}</dt><dd><span class="status-pill" :class="`status-${feedback.status}`">{{ statusLabel(feedback.status) }}</span></dd></div><div><dt>{{ t('feedback.detail.aiStatus') }}</dt><dd><span class="status-pill" :class="`status-${feedback.ai_status}`">{{ statusLabel(feedback.ai_status) }}</span></dd></div><div v-if="feedback.ai_actionability"><dt>{{ t('feedback.detail.actionability') }}</dt><dd>{{ actionabilityLabel(feedback.ai_actionability) }}</dd></div><div v-if="feedback.ai_category"><dt>{{ t('feedback.common.category') }}</dt><dd>{{ feedback.ai_category }}</dd></div><div v-if="feedback.ai_priority"><dt>{{ t('feedback.detail.suggestedPriority') }}</dt><dd>{{ priorityLabel(feedback.ai_priority) }}</dd></div><div><dt>{{ t('feedback.common.sentiment') }}</dt><dd><SentimentBadge :score="feedback.sentiment_score" /></dd></div><div><dt>{{ t('feedback.common.moderation') }}</dt><dd>{{ feedback.moderation_flags.length ? feedback.moderation_flags.map(moderationLabel).join(', ') : t('feedback.common.clear') }}</dd></div><div><dt>{{ t('feedback.common.issue') }}</dt><dd><NuxtLink v-if="feedback.issue_id" :to="`/issues/${feedback.issue_id}`">{{ feedback.issue_public_id }}</NuxtLink><span v-else>{{ t('feedback.common.notLinked') }}</span></dd></div></dl></section>
          <section><h2>{{ t('feedback.detail.reporter') }}</h2><dl><div><dt>{{ t('feedback.common.email') }}</dt><dd>{{ feedback.reporter_email || t('feedback.common.anonymous') }}</dd></div><div><dt>{{ t('feedback.common.updates') }}</dt><dd>{{ feedback.notify_reporter ? t('feedback.common.subscribed') : t('feedback.common.notSubscribed') }}</dd></div></dl></section>
          <section v-if="retryableAttachments.length"><h2>{{ t('feedback.detail.aiFailures') }}</h2><p>{{ plural('feedback.detail.retryableAttachments', retryableAttachments.length, { count: integer(retryableAttachments.length) }) }}</p></section>
        </aside>
      </div>
    </template>
    <AppDialog :open="createIssueOpen" aria-labelledby="create-issue-title" @close="createIssueOpen = false">
      <form class="feedback-issue-modal app-modal" @submit.prevent="createIssue">
        <header><div><p class="app-eyebrow">{{ t('feedback.dialogs.createEyebrow') }}</p><h2 id="create-issue-title">{{ t('feedback.dialogs.createOne') }}</h2></div><button type="button" class="app-icon-button" :title="t('feedback.actions.close')" :aria-label="t('feedback.actions.close')" @click="createIssueOpen = false"><X :size="16" /></button></header>
        <div class="app-modal__body">
          <label><span>{{ t('feedback.dialogs.title') }}</span><input v-model="issueTitle" class="app-input" required maxlength="80" autofocus /></label>
          <label><span>{{ t('feedback.dialogs.description') }}</span><textarea v-model="issueDescription" class="app-input" rows="6" maxlength="10000" /></label>
          <p v-if="feedback?.ai_suggested_title && feedback.ai_suggested_title !== feedback.title" class="feedback-ai-suggestion">{{ t('feedback.dialogs.aiSuggestion', { title: feedback.ai_suggested_title }) }}</p>
        </div>
        <footer><button type="button" class="app-button" :disabled="mutationPending" @click="createIssueOpen = false">{{ t('feedback.actions.cancel') }}</button><button class="app-button app-button--primary" :disabled="mutationPending || !issueTitle.trim()" type="submit"><Plus :size="14" /> {{ mutationPending ? t('feedback.actions.creating') : t('feedback.actions.createIssue') }}</button></footer>
      </form>
    </AppDialog>
    <p v-if="notice" class="feedback-toast" role="status"><CheckCircle2 :size="14" /> {{ notice }}</p>
  </div>
</template>

<style scoped>
.feedback-detail-page { min-height: calc(100vh - 56px); background: var(--surface-subtle); }
.feedback-detail-toolbar { display: flex; min-height: 62px; align-items: center; justify-content: space-between; gap: 12px; padding: 10px clamp(16px, 3vw, 38px); background: var(--surface); border-bottom: 1px solid var(--line); }
.feedback-detail-toolbar > div { display: flex; gap: 7px; }
.feedback-detail-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 30px; padding: 26px clamp(16px, 3vw, 38px) 22px; background: var(--surface); border-bottom: 1px solid var(--line); }
.feedback-detail-heading > div { min-width: 0; max-width: 900px; flex: 1 1 auto; }
.feedback-detail-heading h1 { margin: 7px 0 8px; font-size: 21px; font-weight: 650; line-height: 1.4; }
.feedback-detail-heading h1 { display: flex; align-items: center; gap: 8px; }
.feedback-detail-origin { display: flex; width: 100%; min-width: 0; max-width: 100%; align-items: center; gap: 7px; overflow: hidden; }
.feedback-detail-origin > .app-eyebrow { min-width: 0; flex: 0 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.feedback-detail-origin > a,
.feedback-detail-origin > span:last-child { min-width: 0; flex: 1 1 auto; overflow: hidden; color: var(--muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.feedback-detail-origin > a { display: inline-flex; align-items: center; gap: 4px; }
.feedback-detail-origin > a:hover { color: var(--teal); text-decoration: underline; }
.feedback-title-edit { display: inline-grid; width: 28px; height: 28px; flex: 0 0 auto; place-items: center; padding: 0; background: transparent; color: var(--muted); border: 1px solid transparent; border-radius: 4px; }
.feedback-title-edit:hover { color: var(--teal); border-color: var(--line); }
.feedback-title-editor { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; gap: 7px; margin: 7px 0 8px; }
.feedback-description { max-width: 760px; margin: 0 0 9px; color: var(--ink-soft); font-size: 12px; line-height: 1.55; white-space: pre-wrap; }
.feedback-detail-heading > div > span { color: var(--muted); font-size: 10px; }
.feedback-detail-error { display: flex; align-items: center; gap: 7px; margin: 0; padding: 9px clamp(16px, 3vw, 38px); background: var(--red-soft); color: var(--red); border-bottom: 1px solid var(--red-line); font-size: 11px; }
.feedback-detail-tabs { display: flex; min-height: 46px; padding: 0 clamp(16px, 3vw, 38px); background: var(--surface); border-bottom: 1px solid var(--line); }
.feedback-detail-tabs button { padding: 0 14px; background: transparent; color: var(--muted); border: 0; border-bottom: 2px solid transparent; font-size: 11px; font-weight: 750; }
.feedback-detail-tabs button[data-active='true'] { color: var(--teal); border-bottom-color: var(--teal); }
.feedback-detail-grid { display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 18px; padding: 18px clamp(16px, 3vw, 38px) 48px; }
.feedback-detail-canvas { min-width: 0; }
.feedback-detail-canvas > section { margin-bottom: 18px; background: var(--surface); border: 1px solid var(--line); border-radius: 6px; }
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
.feedback-replay-placeholder { display: grid; min-height: 420px; place-items: center; align-content: center; gap: 10px; padding: 30px; background: var(--surface-soft); color: var(--muted); text-align: center; }
.feedback-replay-placeholder h3,
.feedback-replay-placeholder p { margin: 0; }
.feedback-replay-placeholder[data-failed='true'] > svg { color: var(--red); }
.feedback-replay-placeholder h3 { color: var(--ink); font-size: 15px; }
.feedback-replay-placeholder p { max-width: 65ch; overflow-wrap: anywhere; font-size: 12px; line-height: 1.6; }
.feedback-environment { padding: 18px; }
.feedback-environment dl { margin: 14px 0 0; }
.feedback-environment dl div { display: grid; min-height: 42px; grid-template-columns: 150px minmax(0, 1fr); align-items: center; gap: 18px; border-bottom: 1px solid var(--line); }
.feedback-environment dt { color: var(--muted); font-size: 10px; }
.feedback-environment dd { min-width: 0; margin: 0; overflow-wrap: anywhere; font-size: 10px; }
.feedback-environment a { display: inline-flex; align-items: center; gap: 4px; color: var(--teal); }
.feedback-case-facts { display: flex; flex-direction: column; gap: 12px; }
.feedback-case-facts section { padding: 15px; background: var(--surface); border: 1px solid var(--line); border-radius: 6px; }
.feedback-case-facts dl { margin: 12px 0 0; }
.feedback-case-facts dl div { display: flex; min-height: 38px; align-items: center; justify-content: space-between; gap: 14px; border-bottom: 1px solid var(--line); }
.feedback-case-facts dl div:last-child { border-bottom: 0; }
.feedback-case-facts dt { color: var(--muted); font-size: 9px; }
.feedback-case-facts dd { margin: 0; max-width: 170px; overflow-wrap: anywhere; font-size: 9px; text-align: right; }
.feedback-case-facts a { color: var(--teal); font-weight: 700; }
.feedback-case-facts section > p { margin: 8px 0 0; color: var(--muted); font-size: 10px; line-height: 1.5; }
.feedback-toast { position: fixed; right: 20px; bottom: 20px; z-index: 90; display: flex; align-items: center; gap: 7px; margin: 0; padding: 10px 13px; background: #17342f; color: #fff; border-radius: 5px; box-shadow: var(--shadow-overlay); font-size: 11px; }
.feedback-issue-modal { width: min(560px, calc(100vw - 32px)); }
.feedback-issue-modal label span { color: var(--muted); font-size: 10px; font-weight: 700; }
.feedback-issue-modal textarea { min-height: 130px; padding: 10px; resize: vertical; font: inherit; font-size: 11px; line-height: 1.5; }
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
