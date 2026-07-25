<script setup lang="ts">
import { AlertTriangle, RotateCcw, ShieldCheck, ShieldOff } from '@lucide/vue'
import type { TwoFactorCompliance, TwoFactorPolicy, TwoFactorPolicyMode } from '~/types/api'

// The organization-level security tab: the policy selector plus the compliance
// table. Administrators can see who is enrolled and reset an enrolment, but the
// API never returns a secret or a recovery code to them.
const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const { can, canWrite, member: currentMember } = useAuth()
const { date } = useRegionalFormat()
const localizedError = useLocalizedAPIError()
const { success: showSuccessToast } = useAppToast()

const policy = ref<TwoFactorPolicy | null>(null)
const compliance = ref<TwoFactorCompliance[]>([])
const loading = ref(true)
const saving = ref(false)
const failure = ref('')

const modes: TwoFactorPolicyMode[] = ['optional', 'required']
const enrolledCount = computed(() => compliance.value.filter(entry => entry.enrolled).length)

async function load() {
  loading.value = true
  failure.value = ''
  try {
    const [policyResponse, complianceResponse] = await Promise.all([
      $apiFetch<{ two_factor_policy: TwoFactorPolicy }>('/v1/security/two-factor/policy'),
      can('member:read')
        ? $apiFetch<{ two_factor_compliance: TwoFactorCompliance[] }>('/v1/security/two-factor/compliance')
        : Promise.resolve({ two_factor_compliance: [] }),
    ])
    policy.value = policyResponse.two_factor_policy
    compliance.value = complianceResponse.two_factor_compliance
  } catch (error) {
    failure.value = localizedError.message(error, 'admin.settings.security.errors.load')
  } finally {
    loading.value = false
  }
}

async function selectPolicy(mode: TwoFactorPolicyMode) {
  if (saving.value || policy.value?.policy === mode) return
  saving.value = true
  failure.value = ''
  try {
    const response = await $apiFetch<{ two_factor_policy: TwoFactorPolicy }>(
      '/v1/security/two-factor/policy',
      { method: 'PATCH', body: { policy: mode } },
    )
    policy.value = response.two_factor_policy
    showSuccessToast(t('admin.settings.security.policySaved'))
  } catch (error) {
    failure.value = localizedError.message(error, 'admin.settings.security.errors.policy')
  } finally {
    saving.value = false
  }
}

