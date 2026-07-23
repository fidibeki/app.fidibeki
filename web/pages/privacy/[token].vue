<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Clock3, Download, FileSearch, ShieldCheck } from '@lucide/vue'
import type { ReporterPrivacyView } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { triggerSignedDownload } from '~/utils/download'
import { formatDateTime } from '~/utils/format'

definePageMeta({ layout: false })

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const token = computed(() => String(route.params.token || ''))
const request = ref<ReporterPrivacyView | null>(null)
const loading = ref(true)
const unavailable = ref(false)
const transientError = ref('')
const downloading = ref(false)
let pollTimer: ReturnType<typeof setTimeout> | undefined

const isProcessing = computed(() => request.value?.status === 'processing' ||
  request.value?.job_status === 'pending' || request.value?.job_status === 'processing')

function statusLabel(value: string) {
  return value.replaceAll('_', ' ')
}

function schedulePoll() {
  if (!isProcessing.value) return
  pollTimer = setTimeout(loadStatus, 4000)
}

async function verify() {
  loading.value = true
  unavailable.value = false
  transientError.value = ''
  try {
    const response = await $apiFetch<{ request: ReporterPrivacyView }>(
      `/v1/privacy/requests/${encodeURIComponent(token.value)}/verify`,
      { method: 'POST' },
    )
    request.value = response.request
    schedulePoll()
  } catch (verifyError: unknown) {
    const statusCode = typeof verifyError === 'object' && verifyError && 'statusCode' in verifyError
      ? Number((verifyError as { statusCode?: number }).statusCode)
      : 0
    unavailable.value = statusCode === 404
    if (!unavailable.value) transientError.value = apiErrorMessage(verifyError, 'The data request could not be verified.')
  } finally {
    loading.value = false
  }
}

async function loadStatus() {
  try {
    const response = await $apiFetch<{ request: ReporterPrivacyView }>(
      `/v1/privacy/requests/${encodeURIComponent(token.value)}`,
    )
    request.value = response.request
    transientError.value = ''
    schedulePoll()
  } catch (statusError: unknown) {
    const statusCode = typeof statusError === 'object' && statusError && 'statusCode' in statusError
      ? Number((statusError as { statusCode?: number }).statusCode)
      : 0
    if (statusCode === 404) unavailable.value = true
    else transientError.value = apiErrorMessage(statusError, 'The latest request status could not be loaded.')
  }
}

async function downloadExport() {
  if (!request.value?.download_available || downloading.value) return
  downloading.value = true
  transientError.value = ''
  try {
    const response = await $apiFetch<{ url: string }>(
      `/v1/privacy/requests/${encodeURIComponent(token.value)}/download`,
      { method: 'POST' },
    )
    triggerSignedDownload(response.url, 'fidibeki-reporter-data.zip')
  } catch (downloadError) {
    transientError.value = apiErrorMessage(downloadError, 'Your data archive could not be downloaded.')
  } finally {
    downloading.value = false
  }
}

onMounted(verify)
onBeforeUnmount(() => {
  if (pollTimer) clearTimeout(pollTimer)
})

useHead({
  title: 'Data request - Fidibeki',
  meta: [
    { name: 'robots', content: 'noindex,nofollow' },
    { name: 'referrer', content: 'no-referrer' },
  ],
})
</script>

<template>
  <div class="privacy-page">
    <header class="privacy-brand"><span>F</span><b>Fidibeki</b></header>
    <main>
      <section v-if="loading" class="privacy-state" aria-live="polite"><span class="privacy-spinner" /><h1>Verifying data request</h1><p>This normally takes only a moment.</p></section>
      <section v-else-if="unavailable" class="privacy-state"><AlertTriangle :size="28" /><h1>Data request unavailable</h1><p>This link is invalid, expired, or no longer available.</p></section>
      <section v-else-if="!request" class="privacy-state"><AlertTriangle :size="28" /><h1>Verification interrupted</h1><p>{{ transientError || 'The data request could not be verified.' }}</p><button class="privacy-button privacy-button--secondary" @click="verify">Try again</button></section>
      <template v-else>
        <header class="privacy-heading">
          <div><p>Reporter data rights</p><h1>{{ request.action === 'anonymize' ? 'Data anonymization request' : 'Data export request' }}</h1><span>Secure access expires {{ formatDateTime(request.access_expires_at) }}</span></div>
          <span class="privacy-status" :data-status="request.status">{{ statusLabel(request.status) }}</span>
        </header>

        <p v-if="transientError" class="privacy-error" role="alert"><AlertTriangle :size="15" />{{ transientError }}</p>

        <section v-if="isProcessing" class="privacy-result">
          <Clock3 :size="24" /><div><h2>Your request is processing</h2><p>The workspace is preparing the requested operation. This page refreshes automatically.</p></div>
        </section>
        <section v-else-if="request.status === 'completed' && request.action === 'export'" class="privacy-result">
          <FileSearch :size="24" /><div><h2>Your data archive is ready</h2><p>{{ request.record_count || 0 }} matching records were included. The download link is short-lived and this request expires {{ formatDateTime(request.access_expires_at) }}.</p><button class="privacy-button" :disabled="downloading || !request.download_available" @click="downloadExport"><Download :size="16" />{{ downloading ? 'Preparing...' : 'Download data archive' }}</button></div>
        </section>
        <section v-else-if="request.status === 'completed'" class="privacy-result">
          <CheckCircle2 :size="24" /><div><h2>Anonymization completed</h2><p>Structured reporter identity was removed from {{ request.record_count || 0 }} matching records.</p><p v-if="request.manual_review_count" class="privacy-review">{{ request.manual_review_count }} free-form record{{ request.manual_review_count === 1 ? '' : 's' }} may still contain personal data and require manual workspace review.</p></div>
        </section>
        <section v-else-if="request.status === 'failed'" class="privacy-result">
          <AlertTriangle :size="24" /><div><h2>The request needs attention</h2><p>The workspace administrator has been asked to retry this operation. This secure link remains valid until {{ formatDateTime(request.access_expires_at) }}.</p></div>
        </section>
        <section v-else class="privacy-result">
          <AlertTriangle :size="24" /><div><h2>Data request expired</h2><p>Ask the workspace administrator to create a new verified request.</p></div>
        </section>

        <section class="privacy-security"><ShieldCheck :size="18" /><div><h2>Verified access</h2><p>This page contains no account login and reveals only the status of this specific data request.</p></div></section>
      </template>
    </main>
    <footer>Data rights handled securely by Fidibeki</footer>
  </div>
