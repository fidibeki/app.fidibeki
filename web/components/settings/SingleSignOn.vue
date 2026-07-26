<script setup lang="ts">
import { AlertTriangle, Check, Copy, KeyRound, Plus, ShieldCheck, Trash2 } from '@lucide/vue'
import type { SAMLSettings, SSOMode } from '~/types/api'
import { apiFieldErrors } from '~/utils/api-fetch'

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { can, canWrite } = useAuth()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { dateTime } = useRegionalFormat()
const { success: showSuccessToast } = useAppToast()

const canManage = computed(() => can('tenant:update'))

const { data, status, error, refresh } = await useAsyncData('sso-settings', async () => {
  if (!canManage.value) return null
  return $apiFetch<SAMLSettings>('/v1/sso/saml')
})

const configuration = computed(() => data.value?.sso || null)
const serviceProvider = computed(() => data.value?.service_provider || null)
const warnings = computed(() => data.value?.warnings || [])
const canRequire = computed(() => Boolean(data.value?.can_require))

// A test login returns here with its outcome in the query string.
const testResult = computed(() => {
  const value = route.query.saml_test
  return value === 'passed' || value === 'failed' ? value : ''
})

type ConfigurationSource = 'metadata_url' | 'metadata_xml' | 'manual'
const source = ref<ConfigurationSource>('metadata_url')
const metadataURL = ref('')
const metadataXML = ref('')
const idpEntityID = ref('')
const idpSSOURL = ref('')
const certificate = ref('')
const emailDomains = ref('')
const allowIdPInitiated = ref(false)
const saving = ref(false)
const formError = ref('')
const fieldErrors = ref<Record<string, string>>({})
const busyAction = ref('')
const copied = ref('')

watch(configuration, (value) => {
  if (!value) return
  metadataURL.value = value.idp_metadata_url || ''
  idpEntityID.value = value.idp_entity_id
  idpSSOURL.value = value.idp_sso_url
  emailDomains.value = value.email_domains.join(', ')
  allowIdPInitiated.value = value.allow_idp_initiated
  source.value = value.idp_metadata_url ? 'metadata_url' : 'manual'
}, { immediate: true })

function parsedDomains() {
  return emailDomains.value.split(',').map(value => value.trim()).filter(Boolean)
}

async function save() {
  if (saving.value) return
  saving.value = true
  formError.value = ''
  fieldErrors.value = {}
  try {
    const body: Record<string, unknown> = {
      idp_metadata_url: source.value === 'metadata_url' ? metadataURL.value.trim() : '',
      idp_metadata_xml: source.value === 'metadata_xml' ? metadataXML.value : '',
      idp_entity_id: source.value === 'manual' ? idpEntityID.value.trim() : '',
      idp_sso_url: source.value === 'manual' ? idpSSOURL.value.trim() : '',
      certificates: source.value === 'manual' && certificate.value.trim() ? [certificate.value.trim()] : [],
      email_domains: parsedDomains(),
      allow_idp_initiated: allowIdPInitiated.value,
      // Refreshing from metadata replaces the trusted set; adding a
      // certificate for rotation uses the dedicated control below so both
      // remain accepted.
      replace_certificates: source.value !== 'manual',
    }
    data.value = await $apiFetch<SAMLSettings>('/v1/sso/saml', { method: 'PUT', body })
    metadataXML.value = ''
    certificate.value = ''
    showSuccessToast(t('admin.sso.saved'))
  } catch (saveError) {
    fieldErrors.value = apiFieldErrors(saveError)
    formError.value = localizedError.message(saveError, 'admin.sso.errors.save')
  } finally {
    saving.value = false
  }
}

async function setMode(mode: SSOMode) {
  if (busyAction.value) return
  busyAction.value = 'mode'
  formError.value = ''
  try {
    data.value = await $apiFetch<SAMLSettings>('/v1/sso/saml/mode', { method: 'PATCH', body: { mode } })
    showSuccessToast(t(`admin.sso.modeChanged.${mode}`))
  } catch (modeError) {
    formError.value = localizedError.message(modeError, 'admin.sso.errors.mode')
  } finally {
    busyAction.value = ''
  }
}

async function startTest() {
  if (busyAction.value) return
  busyAction.value = 'test'
  formError.value = ''
  try {
    const { start_url: startURL } = await $apiFetch<{ start_url: string }>(
      '/v1/sso/saml/test', { method: 'POST' },
    )
    await navigateTo(startURL, { external: true })
  } catch (testError) {
    formError.value = localizedError.message(testError, 'admin.sso.errors.test')
    busyAction.value = ''
  }
}

