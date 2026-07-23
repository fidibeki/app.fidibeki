<script setup lang="ts">
import { AlertTriangle, ArrowUpRight, Check, Clock3, CreditCard, ShieldAlert, Trash2, X } from '@lucide/vue'
import type { BillingSummary, PlanCatalogResponse, PlanDefinition, TenantPlan } from '~/types/api'

const route = useRoute()
const { $apiFetch, $refreshSession } = useNuxtApp()
const { can, canWrite, tenant } = useAuth()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { currency, date, dateTime, plural } = useRegionalFormat()
const pendingAction = ref('')
const actionError = ref('')
const deleteOpen = ref(false)
const confirmation = ref('')

const enterprisePlan: PlanDefinition = {
  plan: 'enterprise',
  monthly_price_cents: null,
  yearly_discount_percent: null,
  limits: { projects: -1, feedback_per_month: -1, users: -1, storage_days: -1 },
  features: { ai: true, session_replay: true, slack: true, video: true, audio: true, dom_capture: true, custom_domain: true },
}

const { data, status, error, refresh } = await useAsyncData(
  'billing-summary',
  async () => {
    const [billingResponse, catalog] = await Promise.all([
      $apiFetch<{ billing: BillingSummary }>('/v1/billing'),
      $apiFetch<PlanCatalogResponse>('/v1/plans', { auth: false, retryAuth: false }),
    ])
    return { billing: billingResponse.billing, plans: [...catalog.plans, enterprisePlan] }
  },
)

const billing = computed(() => data.value?.billing)
const plans = computed(() => data.value?.plans || [])
const trialDays = computed(() => {
  if (!billing.value?.trial_ends_at) return null
  return Math.max(0, Math.ceil((new Date(billing.value.trial_ends_at).getTime() - Date.now()) / 86_400_000))
})
const deletionDate = computed(() => {
  if (!billing.value?.deletion_requested_at) return null
  return new Date(new Date(billing.value.deletion_requested_at).getTime() + 7 * 86_400_000)
})

function planLabel(plan: TenantPlan) {
  return t(`admin.plans.${plan}`)
}

function planLimit(value: number) {
  return value < 0 ? t('admin.common.custom') : String(value)
}

function statusLabel(value: string) {
  return t(`admin.statuses.${value}`)
}

function planPrice(cents: number) {
  return currency(cents / 100, 'EUR')
}

async function checkout(plan: TenantPlan) {
  if (!canWrite('billing:manage')) return
  if (plan === 'enterprise') {
    window.location.href = 'mailto:sales@fidibeki.com?subject=Fidibeki%20Enterprise'
    return
  }
  pendingAction.value = `checkout:${plan}`
  actionError.value = ''
  try {
    const response = await $apiFetch<{ url: string }>('/v1/billing/checkout', {
      method: 'POST', body: { plan },
    })
    window.location.assign(response.url)
  } catch (checkoutError) {
    actionError.value = localizedError.message(checkoutError, 'admin.billing.errors.checkout')
    pendingAction.value = ''
  }
}

async function openPortal() {
  if (!canWrite('billing:manage')) return
  pendingAction.value = 'portal'
  actionError.value = ''
  try {
    const response = await $apiFetch<{ url: string }>('/v1/billing/portal', { method: 'POST' })
    window.location.assign(response.url)
  } catch (portalError) {
    actionError.value = localizedError.message(portalError, 'admin.billing.errors.portal')
    pendingAction.value = ''
  }
}

async function requestDeletion() {
  if (!canWrite('billing:manage') || billing.value?.deletion_requested_at) return
  pendingAction.value = 'delete'
  actionError.value = ''
  try {
    await $apiFetch('/v1/billing/delete-account', {
      method: 'POST', body: { confirmation: confirmation.value },
    })
    deleteOpen.value = false
    confirmation.value = ''
    await Promise.all([refresh(), $refreshSession()])
  } catch (deleteError) {
    actionError.value = localizedError.message(deleteError, 'admin.billing.errors.deletion')
  } finally {
    pendingAction.value = ''
  }
}

