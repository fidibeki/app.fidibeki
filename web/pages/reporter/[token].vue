<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Clock3, MessageSquareText, Send } from '@lucide/vue'
import type { ReporterProgress } from '~/types/api'
import { issueStatusKey } from '~/utils/issue-activity'

definePageMeta({ layout: false })

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { dateTime } = useRegionalFormat()
const token = computed(() => String(route.params.token || ''))
const response = ref('')
const sending = ref(false)
const responseError = ref('')
const responseAccepted = ref(false)

const { data, status, error, refresh } = await useAsyncData(
  () => `reporter-progress:${token.value}`,
  () => $apiFetch<{ progress: ReporterProgress }>(`/v1/reporter/progress/${encodeURIComponent(token.value)}`),
  { watch: [token], server: !import.meta.dev },
)

const progress = computed(() => data.value?.progress)
const canRespond = computed(() => progress.value?.status === 'needs_info' &&
  Boolean(progress.value.clarification) && !progress.value.clarification?.responded && !responseAccepted.value)

function statusLabel(value: string) {
  const key = issueStatusKey(value)
  return key ? t(key) : value
}

async function submitResponse() {
  const body = response.value.trim()
  if (!canRespond.value || !body || sending.value) return
  sending.value = true
  responseError.value = ''
  try {
    await $apiFetch(`/v1/reporter/progress/${encodeURIComponent(token.value)}/response`, {
      method: 'POST', body: { response: body },
    })
    responseAccepted.value = true
    response.value = ''
    await refresh()
  } catch (submitError) {
    responseError.value = apiErrorMessage(submitError, 'reporter.responseFailed')
  } finally {
    sending.value = false
  }
}

useHead(() => ({
  title: progress.value
    ? t('reporter.pageTitle', { publicId: progress.value.public_id })
    : t('reporter.pageTitleFallback'),
  meta: [{ name: 'robots', content: 'noindex,nofollow' }],
}))
</script>

<template>
  <div class="reporter-page">
    <header class="reporter-brand"><span>F</span><b>{{ t('reporter.brand') }}</b></header>
    <main>
      <section v-if="status === 'pending'" class="reporter-state" aria-live="polite"><span class="reporter-spinner" /><h1>{{ t('reporter.loading') }}</h1></section>
      <section v-else-if="error || !progress" class="reporter-state" role="alert"><AlertTriangle :size="28" /><h1>{{ t('reporter.unavailable') }}</h1><p>{{ t('reporter.unavailableDescription') }}</p></section>
      <template v-else>
        <header class="reporter-heading">
          <div><p>{{ progress.public_id }}</p><h1>{{ progress.title }}</h1><span>{{ t('reporter.reportedOn', { date: dateTime(progress.created_at) }) }}</span></div>
          <span class="status-pill" :class="`status-${progress.status}`">{{ statusLabel(progress.status) }}</span>
        </header>

        <section v-if="progress.clarification" class="reporter-clarification">
          <header><MessageSquareText :size="18" /><div><h2>{{ t('reporter.clarificationTitle') }}</h2><p>{{ progress.clarification.prompt }}</p></div></header>
          <form v-if="canRespond" @submit.prevent="submitResponse">
            <label for="reporter-response">{{ t('reporter.responseLabel') }}</label>
            <textarea id="reporter-response" v-model="response" maxlength="5000" required autofocus />
            <p v-if="responseError" role="alert">{{ responseError }}</p>
            <button type="submit" :disabled="sending || !response.trim()"><Send :size="15" />{{ sending ? t('reporter.sending') : t('reporter.send') }}</button>
          </form>
          <div v-else class="reporter-answered"><CheckCircle2 :size="17" /><span>{{ responseAccepted ? t('reporter.responseSentNotice') : t('reporter.responseReceivedNotice') }}</span></div>
        </section>

        <section class="reporter-timeline">
          <header><Clock3 :size="17" /><h2>{{ t('reporter.timelineTitle') }}</h2></header>
          <ol>
            <li><span /><div><b>{{ t('reporter.feedbackReceived') }}</b><time :datetime="progress.created_at">{{ dateTime(progress.created_at) }}</time></div></li>
            <li v-for="(event, index) in progress.events" :key="`${event.created_at}-${index}`"><span /><div><b>{{ t('reporter.statusChanged', { status: statusLabel(event.to_status) }) }}</b><time :datetime="event.created_at">{{ dateTime(event.created_at) }}</time></div></li>
          </ol>
        </section>
      </template>
    </main>
    <footer>{{ t('reporter.footer') }}</footer>
  </div>
</template>

