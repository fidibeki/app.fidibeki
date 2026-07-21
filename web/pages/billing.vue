<script setup lang="ts">
import { AlertTriangle, ArrowUpRight, Check, Clock3, CreditCard, ShieldAlert, Trash2, X } from '@lucide/vue'
import type { BillingSummary, TenantPlan } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatDate, formatDateTime } from '~/utils/format'

const route = useRoute()
const { $apiFetch, $refreshSession } = useNuxtApp()
const { can, tenant } = useAuth()
const pendingAction = ref('')
const actionError = ref('')
const deleteOpen = ref(false)
const confirmation = ref('')

const plans: Array<{ id: TenantPlan, price: string, label: string }> = [
  { id: 'starter', price: '$4.99', label: 'Starter' },
  { id: 'growth', price: '$9.99', label: 'Growth' },
  { id: 'scale', price: '$27.99', label: 'Scale' },
  { id: 'enterprise', price: 'Custom', label: 'Enterprise' },
]

const { data, status, error, refresh } = await useAsyncData(
  'billing-summary',
  () => $apiFetch<{ billing: BillingSummary }>('/v1/billing'),
)

const billing = computed(() => data.value?.billing)
const trialDays = computed(() => {
  if (!billing.value?.trial_ends_at) return null
  return Math.max(0, Math.ceil((new Date(billing.value.trial_ends_at).getTime() - Date.now()) / 86_400_000))
})
const deletionDate = computed(() => {
  if (!billing.value?.deletion_requested_at) return null
  return new Date(new Date(billing.value.deletion_requested_at).getTime() + 7 * 86_400_000)
})

async function checkout(plan: TenantPlan) {
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
    actionError.value = apiErrorMessage(checkoutError, 'Checkout could not be started.')
    pendingAction.value = ''
  }
}

async function openPortal() {
  pendingAction.value = 'portal'
  actionError.value = ''
  try {
    const response = await $apiFetch<{ url: string }>('/v1/billing/portal', { method: 'POST' })
    window.location.assign(response.url)
  } catch (portalError) {
    actionError.value = apiErrorMessage(portalError, 'Billing portal could not be opened.')
    pendingAction.value = ''
  }
}

async function requestDeletion() {
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
    actionError.value = apiErrorMessage(deleteError, 'Deletion could not be scheduled.')
  } finally {
    pendingAction.value = ''
  }
}

async function cancelDeletion() {
  pendingAction.value = 'cancel-delete'
  actionError.value = ''
  try {
    await $apiFetch('/v1/billing/cancel-deletion', { method: 'POST' })
    await Promise.all([refresh(), $refreshSession()])
  } catch (cancelError) {
    actionError.value = apiErrorMessage(cancelError, 'Deletion could not be cancelled.')
  } finally {
    pendingAction.value = ''
  }
}

useHead({ title: 'Billing - Fidibeki' })
</script>