async function cancelDeletion() {
  if (!can('billing:manage') || !billing.value?.deletion_requested_at || pendingAction.value) return
  pendingAction.value = 'cancel-delete'
  actionError.value = ''
  try {
    await $apiFetch('/v1/billing/cancel-deletion', { method: 'POST' })
    await Promise.all([refresh(), $refreshSession()])
  } catch (cancelError) {
    actionError.value = localizedError.message(cancelError, 'admin.billing.errors.cancellation')
  } finally {
    pendingAction.value = ''
  }
}

useHead(() => ({ title: t('admin.billing.pageTitle') }))
</script>

<template>
  <div class="app-page billing-page">
    <header class="app-page-head">
      <div><p class="app-eyebrow">{{ t('admin.billing.eyebrow') }}</p><h1>{{ t('admin.billing.title') }}</h1><span>{{ t('admin.billing.description') }}</span></div>
      <NuxtLink class="app-button" to="/settings">{{ t('admin.billing.back') }}</NuxtLink>
    </header>

    <p v-if="route.query.checkout === 'success'" class="billing-notice billing-notice--success"><Check :size="15" /> {{ t('admin.billing.checkoutCompleted') }}</p>
    <p v-else-if="route.query.checkout === 'cancelled'" class="billing-notice"><Clock3 :size="15" /> {{ t('admin.billing.checkoutCancelled') }}</p>
    <p v-if="actionError" class="billing-notice billing-notice--error" role="alert"><AlertTriangle :size="15" />{{ actionError }}</p>

    <section v-if="status === 'pending'" class="app-surface app-state"><div><span class="app-spinner" /><h2>{{ t('admin.billing.loading') }}</h2></div></section>
    <section v-else-if="error" class="app-surface app-state"><div><AlertTriangle :size="24" /><h2>{{ t('admin.billing.unavailable') }}</h2><p>{{ localizedError.message(error, 'admin.billing.errors.load') }}</p><button class="app-button" @click="refresh()">{{ t('admin.common.tryAgain') }}</button></div></section>

    <template v-else-if="billing">
      <section v-if="billing.deletion_requested_at" class="billing-deletion-banner">
        <ShieldAlert :size="20" />
        <div><b>{{ t('admin.billing.deletionScheduled') }}</b><span>{{ t('admin.billing.removalScheduled', { date: deletionDate ? dateTime(deletionDate) : '' }) }}</span></div>
        <button v-if="can('billing:manage')" class="app-button" :disabled="pendingAction === 'cancel-delete'" @click="cancelDeletion">{{ pendingAction === 'cancel-delete' ? t('admin.billing.restoring') : t('admin.billing.cancelDeletion') }}</button>
      </section>

      <section class="billing-summary app-surface">
        <div><p class="app-eyebrow">{{ t('admin.billing.currentPlan') }}</p><h2>{{ planLabel(billing.plan) }}</h2><span v-if="billing.pending_plan">{{ t('admin.billing.changesNextPeriod', { plan: planLabel(billing.pending_plan) }) }}</span><span v-else>{{ statusLabel(billing.status) }}</span></div>
        <div><p class="app-eyebrow">{{ t('admin.billing.trial') }}</p><h2>{{ trialDays == null ? t('admin.billing.notActive') : plural('admin.common.days', trialDays, { count: trialDays }) }}</h2><span>{{ billing.trial_ends_at ? date(billing.trial_ends_at) : t('admin.billing.noTrialEnd') }}</span></div>
        <div><p class="app-eyebrow">{{ t('admin.billing.paymentAccount') }}</p><h2>{{ billing.has_customer ? t('admin.billing.connected') : t('admin.billing.notCreated') }}</h2><span>{{ billing.has_subscription ? t('admin.billing.subscriptionActive') : t('admin.billing.noSubscription') }}</span></div>
        <button v-if="canWrite('billing:manage')" class="app-button" :disabled="pendingAction === 'portal'" @click="openPortal"><CreditCard :size="15" />{{ pendingAction === 'portal' ? t('admin.common.opening') : t('admin.billing.portal') }}<ArrowUpRight :size="13" /></button>
      </section>

      <section class="billing-plans">
        <article v-for="plan in plans" :key="plan.plan" class="billing-plan app-surface" :data-current="billing.plan === plan.plan">
          <header><span v-if="billing.plan === plan.plan">{{ t('admin.billing.current') }}</span><p>{{ planLabel(plan.plan) }}</p><h2>{{ plan.monthly_price_cents == null ? t('admin.common.custom') : planPrice(plan.monthly_price_cents) }}<small v-if="plan.plan !== 'enterprise'">{{ t('admin.billing.perMonth') }}</small></h2></header>
          <dl>
            <div><dt>{{ t('admin.billing.projects') }}</dt><dd>{{ planLimit(plan.limits.projects) }}</dd></div>
            <div><dt>{{ t('admin.billing.members') }}</dt><dd>{{ planLimit(plan.limits.users) }}</dd></div>
            <div><dt>{{ t('admin.billing.aiReplay') }}</dt><dd>{{ plan.features.ai && plan.features.session_replay ? t('admin.billing.included') : t('admin.billing.no') }}</dd></div>
          </dl>
          <button
            v-if="canWrite('billing:manage') && billing.plan !== plan.plan"
            class="app-button"
            :class="{ 'app-button--primary': plan.plan === 'growth' }"
            :disabled="Boolean(pendingAction) || Boolean(billing.deletion_requested_at)"
            @click="billing.has_subscription && plan.plan !== 'enterprise' ? openPortal() : checkout(plan.plan)"
          >
            {{ pendingAction === `checkout:${plan.plan}` ? t('admin.common.opening') : plan.plan === 'enterprise' ? t('admin.billing.contactSales') : billing.has_subscription ? t('admin.billing.changeInPortal') : t('admin.billing.choosePlan') }}
          </button>
          <span v-else-if="billing.plan === plan.plan" class="billing-current"><Check :size="14" /> {{ t('admin.billing.activePlan') }}</span>
        </article>
      </section>

      <section class="billing-danger app-surface">
        <div><p class="app-eyebrow">{{ t('admin.billing.dangerEyebrow') }}</p><h2>{{ t('admin.billing.deleteWorkspace') }}</h2><span>{{ t('admin.billing.recoveryWindow') }}</span></div>
        <button v-if="canWrite('billing:manage') && !billing.deletion_requested_at" class="app-button billing-delete-button" @click="deleteOpen = true"><Trash2 :size="15" /> {{ t('admin.billing.scheduleDeletion') }}</button>
      </section>
    </template>

    <AppDialog :open="deleteOpen" aria-labelledby="delete-title" @close="deleteOpen = false">
      <section class="billing-modal">
        <header><div><p class="app-eyebrow">{{ t('admin.billing.irreversible') }}</p><h2 id="delete-title">{{ t('admin.billing.scheduleDeletion') }}</h2></div><button class="app-icon-button" :title="t('admin.common.close')" @click="deleteOpen = false"><X :size="18" /></button></header>
        <form @submit.prevent="requestDeletion">
          <label>{{ t('admin.billing.typeToConfirm', { value: tenant?.slug }) }}<input v-model.trim="confirmation" class="app-input" required autocomplete="off" autofocus /></label>
          <p v-if="actionError" class="billing-form-error">{{ actionError }}</p>
          <footer><button class="app-button" type="button" @click="deleteOpen = false">{{ t('admin.common.cancel') }}</button><button class="app-button billing-delete-button" type="submit" :disabled="pendingAction === 'delete' || confirmation !== tenant?.slug">{{ pendingAction === 'delete' ? t('admin.billing.scheduling') : t('admin.billing.scheduleDeletion') }}</button></footer>
        </form>
      </section>
    </AppDialog>
  </div>
