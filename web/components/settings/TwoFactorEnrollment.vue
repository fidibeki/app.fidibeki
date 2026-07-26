<script setup lang="ts">
import { AlertTriangle, Check, Copy, KeyRound, ShieldCheck, ShieldOff } from '@lucide/vue'
import type { TwoFactorEnrollment, TwoFactorStatus } from '~/types/api'

// The member-facing two-factor card. It is used both inside the profile page and
// on the mandatory setup route, so the enrolment flow has exactly one
// implementation regardless of how the member arrived at it.
const props = defineProps<{ mandatory?: boolean }>()
const emit = defineEmits<{ enrolled: [] }>()

const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { success: showSuccessToast } = useAppToast()

const status = ref<TwoFactorStatus | null>(null)
const enrollment = ref<TwoFactorEnrollment | null>(null)
const recoveryCodes = ref<string[]>([])
const confirmationCode = ref('')
const managementCode = ref('')
const loading = ref(true)
const busy = ref(false)
const failure = ref('')
const copied = ref(false)

async function loadStatus() {
  loading.value = true
  failure.value = ''
  try {
    const response = await $apiFetch<{ two_factor: TwoFactorStatus }>('/v1/security/two-factor')
    status.value = response.two_factor
  } catch (error) {
    failure.value = localizedError.message(error, 'settings.twoFactor.loadFailed')
  } finally {
    loading.value = false
  }
}

async function startEnrollment() {
  busy.value = true
  failure.value = ''
  try {
    const response = await $apiFetch<{ enrollment: TwoFactorEnrollment }>(
      '/v1/security/two-factor/enroll',
      { method: 'POST' },
    )
    enrollment.value = response.enrollment
    recoveryCodes.value = []
    confirmationCode.value = ''
  } catch (error) {
    failure.value = localizedError.message(error, 'settings.twoFactor.enrollFailed')
  } finally {
    busy.value = false
  }
}

async function confirmEnrollment() {
  busy.value = true
  failure.value = ''
  try {
    const response = await $apiFetch<{ recovery_codes: string[] }>(
      '/v1/security/two-factor/confirm',
      { method: 'POST', body: { code: confirmationCode.value } },
    )
    recoveryCodes.value = response.recovery_codes
    // The secret is dropped as soon as it is confirmed: it is never displayed
    // again and the server will not return it.
    enrollment.value = null
    confirmationCode.value = ''
    await loadStatus()
    showSuccessToast(t('settings.twoFactor.enrolledToast'))
    emit('enrolled')
  } catch (error) {
    failure.value = localizedError.message(error, 'settings.twoFactor.confirmFailed')
  } finally {
    busy.value = false
  }
}

async function regenerateRecoveryCodes() {
  busy.value = true
  failure.value = ''
  try {
    const response = await $apiFetch<{ recovery_codes: string[] }>(
      '/v1/security/two-factor/recovery-codes',
      { method: 'POST', body: { code: managementCode.value } },
    )
    recoveryCodes.value = response.recovery_codes
    managementCode.value = ''
    await loadStatus()
  } catch (error) {
    failure.value = localizedError.message(error, 'settings.twoFactor.regenerateFailed')
  } finally {
    busy.value = false
  }
}

async function removeEnrollment() {
  busy.value = true
  failure.value = ''
  try {
    await $apiFetch('/v1/security/two-factor', {
      method: 'DELETE',
      body: { code: managementCode.value },
    })
    managementCode.value = ''
    recoveryCodes.value = []
    await loadStatus()
    showSuccessToast(t('settings.twoFactor.removedToast'))
  } catch (error) {
    failure.value = localizedError.message(error, 'settings.twoFactor.removeFailed')
  } finally {
    busy.value = false
  }
}

async function copySecret() {
  if (!enrollment.value) return
  try {
    await navigator.clipboard.writeText(enrollment.value.secret)
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } catch {
    failure.value = t('settings.twoFactor.copyFailed')
  }
}

onMounted(loadStatus)
</script>