async function resetMember(entry: TwoFactorCompliance) {
  saving.value = true
  failure.value = ''
  try {
    await $apiFetch(`/v1/members/${entry.member_id}/two-factor/reset`, { method: 'POST' })
    await load()
    showSuccessToast(t('admin.settings.security.resetDone'))
  } catch (error) {
    failure.value = localizedError.message(error, 'admin.settings.security.errors.reset')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="two-factor-policy">
    <section class="app-surface two-factor-policy-section">
      <header>
        <div>
          <p class="app-eyebrow">{{ t('admin.settings.security.eyebrow') }}</p>
          <h2>{{ t('admin.settings.security.policyTitle') }}</h2>
        </div>
        <span class="two-factor-policy-mode" :data-enforced="policy?.policy === 'required'">
          {{ policy ? t(`admin.settings.security.policies.${policy.policy}`) : t('admin.common.loading') }}
        </span>
      </header>
      <div class="two-factor-policy-body">
        <p>{{ t('admin.settings.security.policyDescription') }}</p>
        <p v-if="failure" class="two-factor-policy-alert" role="alert">
          <AlertTriangle :size="15" />{{ failure }}
        </p>
        <div class="two-factor-policy-choices">
          <button
            v-for="mode in modes"
            :key="mode"
            type="button"
            :data-selected="policy?.policy === mode"
            :disabled="!canWrite('tenant:update') || saving || loading"
            @click="selectPolicy(mode)"
          >
            <span>
              <component :is="mode === 'required' ? ShieldCheck : ShieldOff" :size="15" />
              <b>{{ t(`admin.settings.security.policies.${mode}`) }}</b>
            </span>
            <small>{{ t(`admin.settings.security.policyHints.${mode}`) }}</small>
          </button>
        </div>
        <p class="two-factor-policy-note">{{ t('admin.settings.security.lockoutNote') }}</p>
      </div>
    </section>

    <section v-if="can('member:read')" class="app-surface two-factor-policy-section">
      <header>
        <div>
          <p class="app-eyebrow">{{ t('admin.settings.security.complianceEyebrow') }}</p>
          <h2>{{ t('admin.settings.security.complianceTitle') }}</h2>
        </div>
        <span class="two-factor-policy-state">
          <i />{{ t('admin.settings.security.enrolledCount', { enrolled: enrolledCount, total: compliance.length }) }}
        </span>
      </header>
      <div class="two-factor-policy-body two-factor-policy-body--tight">
        <p>{{ t('admin.settings.security.complianceDescription') }}</p>
      </div>
      <div v-if="loading" class="two-factor-policy-empty">
        <span class="app-spinner" /><h3>{{ t('admin.common.loading') }}</h3>
      </div>
      <div v-else class="two-factor-policy-table">
        <div class="two-factor-policy-row two-factor-policy-row--head">
          <span>{{ t('admin.settings.security.columns.member') }}</span>
          <span>{{ t('admin.settings.security.columns.status') }}</span>
          <span>{{ t('admin.settings.security.columns.enrolledAt') }}</span>
          <span />
        </div>
        <div v-for="entry in compliance" :key="entry.member_id" class="two-factor-policy-row">
          <span>
            <b>{{ entry.name || entry.email.split('@')[0] }}</b>
            <small>{{ entry.email }}</small>
          </span>
          <span class="two-factor-policy-state" :data-enrolled="entry.enrolled">
            <i />{{ entry.enrolled ? t('admin.settings.security.enrolled') : t('admin.settings.security.notEnrolled') }}
          </span>
          <span>{{ entry.confirmed_at ? date(entry.confirmed_at) : t('admin.common.never') }}</span>
          <span class="two-factor-policy-actions">
            <button
              v-if="entry.enrolled && canWrite('member:update') && entry.member_id !== currentMember?.id"
              class="app-icon-button"
              type="button"
              :title="t('admin.settings.security.reset')"
              :disabled="saving"
              @click="resetMember(entry)"
            >
              <RotateCcw :size="16" />
            </button>
          </span>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.two-factor-policy { display: grid; gap: 16px; }
.two-factor-policy-section { overflow: hidden; }
.two-factor-policy-section > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; gap: 14px; padding: 11px 16px; border-bottom: 1px solid var(--line); }
.two-factor-policy-section h2 { margin: 3px 0 0; font-size: 14px; }
.two-factor-policy-body { display: grid; gap: 14px; padding: 16px; }
.two-factor-policy-body--tight { padding-bottom: 0; }
.two-factor-policy-body > p { margin: 0; max-width: 78ch; color: var(--muted); font-size: 11px; line-height: 1.6; }
.two-factor-policy-note { margin: 0; max-width: 78ch; color: var(--muted); font-size: 10px; line-height: 1.6; }
.two-factor-policy-mode { padding: 5px 7px; background: var(--surface-soft); color: var(--muted); border-radius: 4px; font-size: 9px; font-weight: 800; text-transform: uppercase; }
.two-factor-policy-mode[data-enforced='true'] { background: var(--green-soft); color: var(--green); }
.two-factor-policy-state { display: inline-flex; align-items: center; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 700; }
.two-factor-policy-state i { width: 7px; height: 7px; background: var(--green); border-radius: 50%; }
.two-factor-policy-state[data-enrolled='false'] i { background: var(--amber-ink); }
.two-factor-policy-choices { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 10px; }
.two-factor-policy-choices button { display: grid; gap: 5px; padding: 13px 14px; background: var(--surface); border: 1px solid var(--line); border-radius: var(--radius); text-align: left; cursor: pointer; }
.two-factor-policy-choices button:hover:not(:disabled) { border-color: var(--line-strong); }
.two-factor-policy-choices button:disabled { cursor: not-allowed; opacity: .55; }
.two-factor-policy-choices button[data-selected='true'] { background: var(--green-soft); border-color: var(--green-line); }
.two-factor-policy-choices span { display: flex; align-items: center; gap: 7px; color: var(--ink-soft); }
.two-factor-policy-choices b { font-size: 11px; font-weight: 750; }
.two-factor-policy-choices small { color: var(--muted); font-size: 10px; line-height: 1.5; }
.two-factor-policy-alert { display: flex; min-height: 38px; align-items: center; gap: 8px; margin: 0; padding: 8px 11px; background: var(--red-soft); color: var(--red); border-radius: 4px; font-size: 10px; font-weight: 700; }
.two-factor-policy-empty { display: grid; place-items: center; gap: 6px; padding: 40px 16px; color: var(--muted); text-align: center; }
.two-factor-policy-empty h3 { margin: 0; color: var(--ink-soft); font-size: 12px; }
.two-factor-policy-table { width: 100%; overflow-x: auto; }
.two-factor-policy-row { display: grid; grid-template-columns: minmax(180px, 1.4fr) 130px 130px 52px; min-height: 62px; align-items: center; gap: 14px; padding: 7px 15px; border-bottom: 1px solid var(--line); color: var(--muted); font-size: 10px; }
.two-factor-policy-row:last-child { border-bottom: 0; }
.two-factor-policy-row--head { min-height: 36px; padding-top: 0; padding-bottom: 0; background: var(--surface-subtle); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.two-factor-policy-row > span { min-width: 0; }
.two-factor-policy-row > span > b { display: block; color: var(--ink-soft); font-size: 11px; }
.two-factor-policy-row small { display: block; margin-top: 3px; overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.two-factor-policy-actions { display: flex; justify-content: flex-end; gap: 4px; }
@media (max-width: 700px) {
  .two-factor-policy-section > header { align-items: flex-start; flex-wrap: wrap; }
  .two-factor-policy-row { min-width: 620px; }
}
</style>