</template>

<style scoped>
.billing-notice { display: flex; align-items: center; gap: 8px; margin: 0 0 12px; padding: 10px 12px; background: var(--amber-soft); color: var(--amber); border: 1px solid #e8c995; border-radius: 5px; font-size: 11px; }
.billing-notice--success { background: var(--green-soft); color: var(--green); border-color: #b5d9c6; }
.billing-notice--error { background: var(--red-soft); color: var(--red); border-color: #e7b5bb; }
.billing-deletion-banner { display: flex; align-items: center; gap: 12px; margin-bottom: 14px; padding: 12px 14px; background: var(--amber-soft); color: #6f420d; border: 1px solid #e4bd80; border-radius: 6px; }
.billing-deletion-banner > div { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 2px; }
.billing-deletion-banner b { font-size: 12px; }
.billing-deletion-banner span { font-size: 10px; }
.billing-summary { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)) auto; align-items: center; margin-bottom: 16px; overflow: hidden; }
.billing-summary > div { min-height: 100px; padding: 18px; border-right: 1px solid var(--line); }
.billing-summary h2 { margin: 5px 0 3px; font-size: 17px; }
.billing-summary span { color: var(--muted); font-size: 10px; text-transform: capitalize; }
.billing-summary > button { margin: 18px; }
.billing-plans { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
.billing-plan { position: relative; overflow: hidden; }
.billing-plan[data-current='true'] { border-color: var(--teal); box-shadow: inset 0 3px 0 var(--teal); }
.billing-plan header { min-height: 112px; padding: 16px; border-bottom: 1px solid var(--line); }
.billing-plan header > span { position: absolute; top: 9px; right: 9px; padding: 3px 6px; background: var(--teal-soft); color: var(--teal); border-radius: 3px; font-size: 8px; font-weight: 800; text-transform: uppercase; }
.billing-plan header p { margin: 14px 0 7px; color: var(--ink-soft); font-size: 11px; font-weight: 750; }
.billing-plan h2 { margin: 0; font-size: 21px; }
.billing-plan h2 small { color: var(--muted); font-size: 9px; font-weight: 500; }
.billing-plan dl { margin: 0; }
.billing-plan dl > div { display: flex; min-height: 40px; align-items: center; justify-content: space-between; gap: 8px; padding: 0 12px; border-bottom: 1px solid var(--line); }
.billing-plan dt, .billing-plan dd { margin: 0; font-size: 9px; }
.billing-plan dt { color: var(--muted); }
.billing-plan dd { color: var(--ink-soft); font-weight: 750; }
.billing-plan > button, .billing-current { display: flex; width: calc(100% - 24px); min-height: 38px; align-items: center; justify-content: center; gap: 6px; margin: 12px; }
.billing-current { color: var(--green); font-size: 10px; font-weight: 750; }
.billing-danger { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-top: 18px; padding: 16px; border-color: #e3b7bc; }
.billing-danger h2 { margin: 3px 0; font-size: 14px; }
.billing-danger span { color: var(--muted); font-size: 10px; }
.billing-delete-button { background: #fff; color: var(--red); border-color: #d8a1a8; }
.billing-delete-button:hover:not(:disabled) { background: var(--red-soft); }
.billing-modal { width: min(500px, 100%); background: #fff; border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.billing-modal > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; padding: 11px 15px; border-bottom: 1px solid var(--line); }
.billing-modal h2 { margin: 3px 0 0; font-size: 15px; }
.billing-modal form { padding: 16px; }
.billing-modal label { display: flex; flex-direction: column; gap: 7px; color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.billing-modal label code { display: inline; }
.billing-modal footer { display: flex; justify-content: flex-end; gap: 8px; margin: 16px -16px -16px; padding: 12px 16px; background: #f8faf9; border-top: 1px solid var(--line); }
.billing-form-error { color: var(--red); font-size: 10px; }
.capitalize { text-transform: capitalize; }
@media (max-width: 1050px) { .billing-plans { grid-template-columns: repeat(2, 1fr); } .billing-summary { grid-template-columns: repeat(3, 1fr); } .billing-summary > button { grid-column: 1 / -1; } }
@media (max-width: 650px) { .billing-summary, .billing-plans { grid-template-columns: 1fr; } .billing-summary > div { min-height: 80px; border-right: 0; border-bottom: 1px solid var(--line); } .billing-deletion-banner, .billing-danger { align-items: stretch; flex-direction: column; } }
</style>
