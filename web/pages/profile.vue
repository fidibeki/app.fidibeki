<script setup lang="ts">
import {
  ImagePlus,
  Save,
  ShieldCheck,
  Trash2,
  UserRound,
} from '@lucide/vue'
import type { Member } from '~/types/api'

const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const { member, session, tenant, workspaceWritable } = useAuth()
const regional = useRegionalContext()
const localizedError = useLocalizedAPIError()
const fileInput = ref<HTMLInputElement | null>(null)
const name = ref(member.value?.name || '')
const locale = ref(member.value?.locale || '')
const timezone = ref(member.value?.timezone || '')
const inheritTimezone = ref(!member.value?.timezone)
const saving = ref(false)
const uploading = ref(false)
const removing = ref(false)
const notice = ref('')
const failure = ref('')
const fields = ref<Record<string, string>>({})
const timezoneSuggestions = [
  'UTC',
  'Africa/Luanda',
  'Africa/Maputo',
  'Africa/Johannesburg',
  'Europe/Lisbon',
  'Europe/London',
  'America/Sao_Paulo',
  'America/New_York',
]

function languageLabel(value: string) {
  const key = {
    en: 'en',
    pt: 'pt',
    'pt-AO': 'ptAO',
    'pt-PT': 'ptPT',
    'en-XA': 'pseudo',
  }[value] || 'en'
  return t(`profile.languages.${key}`)
}

function sourceLabel(source: string) {
  return t(`profile.preferenceSources.${source}`)
}

function applyMember(next: Member) {
  if (!session.value.member) return
  session.value.member = {
    ...session.value.member,
    ...next,
  }
  name.value = session.value.member.name || ''
  locale.value = session.value.member.locale || ''
  timezone.value = session.value.member.timezone || ''
  inheritTimezone.value = !session.value.member.timezone
}

function updateTimezoneMode() {
  if (!inheritTimezone.value && !timezone.value.trim()) {
    timezone.value = regional.timezone.value
  }
}

async function saveProfile() {
  if (!workspaceWritable.value || saving.value) return
  saving.value = true
  notice.value = ''
  failure.value = ''
  fields.value = {}
  try {
    const response = await $apiFetch<{ member: Member }>('/v1/profile', {
      method: 'PATCH',
      body: {
        name: name.value.trim() || null,
        locale: locale.value || null,
        timezone: inheritTimezone.value ? null : timezone.value.trim() || null,
      },
    })
    applyMember(response.member)
    await regional.apply()
    notice.value = 'profile.saved'
  } catch (saveError) {
    fields.value = localizedError.fields(saveError)
    failure.value = Object.keys(fields.value).length
      ? ''
      : localizedError.message(saveError, 'profile.errors.save')
  } finally {
    saving.value = false
  }
}

async function uploadAvatar(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || !member.value || !workspaceWritable.value || uploading.value) return
  uploading.value = true
  notice.value = ''
  failure.value = ''
  fields.value = {}
  try {
    const body = new FormData()
    body.append('image', file)
    const response = await $apiFetch<{ avatar_version: string }>('/v1/profile/avatar', {
      method: 'PUT',
      body,
    })
    applyMember({ ...member.value, avatar_version: response.avatar_version })
    notice.value = 'profile.photoUpdated'
  } catch (uploadError) {
    fields.value = localizedError.fields(uploadError)
    failure.value = Object.keys(fields.value).length
      ? ''
      : localizedError.message(uploadError, 'profile.errors.upload')
  } finally {
    uploading.value = false
  }
}

async function removeAvatar() {
  if (!member.value || !workspaceWritable.value || removing.value) return
  removing.value = true
  notice.value = ''
  failure.value = ''
  try {
    await $apiFetch('/v1/profile/avatar', { method: 'DELETE' })
    applyMember({ ...member.value, avatar_version: null })
    notice.value = 'profile.photoRemoved'
  } catch (removeError) {
    failure.value = localizedError.message(removeError, 'profile.errors.remove')
  } finally {
    removing.value = false
  }
}

useHead({ title: computed(() => t('profile.pageTitle')) })
</script>