</template>

<style scoped>
.privacy-page { min-height: 100vh; background: #f5f7f6; color: #18201f; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
.privacy-brand { display: flex; min-height: 62px; align-items: center; gap: 9px; padding: 0 max(20px, calc((100vw - 760px) / 2)); background: #fff; border-bottom: 1px solid #dce3e1; }
.privacy-brand > span { display: grid; width: 30px; height: 30px; place-items: center; background: #087f73; color: #fff; border-radius: 5px; font-size: 13px; font-weight: 850; }
.privacy-brand b { font-size: 14px; }
main { width: min(760px, calc(100% - 32px)); margin: 0 auto; padding: 48px 0 36px; }
.privacy-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; padding-bottom: 28px; border-bottom: 1px solid #dce3e1; }
.privacy-heading p { margin: 0 0 8px; color: #087f73; font-size: 11px; font-weight: 800; }
.privacy-heading h1 { max-width: 620px; margin: 0 0 10px; font-size: clamp(22px, 4vw, 32px); line-height: 1.2; }
.privacy-heading div > span { color: #66736f; font-size: 11px; }
.privacy-status { flex: 0 0 auto; padding: 5px 8px; background: #e7efed; color: #53615d; border-radius: 4px; font-size: 9px; font-weight: 800; text-transform: capitalize; }
.privacy-status[data-status='completed'] { background: #def4ec; color: #167d68; }
.privacy-status[data-status='failed'], .privacy-status[data-status='expired'] { background: #fce8ea; color: #b42335; }
.privacy-result { display: grid; grid-template-columns: 30px minmax(0, 1fr); gap: 12px; margin-top: 30px; padding: 20px; background: #fff; border: 1px solid #d4dcda; border-radius: 6px; }
.privacy-result > svg { color: #087f73; }
.privacy-result h2, .privacy-security h2 { margin: 1px 0 7px; font-size: 14px; }
.privacy-result p, .privacy-security p { margin: 0; color: #586662; font-size: 12px; line-height: 1.6; }
.privacy-result .privacy-review { margin-top: 10px; padding: 9px 10px; background: #fff9e9; color: #765d1d; border: 1px solid #eadcad; border-radius: 4px; font-size: 10px; }
.privacy-button { display: inline-flex; min-height: 38px; align-items: center; justify-content: center; gap: 7px; margin-top: 16px; padding: 0 15px; background: #087f73; color: #fff; border: 1px solid #087f73; border-radius: 5px; font: inherit; font-size: 11px; font-weight: 800; cursor: pointer; }
.privacy-button--secondary { background: #fff; color: #087f73; }
.privacy-button:disabled { cursor: not-allowed; opacity: .55; }
.privacy-security { display: grid; grid-template-columns: 24px minmax(0, 1fr); gap: 10px; margin-top: 24px; padding: 15px 2px; border-top: 1px solid #dce3e1; }
.privacy-security > svg { color: #66736f; }
.privacy-security h2 { font-size: 12px; }
.privacy-security p { font-size: 10px; }
.privacy-error { display: flex; align-items: center; gap: 8px; margin: 20px 0 -10px; padding: 10px; background: #fce8ea; color: #b42335; border-radius: 4px; font-size: 10px; }
.privacy-state { display: grid; min-height: 420px; place-items: center; align-content: center; gap: 10px; text-align: center; }
.privacy-state h1 { margin: 0; font-size: 22px; }
.privacy-state p { max-width: 360px; margin: 0; color: #66736f; font-size: 12px; line-height: 1.5; }
.privacy-spinner { width: 22px; height: 22px; border: 2px solid #d2ddda; border-top-color: #087f73; border-radius: 50%; animation: spin .7s linear infinite; }
.privacy-page > footer { padding: 20px; color: #77827f; border-top: 1px solid #dce3e1; font-size: 10px; text-align: center; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 600px) {
  main { padding-top: 30px; }
  .privacy-heading { flex-direction: column; gap: 14px; }
  .privacy-result { padding: 16px; }
  .privacy-button { width: 100%; }
}
</style>
