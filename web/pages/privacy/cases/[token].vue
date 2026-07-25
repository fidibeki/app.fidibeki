<script setup lang="ts">
import { AlertTriangle, CheckCircle2, ShieldCheck } from '@lucide/vue'
import type { ReporterPrivacyCaseView } from '~/types/api'

definePageMeta({ layout: false })

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { dateTime } = useRegionalFormat()
const token = computed(() => String(route.params.token || ''))
const privacyCase = ref<ReporterPrivacyCaseView | null>(null)
const loading = ref(true)
const unavailable = ref(false)
const transientError = ref('')

async function verify() {
  loading.value = true
  unavailable.value = false
  transientError.value = ''
  try {
    const response = await $apiFetch<{ case: ReporterPrivacyCaseView }>(
      `/v1/privacy/cases/${encodeURIComponent(token.value)}/verify`,
      { method: 'POST' },
    )
    privacyCase.value = response.case
  } catch (verifyError: unknown) {
    const statusCode = typeof verifyError === 'object' && verifyError && 'statusCode' in verifyError
      ? Number((verifyError as { statusCode?: number }).statusCode)
      : 0
    unavailable.value = statusCode === 404
    if (!unavailable.value) {
      transientError.value = localizedError.message(verifyError, 'admin.privacy.settings.error')
    }
  } finally {
    loading.value = false
  }
}

onMounted(verify)

useHead(() => ({
  title: t('admin.privacy.casePublic.pageTitle'),
  meta: [
    { name: 'robots', content: 'noindex,nofollow' },
    { name: 'referrer', content: 'no-referrer' },
  ],
}))
</script>

<template>
  <div class="privacy-case-page">
    <header class="privacy-case-brand"><span>F</span><b>Fidibeki</b></header>
    <main>
      <section v-if="loading" class="privacy-case-state" aria-live="polite">
        <span class="privacy-case-spinner" />
        <h1>{{ t('admin.privacy.casePublic.verifying') }}</h1>
        <p>{{ t('admin.privacy.casePublic.verifyingDescription') }}</p>
      </section>
      <section v-else-if="unavailable" class="privacy-case-state">
        <AlertTriangle :size="28" />
        <h1>{{ t('admin.privacy.casePublic.unavailable') }}</h1>
        <p>{{ t('admin.privacy.casePublic.unavailableDescription') }}</p>
      </section>
      <section v-else-if="!privacyCase" class="privacy-case-state">
        <AlertTriangle :size="28" />
        <h1>{{ t('admin.privacy.casePublic.interrupted') }}</h1>
        <p>{{ transientError }}</p>
        <button class="privacy-case-button" @click="verify">
          {{ t('admin.privacy.casePublic.tryAgain') }}
        </button>
      </section>
      <template v-else>
        <header class="privacy-case-heading">
          <div>
            <p>{{ t('admin.privacy.casePublic.eyebrow') }}</p>
            <h1>{{ t('admin.privacy.casePublic.title', { id: privacyCase.public_id }) }}</h1>
            <span>{{ t(`admin.privacy.casePublic.rights.${privacyCase.right_type}`) }}</span>
          </div>
          <span class="privacy-case-status">{{ t(`admin.statuses.${privacyCase.status}`) }}</span>
        </header>
        <section class="privacy-case-result">
          <CheckCircle2 :size="24" />
          <div>
            <h2>{{ t('admin.privacy.casePublic.accepted') }}</h2>
            <p>{{ t('admin.privacy.casePublic.acceptedDescription') }}</p>
            <dl>
              <div><dt>{{ t('admin.privacy.casePublic.received', { date: dateTime(privacyCase.received_at) }) }}</dt></div>
              <div><dt>{{ t('admin.privacy.casePublic.deadline', { date: dateTime(privacyCase.deadline_at) }) }}</dt></div>
            </dl>
          </div>
        </section>
        <section class="privacy-case-notice">
          <ShieldCheck :size="18" />
          <p>{{ t('admin.privacy.casePublic.operationalNotice') }}</p>
        </section>
      </template>
    </main>
    <footer>{{ t('admin.privacy.casePublic.footer') }}</footer>
  </div>
</template>

<style scoped>
.privacy-case-page { min-height: 100vh; background: var(--canvas); color: var(--ink); font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
.privacy-case-brand { display: flex; min-height: 62px; align-items: center; gap: 9px; padding: 0 max(20px, calc((100vw - 760px) / 2)); background: var(--surface); border-bottom: 1px solid var(--line); }
.privacy-case-brand > span { display: grid; width: 30px; height: 30px; place-items: center; background: var(--teal-fill); color: var(--on-accent); border-radius: 5px; font-size: 13px; font-weight: 850; }
.privacy-case-brand b { font-size: 14px; }
main { width: min(760px, calc(100% - 32px)); margin: 0 auto; padding: 48px 0 36px; }
.privacy-case-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; padding-bottom: 28px; border-bottom: 1px solid var(--line); }
.privacy-case-heading p { margin: 0 0 8px; color: var(--teal); font-size: 11px; font-weight: 800; }
.privacy-case-heading h1 { margin: 0 0 10px; font-size: clamp(22px, 4vw, 32px); line-height: 1.2; }
.privacy-case-heading div > span { color: var(--muted); font-size: 11px; }
.privacy-case-status { flex: 0 0 auto; padding: 5px 8px; background: var(--teal-soft); color: var(--teal); border-radius: 4px; font-size: 9px; font-weight: 800; }
.privacy-case-result { display: grid; grid-template-columns: 30px minmax(0, 1fr); gap: 12px; margin-top: 30px; padding: 20px; background: var(--surface); border: 1px solid var(--line); border-radius: 6px; }
.privacy-case-result > svg { color: var(--teal); }
.privacy-case-result h2 { margin: 1px 0 7px; font-size: 14px; }
.privacy-case-result p, .privacy-case-notice p { margin: 0; color: var(--muted); font-size: 12px; line-height: 1.6; }
.privacy-case-result dl { display: grid; gap: 5px; margin: 16px 0 0; color: var(--muted); font-size: 10px; }
.privacy-case-result dt { font-weight: 650; }
.privacy-case-notice { display: grid; grid-template-columns: 24px minmax(0, 1fr); gap: 10px; margin-top: 24px; padding: 15px 2px; border-top: 1px solid var(--line); }
.privacy-case-notice > svg { color: var(--muted); }
.privacy-case-notice p { font-size: 10px; }
.privacy-case-state { display: grid; min-height: 420px; place-items: center; align-content: center; gap: 10px; text-align: center; }
.privacy-case-state h1 { margin: 0; font-size: 22px; }
.privacy-case-state p { max-width: 380px; margin: 0; color: var(--muted); font-size: 12px; line-height: 1.5; }
.privacy-case-spinner { width: 22px; height: 22px; border: 2px solid var(--line); border-top-color: var(--teal); border-radius: 50%; animation: spin .7s linear infinite; }
.privacy-case-button { min-height: 38px; padding: 0 15px; background: var(--teal-fill); color: var(--on-accent); border: 1px solid var(--teal); border-radius: 5px; font: inherit; font-size: 11px; font-weight: 800; cursor: pointer; }
.privacy-case-page > footer { padding: 20px; color: var(--muted); border-top: 1px solid var(--line); font-size: 10px; text-align: center; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 600px) {
  main { padding-top: 30px; }
  .privacy-case-heading { flex-direction: column; gap: 14px; }
  .privacy-case-result { padding: 16px; }
}
</style>
