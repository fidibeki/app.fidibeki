<script setup lang="ts">
import {
  ImagePlus,
  Save,
  ShieldCheck,
  Trash2,
  UserRound,
} from '@lucide/vue'
import type { Member } from '~/types/api'
import { apiErrorMessage, apiFieldErrors } from '~/utils/api-fetch'

const { $apiFetch } = useNuxtApp()
const { member, session, tenant, workspaceWritable } = useAuth()
const fileInput = ref<HTMLInputElement | null>(null)
const name = ref(member.value?.name || '')
const saving = ref(false)
const uploading = ref(false)
const removing = ref(false)
const notice = ref('')
const failure = ref('')
const fields = ref<Record<string, string>>({})

function applyMember(next: Member) {
  if (!session.value.member) return
  session.value.member = {
    ...session.value.member,
    ...next,
  }
  name.value = session.value.member.name || ''
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
      body: { name: name.value.trim() || null },
    })
    applyMember(response.member)
    notice.value = 'Profile saved.'
  } catch (saveError) {
    fields.value = apiFieldErrors(saveError)
    failure.value = Object.keys(fields.value).length
      ? ''
      : apiErrorMessage(saveError, 'Profile could not be saved.')
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
    notice.value = 'Profile photo updated.'
  } catch (uploadError) {
    fields.value = apiFieldErrors(uploadError)
    failure.value = Object.keys(fields.value).length
      ? ''
      : apiErrorMessage(uploadError, 'Profile photo could not be uploaded.')
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
    notice.value = 'Profile photo removed.'
  } catch (removeError) {
    failure.value = apiErrorMessage(removeError, 'Profile photo could not be removed.')
  } finally {
    removing.value = false
  }
}

useHead({ title: 'Profile - Fidibeki' })
</script>

<template>
  <div class="app-page profile-page">
    <header class="app-page-head">
      <div>
        <p class="app-eyebrow">Account</p>
        <h1>Profile</h1>
        <span>Your identity across {{ tenant?.name || 'this workspace' }}.</span>
      </div>
    </header>

    <section v-if="member" class="app-surface profile-surface">
      <header>
        <div>
          <p class="app-eyebrow">Member identity</p>
          <h2>{{ member.name || member.email.split('@')[0] }}</h2>
        </div>
        <span class="profile-access"><ShieldCheck :size="13" /> {{ member.role }}</span>
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
              aria-label="Profile photo file"
              @change="uploadAvatar"
            />
            <button
              class="app-button"
              type="button"
              :disabled="!workspaceWritable || uploading"
              @click="fileInput?.click()"
            >
              <ImagePlus :size="14" /> {{ uploading ? 'Uploading...' : member.avatar_version ? 'Replace photo' : 'Upload photo' }}
            </button>
            <button
              v-if="member.avatar_version"
              class="app-icon-button profile-remove"
              type="button"
              title="Remove profile photo"
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
              Display name
              <input v-model="name" class="app-input" maxlength="120" placeholder="Your name" />
              <small v-if="fields.name" class="profile-field-error">{{ fields.name }}</small>
            </label>
            <label>
              Email
              <input class="app-input" :value="member.email" type="email" disabled />
            </label>
            <dl>
              <div><dt>Workspace</dt><dd>{{ tenant?.name }}</dd></div>
              <div><dt>Role</dt><dd class="capitalize">{{ member.role }}</dd></div>
              <div><dt>Access</dt><dd class="capitalize">{{ member.status }}</dd></div>
            </dl>
          </fieldset>
          <div class="profile-form-footer">
            <p v-if="!workspaceWritable">Profile changes are unavailable while this workspace is read-only.</p>
            <p v-else-if="failure" class="profile-failure" role="alert">{{ failure }}</p>
            <p v-else-if="notice" class="profile-notice" role="status">{{ notice }}</p>
            <button class="app-button app-button--primary" type="submit" :disabled="!workspaceWritable || saving">
              <Save :size="14" /> {{ saving ? 'Saving...' : 'Save profile' }}
            </button>
          </div>
        </form>
      </div>
    </section>

    <section v-else class="app-surface app-state">
      <div><UserRound :size="24" /><h2>Profile unavailable</h2></div>
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