async function addCertificate() {
  if (busyAction.value || !certificate.value.trim()) return
  busyAction.value = 'certificate'
  formError.value = ''
  try {
    data.value = await $apiFetch<SAMLSettings>('/v1/sso/saml/certificates', {
      method: 'POST', body: { certificate: certificate.value.trim() },
    })
    certificate.value = ''
    showSuccessToast(t('admin.sso.certificateAdded'))
  } catch (certificateError) {
    formError.value = localizedError.message(certificateError, 'admin.sso.errors.certificate')
  } finally {
    busyAction.value = ''
  }
}

async function removeCertificate(id: string) {
  if (busyAction.value) return
  busyAction.value = id
  formError.value = ''
  try {
    data.value = await $apiFetch<SAMLSettings>(`/v1/sso/saml/certificates/${id}`, { method: 'DELETE' })
    showSuccessToast(t('admin.sso.certificateRemoved'))
  } catch (certificateError) {
    formError.value = localizedError.message(certificateError, 'admin.sso.errors.certificate')
  } finally {
    busyAction.value = ''
  }
}

async function disableSSO() {
  if (busyAction.value) return
  busyAction.value = 'disable'
  formError.value = ''
  try {
    await $apiFetch('/v1/sso/saml', { method: 'DELETE' })
    await refresh()
    showSuccessToast(t('admin.sso.disabled'))
  } catch (disableError) {
    formError.value = localizedError.message(disableError, 'admin.sso.errors.disable')
  } finally {
    busyAction.value = ''
  }
}

async function copy(label: string, value: string) {
  try {
    await navigator.clipboard.writeText(value)
    copied.value = label
    setTimeout(() => { if (copied.value === label) copied.value = '' }, 1500)
  } catch {
    // Clipboard access can be denied; the value stays selectable in the field.
  }
}

const modes: SSOMode[] = ['disabled', 'optional', 'required']
const sources: ConfigurationSource[] = ['metadata_url', 'metadata_xml', 'manual']
const serviceProviderFields = computed(() => {
  const provider = serviceProvider.value
  if (!provider) return []
  return [
    { key: 'entity_id', value: provider.entity_id },
    { key: 'acs_url', value: provider.acs_url },
    { key: 'metadata_url', value: provider.metadata_url },
  ]
})
</script>