<template>
  <div class="app-page billing-page">
    <header class="app-page-head">
      <div><p class="app-eyebrow">Account</p><h1>Billing</h1><span>Plan, payment, and workspace lifecycle.</span></div>
      <NuxtLink class="app-button" to="/settings">Back to settings</NuxtLink>
    </header>

    <p v-if="route.query.checkout === 'success'" class="billing-notice billing-notice--success"><Check :size="15" /> Checkout completed. Subscription changes appear after Stripe confirms the payment.</p>
    <p v-else-if="route.query.checkout === 'cancelled'" class="billing-notice"><Clock3 :size="15" /> Checkout was cancelled.</p>
    <p v-if="actionError" class="billing-notice billing-notice--error" role="alert"><AlertTriangle :size="15" />{{ actionError }}</p>

    <section v-if="status === 'pending'" class="app-surface app-state"><div><span class="app-spinner" /><h2>Loading billing</h2></div></section>
    <section v-else-if="error" class="app-surface app-state"><div><AlertTriangle :size="24" /><h2>Billing unavailable</h2><p>{{ apiErrorMessage(error) }}</p><button class="app-button" @click="refresh()">Try again</button></div></section>

    <template v-else-if="billing">
      <section v-if="billing.deletion_requested_at" class="billing-deletion-banner">
        <ShieldAlert :size="20" />
        <div><b>Deletion scheduled</b><span>Workspace removal is scheduled for {{ deletionDate ? formatDateTime(deletionDate) : '' }}.</span></div>
        <button v-if="can('billing:manage')" class="app-button" :disabled="pendingAction === 'cancel-delete'" @click="cancelDeletion">{{ pendingAction === 'cancel-delete' ? 'Restoring...' : 'Cancel deletion' }}</button>
      </section>

      <section class="billing-summary app-surface">
        <div><p class="app-eyebrow">Current plan</p><h2 class="capitalize">{{ billing.plan }}</h2><span v-if="billing.pending_plan">Changes to {{ billing.pending_plan }} next period</span><span v-else>{{ billing.status }}</span></div>
        <div><p class="app-eyebrow">Trial</p><h2>{{ trialDays == null ? 'Not active' : `${trialDays} days` }}</h2><span>{{ billing.trial_ends_at ? formatDate(billing.trial_ends_at) : 'No trial end date' }}</span></div>
        <div><p class="app-eyebrow">Payment account</p><h2>{{ billing.has_customer ? 'Connected' : 'Not created' }}</h2><span>{{ billing.has_subscription ? 'Subscription active' : 'No subscription' }}</span></div>
        <button v-if="can('billing:manage')" class="app-button" :disabled="pendingAction === 'portal'" @click="openPortal"><CreditCard :size="15" />{{ pendingAction === 'portal' ? 'Opening...' : 'Billing portal' }}<ArrowUpRight :size="13" /></button>
      </section>

      <section class="billing-plans">
        <article v-for="plan in plans" :key="plan.id" class="billing-plan app-surface" :data-current="billing.plan === plan.id">
          <header><span v-if="billing.plan === plan.id">Current</span><p>{{ plan.label }}</p><h2>{{ plan.price }}<small v-if="plan.id !== 'enterprise'"> / month</small></h2></header>
          <dl>
            <div><dt>Projects</dt><dd>{{ plan.id === 'starter' ? '1' : plan.id === 'growth' ? '4' : plan.id === 'scale' ? '10' : 'Custom' }}</dd></div>
            <div><dt>Members</dt><dd>{{ plan.id === 'starter' ? '1' : plan.id === 'growth' ? '3' : plan.id === 'scale' ? '10' : 'Custom' }}</dd></div>
            <div><dt>AI and replay</dt><dd>{{ plan.id === 'starter' ? 'No' : 'Included' }}</dd></div>
          </dl>
          <button
            v-if="can('billing:manage') && billing.plan !== plan.id"
            class="app-button"
            :class="{ 'app-button--primary': plan.id === 'growth' }"
            :disabled="Boolean(pendingAction) || Boolean(billing.deletion_requested_at)"
            @click="billing.has_subscription && plan.id !== 'enterprise' ? openPortal() : checkout(plan.id)"
          >
            {{ pendingAction === `checkout:${plan.id}` ? 'Opening...' : plan.id === 'enterprise' ? 'Contact sales' : billing.has_subscription ? 'Change in portal' : 'Choose plan' }}
          </button>
          <span v-else-if="billing.plan === plan.id" class="billing-current"><Check :size="14" /> Active plan</span>
        </article>
      </section>

      <section class="billing-danger app-surface">
        <div><p class="app-eyebrow">Danger zone</p><h2>Delete workspace</h2><span>Seven-day recovery window before permanent deletion.</span></div>
        <button v-if="can('billing:manage') && !billing.deletion_requested_at" class="app-button billing-delete-button" @click="deleteOpen = true"><Trash2 :size="15" /> Schedule deletion</button>
      </section>
    </template>

    <AppDialog :open="deleteOpen" aria-labelledby="delete-title" @close="deleteOpen = false">
      <section class="billing-modal">
        <header><div><p class="app-eyebrow">Irreversible after grace</p><h2 id="delete-title">Schedule workspace deletion</h2></div><button class="app-icon-button" title="Close" @click="deleteOpen = false"><X :size="18" /></button></header>
        <form @submit.prevent="requestDeletion">
          <label>Type <code>{{ tenant?.slug }}</code> to confirm<input v-model.trim="confirmation" class="app-input" required autocomplete="off" autofocus /></label>
          <p v-if="actionError" class="billing-form-error">{{ actionError }}</p>
          <footer><button class="app-button" type="button" @click="deleteOpen = false">Cancel</button><button class="app-button billing-delete-button" type="submit" :disabled="pendingAction === 'delete' || confirmation !== tenant?.slug">{{ pendingAction === 'delete' ? 'Scheduling...' : 'Schedule deletion' }}</button></footer>
        </form>
      </section>
    </AppDialog>
  </div>
</template>

<style scoped>
.billing-page { max-width: 1320px; margin: 0 auto; }
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