<template>
  <div class="app-page profile-page">
    <header class="app-page-head">
      <div>
        <p class="app-eyebrow">{{ t('profile.eyebrow') }}</p>
        <h1>{{ t('profile.title') }}</h1>
        <span>{{ t('profile.subtitle', { workspace: tenant?.name || t('profile.workspaceFallback') }) }}</span>
      </div>
    </header>

    <section v-if="member" class="app-surface profile-surface">
      <header>
        <div>
          <p class="app-eyebrow">{{ t('profile.memberIdentity') }}</p>
          <h2>{{ member.name || member.email.split('@')[0] }}</h2>
        </div>
        <span class="profile-access"><ShieldCheck :size="13" /> {{ t(`profile.roles.${member.role}`) }}</span>
      </header>

      <div class="profile-body">
        <aside>
          <div class="profile-avatar">
            <MemberAvatar
              :member-id="member.id"
              :name="member.name"
              :email="member.email"
              :avatar-version="member.avatar_version"
              :size="256"
            />
          </div>
          <div class="profile-avatar-actions">
            <input
              ref="fileInput"
              class="sr-only"
              type="file"
              accept=".png,.jpg,.jpeg,.webp,image/png,image/jpeg,image/webp"
              :disabled="!workspaceWritable || uploading"
              :aria-label="t('profile.photoFile')"
              @change="uploadAvatar"
            />
            <button
              class="app-button"
              type="button"
              :disabled="!workspaceWritable || uploading"
              @click="fileInput?.click()"
            >
              <ImagePlus :size="14" /> {{ uploading ? t('profile.uploading') : member.avatar_version ? t('profile.replacePhoto') : t('profile.uploadPhoto') }}
            </button>
            <button
              v-if="member.avatar_version"
              class="app-icon-button profile-remove"
              type="button"
              :title="t('profile.removePhoto')"
              :disabled="!workspaceWritable || removing"
              @click="removeAvatar"
            >
              <Trash2 :size="15" />
            </button>
          </div>
          <p v-if="fields.image" class="profile-field-error" role="alert">{{ fields.image }}</p>
        </aside>

        <form @submit.prevent="saveProfile">
          <fieldset :disabled="!workspaceWritable || saving">
            <label>
              {{ t('profile.displayName') }}
              <input v-model="name" class="app-input" maxlength="120" :placeholder="t('profile.namePlaceholder')" />
              <small v-if="fields.name" class="profile-field-error">{{ fields.name }}</small>
            </label>
            <label>
              {{ t('profile.email') }}
              <input class="app-input" :value="member.email" type="email" disabled />
            </label>
            <dl>
              <div><dt>{{ t('profile.workspace') }}</dt><dd>{{ tenant?.name }}</dd></div>
              <div><dt>{{ t('profile.role') }}</dt><dd>{{ t(`profile.roles.${member.role}`) }}</dd></div>
              <div><dt>{{ t('profile.access') }}</dt><dd>{{ t(`profile.statuses.${member.status}`) }}</dd></div>
            </dl>
            <div class="profile-section-heading">
              <h3>{{ t('profile.regionalTitle') }}</h3>
              <p>{{ t('profile.regionalDescription') }}</p>
            </div>
            <div class="profile-field">
              <label for="profile-language">{{ t('profile.language') }}</label>
              <select id="profile-language" v-model="locale" class="app-select">
                <option value="">{{ t('profile.languageInherit') }}</option>
                <option value="en">{{ t('profile.languages.en') }}</option>
                <option value="pt">{{ t('profile.languages.pt') }}</option>
                <option value="pt-AO">{{ t('profile.languages.ptAO') }}</option>
                <option value="pt-PT">{{ t('profile.languages.ptPT') }}</option>
                <option v-if="locale === 'en-XA'" value="en-XA">{{ t('profile.languages.pseudo') }}</option>
              </select>
              <small>{{ t('profile.effectivePreference', {
                value: languageLabel(regional.effectiveLocale.value),
                source: sourceLabel(regional.localeSource.value),
              }) }}</small>
              <small v-if="fields.locale" class="profile-field-error">{{ fields.locale }}</small>
            </div>
            <div class="profile-field">
              <label for="profile-timezone">{{ t('profile.timezone') }}</label>
              <label class="profile-checkbox">
                <input v-model="inheritTimezone" type="checkbox" @change="updateTimezoneMode" />
                <span>{{ t('profile.timezoneInherit') }}</span>
              </label>
              <input
                id="profile-timezone"
                v-model="timezone"
                class="app-input"
                list="profile-timezones"
                maxlength="100"
                :required="!inheritTimezone"
                :disabled="inheritTimezone"
                :placeholder="t('profile.timezonePlaceholder')"
              />
              <datalist id="profile-timezones">
                <option v-for="suggestion in timezoneSuggestions" :key="suggestion" :value="suggestion" />
              </datalist>
              <small>{{ t('profile.effectivePreference', {
                value: regional.timezone.value,
                source: sourceLabel(regional.timezoneSource.value),
              }) }}</small>
              <small v-if="fields.timezone" class="profile-field-error">{{ fields.timezone }}</small>
            </div>
          </fieldset>
          <div class="profile-form-footer">
            <p v-if="!workspaceWritable">{{ t('profile.readonly') }}</p>
            <p v-else-if="failure" class="profile-failure" role="alert">{{ failure }}</p>
            <p v-else-if="notice" class="profile-notice" role="status">{{ t(notice) }}</p>
            <button class="app-button app-button--primary" type="submit" :disabled="!workspaceWritable || saving">
              <Save :size="14" /> {{ saving ? t('profile.saving') : t('profile.save') }}
            </button>
          </div>
        </form>
      </div>
    </section>

    <section v-else class="app-surface app-state">
      <div><UserRound :size="24" /><h2>{{ t('profile.unavailable') }}</h2></div>
    </section>
  </div>