<template>
  <div class="sso">
    <section v-if="!canManage" class="app-surface app-state">
      <div>
        <ShieldCheck :size="24" />
        <h2>{{ t('admin.sso.accessRequired') }}</h2>
        <p>{{ t('admin.sso.accessRequiredDescription') }}</p>
      </div>
    </section>
    <section v-else-if="status === 'pending'" class="app-surface app-state">
      <div><span class="app-spinner" /><h2>{{ t('admin.sso.loading') }}</h2></div>
    </section>
    <section v-else-if="error" class="app-surface app-state">
      <div>
        <AlertTriangle :size="24" />
        <h2>{{ t('admin.sso.unavailable') }}</h2>
        <p>{{ localizedError.message(error, 'admin.sso.errors.load') }}</p>
        <button class="app-button" @click="refresh()">{{ t('admin.common.tryAgain') }}</button>
      </div>
    </section>

    <template v-else>
      <p v-if="testResult === 'passed'" class="sso-notice sso-notice--success" role="status">
        <Check :size="15" /><span>{{ t('admin.sso.testPassed') }}</span>
      </p>
      <p v-else-if="testResult === 'failed'" class="sso-notice sso-notice--error" role="alert">
        <AlertTriangle :size="15" /><span>{{ t('admin.sso.testFailed') }}</span>
      </p>

      <section class="app-surface sso-section">
        <header>
          <div>
            <p class="app-eyebrow">{{ t('admin.sso.eyebrow') }}</p>
            <h2>{{ t('admin.sso.serviceProviderTitle') }}</h2>
          </div>
        </header>
        <div class="sso-body">
          <p class="sso-hint">{{ t('admin.sso.serviceProviderHint') }}</p>
          <div v-if="serviceProvider" class="sso-fields">
            <label v-for="field in serviceProviderFields" :key="field.key" class="wide">
              {{ t(`admin.sso.serviceProvider.${field.key}`) }}
              <span class="sso-copy-field">
                <input class="app-input" type="text" readonly :value="field.value" />
                <button
                  class="app-icon-button" type="button"
                  :title="t('admin.sso.copy')" @click="copy(field.key, field.value)"
                >
                  <Check v-if="copied === field.key" :size="16" /><Copy v-else :size="16" />
                </button>
              </span>
            </label>
          </div>
        </div>
      </section>

      <section class="app-surface sso-section">
        <header>
          <div>
            <p class="app-eyebrow">{{ t('admin.sso.eyebrow') }}</p>
            <h2>{{ t('admin.sso.identityProviderTitle') }}</h2>
          </div>
          <div class="sso-header-actions">
            <button
              v-if="data?.configured" class="app-button" type="button"
              :disabled="Boolean(busyAction) || !canWrite" @click="startTest"
            >
              <KeyRound :size="14" /> {{ busyAction === 'test' ? t('admin.common.saving') : t('admin.sso.runTest') }}
            </button>
            <button
              class="app-button app-button--primary" type="submit"
              form="sso-configuration-form" :disabled="saving || !canWrite"
            >
              {{ saving ? t('admin.common.saving') : t('admin.sso.save') }}
            </button>
          </div>
        </header>
        <form id="sso-configuration-form" class="sso-body" @submit.prevent="save">
          <div class="sso-sources" role="radiogroup" :aria-label="t('admin.sso.sourceLabel')">
            <label v-for="option in sources" :key="option" :data-active="source === option">
              <input v-model="source" type="radio" :value="option" />
              <span>{{ t(`admin.sso.source.${option}`) }}</span>
            </label>
          </div>

          <div class="sso-fields">
            <label v-if="source === 'metadata_url'" class="wide">
              {{ t('admin.sso.fields.metadataURL') }}
              <input
                v-model.trim="metadataURL" class="app-input" type="url" inputmode="url"
                :placeholder="t('admin.sso.fields.metadataURLPlaceholder')"
              />
              <small v-if="fieldErrors.idp_metadata_url" class="sso-field-error">{{ fieldErrors.idp_metadata_url }}</small>
            </label>

            <label v-else-if="source === 'metadata_xml'" class="wide">
              {{ t('admin.sso.fields.metadataXML') }}
              <textarea v-model="metadataXML" class="app-input sso-textarea" rows="8" spellcheck="false" />
              <small v-if="fieldErrors.idp_metadata_xml" class="sso-field-error">{{ fieldErrors.idp_metadata_xml }}</small>
            </label>

            <template v-else>
              <label>
                {{ t('admin.sso.fields.entityID') }}
                <input v-model.trim="idpEntityID" class="app-input" type="text" />
                <small v-if="fieldErrors.idp_entity_id" class="sso-field-error">{{ fieldErrors.idp_entity_id }}</small>
              </label>
              <label>
                {{ t('admin.sso.fields.ssoURL') }}
                <input v-model.trim="idpSSOURL" class="app-input" type="url" inputmode="url" />
                <small v-if="fieldErrors.idp_sso_url" class="sso-field-error">{{ fieldErrors.idp_sso_url }}</small>
              </label>
              <label class="wide">
                {{ t('admin.sso.fields.certificate') }}
                <textarea v-model="certificate" class="app-input sso-textarea" rows="6" spellcheck="false" />
                <small v-if="fieldErrors.certificates" class="sso-field-error">{{ fieldErrors.certificates }}</small>
              </label>
            </template>

            <label class="wide">
              {{ t('admin.sso.fields.emailDomains') }}
              <input
                v-model.trim="emailDomains" class="app-input" type="text"
                :placeholder="t('admin.sso.fields.emailDomainsPlaceholder')"
              />
              <small class="sso-hint">{{ t('admin.sso.fields.emailDomainsHint') }}</small>
              <small v-if="fieldErrors.email_domains" class="sso-field-error">{{ fieldErrors.email_domains }}</small>
            </label>
          </div>

          <label class="sso-toggle">
            <input v-model="allowIdPInitiated" type="checkbox" />
            <span>
              <b>{{ t('admin.sso.fields.allowIdPInitiated') }}</b>
              <small>{{ t('admin.sso.fields.allowIdPInitiatedHint') }}</small>
            </span>
          </label>

          <p v-if="formError" class="sso-notice sso-notice--error" role="alert">
            <AlertTriangle :size="15" /><span>{{ formError }}</span>
          </p>
        </form>
      </section>

      <section v-if="data?.configured && configuration" class="app-surface sso-section">
        <header>
          <div>
            <p class="app-eyebrow">{{ t('admin.sso.eyebrow') }}</p>
            <h2>{{ t('admin.sso.enforcementTitle') }}</h2>
          </div>
        </header>
        <div class="sso-body">
          <p v-if="warnings.includes('mfa_managed_by_idp')" class="sso-notice sso-notice--info" role="status">
            <ShieldCheck :size="15" /><span>{{ t('admin.sso.mfaManagedByIdP') }}</span>
          </p>
          <p v-if="warnings.includes('no_valid_certificate')" class="sso-notice sso-notice--error" role="alert">
            <AlertTriangle :size="15" /><span>{{ t('admin.sso.warnings.no_valid_certificate') }}</span>
          </p>
          <p v-if="warnings.includes('certificate_expiring')" class="sso-notice sso-notice--warning" role="alert">
            <AlertTriangle :size="15" /><span>{{ t('admin.sso.warnings.certificate_expiring') }}</span>
          </p>

          <div class="sso-modes">
            <button
              v-for="mode in modes" :key="mode" type="button" class="sso-mode"
              :data-active="configuration.mode === mode"
              :disabled="Boolean(busyAction) || !canWrite || (mode === 'required' && !canRequire)"
              @click="setMode(mode)"
            >
              <b>{{ t(`admin.sso.mode.${mode}`) }}</b>
              <small>{{ t(`admin.sso.modeDescription.${mode}`) }}</small>
            </button>
          </div>
          <p v-if="!canRequire" class="sso-hint">{{ t('admin.sso.requiredBlocked') }}</p>
        </div>
        <dl class="sso-ledger">
          <div>
            <dt>{{ t('admin.sso.lastTested') }}</dt>
            <dd>{{ configuration.last_tested_at ? dateTime(configuration.last_tested_at) : t('admin.sso.never') }}</dd>
          </div>
          <div>
            <dt>{{ t('admin.sso.lastLogin') }}</dt>
            <dd>{{ configuration.last_successful_login_at ? dateTime(configuration.last_successful_login_at) : t('admin.sso.never') }}</dd>
          </div>
        </dl>
      </section>

      <section v-if="data?.configured && configuration" class="app-surface sso-section">
        <header>
          <div>
            <p class="app-eyebrow">{{ t('admin.sso.eyebrow') }}</p>
            <h2>{{ t('admin.sso.certificatesTitle') }}</h2>
          </div>
          <button
            class="app-button sso-danger-button" type="button"
            :disabled="Boolean(busyAction) || !canWrite" @click="disableSSO"
          >
            <Trash2 :size="14" /> {{ t('admin.sso.disable') }}
          </button>
        </header>
        <div class="sso-table">
          <div class="sso-row sso-row--head">
            <span>{{ t('admin.sso.columns.certificate') }}</span>
            <span>{{ t('admin.sso.columns.fingerprint') }}</span>
            <span>{{ t('admin.sso.columns.expiry') }}</span>
            <span />
          </div>
          <div v-for="item in configuration.certificates" :key="item.id" class="sso-row">
            <span><b>{{ item.subject || t('admin.sso.unnamedCertificate') }}</b></span>
            <span><code>{{ item.fingerprint.slice(0, 24) }}</code></span>
            <span>{{ dateTime(item.not_after) }}</span>
            <button
              class="app-icon-button sso-danger-icon" type="button"
              :title="t('admin.sso.removeCertificate')"
              :disabled="Boolean(busyAction) || !canWrite" @click="removeCertificate(item.id)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
        <div class="sso-body">
          <p class="sso-hint">{{ t('admin.sso.certificatesHint') }}</p>
          <div class="sso-fields">
            <label class="wide">
              {{ t('admin.sso.fields.newCertificate') }}
              <textarea v-model="certificate" class="app-input sso-textarea" rows="5" spellcheck="false" />
            </label>
          </div>
          <div>
            <button
              class="app-button" type="button"
              :disabled="Boolean(busyAction) || !canWrite || !certificate.trim()" @click="addCertificate"
            >
              <Plus :size="14" /> {{ t('admin.sso.addCertificate') }}
            </button>
          </div>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.sso { display: grid; gap: 14px; }
