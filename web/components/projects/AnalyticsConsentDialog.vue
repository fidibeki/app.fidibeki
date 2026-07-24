<script setup lang="ts">
import { Check, Code2, ShieldCheck, X } from '@lucide/vue'
import type {
  AnalyticsConsentConfig,
  AnalyticsConsentMode,
  AnalyticsConsentNotice,
} from '~/types/api'

const props = defineProps<{
  open: boolean
  initial: AnalyticsConsentConfig
  projectName: string
  defaultLocale: string
}>()
const emit = defineEmits<{
  cancel: []
  confirm: [configuration: AnalyticsConsentConfig]
}>()
const { t } = useI18n()
const identifier = useId()
const localeOptions = ['en', 'pt', 'pt-ao', 'pt-pt']
const draft = ref<AnalyticsConsentConfig>(emptyConsent())
const selectedLocale = ref('en')

watch(() => props.open, (open) => {
  if (!open) return
  draft.value = normalizeInitial(props.initial)
  selectedLocale.value = draft.value.supported_locales[0] || normalizedDefaultLocale()
}, { immediate: true })

const selectedNotice = computed({
  get: () => draft.value.notices[selectedLocale.value] || defaultNotice(selectedLocale.value),
  set: (notice: AnalyticsConsentNotice) => {
    draft.value.notices[selectedLocale.value] = notice
  },
})

const valid = computed(() => {
  const value = draft.value
  if (value.mode === 'none' || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(value.notice_version)) return false
  if (value.expiry_days < 1 || value.expiry_days > 365 || value.supported_locales.length === 0) return false
  if (value.mode === 'customer_cmp') return true
  if (!value.controller_name.trim() || !validPrivacyURL(value.privacy_policy_url)) return false
  if (value.retention_days < 1 || value.retention_days > 3650) return false
  return value.supported_locales.every((locale) => {
    const notice = value.notices[locale]
    return notice && notice.title.trim() && notice.description.trim() &&
      notice.accept_label.trim() && notice.reject_label.trim() && notice.withdraw_label.trim()
  })
})

const invalidatesExistingGrants = computed(() =>
  props.initial.mode !== 'none' &&
  (
    draft.value.mode !== props.initial.mode ||
    draft.value.notice_version !== props.initial.notice_version
  ),
)

function emptyConsent(): AnalyticsConsentConfig {
  return {
    mode: 'none',
    notice_version: '2026-07',
    controller_name: props.projectName,
    privacy_policy_url: '',
    supported_locales: [],
    retention_days: 30,
    expiry_days: 30,
    notices: {},
  }
}

function normalizeInitial(initial: AnalyticsConsentConfig): AnalyticsConsentConfig {
  const value = cloneConsent(initial)
  if (!value.mode || value.mode === 'none') return emptyConsent()
  value.supported_locales = [...new Set((value.supported_locales || []).map(locale => locale.toLowerCase()))]
  return value
}

function cloneConsent(value: AnalyticsConsentConfig): AnalyticsConsentConfig {
  return {
    ...value,
    supported_locales: [...(value.supported_locales || [])],
    notices: Object.fromEntries(
      Object.entries(value.notices || {}).map(([locale, notice]) => [locale, { ...notice }]),
    ),
  }
}

function normalizedDefaultLocale() {
  const locale = props.defaultLocale.trim().toLowerCase()
  return localeOptions.includes(locale) ? locale : locale.split('-', 1)[0] || 'en'
}

function selectMode(mode: Exclude<AnalyticsConsentMode, 'none'>) {
  draft.value.mode = mode
  if (draft.value.supported_locales.length === 0) toggleLocale(normalizedDefaultLocale(), true)
}

