<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Clock3, MessageSquareText, RotateCcw, Send } from '@lucide/vue'
import type { ReporterProgress } from '~/types/api'
import { issueStatusKey } from '~/utils/issue-activity'
import { extractAPIError } from '~/utils/safe-api-error'

definePageMeta({ layout: false })

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { dateTime, plural } = useRegionalFormat()
const token = computed(() => String(route.params.token || ''))
const response = ref('')
const sending = ref(false)
const responseError = ref('')
const responseAccepted = ref(false)
const confirmationRegion = ref<HTMLElement | null>(null)

const { data, status, error, refresh } = await useAsyncData(
  () => `reporter-progress:${token.value}`,
  () => $apiFetch<{ progress: ReporterProgress }>(`/v1/reporter/progress/${encodeURIComponent(token.value)}`),
  { watch: [token], server: !import.meta.dev },
)

const progress = computed(() => data.value?.progress)
const isInvalidLink = computed(() => extractAPIError(error.value)?.code === 'not_found')
const canRespond = computed(() => progress.value?.status === 'needs_info' &&
  Boolean(progress.value.clarification) && !progress.value.clarification?.responded && !responseAccepted.value)

watch(token, () => {
  response.value = ''
  responseError.value = ''
  responseAccepted.value = false
})

function statusLabel(value: string) {
  const key = issueStatusKey(value)
  return key ? t(key) : value
}