.sso-section { overflow: hidden; }
.sso-section > header {
  display: flex; min-height: 68px; align-items: center; justify-content: space-between;
  gap: 14px; padding: 11px 16px; border-bottom: 1px solid var(--line);
}
.sso-section h2 { margin: 3px 0 0; font-size: 14px; }
.sso-header-actions { display: flex; align-items: center; gap: 8px; }
.sso-body { display: grid; gap: 14px; padding: 18px; }
.sso-body + .sso-body { border-top: 1px solid var(--line); }
.sso-hint { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.55; }

.sso-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 13px; }
.sso-fields label {
  display: flex; min-width: 0; flex-direction: column; gap: 6px;
  color: var(--ink-soft); font-size: 10px; font-weight: 750;
}
.sso-fields .wide { grid-column: 1 / -1; }
/* Hints and errors sit inside a label, so they must opt out of its weight. */
.sso-fields label small { font-weight: 500; }
.sso-fields label .sso-hint { font-size: 9px; }
.sso-field-error { color: var(--red); font-size: 9px; font-weight: 700; }
.sso-textarea { min-height: 92px; font-family: monospace; font-size: 10px; line-height: 1.5; resize: vertical; }
.sso-copy-field { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 6px; }
.sso-copy-field input { font-family: monospace; font-size: 10px; }

