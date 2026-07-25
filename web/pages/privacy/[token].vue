<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Clock3, Download, FileSearch, ShieldCheck } from '@lucide/vue'
import type { ReporterPrivacyView } from '~/types/api'
import { triggerSignedDownload } from '~/utils/download'

definePageMeta({ layout: false })

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { dateTime, plural } = useRegionalFormat()
const token = computed(() => String(route.params.token || ''))
const request = ref<ReporterPrivacyView | null>(null)
const loading = ref(true)
const unavailable = ref(false)
const transientError = ref('')
const downloading = ref(false)
let pollTimer: ReturnType<typeof setTimeout> | undefined

const isProcessing = computed(() => request.value?.status === 'processing' ||
  request.value?.status === 'awaiting_manual_review' ||
  request.value?.job_status === 'pending' || request.value?.job_status === 'processing')

function statusLabel(value: string) {
  return t(`admin.statuses.${value}`)
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
    if (!unavailable.value) transientError.value = localizedError.message(verifyError, 'admin.privacy.public.errors.verify')
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
    else transientError.value = localizedError.message(statusError, 'admin.privacy.public.errors.status')
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
    transientError.value = localizedError.message(downloadError, 'admin.privacy.public.errors.download')
  } finally {
    downloading.value = false
  }
}

onMounted(verify)
onBeforeUnmount(() => {
  if (pollTimer) clearTimeout(pollTimer)
})

useHead(() => ({
  title: t('admin.privacy.public.pageTitle'),
  meta: [
    { name: 'robots', content: 'noindex,nofollow' },
    { name: 'referrer', content: 'no-referrer' },
  ],
}))
</script>

<template>
  <div class="privacy-page">
    <header class="privacy-brand"><span>F</span><b>Fidibeki</b></header>
    <main>
      <section v-if="loading" class="privacy-state" aria-live="polite"><span class="privacy-spinner" /><h1>{{ t('admin.privacy.public.verifying') }}</h1><p>{{ t('admin.privacy.public.verifyingDescription') }}</p></section>
      <section v-else-if="unavailable" class="privacy-state"><AlertTriangle :size="28" /><h1>{{ t('admin.privacy.public.unavailable') }}</h1><p>{{ t('admin.privacy.public.unavailableDescription') }}</p></section>
      <section v-else-if="!request" class="privacy-state"><AlertTriangle :size="28" /><h1>{{ t('admin.privacy.public.interrupted') }}</h1><p>{{ transientError || t('admin.privacy.public.errors.verify') }}</p><button class="privacy-button privacy-button--secondary" @click="verify">{{ t('admin.privacy.public.tryAgain') }}</button></section>
      <template v-else>
        <header class="privacy-heading">
          <div><p>{{ t('admin.privacy.public.eyebrow') }}</p><h1>{{ request.action === 'anonymize' ? t('admin.privacy.public.anonymizationRequest') : t('admin.privacy.public.exportRequest') }}</h1><span>{{ t('admin.privacy.public.accessExpires', { date: dateTime(request.access_expires_at) }) }}</span></div>
          <span class="privacy-status" :data-status="request.status">{{ statusLabel(request.status) }}</span>
        </header>

        <p v-if="transientError" class="privacy-error" role="alert"><AlertTriangle :size="15" />{{ transientError }}</p>

        <section v-if="isProcessing" class="privacy-result">
          <Clock3 :size="24" /><div><h2>{{ t('admin.privacy.public.processing') }}</h2><p>{{ t('admin.privacy.public.processingDescription') }}</p></div>
        </section>
        <section v-else-if="request.status === 'completed' && request.action === 'export'" class="privacy-result">
          <FileSearch :size="24" /><div><h2>{{ t('admin.privacy.public.archiveReady') }}</h2><p>{{ t('admin.privacy.public.archiveDescription', { records: plural('admin.common.records', request.record_count || 0, { count: request.record_count || 0 }), date: dateTime(request.access_expires_at) }) }}</p><button class="privacy-button" :disabled="downloading || !request.download_available" @click="downloadExport"><Download :size="16" />{{ downloading ? t('admin.common.preparing') : t('admin.privacy.public.download') }}</button></div>
        </section>
        <section v-else-if="request.status === 'completed'" class="privacy-result">
          <CheckCircle2 :size="24" /><div><h2>{{ t('admin.privacy.public.anonymizationCompleted') }}</h2><p>{{ t('admin.privacy.public.anonymizationDescription', { records: plural('admin.common.records', request.record_count || 0, { count: request.record_count || 0 }) }) }}</p><p v-if="request.manual_review_count" class="privacy-review">{{ t('admin.privacy.public.manualReview', { records: plural('admin.common.manualReviewRecords', request.manual_review_count, { count: request.manual_review_count }) }) }}</p></div>
        </section>
        <section v-else-if="request.status === 'failed'" class="privacy-result">
          <AlertTriangle :size="24" /><div><h2>{{ t('admin.privacy.public.needsAttention') }}</h2><p>{{ t('admin.privacy.public.needsAttentionDescription', { date: dateTime(request.access_expires_at) }) }}</p></div>
        </section>
        <section v-else class="privacy-result">
          <AlertTriangle :size="24" /><div><h2>{{ t('admin.privacy.public.expired') }}</h2><p>{{ t('admin.privacy.public.expiredDescription') }}</p></div>
        </section>

        <section class="privacy-security"><ShieldCheck :size="18" /><div><h2>{{ t('admin.privacy.public.verifiedAccess') }}</h2><p>{{ t('admin.privacy.public.verifiedDescription') }}</p></div></section>
      </template>
    </main>
    <footer>{{ t('admin.privacy.public.footer') }}</footer>
  </div>