function toggleLocale(locale: string, checked: boolean) {
  if (checked) {
    if (!draft.value.supported_locales.includes(locale)) draft.value.supported_locales.push(locale)
    draft.value.notices[locale] ||= defaultNotice(locale)
    selectedLocale.value = locale
    return
  }
  draft.value.supported_locales = draft.value.supported_locales.filter(value => value !== locale)
  if (selectedLocale.value === locale) selectedLocale.value = draft.value.supported_locales[0] || normalizedDefaultLocale()
}

function defaultNotice(locale: string): AnalyticsConsentNotice {
  if (locale.startsWith('pt')) {
    return {
      title: 'Ajude-nos a perceber como este widget é utilizado',
      description: 'Autorize a recolha pseudónima de dados analíticos das suas interações com este widget.',
      accept_label: 'Aceitar',
      reject_label: 'Recusar',
      withdraw_label: 'Retirar consentimento',
    }
  }
  return {
    title: 'Help us understand how this widget is used',
    description: 'Allow pseudonymous collection of analytics about your interactions with this widget.',
    accept_label: 'Accept',
    reject_label: 'Reject',
    withdraw_label: 'Withdraw consent',
  }
}

function validPrivacyURL(value: string) {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && !url.username && !url.password && !url.search && !url.hash
  } catch {
    return false
  }
}

function confirm() {
  if (!valid.value) return
  emit('confirm', cloneConsent(draft.value))
}
</script>