<style scoped>
.reporter-page { min-height: 100vh; background: #f5f7f6; color: #18201f; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
.reporter-brand { display: flex; min-height: 62px; align-items: center; gap: 9px; padding: 0 max(20px, calc((100vw - 760px) / 2)); background: #fff; border-bottom: 1px solid #dce3e1; }
.reporter-brand > span { display: grid; width: 30px; height: 30px; place-items: center; background: #087f73; color: #fff; border-radius: 5px; font-size: 13px; font-weight: 850; }
.reporter-brand b { font-size: 14px; }
main { width: min(760px, calc(100% - 32px)); margin: 0 auto; padding: 48px 0 36px; }
.reporter-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; padding-bottom: 28px; border-bottom: 1px solid #dce3e1; }
.reporter-heading p { margin: 0 0 8px; color: #087f73; font-size: 11px; font-weight: 800; }
.reporter-heading h1 { max-width: 620px; margin: 0 0 10px; font-size: clamp(22px, 4vw, 32px); line-height: 1.2; }
.reporter-heading div > span { color: #66736f; font-size: 11px; }
.status-pill { flex: 0 0 auto; text-transform: capitalize; }
.reporter-clarification { margin: 30px 0 0; overflow: hidden; background: #fff; border: 1px solid #d4dcda; border-radius: 6px; }
.reporter-clarification > header { display: grid; grid-template-columns: 22px minmax(0, 1fr); gap: 10px; padding: 18px; border-bottom: 1px solid #e4e9e7; }
.reporter-clarification h2, .reporter-timeline h2 { margin: 0; font-size: 13px; }
.reporter-clarification header p { margin: 7px 0 0; color: #43504d; font-size: 13px; line-height: 1.55; white-space: pre-wrap; }
.reporter-clarification form { display: grid; gap: 8px; padding: 18px; }
.reporter-clarification label { color: #43504d; font-size: 10px; font-weight: 800; }
.reporter-clarification textarea { min-height: 120px; padding: 11px; border: 1px solid #aab7b3; border-radius: 5px; resize: vertical; font: inherit; font-size: 13px; line-height: 1.5; }
.reporter-clarification form p { margin: 0; color: #b42335; font-size: 11px; }
.reporter-clarification button { display: inline-flex; min-height: 38px; align-items: center; justify-content: center; justify-self: end; gap: 7px; padding: 0 15px; background: #087f73; color: #fff; border: 0; border-radius: 5px; font: inherit; font-size: 11px; font-weight: 800; cursor: pointer; }
.reporter-clarification button:disabled { cursor: not-allowed; opacity: .55; }
.reporter-answered { display: flex; align-items: center; gap: 8px; padding: 16px 18px; color: #167d68; font-size: 11px; font-weight: 750; }
.reporter-timeline { margin-top: 30px; }
.reporter-timeline > header { display: flex; align-items: center; gap: 8px; padding-bottom: 13px; border-bottom: 1px solid #dce3e1; }
.reporter-timeline ol { margin: 0; padding: 18px 3px; list-style: none; }
.reporter-timeline li { display: grid; min-height: 58px; grid-template-columns: 14px minmax(0, 1fr); gap: 10px; }
.reporter-timeline li > span { position: relative; width: 8px; height: 8px; margin-top: 3px; background: #087f73; border: 2px solid #f5f7f6; border-radius: 50%; box-shadow: 0 0 0 1px #087f73; }
.reporter-timeline li > span::after { position: absolute; top: 10px; left: 2px; width: 1px; height: 43px; background: #cfd8d5; content: ''; }
.reporter-timeline li:last-child > span::after { display: none; }
.reporter-timeline li div { display: flex; flex-direction: column; gap: 4px; }
.reporter-timeline b { font-size: 11px; text-transform: capitalize; }
.reporter-timeline time { color: #66736f; font-size: 10px; }
.reporter-state { display: grid; min-height: 420px; place-items: center; align-content: center; gap: 10px; text-align: center; }
.reporter-state h1 { margin: 0; font-size: 22px; }
.reporter-state p { max-width: 360px; margin: 0; color: #66736f; font-size: 12px; line-height: 1.5; }
.reporter-spinner { width: 22px; height: 22px; border: 2px solid #d2ddda; border-top-color: #087f73; border-radius: 50%; animation: spin .7s linear infinite; }
.reporter-page > footer { padding: 20px; color: #77827f; border-top: 1px solid #dce3e1; font-size: 10px; text-align: center; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 600px) { main { padding-top: 30px; } .reporter-heading { flex-direction: column; gap: 14px; } .reporter-clarification button { width: 100%; } }
</style>
