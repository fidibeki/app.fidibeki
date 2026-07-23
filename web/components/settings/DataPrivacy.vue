<script setup lang="ts">
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock3,
  Download,
  FileArchive,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  X,
} from '@lucide/vue'
import type {
  EffectiveRetention,
  PrivacyExport,
  ReporterPrivacyAction,
  ReporterPrivacyRequest,
  RetentionPolicy,
  ReviewDecisionStatus,
  ReviewItem,
} from '~/types/api'
import { triggerSignedDownload } from '~/utils/download'

const { $apiFetch } = useNuxtApp()
const { can, canWrite } = useAuth()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { dateTime, decimal, plural } = useRegionalFormat()
const canReadRetention = computed(() => can('tenant:read'))
const canManagePrivacy = computed(() => can('tenant:update'))

const { data, status, error, refresh } = await useAsyncData('privacy-settings-data', async () => {
  const retention = canReadRetention.value
    ? $apiFetch<{ retention: EffectiveRetention }>('/v1/privacy/retention')
    : Promise.resolve<{ retention?: EffectiveRetention }>({})
  const exports = canManagePrivacy.value
    ? $apiFetch<{ exports: PrivacyExport[] }>('/v1/privacy/exports')
    : Promise.resolve({ exports: [] as PrivacyExport[] })
  const requests = canManagePrivacy.value
    ? $apiFetch<{ requests: ReporterPrivacyRequest[] }>('/v1/privacy/requests')
    : Promise.resolve({ requests: [] as ReporterPrivacyRequest[] })
  const [retentionResult, exportsResult, requestsResult] = await Promise.all([retention, exports, requests])
  return {
    retention: retentionResult.retention,
    exports: exportsResult.exports,
    requests: requestsResult.requests,
  }
})

const actionPending = ref('')
const actionError = ref('')
const actionNotice = ref('')
const reporterEmail = ref('')
const reporterAction = ref<ReporterPrivacyAction>('export')
const anonymizeDialogOpen = ref(false)
const anonymizeConfirmation = ref('')

function statusLabel(value: string) {
  return t(`admin.statuses.${value}`)
}

function statusTone(value: string) {
  if (value === 'completed') return 'success'
  if (value === 'failed' || value === 'expired') return 'danger'
  return 'pending'
}

function formatBytes(value?: number) {
  if (!value) return '—'
  if (value < 1024) return `${value} B`
  const units = ['KB', 'MB', 'GB']
  let amount = value / 1024
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index += 1
  }
  return `${decimal(amount, { maximumFractionDigits: amount >= 10 ? 0 : 1 })} ${units[index]}`
}

function retentionLabel(policy: RetentionPolicy) {
  return t(`admin.privacy.settings.category.${policy.category}`)
}

function retentionDuration(policy: RetentionPolicy) {
  if (policy.category === 'workflow_metadata') return t('admin.privacy.settings.duration.workspaceLifetime')
  if (policy.category === 'unverified_requests') {
    return plural('admin.privacy.settings.duration.hours', 48, { count: 48 })
  }
  if (policy.duration_days != null) {
    return plural('admin.privacy.settings.duration.days', policy.duration_days, { count: policy.duration_days })
  }
  return t('admin.common.custom')
}

function retentionSource(source: string) {
  if (source === 'Workspace lifecycle') return t('admin.privacy.settings.source.workspaceLifecycle')
  if (source === 'Plan storage limit') return t('admin.privacy.settings.source.planStorageLimit')
  return t('admin.privacy.settings.source.platformDefault')
}

function actionLabel(action: ReporterPrivacyAction) {
  return t(action === 'anonymize'
    ? 'admin.privacy.settings.actionAnonymize'
    : 'admin.privacy.settings.actionExport')
}

async function runAction(key: string, operation: () => Promise<void>) {
  if (actionPending.value) return
  actionPending.value = key
  actionError.value = ''
  actionNotice.value = ''
  try {
    await operation()
  } catch (operationError) {
    actionError.value = localizedError.message(operationError, 'admin.privacy.settings.error')
  } finally {
    actionPending.value = ''
  }
}