async function submitResponse() {
  const body = response.value.trim()
  if (!canRespond.value || !body || sending.value) return
  const submissionToken = token.value
  sending.value = true
  responseError.value = ''
  try {
    await $apiFetch(`/v1/reporter/progress/${encodeURIComponent(submissionToken)}/response`, {
      method: 'POST', body: { response: body },
    })
    if (token.value !== submissionToken) return
    responseAccepted.value = true
    response.value = ''
    await refresh()
    await nextTick()
    confirmationRegion.value?.focus()
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
    <header class="reporter-topbar">
      <span class="app-brand"><img src="/brand/fidibeki-color.png" alt="Fidibeki" /></span>
    </header>
    <main class="app-page">
      <div class="reporter-container">
        <section v-if="status === 'idle' || status === 'pending'" class="app-surface app-state" aria-live="polite">
          <div><span class="app-spinner" /><h1>{{ t('reporter.loading') }}</h1></div>
        </section>
        <section v-else-if="isInvalidLink" class="app-surface app-state" role="alert">
          <div><AlertTriangle :size="24" /><h1>{{ t('reporter.unavailable') }}</h1><p>{{ t('reporter.unavailableDescription') }}</p></div>
        </section>
        <section v-else-if="status === 'error' || !progress" class="app-surface app-state" role="alert">
          <div>
            <AlertTriangle :size="24" /><h1>{{ t('reporter.errorTitle') }}</h1><p>{{ apiErrorMessage(error, 'reporter.errorDescription') }}</p>
            <button type="button" class="app-button app-button--primary" @click="refresh()"><RotateCcw :size="14" /> {{ t('reporter.retry') }}</button>
          </div>
        </section>
        <template v-else>
          <header class="app-page-head reporter-heading">
            <div>
              <p class="app-eyebrow">{{ progress.public_id }}</p>
              <h1>{{ progress.title }}</h1>
              <span>{{ t('reporter.reportedOn', { date: dateTime(progress.created_at) }) }}</span>
            </div>
            <span class="status-pill" :class="`status-${progress.status}`">{{ statusLabel(progress.status) }}</span>
          </header>

          <section v-if="progress.clarification" class="app-surface reporter-clarification">
            <header>
              <MessageSquareText :size="16" />
              <div><h2>{{ t('reporter.clarificationTitle') }}</h2><p>{{ progress.clarification.prompt }}</p></div>
            </header>
            <form v-if="canRespond" @submit.prevent="submitResponse">
              <label for="reporter-response">{{ t('reporter.responseLabel') }}</label>
              <textarea id="reporter-response" v-model="response" maxlength="5000" required autofocus />
              <p v-if="responseError" role="alert">{{ responseError }}</p>
              <button type="submit" class="app-button app-button--primary" :disabled="sending || !response.trim()">
                <Send :size="14" />{{ sending ? t('reporter.sending') : t('reporter.send') }}
              </button>
            </form>
            <div v-else ref="confirmationRegion" class="reporter-answered" role="status" tabindex="-1">
              <CheckCircle2 :size="16" /><span>{{ responseAccepted ? t('reporter.responseSentNotice') : t('reporter.responseReceivedNotice') }}</span>
            </div>
          </section>

          <section class="app-surface reporter-timeline">
            <header><Clock3 :size="15" /><h2>{{ t('reporter.timelineTitle') }}</h2><span>{{ plural('reporter.timelineCount', 1 + progress.events.length) }}</span></header>
            <ol>
              <li><span /><div><p>{{ t('reporter.feedbackReceived') }}</p><time :datetime="progress.created_at">{{ dateTime(progress.created_at) }}</time></div></li>
              <li v-for="(event, index) in progress.events" :key="`${event.created_at}-${index}`">
                <span /><div><p>{{ t('reporter.statusChanged', { status: statusLabel(event.to_status) }) }}</p><time :datetime="event.created_at">{{ dateTime(event.created_at) }}</time></div>
              </li>
            </ol>
            <p v-if="!progress.events.length" class="reporter-timeline-empty">{{ t('reporter.timelineEmpty') }}</p>
          </section>
        </template>
      </div>
    </main>
    <footer class="reporter-footer">{{ t('reporter.footer') }}</footer>
  </div>
</template>

<style scoped>
.reporter-page {
  display: flex;
  min-height: 100vh;
  flex-direction: column;
}

.reporter-topbar {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  min-height: 56px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  padding: 0 16px;
  background: var(--surface);
  border-bottom: 1px solid var(--line);
}

.app-page {
  flex: 1 0 auto;
}

.reporter-container {
  max-width: 720px;
  margin: 0 auto;
}

.reporter-container .app-state {
  min-height: 320px;
}

.reporter-heading h1 {
  max-width: 600px;
  font-size: 26px;
  line-height: 1.25;
}

.reporter-heading > div > span {
  display: block;
  margin-top: 5px;
}

.reporter-clarification {
  margin-bottom: 16px;
}

.reporter-clarification > header {
  display: grid;
  grid-template-columns: 20px minmax(0, 1fr);
  gap: 10px;
  padding: 14px 16px;
  color: var(--teal);
  border-bottom: 1px solid var(--line);
}

.reporter-clarification > header > div {
  color: var(--ink);
}

.reporter-clarification h2 {
  margin: 0;
  font-size: 13px;
}

.reporter-clarification header p {
  margin: 6px 0 0;
  overflow-wrap: anywhere;
  color: var(--ink-soft);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
}

.reporter-clarification form {
  display: grid;
  gap: 8px;
  padding: 16px;
}

.reporter-clarification label {
  color: var(--muted);
  font-size: 10px;
  font-weight: 750;
  text-transform: uppercase;
}

.reporter-clarification textarea {
  min-height: 110px;
  padding: 10px;
  border: 1px solid var(--line-strong);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  line-height: 1.5;
  resize: vertical;
}

.reporter-clarification textarea:focus-visible {
  border-color: var(--teal);
  outline: 0;
  box-shadow: 0 0 0 3px rgb(8 127 115 / 10%);
}

.reporter-clarification form p[role='alert'] {
  margin: 0;
  color: var(--red);
  font-size: 11px;
}

.reporter-clarification form button {
  justify-self: end;
}

.reporter-answered {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 14px 16px;
  color: var(--green);
  font-size: 12px;
  font-weight: 700;
}

.reporter-timeline > header {
  display: flex;
  min-height: 46px;
  align-items: center;
  gap: 8px;
  padding: 0 16px;
  border-bottom: 1px solid var(--line);
}

.reporter-timeline h2 {
  flex: 1 0 auto;
  margin: 0;
  font-size: 13px;
}

.reporter-timeline > header > span {
  color: var(--muted);
  font-size: 9px;
}

.reporter-timeline ol {
  margin: 0;
  padding: 16px;
  list-style: none;
}

.reporter-timeline li {
  display: grid;
  min-height: 52px;
  grid-template-columns: 14px minmax(0, 1fr);
  gap: 10px;
}

.reporter-timeline li > span {
  position: relative;
  width: 8px;
  height: 8px;
  margin-top: 4px;
  background: var(--teal);
  border: 2px solid var(--surface);
  border-radius: 50%;
  box-shadow: 0 0 0 1px var(--teal);
}

.reporter-timeline li > span::after {
  position: absolute;
  top: 10px;
  left: 2px;
  width: 1px;
  height: 38px;
  background: var(--line);
  content: '';
}

.reporter-timeline li:last-child > span::after {
  display: none;
}

.reporter-timeline li p {
  margin: 0 0 4px;
  overflow-wrap: anywhere;
  font-size: 11px;
  font-weight: 650;
}

.reporter-timeline li time {
  color: var(--muted);
  font-size: 10px;
}

.reporter-timeline-empty {
  margin: 0;
  padding: 0 16px 16px;
  color: var(--muted);
  font-size: 11px;
  line-height: 1.5;
}

.reporter-footer {
  flex: 0 0 auto;
  padding: 18px;
  color: var(--muted);
  font-size: 10px;
  text-align: center;
  border-top: 1px solid var(--line);
}

@media (max-width: 600px) {
  .reporter-heading h1 {
    font-size: 22px;
  }

  .reporter-clarification form button {
    width: 100%;
    justify-self: stretch;
  }
}
</style>