<template>
  <section class="app-surface two-factor">
    <header>
      <div>
        <p class="app-eyebrow">{{ t('settings.twoFactor.eyebrow') }}</p>
        <h2>{{ t('settings.twoFactor.title') }}</h2>
      </div>
      <span class="two-factor-badge" :data-enabled="Boolean(status?.enrolled)">
        <component :is="status?.enrolled ? ShieldCheck : ShieldOff" :size="12" />
        {{ status?.enrolled ? t('settings.twoFactor.statusOn') : t('settings.twoFactor.statusOff') }}
      </span>
    </header>

    <div class="two-factor-body">
      <p class="two-factor-lead">
        {{ props.mandatory ? t('settings.twoFactor.mandatoryDescription') : t('settings.twoFactor.description') }}
      </p>

      <p v-if="failure" class="two-factor-alert" role="alert"><AlertTriangle :size="15" />{{ failure }}</p>

      <div v-if="loading" class="two-factor-empty">
        <span class="app-spinner" /><h3>{{ t('settings.twoFactor.loading') }}</h3>
      </div>

      <!-- The recovery codes are shown exactly once, right after they are created. -->
      <div v-if="recoveryCodes.length" class="two-factor-recovery">
        <h3>{{ t('settings.twoFactor.recoveryTitle') }}</h3>
        <p>{{ t('settings.twoFactor.recoveryWarning') }}</p>
        <ul>
          <li v-for="recoveryCode in recoveryCodes" :key="recoveryCode"><code>{{ recoveryCode }}</code></li>
        </ul>
        <button class="app-button" type="button" @click="recoveryCodes = []">
          <Check :size="15" />{{ t('settings.twoFactor.recoverySaved') }}
        </button>
      </div>

      <template v-if="!loading && !status?.enrolled">
        <div v-if="enrollment" class="two-factor-setup">
          <ol>
            <li>{{ t('settings.twoFactor.stepScan') }}</li>
            <li>{{ t('settings.twoFactor.stepConfirm') }}</li>
          </ol>
          <div class="two-factor-provision">
            <div class="two-factor-qr" v-html="enrollment.qr_code_svg" />
            <div class="two-factor-key">
              <p class="app-eyebrow">{{ t('settings.twoFactor.manualSecret') }}</p>
              <div>
                <code>{{ enrollment.secret }}</code>
                <button
                  class="app-icon-button"
                  type="button"
                  :title="t('settings.twoFactor.copySecret')"
                  @click="copySecret"
                >
                  <Check v-if="copied" :size="15" />
                  <Copy v-else :size="15" />
                </button>
              </div>
            </div>
          </div>
          <form class="two-factor-form" @submit.prevent="confirmEnrollment">
            <label>
              {{ t('settings.twoFactor.codeLabel') }}
              <input
                v-model="confirmationCode"
                class="app-input two-factor-code"
                inputmode="numeric"
                autocomplete="one-time-code"
                maxlength="7"
                :placeholder="t('settings.twoFactor.codePlaceholder')"
                required
              />
            </label>
            <button class="app-button app-button--primary" type="submit" :disabled="busy">
              {{ busy ? t('settings.twoFactor.confirming') : t('settings.twoFactor.confirm') }}
            </button>
          </form>
        </div>
        <div v-else class="two-factor-start">
          <button class="app-button app-button--primary" type="button" :disabled="busy" @click="startEnrollment">
            <KeyRound :size="15" />{{ t('settings.twoFactor.start') }}
          </button>
        </div>
      </template>

      <template v-else-if="!loading && status">
        <dl class="two-factor-ledger">
          <div>
            <dt>{{ t('settings.twoFactor.recoveryRemaining') }}</dt>
            <dd>{{ status.recovery_codes_remaining }}</dd>
          </div>
          <div>
            <dt>{{ t('settings.twoFactor.policy') }}</dt>
            <dd>{{ t(`settings.twoFactor.policies.${status.policy}`) }}</dd>
          </div>
        </dl>
        <form class="two-factor-form" @submit.prevent="regenerateRecoveryCodes">
          <label>
            {{ t('settings.twoFactor.managementCodeLabel') }}
            <input
              v-model="managementCode"
              class="app-input two-factor-code"
              autocomplete="one-time-code"
              maxlength="13"
              :placeholder="t('settings.twoFactor.codePlaceholder')"
              required
            />
          </label>
          <div class="two-factor-actions">
            <button class="app-button" type="submit" :disabled="busy">
              {{ t('settings.twoFactor.regenerate') }}
            </button>
            <button
              v-if="status.can_remove"
              class="app-button two-factor-danger"
              type="button"
              :disabled="busy || !managementCode"
              @click="removeEnrollment"
            >
              <ShieldOff :size="15" />{{ t('settings.twoFactor.remove') }}
            </button>
          </div>
          <small v-if="!status.can_remove">{{ t('settings.twoFactor.removeBlocked') }}</small>
        </form>
      </template>
    </div>
  </section>