async function createWorkspaceExport() {
  if (!canWrite('tenant:update')) return
  await runAction('create-export', async () => {
    const response = await $apiFetch<{ created: boolean }>('/v1/privacy/exports', { method: 'POST' })
    actionNotice.value = response.created
      ? t('admin.privacy.settings.notices.exportQueued')
      : t('admin.privacy.settings.notices.exportExists')
    await refresh()
  })
}

async function downloadWorkspaceExport(item: PrivacyExport) {
  await runAction(`download:${item.id}`, async () => {
    const response = await $apiFetch<{ url: string }>(`/v1/privacy/exports/${item.id}/download`, { method: 'POST' })
    triggerSignedDownload(response.url, 'fidibeki-workspace-export.zip')
  })
}

async function retryJob(jobID: string) {
  if (!canWrite('tenant:update')) return
  await runAction(`retry:${jobID}`, async () => {
    await $apiFetch(`/v1/privacy/exports/${jobID}/retry`, { method: 'POST' })
    actionNotice.value = t('admin.privacy.settings.notices.retryQueued')
    await refresh()
  })
}

async function startReporterRequest() {
  if (!reporterEmail.value.trim() || !canWrite('tenant:update')) return
  if (reporterAction.value === 'anonymize') {
    anonymizeConfirmation.value = ''
    anonymizeDialogOpen.value = true
    return
  }
  await createReporterRequest()
}

async function createReporterRequest() {
  const email = reporterEmail.value.trim()
  if (!email || !canWrite('tenant:update')) return
  await runAction('create-request', async () => {
    await $apiFetch('/v1/privacy/requests', {
      method: 'POST',
      body: { email, action: reporterAction.value },
    })
    actionNotice.value = t('admin.privacy.settings.notices.verificationQueued', { email })
    anonymizeDialogOpen.value = false
    anonymizeConfirmation.value = ''
    reporterEmail.value = ''
    reporterAction.value = 'export'
    await refresh()
  })
}

const reviewExpanded = ref<Record<string, boolean>>({})
const reviewItems = ref<Record<string, ReviewItem[]>>({})
const reviewReasons = ref<Record<string, string>>({})

function resourceLabel(item: ReviewItem) {
  return t(`admin.privacy.settings.review.resource.${item.resource_type}`)
}

function fieldsLabel(item: ReviewItem) {
  return item.fields.map(field => t(`admin.privacy.settings.review.field.${field}`, field)).join(', ')
}

function disclosedDestinationsLabel(item: ReviewItem) {
  const destinations = item.disclosed_destinations
  if (!destinations) return ''
  return Object.entries(destinations)
    .map(([kind, count]) => `${t(`admin.privacy.settings.review.destination.${kind}`, kind)} (${count})`)
    .join(', ')
}

async function loadReviewItems(request: ReporterPrivacyRequest) {
  const response = await $apiFetch<{ review_items: ReviewItem[] }>(
    `/v1/privacy/requests/${request.id}/review-items`,
  )
  reviewItems.value = { ...reviewItems.value, [request.id]: response.review_items }
}

async function toggleReview(request: ReporterPrivacyRequest) {
  const isOpen = Boolean(reviewExpanded.value[request.id])
  if (isOpen) {
    reviewExpanded.value = { ...reviewExpanded.value, [request.id]: false }
    return
  }
  await runAction(`review:${request.id}`, async () => {
    await loadReviewItems(request)
    reviewExpanded.value = { ...reviewExpanded.value, [request.id]: true }
  })
}

async function decideReviewItem(request: ReporterPrivacyRequest, item: ReviewItem, status: ReviewDecisionStatus) {
  if (!canWrite('tenant:update')) return
  const reason = (reviewReasons.value[item.id] || '').trim()
  if (status === 'retained_exception' && !reason) {
    actionError.value = t('admin.privacy.settings.review.reasonRequired')
    return
  }
  await runAction(`decide:${item.id}`, async () => {
    await $apiFetch(`/v1/privacy/requests/${request.id}/review-items/${item.id}/decide`, {
      method: 'POST',
      body: { status, reason },
    })
    await loadReviewItems(request)
    await refresh()
  })
}

