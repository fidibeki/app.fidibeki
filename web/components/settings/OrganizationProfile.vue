<script setup lang="ts">
import {
  ExternalLink,
  ImagePlus,
  Save,
  Trash2,
} from '@lucide/vue'
import type { OrganizationProfile } from '~/types/api'
import { apiFieldErrors } from '~/utils/api-fetch'

const emit = defineEmits<{
  updated: [profile: OrganizationProfile]
}>()

const { $apiFetch } = useNuxtApp()
const { canWrite, session, tenant } = useAuth()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { dateTime } = useRegionalFormat()
const uploadInput = ref<HTMLInputElement | null>(null)
const saving = ref(false)
const uploading = ref(false)
const removingLogo = ref(false)
const notice = ref('')
const failure = ref('')
const fields = ref<Record<string, string>>({})

const { data, status, error, refresh } = await useAsyncData(
  'organization-profile',
  () => $apiFetch<{ organization_profile: OrganizationProfile }>('/v1/organization-profile'),
)

const form = reactive({
  name: '',
  short_name: '',
  website_url: '',
  support_email: '',
  accent_color: '#087f73',
  default_locale: '',
  default_timezone: '',
  privacy_policy_url: '',
  terms_url: '',
})

const profile = computed(() => data.value?.organization_profile)
const writable = computed(() => canWrite('tenant:update'))
const previewDate = computed(() => {
  const locale = form.default_locale || 'en'
  const timeZone = form.default_timezone || 'UTC'
  try {
    return new Intl.DateTimeFormat(locale, {
      dateStyle: 'medium',
      timeStyle: 'short',
      timeZone,
    }).format(new Date('2026-07-23T12:00:00Z'))
  } catch {
    return dateTime('2026-07-23T12:00:00Z')
  }
})

function setForm(value: OrganizationProfile) {
  form.name = value.name
  form.short_name = value.short_name || ''
  form.website_url = value.website_url || ''
  form.support_email = value.support_email || ''
  form.accent_color = value.accent_color || '#087f73'
  form.default_locale = value.default_locale || ''
  form.default_timezone = value.default_timezone || ''
  form.privacy_policy_url = value.privacy_policy_url || ''
  form.terms_url = value.terms_url || ''
}

function syncSession(value: OrganizationProfile) {
  if (!session.value.tenant) return
  Object.assign(session.value.tenant, {
    name: value.name,
    short_name: value.short_name,
    website_url: value.website_url,
    support_email: value.support_email,
    default_locale: value.default_locale,
    default_timezone: value.default_timezone,
    privacy_policy_url: value.privacy_policy_url,
    terms_url: value.terms_url,
    logo_version: value.logo_version,
  })
  session.value.tenant.widget_appearance_defaults = {
    ...(session.value.tenant.widget_appearance_defaults || {}),
    ...(value.accent_color ? { accent_color: value.accent_color } : {}),
  }
  if (!value.accent_color) {
    delete session.value.tenant.widget_appearance_defaults.accent_color
  }
  emit('updated', value)
}

watch(profile, (value) => {
  if (value) setForm(value)
}, { immediate: true })

function optional(value: string) {
  return value.trim() || null
}

async function save() {
  if (!writable.value || saving.value) return
  saving.value = true
  notice.value = ''
  failure.value = ''
  fields.value = {}
  try {
    const response = await $apiFetch<{ organization_profile: OrganizationProfile }>(
      '/v1/organization-profile',
      {
        method: 'PATCH',
        body: {
          name: form.name,
          short_name: optional(form.short_name),
          website_url: optional(form.website_url),
          support_email: optional(form.support_email),
          accent_color: optional(form.accent_color),
          default_locale: optional(form.default_locale),
          default_timezone: optional(form.default_timezone),
          privacy_policy_url: optional(form.privacy_policy_url),
          terms_url: optional(form.terms_url),
        },
      },
    )
    data.value = response
    setForm(response.organization_profile)
    syncSession(response.organization_profile)
    notice.value = t('admin.organization.notices.saved')
  } catch (saveError) {
    fields.value = apiFieldErrors(saveError)
    failure.value = Object.keys(fields.value).length
      ? ''
      : localizedError.message(saveError, 'admin.organization.errors.save')
  } finally {
    saving.value = false
  }
}