</template>

<style scoped>
.two-factor { overflow: hidden; }
.two-factor > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; gap: 14px; padding: 11px 16px; border-bottom: 1px solid var(--line); }
.two-factor h2 { margin: 3px 0 0; font-size: 14px; }
.two-factor-badge { display: inline-flex; align-items: center; gap: 5px; padding: 5px 7px; background: var(--surface-soft); color: var(--muted); border-radius: 4px; font-size: 9px; font-weight: 800; text-transform: uppercase; white-space: nowrap; }
.two-factor-badge[data-enabled='true'] { background: var(--green-soft); color: var(--green); }
.two-factor-body { display: grid; gap: 16px; padding: 16px; }
.two-factor-lead { margin: 0; max-width: 78ch; color: var(--muted); font-size: 11px; line-height: 1.6; }
.two-factor-alert { display: flex; min-height: 38px; align-items: center; gap: 8px; margin: 0; padding: 8px 11px; background: var(--red-soft); color: var(--red); border-radius: 4px; font-size: 10px; font-weight: 700; }
.two-factor-empty { display: grid; place-items: center; gap: 6px; padding: 24px 16px; color: var(--muted); text-align: center; }
.two-factor-empty h3 { margin: 0; color: var(--ink-soft); font-size: 12px; }
.two-factor-start { display: flex; }
.two-factor-setup { display: grid; gap: 16px; }
.two-factor-setup ol { display: grid; gap: 5px; margin: 0; padding-left: 17px; color: var(--muted); font-size: 11px; line-height: 1.6; }
.two-factor-provision { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 16px; }
.two-factor-qr { width: 176px; padding: 9px; background: #ffffff; border: 1px solid var(--line); border-radius: var(--radius); }
.two-factor-qr :deep(svg) { display: block; width: 100%; height: auto; }
.two-factor-key { display: grid; gap: 7px; }
.two-factor-key > div { display: flex; align-items: center; gap: 6px; padding: 7px 9px; background: var(--surface-subtle); border: 1px solid var(--line); border-radius: var(--radius); }
.two-factor-key code { color: var(--ink-soft); font-size: 11px; letter-spacing: .1em; word-break: break-all; }
.two-factor-form { display: grid; gap: 10px; max-width: 320px; }
.two-factor-form label { display: grid; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.two-factor-form small { color: var(--muted); font-size: 10px; line-height: 1.5; }
.two-factor-code { letter-spacing: .22em; text-align: center; font-size: 15px; }
.two-factor-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.two-factor-danger { color: var(--red); border-color: var(--red-line); }
.two-factor-ledger { display: flex; flex-wrap: wrap; gap: 26px; margin: 0; }
.two-factor-ledger dt { color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.two-factor-ledger dd { margin: 3px 0 0; color: var(--ink-soft); font-size: 13px; font-weight: 700; }
.two-factor-recovery { display: grid; gap: 10px; padding: 14px; background: var(--amber-soft); border: 1px solid var(--amber-line); border-radius: var(--radius); }
.two-factor-recovery h3 { margin: 0; color: var(--amber-ink); font-size: 12px; }
.two-factor-recovery > p { margin: 0; max-width: 74ch; color: var(--amber-ink); font-size: 10px; line-height: 1.6; }
.two-factor-recovery ul { display: grid; grid-template-columns: repeat(auto-fill, minmax(116px, 1fr)); gap: 6px; margin: 0; padding: 0; list-style: none; }
.two-factor-recovery code { color: var(--ink-soft); font-size: 12px; letter-spacing: .06em; }
.two-factor-recovery button { justify-self: start; }
@media (max-width: 700px) {
  .two-factor > header { align-items: flex-start; flex-wrap: wrap; }
}
</style>