async function finalizeRequest(request: ReporterPrivacyRequest) {
  if (!canWrite('tenant:update')) return
  await runAction(`finalize:${request.id}`, async () => {
    const response = await $apiFetch<{ completed: boolean, pending: number }>(
      `/v1/privacy/requests/${request.id}/finalize`,
      { method: 'POST' },
    )
    actionNotice.value = response.completed
      ? t('admin.privacy.settings.notices.reviewCompleted')
      : t('admin.privacy.settings.notices.reviewPending', { count: response.pending })
    await loadReviewItems(request)
    await refresh()
  })
}
</script>

<template>
  <div class="privacy-settings">
    <section v-if="status === 'pending'" class="app-surface app-state">
      <div><span class="app-spinner" /><h2>{{ t('admin.privacy.settings.loading') }}</h2></div>
    </section>
    <section v-else-if="error" class="app-surface app-state">
      <div><AlertTriangle :size="24" /><h2>{{ t('admin.privacy.settings.unavailable') }}</h2><p>{{ localizedError.message(error, 'admin.privacy.settings.error') }}</p><button class="app-button" @click="refresh()">{{ t('admin.common.tryAgain') }}</button></div>
    </section>
    <template v-else-if="data">
      <p v-if="actionError" class="privacy-alert privacy-alert--error" role="alert"><AlertTriangle :size="15" />{{ actionError }}</p>
      <p v-if="actionNotice" class="privacy-alert privacy-alert--success" role="status"><CheckCircle2 :size="15" />{{ actionNotice }}</p>

      <section class="app-surface privacy-section">
        <header>
          <div><p class="app-eyebrow">{{ t('admin.privacy.settings.policyEyebrow') }}</p><h2>{{ t('admin.privacy.settings.retention') }}</h2></div>
          <span v-if="data.retention" class="privacy-plan">{{ t('admin.privacy.settings.plan', { plan: t(`admin.plans.${data.retention.plan}`) }) }}</span>
        </header>
        <div v-if="!canReadRetention" class="privacy-empty"><ShieldCheck :size="22" /><h3>{{ t('admin.privacy.settings.readRequired') }}</h3></div>
        <div v-else class="privacy-table privacy-retention-table">
          <div class="privacy-row privacy-row--head"><span>{{ t('admin.privacy.settings.columns.category') }}</span><span>{{ t('admin.privacy.settings.columns.retention') }}</span><span>{{ t('admin.privacy.settings.columns.source') }}</span></div>
          <div v-for="policy in data.retention?.policies || []" :key="policy.category" class="privacy-row">
            <span><b>{{ retentionLabel(policy) }}</b><small>{{ policy.category }}</small></span>
            <span>{{ retentionDuration(policy) }}</span>
            <span>{{ retentionSource(policy.source) }}</span>
          </div>
        </div>
      </section>

      <section class="app-surface privacy-section">
        <header>
          <div><p class="app-eyebrow">{{ t('admin.privacy.settings.portabilityEyebrow') }}</p><h2>{{ t('admin.privacy.settings.exports') }}</h2></div>
          <button v-if="canWrite('tenant:update')" class="app-button app-button--primary" :disabled="Boolean(actionPending)" @click="createWorkspaceExport">
            <FileArchive :size="15" />{{ actionPending === 'create-export' ? t('admin.common.queuing') : t('admin.privacy.settings.generateExport') }}
          </button>
        </header>
        <div v-if="!canManagePrivacy" class="privacy-empty"><ShieldCheck :size="22" /><h3>{{ t('admin.privacy.settings.administrationRequired') }}</h3></div>
        <div v-else-if="!data.exports.length" class="privacy-empty"><FileArchive :size="22" /><h3>{{ t('admin.privacy.settings.noExports') }}</h3><p>{{ t('admin.privacy.settings.exportsDescription') }}</p></div>
        <div v-else class="privacy-table privacy-export-table">
          <div class="privacy-row privacy-row--head"><span>{{ t('admin.privacy.settings.columns.created') }}</span><span>{{ t('admin.privacy.settings.columns.status') }}</span><span>{{ t('admin.privacy.settings.columns.archive') }}</span><span>{{ t('admin.privacy.settings.columns.expires') }}</span><span /></div>
          <div v-for="item in data.exports" :key="item.id" class="privacy-row">
            <span><b>{{ dateTime(item.created_at) }}</b><small>{{ plural('admin.common.attempts', item.attempts, { count: item.attempts }) }}</small></span>
            <span class="privacy-status" :data-tone="statusTone(item.status)"><i />{{ statusLabel(item.status) }}</span>
            <span>{{ formatBytes(item.object_size_bytes) }}</span>
            <span>{{ item.expires_at ? dateTime(item.expires_at) : '—' }}</span>
            <span class="privacy-actions">
              <button v-if="item.status === 'completed'" class="app-icon-button" :title="t('admin.privacy.settings.downloadExport')" :disabled="Boolean(actionPending)" @click="downloadWorkspaceExport(item)"><Download :size="16" /></button>
              <button v-if="item.status === 'failed' || item.status === 'expired'" class="app-icon-button" :title="t('admin.privacy.settings.retryExport')" :disabled="Boolean(actionPending)" @click="retryJob(item.id)"><RefreshCw :size="16" /></button>
            </span>
          </div>
        </div>
      </section>

      <section class="app-surface privacy-section">
        <header><div><p class="app-eyebrow">{{ t('admin.privacy.settings.reporterEyebrow') }}</p><h2>{{ t('admin.privacy.settings.requests') }}</h2></div></header>
        <div v-if="!canManagePrivacy" class="privacy-empty"><ShieldCheck :size="22" /><h3>{{ t('admin.privacy.settings.administrationRequired') }}</h3></div>
        <template v-else>
          <form class="privacy-request-form" @submit.prevent="startReporterRequest">
            <label>{{ t('admin.privacy.settings.reporterEmail') }}<input v-model.trim="reporterEmail" class="app-input" type="email" maxlength="320" required placeholder="reporter@example.com" /></label>
            <label>{{ t('admin.privacy.settings.requestType') }}<select v-model="reporterAction" class="app-select"><option value="export">{{ t('admin.privacy.settings.exportReporter') }}</option><option value="anonymize">{{ t('admin.privacy.settings.anonymizeReporter') }}</option></select></label>
            <button class="app-button" :class="reporterAction === 'anonymize' ? 'privacy-danger-button' : 'app-button--primary'" type="submit" :disabled="Boolean(actionPending) || !reporterEmail">
              <ShieldAlert v-if="reporterAction === 'anonymize'" :size="15" /><FileArchive v-else :size="15" />
              {{ actionPending === 'create-request' ? t('admin.common.queuing') : reporterAction === 'anonymize' ? t('admin.privacy.settings.reviewAnonymization') : t('admin.privacy.settings.sendVerification') }}
            </button>
          </form>
          <div v-if="!data.requests.length" class="privacy-empty privacy-empty--compact"><ShieldCheck :size="22" /><h3>{{ t('admin.privacy.settings.noRequests') }}</h3><p>{{ t('admin.privacy.settings.noRequestsDescription') }}</p></div>
          <div v-else class="privacy-table privacy-request-table">
            <div class="privacy-row privacy-row--head"><span>{{ t('admin.privacy.settings.columns.reporter') }}</span><span>{{ t('admin.privacy.settings.columns.request') }}</span><span>{{ t('admin.privacy.settings.columns.status') }}</span><span>{{ t('admin.privacy.settings.columns.access') }}</span><span /></div>
            <template v-for="item in data.requests" :key="item.id">
              <div class="privacy-row">
                <span><b>{{ item.reporter_email || t('admin.privacy.settings.identityRemoved') }}</b><small>{{ dateTime(item.created_at) }}</small></span>
                <span class="privacy-action-label">{{ actionLabel(item.action) }}</span>
                <span>
                  <span class="privacy-status" :data-tone="statusTone(item.status)"><i />{{ statusLabel(item.status) }}</span>
                  <small v-if="item.job_status && item.job_status !== item.status">{{ t('admin.privacy.settings.jobStatus', { status: statusLabel(item.job_status) }) }}</small>
                </span>
                <span>
                  {{ item.access_expires_at ? dateTime(item.access_expires_at) : dateTime(item.verification_expires_at) }}
                  <small v-if="item.manual_review_count">{{ t('admin.privacy.settings.manualReview', { count: item.manual_review_count }) }}</small>
                </span>
                <span class="privacy-actions">
                  <button v-if="item.job_id && (item.status === 'failed' || item.job_status === 'failed')" class="app-icon-button" :title="t('admin.privacy.settings.retryRequest')" :disabled="Boolean(actionPending)" @click="retryJob(item.job_id)"><RefreshCw :size="16" /></button>
                  <button v-if="item.status === 'awaiting_manual_review'" class="app-icon-button" :title="t('admin.privacy.settings.review.toggle')" :disabled="Boolean(actionPending)" @click="toggleReview(item)">
                    <ChevronUp v-if="reviewExpanded[item.id]" :size="16" /><ChevronDown v-else :size="16" />
                  </button>
                </span>
              </div>
              <div v-if="item.status === 'awaiting_manual_review' && reviewExpanded[item.id]" class="privacy-review-queue">
                <header>
                  <h3>{{ t('admin.privacy.settings.review.title') }}</h3>
                  <button class="app-button" :disabled="Boolean(actionPending)" @click="finalizeRequest(item)">
                    {{ actionPending === `finalize:${item.id}` ? t('admin.common.queuing') : t('admin.privacy.settings.review.finalize') }}
                  </button>
                </header>
                <p v-if="!(reviewItems[item.id] || []).length" class="privacy-review-queue-empty">{{ t('admin.privacy.settings.review.empty') }}</p>
                <div v-for="reviewItem in reviewItems[item.id] || []" :key="reviewItem.id" class="privacy-review-item">
                  <div class="privacy-review-item-meta">
                    <b>{{ resourceLabel(reviewItem) }}</b>
                    <span>{{ fieldsLabel(reviewItem) }}</span>
                    <small v-if="disclosedDestinationsLabel(reviewItem)">{{ t('admin.privacy.settings.review.disclosedTo', { destinations: disclosedDestinationsLabel(reviewItem) }) }}</small>
                  </div>
                  <template v-if="reviewItem.status === 'pending'">
                    <input
                      v-model.trim="reviewReasons[reviewItem.id]"
                      class="app-input privacy-review-reason"
                      :placeholder="t('admin.privacy.settings.review.reasonPlaceholder')"
                      maxlength="2000"
                    />
                    <div class="privacy-review-item-actions">
                      <button class="app-button" :disabled="Boolean(actionPending)" @click="decideReviewItem(item, reviewItem, 'redacted')">{{ t('admin.privacy.settings.review.decisionRedacted') }}</button>
                      <button class="app-button" :disabled="Boolean(actionPending)" @click="decideReviewItem(item, reviewItem, 'deleted')">{{ t('admin.privacy.settings.review.decisionDeleted') }}</button>
                      <button class="app-button" :disabled="Boolean(actionPending)" @click="decideReviewItem(item, reviewItem, 'not_personal_data')">{{ t('admin.privacy.settings.review.decisionNotPersonalData') }}</button>
                      <button class="app-button privacy-danger-button" :disabled="Boolean(actionPending)" @click="decideReviewItem(item, reviewItem, 'retained_exception')">{{ t('admin.privacy.settings.review.decisionRetainedException') }}</button>
                    </div>
                  </template>
                  <div v-else class="privacy-review-item-decided">
                    <span class="privacy-status" :data-tone="reviewItem.status === 'retained_exception' ? 'pending' : 'success'">{{ t(`admin.privacy.settings.review.decision.${reviewItem.status}`) }}</span>
                    <small v-if="reviewItem.decision_reason">{{ reviewItem.decision_reason }}</small>
                  </div>
                </div>
              </div>
            </template>
          </div>
          <p class="privacy-review-note"><Clock3 :size="14" />{{ t('admin.privacy.settings.reviewNote') }}</p>
        </template>
      </section>
    </template>

    <AppDialog :open="anonymizeDialogOpen" aria-labelledby="privacy-anonymize-title" @close="anonymizeDialogOpen = false">
      <section class="privacy-dialog">
        <header><div><p class="app-eyebrow">{{ t('admin.privacy.settings.destructiveEyebrow') }}</p><h2 id="privacy-anonymize-title">{{ t('admin.privacy.settings.confirmAnonymization') }}</h2></div><button class="app-icon-button" :title="t('admin.common.close')" @click="anonymizeDialogOpen = false"><X :size="18" /></button></header>
        <form @submit.prevent="createReporterRequest">
          <p>{{ t('admin.privacy.settings.anonymizationDescription') }}</p>
          <label><span>{{ t('admin.privacy.settings.typeToConfirm', { value: reporterEmail }) }}</span><input v-model.trim="anonymizeConfirmation" class="app-input" autocomplete="off" required autofocus /></label>
          <footer><button class="app-button" type="button" @click="anonymizeDialogOpen = false">{{ t('admin.common.cancel') }}</button><button class="app-button privacy-danger-button" type="submit" :disabled="Boolean(actionPending) || anonymizeConfirmation !== reporterEmail">{{ actionPending === 'create-request' ? t('admin.common.queuing') : t('admin.privacy.settings.sendAnonymization') }}</button></footer>
        </form>
      </section>
    </AppDialog>
  </div>