</template>

<style scoped>
.profile-page {
  max-width: 920px;
}
.profile-surface {
  overflow: hidden;
}
.profile-surface > header {
  display: flex;
  min-height: 68px;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 11px 16px;
  border-bottom: 1px solid var(--line);
}
.profile-surface h2 {
  margin: 3px 0 0;
  font-size: 14px;
}
.profile-access {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--teal);
  font-size: 9px;
  font-weight: 750;
  text-transform: capitalize;
}
.profile-body {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
}
.profile-body > aside {
  padding: 24px;
  background: #f8faf9;
  border-right: 1px solid var(--line);
}
.profile-avatar {
  width: 136px;
  height: 136px;
  margin: 0 auto;
  overflow: hidden;
  border: 1px solid var(--line-strong);
  border-radius: 6px;
  font-size: 24px;
}
.profile-avatar-actions {
  display: flex;
  justify-content: center;
  gap: 5px;
  margin-top: 12px;
}
.profile-remove {
  color: var(--red);
}
.profile-body > form {
  min-width: 0;
}
.profile-body fieldset {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  margin: 0;
  padding: 24px;
  border: 0;
}
.profile-body label {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 6px;
  color: var(--ink-soft);
  font-size: 10px;
  font-weight: 750;
}
.profile-field {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 6px;
}
.profile-field > label:first-child {
  color: var(--ink-soft);
  font-size: 10px;
  font-weight: 750;
}
.profile-body .profile-checkbox {
  display: flex;
  min-height: 22px;
  align-items: center;
  flex-direction: row;
  gap: 7px;
  color: var(--muted);
  font-size: 9px;
  font-weight: 650;
}
.profile-checkbox input {
  width: 14px;
  height: 14px;
  margin: 0;
  accent-color: var(--teal);
}
.profile-body label > small,
.profile-field > small {
  color: var(--muted);
  font-size: 9px;
  font-weight: 500;
  line-height: 1.4;
}
.profile-section-heading {
  grid-column: 1 / -1;
  padding-top: 8px;
  border-top: 1px solid var(--line);
}
.profile-section-heading h3 {
  margin: 0;
  color: var(--ink);
  font-size: 11px;
}
.profile-section-heading p {
  max-width: 560px;
  margin: 4px 0 0;
  color: var(--muted);
  font-size: 9px;
  line-height: 1.5;
}
.profile-body dl {
  grid-column: 1 / -1;
  margin: 5px 0 0;
  border-top: 1px solid var(--line);
}
.profile-body dl > div {
  display: flex;
  min-height: 42px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--line);
}
.profile-body dt {
  color: var(--muted);
  font-size: 9px;
}
.profile-body dd {
  margin: 0;
  color: var(--ink-soft);
  font-size: 10px;
  font-weight: 700;
}
.profile-form-footer {
  display: flex;
  min-height: 64px;
  align-items: center;
  justify-content: flex-end;
  gap: 16px;
  padding: 12px 24px;
  background: #f8faf9;
  border-top: 1px solid var(--line);
}
.profile-form-footer p {
  margin: 0 auto 0 0;
  color: var(--muted);
  font-size: 9px;
}
.profile-form-footer .profile-failure,
.profile-field-error {
  color: var(--red);
}
.profile-form-footer .profile-notice {
  color: var(--green);
}
.profile-field-error {
  margin: 6px 0 0;
  font-size: 9px;
  font-weight: 650;
}
@media (max-width: 700px) {
  .profile-body {
    grid-template-columns: 1fr;
  }
  .profile-body > aside {
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }
}
@media (max-width: 520px) {
  .profile-body fieldset {
    grid-template-columns: 1fr;
    padding: 18px;
  }
  .profile-body dl {
    grid-column: auto;
  }
  .profile-form-footer {
    align-items: stretch;
    flex-direction: column;
    padding: 14px 18px;
  }
  .profile-form-footer .app-button {
    width: 100%;
  }
}
</style>
