<script setup lang="ts">
import {
  AlertTriangle,
  CheckCircle2,
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
} from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { triggerSignedDownload } from '~/utils/download'
import { formatDateTime } from '~/utils/format'

const { $apiFetch } = useNuxtApp()
const { can, canWrite } = useAuth()
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
  return value.replaceAll('_', ' ')
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
  return `${amount.toFixed(amount >= 10 ? 0 : 1)} ${units[index]}`
}

async function runAction(key: string, operation: () => Promise<void>) {
  if (actionPending.value) return
  actionPending.value = key
  actionError.value = ''
  actionNotice.value = ''
  try {
    await operation()
  } catch (operationError) {
    actionError.value = apiErrorMessage(operationError, 'The privacy action could not be completed.')
  } finally {
    actionPending.value = ''
  }
}

async function createWorkspaceExport() {
  if (!canWrite('tenant:update')) return
  await runAction('create-export', async () => {
    const response = await $apiFetch<{ created: boolean }>('/v1/privacy/exports', { method: 'POST' })
    actionNotice.value = response.created
      ? 'Workspace export queued.'
      : 'An active workspace export already exists.'
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
    actionNotice.value = 'Privacy job queued again.'
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
    actionNotice.value = `Verification email queued for ${email}.`
    anonymizeDialogOpen.value = false
    anonymizeConfirmation.value = ''
    reporterEmail.value = ''
    reporterAction.value = 'export'
    await refresh()
  })
}
</script>