</template>

<style scoped>
.privacy-settings { display: grid; gap: 16px; }
.privacy-section { overflow: hidden; }
.privacy-section > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; gap: 14px; padding: 11px 16px; border-bottom: 1px solid var(--line); }
.privacy-section h2 { margin: 3px 0 0; font-size: 14px; }
.privacy-plan { color: var(--teal); font-size: 9px; font-weight: 800; text-transform: uppercase; }
.privacy-alert { display: flex; min-height: 38px; align-items: center; gap: 8px; margin: 0; padding: 8px 11px; border-radius: 4px; font-size: 10px; font-weight: 700; }
.privacy-alert--error { background: var(--red-soft); color: var(--red); }
.privacy-alert--success { background: var(--green-soft); color: var(--green); }
.privacy-table { width: 100%; }
.privacy-row { display: grid; min-height: 60px; align-items: center; gap: 14px; padding: 7px 15px; border-bottom: 1px solid var(--line); color: var(--muted); font-size: 10px; }
.privacy-row:last-child { border-bottom: 0; }
.privacy-row--head { min-height: 36px; padding-top: 0; padding-bottom: 0; background: #f8faf9; font-size: 9px; font-weight: 750; text-transform: uppercase; }
.privacy-row > span { min-width: 0; }
.privacy-row b { display: block; overflow: hidden; color: var(--ink-soft); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.privacy-row small { display: block; margin-top: 3px; overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.privacy-retention-table .privacy-row { grid-template-columns: minmax(220px, 1.3fr) minmax(110px, .5fr) minmax(140px, .7fr); }
.privacy-export-table .privacy-row { grid-template-columns: minmax(190px, 1.1fr) 110px 90px minmax(170px, .9fr) 38px; }
.privacy-request-table .privacy-row { grid-template-columns: minmax(220px, 1.2fr) 90px 140px minmax(180px, .9fr) 38px; }
.privacy-status { display: inline-flex; align-items: center; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 700; text-transform: capitalize; }
.privacy-status i { width: 7px; height: 7px; background: #b98a26; border-radius: 50%; }
.privacy-status[data-tone='success'] i { background: var(--green); }
.privacy-status[data-tone='danger'] i { background: var(--red); }
.privacy-action-label { color: var(--ink-soft); font-weight: 700; text-transform: capitalize; }
.privacy-actions { display: flex; justify-content: flex-end; }
.privacy-request-form { display: grid; grid-template-columns: minmax(220px, 1.4fr) minmax(190px, .8fr) auto; align-items: end; gap: 12px; padding: 14px 15px; background: var(--surface-soft); border-bottom: 1px solid var(--line); }
.privacy-request-form label, .privacy-dialog label { display: flex; flex-direction: column; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.privacy-request-form .app-button { white-space: nowrap; }
.privacy-danger-button { background: var(--red); color: #fff; border-color: var(--red); }
.privacy-danger-button:hover:not(:disabled) { background: #962f3a; color: #fff; border-color: #962f3a; }
.privacy-review-note { display: flex; min-height: 42px; align-items: center; gap: 8px; margin: 0; padding: 8px 15px; background: #fff9e9; color: #765d1d; border-top: 1px solid #eadcad; font-size: 9px; line-height: 1.5; }
.privacy-review-queue { display: grid; gap: 10px; padding: 14px 15px; background: #fff9e9; border-top: 1px solid #eadcad; border-bottom: 1px solid #eadcad; }
.privacy-review-queue > header { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.privacy-review-queue h3 { margin: 0; color: #765d1d; font-size: 11px; }
.privacy-review-queue-empty { margin: 0; color: #765d1d; font-size: 10px; }
.privacy-review-item { display: grid; gap: 8px; padding: 10px; background: #fff; border: 1px solid #eadcad; border-radius: 4px; }
.privacy-review-item-meta { display: flex; flex-wrap: wrap; align-items: baseline; gap: 8px; font-size: 10px; }
.privacy-review-item-meta b { color: var(--ink-soft); }
.privacy-review-item-meta span { color: var(--muted); }
.privacy-review-item-meta small { flex-basis: 100%; color: var(--muted); }
.privacy-review-reason { font-size: 10px; }
.privacy-review-item-actions { display: flex; flex-wrap: wrap; gap: 6px; }
.privacy-review-item-actions .app-button { padding: 0 10px; font-size: 9px; }
.privacy-review-item-decided { display: flex; align-items: center; gap: 8px; }
.privacy-review-item-decided small { color: var(--muted); font-size: 10px; }
.privacy-empty { display: grid; min-height: 190px; place-items: center; align-content: center; gap: 7px; padding: 20px; color: var(--muted); text-align: center; }
.privacy-empty--compact { min-height: 130px; }
.privacy-empty h3, .privacy-empty p { margin: 0; }
.privacy-empty h3 { color: var(--ink-soft); font-size: 12px; }
.privacy-empty p { max-width: 420px; font-size: 9px; line-height: 1.5; }
.privacy-dialog { width: min(560px, 100%); overflow: hidden; background: #fff; border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.privacy-dialog > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; gap: 14px; padding: 10px 15px; border-bottom: 1px solid var(--line); }
.privacy-dialog h2 { margin: 3px 0 0; font-size: 15px; }
.privacy-dialog form { display: grid; gap: 15px; padding: 16px; }
.privacy-dialog form > p { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.65; }
.privacy-dialog label b { color: var(--red); }
.privacy-dialog footer { display: flex; justify-content: flex-end; gap: 8px; margin: 1px -16px -16px; padding: 12px 16px; background: #f8faf9; border-top: 1px solid var(--line); }
@media (max-width: 900px) {
  .privacy-row--head { display: none; }
  .privacy-retention-table .privacy-row { grid-template-columns: minmax(0, 1fr) auto; }
  .privacy-retention-table .privacy-row > span:last-child { display: none; }
  .privacy-export-table .privacy-row, .privacy-request-table .privacy-row { grid-template-columns: minmax(0, 1fr) auto 38px; }
  .privacy-export-table .privacy-row > span:nth-child(3), .privacy-export-table .privacy-row > span:nth-child(4), .privacy-request-table .privacy-row > span:nth-child(4) { display: none; }
  .privacy-request-form { grid-template-columns: 1fr 1fr; }
  .privacy-request-form .app-button { grid-column: 1 / -1; justify-self: end; }
}
@media (max-width: 650px) {
  .privacy-section > header { align-items: flex-start; flex-direction: column; }
  .privacy-section > header .app-button { width: 100%; }
  .privacy-row b { overflow: visible; line-height: 1.35; text-overflow: clip; white-space: normal; }
  .privacy-retention-table .privacy-row { grid-template-columns: minmax(0, 1fr) auto; }
  .privacy-retention-table .privacy-row > span:last-child { display: none; }
  .privacy-export-table .privacy-row, .privacy-request-table .privacy-row { grid-template-columns: minmax(0, 1fr) 38px; }
  .privacy-export-table .privacy-row > span:not(:first-child):not(:last-child), .privacy-request-table .privacy-row > span:not(:first-child):not(:last-child) { display: none; }
  .privacy-request-form { grid-template-columns: 1fr; }
  .privacy-request-form .app-button { grid-column: auto; width: 100%; }
  .privacy-dialog footer { flex-direction: column-reverse; }
  .privacy-dialog footer .app-button { width: 100%; }
}
</style>