<template>
  <AppDialog
    :open="open"
    initial-focus="[data-consent-mode]"
    :aria-labelledby="`${identifier}-title`"
    @close="emit('cancel')"
  >
    <section class="app-modal analytics-consent-dialog">
      <header>
        <div>
          <p class="app-eyebrow">{{ t('projects.editor.analytics.consent.eyebrow') }}</p>
          <h2 :id="`${identifier}-title`">{{ t('projects.editor.analytics.consent.title') }}</h2>
        </div>
        <button class="app-icon-button" type="button" :title="t('common.cancel')" :aria-label="t('common.cancel')" @click="emit('cancel')">
          <X :size="16" />
        </button>
      </header>

      <div class="app-modal__body">
        <div class="analytics-consent-anonymous" role="note">
          <ShieldCheck :size="17" />
          <p><b>{{ t('projects.editor.analytics.consent.anonymousTitle') }}</b><span>{{ t('projects.editor.analytics.consent.anonymousDescription') }}</span></p>
        </div>

        <div class="analytics-consent-modes" role="group" :aria-label="t('projects.editor.analytics.consent.mode')">
          <button
            type="button"
            data-consent-mode
            :data-active="draft.mode === 'fidibeki_managed'"
            @click="selectMode('fidibeki_managed')"
          >
            <span class="analytics-consent-mode-marker" aria-hidden="true">
              <Check v-if="draft.mode === 'fidibeki_managed'" :size="14" />
            </span>
            <span><b>{{ t('projects.editor.analytics.consent.managed') }}</b><small>{{ t('projects.editor.analytics.consent.managedHint') }}</small></span>
          </button>
          <button
            type="button"
            :data-active="draft.mode === 'customer_cmp'"
            @click="selectMode('customer_cmp')"
          >
            <span class="analytics-consent-mode-marker" aria-hidden="true">
              <Check v-if="draft.mode === 'customer_cmp'" :size="14" />
            </span>
            <span><b>{{ t('projects.editor.analytics.consent.customer') }}</b><small>{{ t('projects.editor.analytics.consent.customerHint') }}</small></span>
          </button>
        </div>

        <template v-if="draft.mode !== 'none'">
          <div class="analytics-consent-grid">
            <label>
              {{ t('projects.editor.analytics.consent.noticeVersion') }}
              <input v-model.trim="draft.notice_version" class="app-input" maxlength="64" pattern="[A-Za-z0-9][A-Za-z0-9._-]{0,63}" required />
            </label>
            <label>
              {{ t('projects.editor.analytics.consent.expiryDays') }}
              <input v-model.number="draft.expiry_days" class="app-input" type="number" min="1" max="365" required />
            </label>
            <label v-if="draft.mode === 'fidibeki_managed'">
              {{ t('projects.editor.analytics.consent.controllerName') }}
              <input v-model.trim="draft.controller_name" class="app-input" maxlength="160" required />
            </label>
            <label v-if="draft.mode === 'fidibeki_managed'">
              {{ t('projects.editor.analytics.consent.retentionDays') }}
              <input v-model.number="draft.retention_days" class="app-input" type="number" min="1" max="3650" required />
            </label>
            <label v-if="draft.mode === 'fidibeki_managed'" class="wide">
              {{ t('projects.editor.analytics.consent.privacyURL') }}
              <input v-model.trim="draft.privacy_policy_url" class="app-input" type="url" inputmode="url" placeholder="https://example.com/privacy" required />
            </label>
          </div>

          <p v-if="invalidatesExistingGrants" class="analytics-consent-revision-warning" role="alert">
            {{ t('projects.editor.analytics.consent.revisionWarning') }}
          </p>

          <fieldset class="analytics-consent-locales">
            <legend>{{ t('projects.editor.analytics.consent.locales') }}</legend>
            <label v-for="locale in localeOptions" :key="locale">
              <input
                type="checkbox"
                :checked="draft.supported_locales.includes(locale)"
                @change="toggleLocale(locale, ($event.target as HTMLInputElement).checked)"
              />
              <span>{{ t(`projects.editor.analytics.consent.localeNames.${locale.replace('-', '')}`) }}</span>
            </label>
          </fieldset>

          <template v-if="draft.mode === 'fidibeki_managed' && draft.supported_locales.length">
            <div class="analytics-consent-tabs" role="tablist" :aria-label="t('projects.editor.analytics.consent.noticeCopy')">
              <button
                v-for="locale in draft.supported_locales"
                :key="locale"
                type="button"
                role="tab"
                :aria-selected="selectedLocale === locale"
                :data-active="selectedLocale === locale"
                @click="selectedLocale = locale"
              >
                {{ locale }}
              </button>
            </div>
            <div class="analytics-consent-grid">
              <label class="wide">{{ t('projects.editor.analytics.consent.noticeTitle') }}<input v-model="selectedNotice.title" class="app-input" maxlength="160" required /></label>
              <label class="wide">{{ t('projects.editor.analytics.consent.noticeDescription') }}<textarea v-model="selectedNotice.description" class="app-input" maxlength="1000" required /></label>
              <label>{{ t('projects.editor.analytics.consent.acceptLabel') }}<input v-model="selectedNotice.accept_label" class="app-input" maxlength="80" required /></label>
              <label>{{ t('projects.editor.analytics.consent.rejectLabel') }}<input v-model="selectedNotice.reject_label" class="app-input" maxlength="80" required /></label>
              <label class="wide">{{ t('projects.editor.analytics.consent.withdrawLabel') }}<input v-model="selectedNotice.withdraw_label" class="app-input" maxlength="80" required /></label>
            </div>
          </template>

          <div v-if="draft.mode === 'customer_cmp'" class="analytics-consent-runtime">
            <div><Code2 :size="16" /><p><b>{{ t('projects.editor.analytics.consent.runtimeTitle') }}</b><span>{{ t('projects.editor.analytics.consent.runtimeDescription') }}</span></p></div>
            <pre>fidibeki.analytics.grantConsent({
  consentId: "cmp-reference",
  purpose: "widget_analytics",
  noticeVersion: "{{ draft.notice_version }}",
  locale: "{{ draft.supported_locales[0] || 'en' }}",
  source: "customer_cmp"
})</pre>
          </div>
        </template>
      </div>

      <footer>
        <button class="app-button" type="button" @click="emit('cancel')">{{ t('common.cancel') }}</button>
        <button class="app-button app-button--primary" type="button" :disabled="!valid" @click="confirm">
          <Check :size="14" /> {{ t('projects.editor.analytics.consent.confirm') }}
        </button>
      </footer>
    </section>
  </AppDialog>