.sso-sources { display: flex; flex-wrap: wrap; gap: 7px; }
.sso-sources label {
  display: inline-flex; align-items: center; gap: 7px; padding: 7px 12px;
  background: var(--surface); border: 1px solid var(--line-strong); border-radius: var(--radius);
  color: var(--ink-soft); font-size: 10px; font-weight: 750; cursor: pointer;
}
.sso-sources label[data-active='true'] { background: var(--teal-soft); border-color: var(--teal); color: var(--teal); }

.sso-toggle {
  display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: start; gap: 9px;
  padding: 12px 14px; background: var(--surface-subtle); border: 1px solid var(--line); border-radius: var(--radius);
}
.sso-toggle span { display: grid; gap: 3px; }
.sso-toggle b { color: var(--ink-soft); font-size: 11px; }
.sso-toggle small { color: var(--muted); font-size: 9px; line-height: 1.5; }

.sso-modes { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 9px; }
.sso-mode {
  display: grid; align-content: start; gap: 5px; min-height: 78px; padding: 12px 13px;
  background: var(--surface); border: 1px solid var(--line-strong); border-radius: var(--radius);
  text-align: left; cursor: pointer;
}
.sso-mode b { color: var(--ink-soft); font-size: 11px; }
.sso-mode small { color: var(--muted); font-size: 9px; line-height: 1.5; }
.sso-mode[data-active='true'] { background: var(--teal-soft); border-color: var(--teal); }
.sso-mode[data-active='true'] b { color: var(--teal); }
.sso-mode:disabled { cursor: not-allowed; opacity: .55; }

.sso-ledger { margin: 0; border-top: 1px solid var(--line); }
.sso-ledger > div {
  display: flex; min-height: 48px; align-items: center; justify-content: space-between;
  gap: 14px; padding: 0 16px; border-bottom: 1px solid var(--line);
}
.sso-ledger > div:last-child { border-bottom: 0; }
.sso-ledger dt { color: var(--muted); font-size: 10px; }
.sso-ledger dd { margin: 0; color: var(--ink-soft); font-size: 11px; font-weight: 700; }

.sso-table { width: 100%; }
.sso-row {
  display: grid; grid-template-columns: minmax(180px, 1.4fr) minmax(150px, 1fr) 150px 38px;
  min-height: 56px; align-items: center; gap: 14px; padding: 7px 15px;
  border-bottom: 1px solid var(--line); color: var(--muted); font-size: 10px;
}
.sso-row--head {
  min-height: 36px; padding-top: 0; padding-bottom: 0; background: var(--surface-subtle);
  font-size: 9px; font-weight: 750; text-transform: uppercase;
}
.sso-row > span { min-width: 0; }
.sso-row > span > b { display: block; color: var(--ink-soft); font-size: 11px; }
.sso-row code { display: block; overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.sso-danger-icon { color: var(--red); }
/* Matches the destructive text button in billing.vue; there is no
   app-button--danger modifier in the design system. */
.sso-danger-button { background: var(--surface); border-color: var(--red-line); color: var(--red); }

.sso-notice {
  display: grid; grid-template-columns: 15px minmax(0, 1fr); align-items: center; gap: 8px;
  margin: 0; padding: 11px 13px; border: 1px solid var(--line); border-radius: var(--radius);
  font-size: 11px; line-height: 1.5;
}
.sso-notice--success { background: var(--green-soft); border-color: var(--green-line); color: var(--green); }
.sso-notice--error { background: var(--red-soft); border-color: var(--red-line); color: var(--red); }
.sso-notice--warning { background: var(--amber-soft); border-color: var(--amber-line); color: var(--amber-ink); }
.sso-notice--info { background: var(--teal-soft); border-color: var(--teal-line); color: var(--teal); }

/* Mirrors the settings tables: the head row drops out and only the primary
   column and its action survive. */
@media (max-width: 900px) {
  .sso-fields, .sso-modes { grid-template-columns: 1fr; }
  .sso-row--head { display: none; }
  .sso-row { grid-template-columns: minmax(0, 1fr) 38px; }
  .sso-row > span:not(:first-child) { display: none; }
}
</style>
