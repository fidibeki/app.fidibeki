<script setup lang="ts">
import { canForceDelete } from '~/utils/destructive-confirmation'

type Member = { id: string; email: string; name?: string; status: string; last_login_at?: string }
type Detail = { tenant: { id: string; name: string; slug: string; plan: string; status: string; billing_email?: string }; member_count: number; project_count: number; feedback_count: number; owner?: Member }
type Audit = { events: Array<{ id: string; kind: string; actor_type: string; action?: string; outcome?: string; created_at: string }>; next_cursor?: string }
type RecoveryCandidates = { members: Member[]; next_cursor?: string }
const route = useRoute()
const { $platformApi } = useNuxtApp()
const id = String(route.params.id)
const { data: detail, error, refresh: refreshDetail } = await useAsyncData(`workspace:${id}`, () => $platformApi<Detail>(`/admin/backoffice/tenants/${id}`), { server: false })
const { data: audit, refresh: refreshAudit } = await useAsyncData(`audit:${id}`, () => $platformApi<Audit>(`/admin/backoffice/tenants/${id}/access-events`), { server: false })
const { data: recoveryCandidates, refresh: refreshRecoveryCandidates } = await useAsyncData(`recovery-candidates:${id}`, () => $platformApi<RecoveryCandidates>(`/admin/backoffice/tenants/${id}/recovery-candidates`), { server: false })
const typedSlug = ref('')
const deleting = ref(false)
const deleteError = ref('')
const saving = ref(false)
const saveError = ref('')
const name = ref('')
const plan = ref('')
const status = ref('')
const auditEvents = ref<Audit['events']>([])
const auditCursor = ref('')
const recoveryMembers = ref<Member[]>([])
const recoveryCursor = ref('')
const loadingRecoveryCandidates = ref(false)
const recoveryMember = ref('')
const recoveryConfirmation = ref('')
const recoveryReference = ref('')
const recoveryReason = ref('')
const recovering = ref(false)
const recoveryError = ref('')
watch(detail, (value) => { if (value) { name.value = value.tenant.name; plan.value = value.tenant.plan; status.value = value.tenant.status } }, { immediate: true })
watch(audit, (value) => { if (value) { auditEvents.value = value.events; auditCursor.value = value.next_cursor || '' } }, { immediate: true })
watch(recoveryCandidates, (value) => { if (value) { recoveryMembers.value = value.members; recoveryCursor.value = value.next_cursor || '' } }, { immediate: true })
const canDelete = computed(() => detail.value ? canForceDelete(typedSlug.value, detail.value.tenant.slug) : false)
const canRecover = computed(() => detail.value ? canForceDelete(recoveryConfirmation.value, detail.value.tenant.slug) : false)
const eligibleRecoveryMembers = computed(() => recoveryMembers.value)
async function saveWorkspace() {
  if (!detail.value || saving.value) return
  saving.value = true; saveError.value = ''
  try {
    await $platformApi(`/admin/tenants/${id}`, { method: 'PATCH', body: { name: name.value, plan: plan.value, status: status.value } })
    await Promise.all([refreshDetail(), refreshAudit()])
  } catch { saveError.value = 'The workspace could not be updated. No change was confirmed.' } finally { saving.value = false }
}
async function forceDelete() {
  if (!detail.value || !canDelete.value || deleting.value) return
  deleting.value = true; deleteError.value = ''
  try { await $platformApi(`/admin/tenants/${id}`, { method: 'DELETE' }); await navigateTo('/workspaces') } catch { deleteError.value = 'The workspace could not be deleted. No change was confirmed.' } finally { deleting.value = false }
}
async function loadMoreAudit() {
  if (!auditCursor.value) return
  const page = await $platformApi<Audit>(`/admin/backoffice/tenants/${id}/access-events?cursor=${encodeURIComponent(auditCursor.value)}`)
  auditEvents.value.push(...page.events); auditCursor.value = page.next_cursor || ''
}
async function loadMoreRecoveryCandidates() {
  if (!recoveryCursor.value || loadingRecoveryCandidates.value) return
  loadingRecoveryCandidates.value = true
  recoveryError.value = ''
  try {
    const page = await $platformApi<RecoveryCandidates>(`/admin/backoffice/tenants/${id}/recovery-candidates?cursor=${encodeURIComponent(recoveryCursor.value)}`)
    recoveryMembers.value.push(...page.members)
    recoveryCursor.value = page.next_cursor || ''
  } catch { recoveryError.value = 'Eligible members could not be loaded. Retry before recovering ownership.' } finally { loadingRecoveryCandidates.value = false }
}
async function recoverOwnership() {
  if (!detail.value || !recoveryMember.value || !canRecover.value || recovering.value) return
  recovering.value = true; recoveryError.value = ''
  try {
    await $platformApi(`/admin/tenants/${id}/ownership/recover`, { method: 'POST', body: { target_member_id: recoveryMember.value, confirmation: recoveryConfirmation.value, verification_reference: recoveryReference.value, reason: recoveryReason.value } })
    await Promise.all([refreshDetail(), refreshAudit(), refreshRecoveryCandidates()])
    recoveryMember.value = ''; recoveryConfirmation.value = ''; recoveryReference.value = ''; recoveryReason.value = ''
  } catch { recoveryError.value = 'Ownership recovery was not confirmed. Check the verification evidence and recent authentication.' } finally { recovering.value = false }
}
</script>
<template>
  <section v-if="detail">
    <p class="eyebrow">Workspace / {{ detail.tenant.slug }}</p><h1 class="title">{{ detail.tenant.name }}</h1>
    <div class="metrics"><article class="metric"><b>{{ detail.member_count }}</b><span>Members</span></article><article class="metric"><b>{{ detail.project_count }}</b><span>Projects</span></article><article class="metric"><b>{{ detail.feedback_count }}</b><span>Feedback items</span></article><article class="metric"><b>{{ detail.tenant.plan }}</b><span>{{ detail.tenant.status }}</span></article></div>
    <form class="panel" @submit.prevent="saveWorkspace"><p class="eyebrow">Workspace controls</p><h2>Lifecycle and plan</h2><label>Name<input v-model="name" required maxlength="120"></label><label>Plan<select v-model="plan"><option value="starter">starter</option><option value="growth">growth</option><option value="scale">scale</option><option value="enterprise">enterprise</option></select></label><label>Status<select v-model="status"><option value="trial">trial</option><option value="active">active</option><option value="suspended">suspended</option><option value="cancelled">cancelled</option></select></label><button class="button" :disabled="saving">{{ saving ? 'Saving…' : 'Save changes' }}</button><p v-if="saveError" class="error" role="alert">{{ saveError }}</p></form>
    <form class="panel" @submit.prevent="recoverOwnership"><p class="eyebrow">Ownership recovery</p><h2>Current owner: {{ detail.owner?.email || 'Unassigned' }}</h2><p class="muted">Recovery requires a recently authenticated platform admin, an active member who has signed in, and recorded verification evidence.</p><label>Verified member<select v-model="recoveryMember" required><option value="">Choose an eligible member</option><option v-for="member in eligibleRecoveryMembers" :key="member.id" :value="member.id">{{ member.email }}</option></select></label><button v-if="recoveryCursor" class="button secondary" type="button" :disabled="loadingRecoveryCandidates" @click="loadMoreRecoveryCandidates">{{ loadingRecoveryCandidates ? 'Loading…' : 'Load more eligible members' }}</button><label>Verification reference<input v-model="recoveryReference" required minlength="3"></label><label>Reason<textarea v-model="recoveryReason" required minlength="10" /></label><label>Type {{ detail.tenant.slug }} to confirm<input v-model="recoveryConfirmation" :placeholder="detail.tenant.slug" aria-label="Confirm ownership recovery"></label><button class="button" :disabled="recovering || !canRecover">{{ recovering ? 'Recovering…' : 'Recover ownership' }}</button><p v-if="recoveryError" class="error" role="alert">{{ recoveryError }}</p></form>
    <section class="panel"><p class="eyebrow">Audit ledger</p><table class="table"><thead><tr><th>Event</th><th>Actor</th><th>Outcome</th><th>When</th></tr></thead><tbody><tr v-for="event in auditEvents" :key="event.id"><td>{{ event.kind }}</td><td>{{ event.actor_type }}</td><td>{{ event.outcome || '—' }}</td><td>{{ new Date(event.created_at).toLocaleString() }}</td></tr></tbody></table><button v-if="auditCursor" class="button" @click="loadMoreAudit">Load more audit events</button></section>
    <section class="panel"><p class="eyebrow error">Danger zone</p><h2>Force-delete this workspace</h2><p class="muted">This runs the platform’s audited deletion executor. Type <b>{{ detail.tenant.slug }}</b> exactly to enable it.</p><input v-model="typedSlug" :placeholder="detail.tenant.slug" aria-label="Confirm workspace slug"><button class="button" :disabled="!canDelete || deleting" @click="forceDelete">{{ deleting ? 'Deleting…' : 'Force delete workspace' }}</button><p v-if="deleteError" class="error" role="alert">{{ deleteError }}</p></section>
  </section>
  <section v-else><h1 class="title">Workspace unavailable</h1><p class="error">{{ error ? 'The workspace could not be loaded.' : 'Loading…' }}</p></section>
</template>