async function uploadLogo(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || !writable.value || uploading.value || !profile.value) return
  uploading.value = true
  notice.value = ''
  failure.value = ''
  fields.value = {}
  try {
    const body = new FormData()
    body.append('image', file)
    const response = await $apiFetch<{ logo_version: string }>(
      '/v1/organization-profile/logo',
      { method: 'PUT', body },
    )
    data.value = {
      organization_profile: {
        ...profile.value,
        logo_version: response.logo_version,
      },
    }
    syncSession(data.value.organization_profile)
    notice.value = t('admin.organization.notices.logoUpdated')
  } catch (uploadError) {
    fields.value = apiFieldErrors(uploadError)
    failure.value = Object.keys(fields.value).length
      ? ''
      : localizedError.message(uploadError, 'admin.organization.errors.upload')
  } finally {
    uploading.value = false
  }
}

async function removeLogo() {
  if (!writable.value || removingLogo.value || !profile.value) return
  removingLogo.value = true
  notice.value = ''
  failure.value = ''
  try {
    await $apiFetch('/v1/organization-profile/logo', { method: 'DELETE' })
    data.value = {
      organization_profile: {
        ...profile.value,
        logo_version: null,
      },
    }
    syncSession(data.value.organization_profile)
    notice.value = t('admin.organization.notices.logoRemoved')
  } catch (removeError) {
    failure.value = localizedError.message(removeError, 'admin.organization.errors.remove')
  } finally {
    removingLogo.value = false
  }
}
</script>