</template>

<style scoped>
.analytics-consent-dialog { width: min(720px, calc(100vw - 40px)); }
.analytics-consent-anonymous { display: grid; grid-template-columns: 20px minmax(0, 1fr); gap: 10px; padding: 10px 0; color: var(--teal); border-bottom: 1px solid var(--line); }
.analytics-consent-anonymous p, .analytics-consent-runtime p { display: grid; gap: 3px; margin: 0; }
.analytics-consent-anonymous b, .analytics-consent-runtime b { color: var(--ink-soft); font-size: 11px; }
.analytics-consent-anonymous span, .analytics-consent-runtime span { color: var(--muted); font-size: 10px; line-height: 1.5; }
.analytics-consent-modes { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); border: 1px solid var(--line-strong); border-radius: 6px; overflow: hidden; }
.analytics-consent-modes > button { display: grid; min-height: 76px; grid-template-columns: 18px minmax(0, 1fr); align-items: start; gap: 8px; padding: 12px; background: #fff; color: var(--muted); border: 0; text-align: left; }
.analytics-consent-modes > button + button { border-left: 1px solid var(--line-strong); }
.analytics-consent-modes > button[data-active='true'] { background: var(--teal-soft); color: var(--teal); }
.analytics-consent-mode-marker { display: block; width: 18px; min-height: 14px; }
.analytics-consent-modes span { display: grid; gap: 4px; }
.analytics-consent-modes b { color: var(--ink-soft); font-size: 11px; }
.analytics-consent-modes small { font-size: 9px; line-height: 1.45; }
.analytics-consent-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.analytics-consent-grid label { display: grid; gap: 6px; color: var(--muted); font-size: 10px; font-weight: 700; }
.analytics-consent-grid .wide { grid-column: 1 / -1; }
.analytics-consent-grid textarea { min-height: 86px; resize: vertical; }
.analytics-consent-revision-warning { margin: 0; padding: 9px 10px; background: #fff8e8; color: #72520d; border: 1px solid #e9cc84; border-radius: 5px; font-size: 10px; line-height: 1.5; }
.analytics-consent-locales { display: flex; flex-wrap: wrap; gap: 8px 16px; margin: 0; padding: 0; border: 0; }
.analytics-consent-locales legend { width: 100%; margin-bottom: 7px; color: var(--muted); font-size: 10px; font-weight: 700; }
.analytics-consent-locales label { display: flex; align-items: center; gap: 6px; }
.analytics-consent-tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--line); }
.analytics-consent-tabs button { min-height: 30px; padding: 0 10px; background: transparent; color: var(--muted); border: 0; border-bottom: 2px solid transparent; }
.analytics-consent-tabs button[data-active='true'] { color: var(--teal); border-bottom-color: var(--teal); }
.analytics-consent-runtime { display: grid; gap: 10px; }
.analytics-consent-runtime > div { display: grid; grid-template-columns: 20px minmax(0, 1fr); gap: 9px; }
.analytics-consent-runtime pre { max-width: 100%; margin: 0; padding: 12px; overflow-x: auto; background: #f3f6f5; color: var(--ink-soft); border: 1px solid var(--line); border-radius: 5px; font: 10px/1.55 ui-monospace, SFMono-Regular, Menlo, monospace; }

@media (max-width: 600px) {
  .analytics-consent-dialog { width: 100%; }
  .analytics-consent-modes, .analytics-consent-grid { grid-template-columns: 1fr; }
  .analytics-consent-modes > button + button { border-top: 1px solid var(--line-strong); border-left: 0; }
  .analytics-consent-grid .wide { grid-column: auto; }
  .analytics-consent-dialog > footer { align-items: stretch; flex-direction: column-reverse; }
}
</style>