<template>
  <div class="privacy-settings">
    <section v-if="status === 'pending'" class="app-surface app-state">
      <div><span class="app-spinner" /><h2>Loading data controls</h2></div>
    </section>
    <section v-else-if="error" class="app-surface app-state">
      <div><AlertTriangle :size="24" /><h2>Data controls unavailable</h2><p>{{ apiErrorMessage(error) }}</p><button class="app-button" @click="refresh()">Try again</button></div>
    </section>
    <template v-else-if="data">
      <p v-if="actionError" class="privacy-alert privacy-alert--error" role="alert"><AlertTriangle :size="15" />{{ actionError }}</p>
      <p v-if="actionNotice" class="privacy-alert privacy-alert--success" role="status"><CheckCircle2 :size="15" />{{ actionNotice }}</p>

      <section class="app-surface privacy-section">
        <header>
          <div><p class="app-eyebrow">Effective policy</p><h2>Retention schedule</h2></div>
          <span v-if="data.retention" class="privacy-plan">{{ data.retention.plan }} plan</span>
        </header>
        <div v-if="!canReadRetention" class="privacy-empty"><ShieldCheck :size="22" /><h3>Workspace read access required</h3></div>
        <div v-else class="privacy-table privacy-retention-table">
          <div class="privacy-row privacy-row--head"><span>Data category</span><span>Retention</span><span>Source</span></div>
          <div v-for="policy in data.retention?.policies || []" :key="policy.category" class="privacy-row">
            <span><b>{{ policy.label }}</b><small>{{ policy.category.replaceAll('_', ' ') }}</small></span>
            <span>{{ policy.duration }}</span>
            <span>{{ policy.source }}</span>
          </div>
        </div>
      </section>

      <section class="app-surface privacy-section">
        <header>
          <div><p class="app-eyebrow">Workspace portability</p><h2>Workspace exports</h2></div>
          <button v-if="canWrite('tenant:update')" class="app-button app-button--primary" :disabled="Boolean(actionPending)" @click="createWorkspaceExport">
            <FileArchive :size="15" />{{ actionPending === 'create-export' ? 'Queuing...' : 'Generate export' }}
          </button>
        </header>
        <div v-if="!canManagePrivacy" class="privacy-empty"><ShieldCheck :size="22" /><h3>Workspace administration required</h3></div>
        <div v-else-if="!data.exports.length" class="privacy-empty"><FileArchive :size="22" /><h3>No workspace exports</h3><p>Exports are encrypted in transit and expire after seven days.</p></div>
        <div v-else class="privacy-table privacy-export-table">
          <div class="privacy-row privacy-row--head"><span>Created</span><span>Status</span><span>Archive</span><span>Expires</span><span /></div>
          <div v-for="item in data.exports" :key="item.id" class="privacy-row">
            <span><b>{{ formatDateTime(item.created_at) }}</b><small>{{ item.attempts }} attempt{{ item.attempts === 1 ? '' : 's' }}</small></span>
            <span class="privacy-status" :data-tone="statusTone(item.status)"><i />{{ statusLabel(item.status) }}</span>
            <span>{{ formatBytes(item.object_size_bytes) }}</span>
            <span>{{ item.expires_at ? formatDateTime(item.expires_at) : '—' }}</span>
            <span class="privacy-actions">
              <button v-if="item.status === 'completed'" class="app-icon-button" title="Download workspace export" :disabled="Boolean(actionPending)" @click="downloadWorkspaceExport(item)"><Download :size="16" /></button>
              <button v-if="item.status === 'failed' || item.status === 'expired'" class="app-icon-button" title="Retry workspace export" :disabled="Boolean(actionPending)" @click="retryJob(item.id)"><RefreshCw :size="16" /></button>
            </span>
          </div>
        </div>
      </section>

      <section class="app-surface privacy-section">
        <header><div><p class="app-eyebrow">Reporter rights</p><h2>Data requests</h2></div></header>
        <div v-if="!canManagePrivacy" class="privacy-empty"><ShieldCheck :size="22" /><h3>Workspace administration required</h3></div>
        <template v-else>
          <form class="privacy-request-form" @submit.prevent="startReporterRequest">
            <label>Reporter email<input v-model.trim="reporterEmail" class="app-input" type="email" maxlength="320" required placeholder="reporter@example.com" /></label>
            <label>Request type<select v-model="reporterAction" class="app-select"><option value="export">Export reporter data</option><option value="anonymize">Anonymize reporter data</option></select></label>
            <button class="app-button" :class="reporterAction === 'anonymize' ? 'privacy-danger-button' : 'app-button--primary'" type="submit" :disabled="Boolean(actionPending) || !reporterEmail">
              <ShieldAlert v-if="reporterAction === 'anonymize'" :size="15" /><FileArchive v-else :size="15" />
              {{ actionPending === 'create-request' ? 'Queuing...' : reporterAction === 'anonymize' ? 'Review anonymization' : 'Send verification' }}
            </button>
          </form>
          <div v-if="!data.requests.length" class="privacy-empty privacy-empty--compact"><ShieldCheck :size="22" /><h3>No reporter requests</h3><p>The reporter must verify ownership before any data is exported or changed.</p></div>
          <div v-else class="privacy-table privacy-request-table">
            <div class="privacy-row privacy-row--head"><span>Reporter</span><span>Request</span><span>Status</span><span>Access</span><span /></div>
            <div v-for="item in data.requests" :key="item.id" class="privacy-row">
              <span><b>{{ item.reporter_email || 'Identity removed' }}</b><small>{{ formatDateTime(item.created_at) }}</small></span>
              <span class="privacy-action-label">{{ item.action }}</span>
              <span>
                <span class="privacy-status" :data-tone="statusTone(item.status)"><i />{{ statusLabel(item.status) }}</span>
                <small v-if="item.job_status && item.job_status !== item.status">Job: {{ statusLabel(item.job_status) }}</small>
              </span>
              <span>
                {{ item.access_expires_at ? formatDateTime(item.access_expires_at) : formatDateTime(item.verification_expires_at) }}
                <small v-if="item.manual_review_count">Manual review: {{ item.manual_review_count }}</small>
              </span>
              <span class="privacy-actions">
                <button v-if="item.job_id && (item.status === 'failed' || item.job_status === 'failed')" class="app-icon-button" title="Retry reporter request" :disabled="Boolean(actionPending)" @click="retryJob(item.job_id)"><RefreshCw :size="16" /></button>
              </span>
            </div>
          </div>
          <p class="privacy-review-note"><Clock3 :size="14" />Free-form text may still contain personal data. Completed anonymization requests report any records that require manual review.</p>
        </template>
      </section>
    </template>

    <AppDialog :open="anonymizeDialogOpen" aria-labelledby="privacy-anonymize-title" @close="anonymizeDialogOpen = false">
      <section class="privacy-dialog">
        <header><div><p class="app-eyebrow">Destructive data action</p><h2 id="privacy-anonymize-title">Confirm reporter anonymization</h2></div><button class="app-icon-button" title="Close" @click="anonymizeDialogOpen = false"><X :size="18" /></button></header>
        <form @submit.prevent="createReporterRequest">
          <p>This request irreversibly removes structured identity data after the reporter verifies ownership. Operational records remain, and free-form content may require manual review.</p>
          <label><span>Type <b>{{ reporterEmail }}</b> to confirm</span><input v-model.trim="anonymizeConfirmation" class="app-input" autocomplete="off" required autofocus /></label>
          <footer><button class="app-button" type="button" @click="anonymizeDialogOpen = false">Cancel</button><button class="app-button privacy-danger-button" type="submit" :disabled="Boolean(actionPending) || anonymizeConfirmation !== reporterEmail">{{ actionPending === 'create-request' ? 'Queuing...' : 'Send anonymization request' }}</button></footer>
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
.privacy-review-note { display: flex; min-height: 42px; align-items: center; gap: 8px; margin: 0; padding: 8px 15px; background: #fff9e9; color: #765d1d; border-top: 1px solid #eadcad; font-size: 9px; line-height: 1.5; }
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