<template>
  <section class="app-surface organization-profile">
    <header>
      <div>
        <p class="app-eyebrow">{{ t('admin.organization.eyebrow') }}</p>
        <h2>{{ t('admin.organization.title') }}</h2>
      </div>
      <button
        v-if="status === 'success'"
        class="app-button app-button--primary"
        type="submit"
        form="organization-profile-form"
        :disabled="saving || !writable"
      >
        <Save :size="14" /> {{ saving ? t('admin.common.saving') : t('admin.organization.save') }}
      </button>
    </header>

    <div v-if="status === 'pending'" class="organization-state">
      <span class="app-spinner" />
      <span>{{ t('admin.organization.loading') }}</span>
    </div>
    <div v-else-if="error" class="organization-state organization-state--error">
      <span>{{ localizedError.message(error, 'admin.organization.unavailable') }}</span>
      <button class="app-button" @click="refresh()">{{ t('admin.common.tryAgain') }}</button>
    </div>

    <form
      v-else-if="profile"
      id="organization-profile-form"
      class="organization-body"
      @submit.prevent="save"
    >
      <aside class="organization-identity">
        <div class="organization-logo-preview">
          <WorkspaceMark
            :tenant-id="tenant?.id || ''"
            :name="form.name"
            :logo-version="profile.logo_version"
            :size="256"
            :alt="form.name ? t('admin.organization.logoAlt', { name: form.name }) : t('admin.organization.logo')"
          />
        </div>
        <div class="organization-logo-actions">
          <input
            ref="uploadInput"
            class="sr-only"
            type="file"
            accept=".png,.jpg,.jpeg,.webp,image/png,image/jpeg,image/webp"
            :disabled="!writable || uploading"
            :aria-label="t('admin.organization.logoFile')"
            @change="uploadLogo"
          />
          <button
            class="app-button"
            type="button"
            :disabled="!writable || uploading"
            @click="uploadInput?.click()"
          >
            <ImagePlus :size="14" /> {{ uploading ? t('admin.organization.uploading') : profile.logo_version ? t('admin.organization.replaceLogo') : t('admin.organization.uploadLogo') }}
          </button>
          <button
            v-if="profile.logo_version"
            class="app-icon-button organization-remove"
            type="button"
            :title="t('admin.organization.removeLogo')"
            :disabled="!writable || removingLogo"
            @click="removeLogo"
          >
            <Trash2 :size="15" />
          </button>
        </div>
        <p v-if="fields.image" class="organization-field-error" role="alert">{{ fields.image }}</p>

        <div class="organization-customer-preview" :style="{ '--organization-accent': form.accent_color }">
          <span>{{ t('admin.organization.preview') }}</span>
          <b>{{ form.name || t('admin.organization.workspaceName') }}</b>
          <small>{{ form.support_email || t('admin.organization.supportFallback') }}</small>
          <time>{{ previewDate }}</time>
          <a v-if="form.website_url" :href="form.website_url" target="_blank" rel="noreferrer">
            {{ t('admin.organization.website') }} <ExternalLink :size="11" />
          </a>
        </div>
      </aside>

      <fieldset :disabled="!writable || saving">
        <div class="organization-form-grid">
          <label>
            {{ t('admin.organization.publicName') }}
            <input v-model.trim="form.name" class="app-input" required maxlength="120" />
            <small v-if="fields.name" class="organization-field-error">{{ fields.name }}</small>
          </label>
          <label>
            {{ t('admin.organization.shortName') }}
            <input v-model.trim="form.short_name" class="app-input" maxlength="32" placeholder="ACME" />
            <small v-if="fields.short_name" class="organization-field-error">{{ fields.short_name }}</small>
          </label>
          <label>
            {{ t('admin.organization.website') }}
            <input v-model.trim="form.website_url" class="app-input" type="url" maxlength="2048" placeholder="https://example.com" />
            <small v-if="fields.website_url" class="organization-field-error">{{ fields.website_url }}</small>
          </label>
          <label>
            {{ t('admin.organization.supportEmail') }}
            <input v-model.trim="form.support_email" class="app-input" type="email" maxlength="320" placeholder="support@example.com" />
            <small v-if="fields.support_email" class="organization-field-error">{{ fields.support_email }}</small>
          </label>
          <label>
            {{ t('admin.organization.defaultLanguage') }}
            <select v-model="form.default_locale" class="app-select">
              <option value="">{{ t('admin.organization.browserDefault') }}</option>
              <option value="en">{{ t('admin.organization.english') }}</option>
              <option value="pt">{{ t('admin.organization.portuguese') }}</option>
              <option value="pt-AO">{{ t('admin.organization.portugueseAngola') }}</option>
              <option value="pt-PT">{{ t('admin.organization.portuguesePortugal') }}</option>
            </select>
            <small v-if="fields.default_locale" class="organization-field-error">{{ fields.default_locale }}</small>
          </label>
          <label>
            {{ t('admin.organization.defaultTimezone') }}
            <input v-model.trim="form.default_timezone" class="app-input" list="organization-timezones" maxlength="100" placeholder="Europe/Lisbon" />
            <datalist id="organization-timezones">
              <option value="Africa/Luanda" />
              <option value="Europe/Lisbon" />
              <option value="UTC" />
              <option value="America/New_York" />
              <option value="America/Sao_Paulo" />
            </datalist>
            <small v-if="fields.default_timezone" class="organization-field-error">{{ fields.default_timezone }}</small>
          </label>
          <label class="organization-accent-field">
            {{ t('admin.organization.brandAccent') }}
            <span>
              <input v-model="form.accent_color" type="color" :aria-label="t('admin.organization.brandAccentSwatch')" />
              <input v-model.trim="form.accent_color" class="app-input" pattern="^#[0-9A-Fa-f]{6}$" maxlength="7" />
            </span>
            <small v-if="fields.accent_color" class="organization-field-error">{{ fields.accent_color }}</small>
          </label>
          <label>
            {{ t('admin.organization.privacyPolicy') }}
            <input v-model.trim="form.privacy_policy_url" class="app-input" type="url" maxlength="2048" placeholder="https://example.com/privacy" />
            <small v-if="fields.privacy_policy_url" class="organization-field-error">{{ fields.privacy_policy_url }}</small>
          </label>
          <label class="wide">
            {{ t('admin.organization.terms') }}
            <input v-model.trim="form.terms_url" class="app-input" type="url" maxlength="2048" placeholder="https://example.com/terms" />
            <small v-if="fields.terms_url" class="organization-field-error">{{ fields.terms_url }}</small>
          </label>
        </div>
        <p v-if="!writable" class="organization-readonly">{{ t('admin.organization.readOnly') }}</p>
        <p v-if="failure" class="settings-form-error" role="alert">{{ failure }}</p>
        <p v-if="notice" class="organization-notice" role="status">{{ notice }}</p>
      </fieldset>
    </form>
  </section>