</template>

<style scoped>
.privacy-page { min-height: 100vh; background: var(--canvas); color: var(--ink); font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
.privacy-brand { display: flex; min-height: 62px; align-items: center; gap: 9px; padding: 0 max(20px, calc((100vw - 760px) / 2)); background: var(--surface); border-bottom: 1px solid var(--line); }
.privacy-brand > span { display: grid; width: 30px; height: 30px; place-items: center; background: var(--teal-fill); color: var(--on-accent); border-radius: 5px; font-size: 13px; font-weight: 850; }
.privacy-brand b { font-size: 14px; }
main { width: min(760px, calc(100% - 32px)); margin: 0 auto; padding: 48px 0 36px; }
.privacy-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; padding-bottom: 28px; border-bottom: 1px solid var(--line); }
.privacy-heading p { margin: 0 0 8px; color: var(--teal); font-size: 11px; font-weight: 800; }
.privacy-heading h1 { max-width: 620px; margin: 0 0 10px; font-size: clamp(22px, 4vw, 32px); line-height: 1.2; }
.privacy-heading div > span { color: var(--muted); font-size: 11px; }
.privacy-status { flex: 0 0 auto; padding: 5px 8px; background: var(--surface-soft); color: var(--muted); border-radius: 4px; font-size: 9px; font-weight: 800; text-transform: capitalize; }
.privacy-status[data-status='completed'] { background: var(--teal-soft); color: var(--teal); }
.privacy-status[data-status='failed'], .privacy-status[data-status='expired'] { background: var(--red-soft); color: var(--red); }
.privacy-result { display: grid; grid-template-columns: 30px minmax(0, 1fr); gap: 12px; margin-top: 30px; padding: 20px; background: var(--surface); border: 1px solid var(--line); border-radius: 6px; }
.privacy-result > svg { color: var(--teal); }
.privacy-result h2, .privacy-security h2 { margin: 1px 0 7px; font-size: 14px; }
.privacy-result p, .privacy-security p { margin: 0; color: var(--muted); font-size: 12px; line-height: 1.6; }
.privacy-result .privacy-review { margin-top: 10px; padding: 9px 10px; background: var(--amber-soft); color: var(--amber-ink); border: 1px solid var(--amber-line); border-radius: 4px; font-size: 10px; }
.privacy-button { display: inline-flex; min-height: 38px; align-items: center; justify-content: center; gap: 7px; margin-top: 16px; padding: 0 15px; background: var(--teal-fill); color: var(--on-accent); border: 1px solid var(--teal); border-radius: 5px; font: inherit; font-size: 11px; font-weight: 800; cursor: pointer; }
.privacy-button--secondary { background: var(--surface); color: var(--teal); }
.privacy-button:disabled { cursor: not-allowed; opacity: .55; }
.privacy-security { display: grid; grid-template-columns: 24px minmax(0, 1fr); gap: 10px; margin-top: 24px; padding: 15px 2px; border-top: 1px solid var(--line); }
.privacy-security > svg { color: var(--muted); }
.privacy-security h2 { font-size: 12px; }
.privacy-security p { font-size: 10px; }
.privacy-error { display: flex; align-items: center; gap: 8px; margin: 20px 0 -10px; padding: 10px; background: var(--red-soft); color: var(--red); border-radius: 4px; font-size: 10px; }
.privacy-state { display: grid; min-height: 420px; place-items: center; align-content: center; gap: 10px; text-align: center; }
.privacy-state h1 { margin: 0; font-size: 22px; }
.privacy-state p { max-width: 360px; margin: 0; color: var(--muted); font-size: 12px; line-height: 1.5; }
.privacy-spinner { width: 22px; height: 22px; border: 2px solid var(--line); border-top-color: var(--teal); border-radius: 50%; animation: spin .7s linear infinite; }
.privacy-page > footer { padding: 20px; color: var(--muted); border-top: 1px solid var(--line); font-size: 10px; text-align: center; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 600px) {
  main { padding-top: 30px; }
  .privacy-heading { flex-direction: column; gap: 14px; }
  .privacy-result { padding: 16px; }
  .privacy-button { width: 100%; }
}
</style>