</template>

<style scoped>
.organization-profile {
  grid-column: 1 / -1;
  overflow: hidden;
}
.organization-profile > header {
  display: flex;
  min-height: 68px;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 11px 16px;
  border-bottom: 1px solid var(--line);
}
.organization-profile h2 {
  margin: 3px 0 0;
  font-size: 14px;
}
.organization-state {
  display: flex;
  min-height: 180px;
  align-items: center;
  justify-content: center;
  gap: 9px;
  color: var(--muted);
  font-size: 10px;
}
.organization-state--error {
  flex-direction: column;
}
.organization-body {
  display: grid;
  grid-template-columns: 230px minmax(0, 1fr);
}
.organization-identity {
  padding: 18px;
  background: #f8faf9;
  border-right: 1px solid var(--line);
}
.organization-logo-preview {
  width: 112px;
  height: 112px;
  margin: 0 auto;
  overflow: hidden;
  background: #fff;
  border: 1px solid var(--line-strong);
  border-radius: 6px;
  font-size: 22px;
}
.organization-logo-actions {
  display: flex;
  justify-content: center;
  gap: 5px;
  margin-top: 11px;
}
.organization-remove {
  color: var(--red);
}
.organization-customer-preview {
  display: flex;
  margin-top: 22px;
  padding-left: 11px;
  flex-direction: column;
  gap: 5px;
  border-left: 3px solid var(--organization-accent, var(--teal));
}
.organization-customer-preview > span {
  color: var(--muted);
  font-size: 8px;
  font-weight: 750;
  text-transform: uppercase;
}
.organization-customer-preview b {
  overflow-wrap: anywhere;
  font-size: 12px;
}
.organization-customer-preview small,
.organization-customer-preview time,
.organization-customer-preview a {
  color: var(--muted);
  font-size: 9px;
  text-decoration: none;
}
.organization-customer-preview a {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--teal);
}
.organization-body > fieldset {
  min-width: 0;
  margin: 0;
  padding: 18px;
  border: 0;
}
.organization-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 13px;
}
.organization-form-grid label {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 6px;
  color: var(--ink-soft);
  font-size: 10px;
  font-weight: 750;
}
.organization-form-grid .wide {
  grid-column: 1 / -1;
}
.organization-accent-field > span {
  display: grid;
  grid-template-columns: 38px minmax(0, 1fr);
  gap: 7px;
}
.organization-accent-field input[type='color'] {
  width: 38px;
  height: 38px;
  padding: 3px;
  background: #fff;
  border: 1px solid var(--line-strong);
  border-radius: 5px;
}
.organization-field-error {
  margin: 6px 0 0;
  color: var(--red);
  font-size: 9px;
  font-weight: 650;
}
.organization-readonly,
.organization-notice {
  margin: 13px 0 0;
  color: var(--muted);
  font-size: 9px;
}
.organization-notice {
  color: var(--green);
}
.settings-form-error {
  margin-top: 13px;
  padding: 9px 10px;
  background: var(--red-soft);
  color: var(--red);
  border-radius: 4px;
  font-size: 10px;
}
@media (max-width: 760px) {
  .organization-body {
    grid-template-columns: 1fr;
  }
  .organization-identity {
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }
}
@media (max-width: 540px) {
  .organization-profile > header {
    align-items: flex-start;
    flex-direction: column;
  }
  .organization-profile > header .app-button {
    width: 100%;
  }
  .organization-form-grid {
    grid-template-columns: 1fr;
  }
  .organization-form-grid .wide {
    grid-column: auto;
  }
}
</style>
