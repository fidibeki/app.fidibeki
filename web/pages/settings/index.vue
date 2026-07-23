<script setup lang="ts">
import {
  AlertTriangle,
  Check,
  Copy,
  KeyRound,
  Link2,
  MailCheck,
  Plus,
  ShieldCheck,
  Trash2,
  UserPlus,
  Users,
  X,
} from '@lucide/vue'
import {
  permissionLabel,
  permissionPresets,
  tenantPermissionGroups,
  type PermissionPreset,
} from '~/data/permissions'
import type {
  APIKey,
  Integration,
  IntegrationEvent,
  IntegrationKind,
  Member,
  MemberDomain,
  TenantConfiguration,
  WidgetAppearanceOverrides,
} from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatDate, formatMonthlyEuro, formatWholeNumber } from '~/utils/format'
import { cleanAppearanceOverrides, defaultWidgetAppearance } from '~/utils/widget-appearance'

const route = useRoute()
const runtimeConfig = useRuntimeConfig()
const { $apiFetch } = useNuxtApp()
const { can, canWrite, member: currentMember } = useAuth()

const tabs = [
  { id: 'workspace', label: 'Workspace' },
  { id: 'members', label: 'Members' },
  { id: 'keys', label: 'API keys' },
  { id: 'integrations', label: 'Integrations' },
] as const
type SettingsTab = typeof tabs[number]['id']

const activeTab = computed<SettingsTab>(() => {
  const value = typeof route.query.tab === 'string' ? route.query.tab : 'workspace'
  return tabs.some((tab) => tab.id === value) ? value as SettingsTab : 'workspace'
})

const { data, status, error, refresh } = await useAsyncData('settings-data', async () => {
  const [configuration, members, memberDomains, keys, integrations] = await Promise.all([
    $apiFetch<TenantConfiguration>('/v1/config'),
    can('member:read')
      ? $apiFetch<{ members: Member[] }>('/v1/members')
      : Promise.resolve({ members: [] }),
    can('member:read')
      ? $apiFetch<{ member_domains: MemberDomain[] }>('/v1/member-domains')
      : Promise.resolve({ member_domains: [] }),
    can('api_key:read')
      ? $apiFetch<{ api_keys: APIKey[] }>('/v1/api-keys')
      : Promise.resolve({ api_keys: [] }),
    can('integration:read')
      ? $apiFetch<{ integrations: Integration[] }>('/v1/integrations')
      : Promise.resolve({ integrations: [] }),
  ])
  return {
    configuration,
    members: members.members,
    memberDomains: memberDomains.member_domains,
    keys: keys.api_keys,
    integrations: integrations.integrations,
  }
})

const modal = ref<'member' | 'key' | 'integration' | null>(null)
const pending = ref(false)
const formError = ref('')
const memberMode = ref<'invite' | 'edit'>('invite')
const selectedMember = ref<Member | null>(null)
const memberEmail = ref('')
const memberPreset = ref<PermissionPreset>('viewer')
const memberPermissions = ref<string[]>([])
const domainVerificationEmail = ref('')
const domainVerificationPending = ref(false)
const domainVerificationError = ref('')
const domainVerificationNotice = ref('')
const keyName = ref('')
const keyRateLimit = ref(1000)
const keyExpiry = ref('never')
const keyPreset = ref<PermissionPreset>('viewer')
const keyPermissions = ref<string[]>([])
const revealedToken = ref('')
const tokenCopied = ref(false)
const integrationKind = ref<IntegrationKind>('webhook')
const integrationName = ref('')
const integrationURL = ref('')
const integrationChannel = ref('')
const integrationEvents = ref<IntegrationEvent[]>(['feedback.created'])
const workspaceAppearance = ref<WidgetAppearanceOverrides>({})
const appearanceNotice = ref('')
const revealedSecret = ref('')
const workspaceHost = computed(() => {
  const slug = data.value?.configuration.tenant.slug
  if (!slug) return ''
  return `${slug}.${String(runtimeConfig.public.baseDomain || 'fidibeki.com')}`
})
const hasVerifiedMemberDomains = computed(() => data.value?.memberDomains.some(domain => domain.status === 'verified') === true)

watch(data, (value) => {
  if (!value) return
  workspaceAppearance.value = { ...(value.configuration.tenant.widget_appearance_defaults || {}) }
}, { immediate: true })

const availablePermissionGroups = computed(() => tenantPermissionGroups.map((group) => ({
  ...group,
  permissions: group.permissions.filter((permission) => currentMember.value?.permissions.includes(permission)),
})).filter((group) => group.permissions.length))

const planPrice = computed(() => {
  const cents = data.value?.configuration.plan.monthly_price_cents
  return formatMonthlyEuro(cents)
})

const featureLabels: Record<string, string> = {
  ai: 'AI',
  session_replay: 'Session Replay',
  slack: 'Slack',
  video: 'Video',
  audio: 'Audio',
  dom_capture: 'DOM Capture',
  custom_domain: 'Custom Domain',
}

function applyPreset(target: 'member' | 'key', preset: PermissionPreset) {
  const allowed = new Set(currentMember.value?.permissions || [])
  const permissions = permissionPresets[preset].filter((permission) => allowed.has(permission))
  if (target === 'member') {
    memberPreset.value = preset
    memberPermissions.value = [...permissions]
  } else {
    keyPreset.value = preset
    keyPermissions.value = [...permissions]
  }
}

function togglePermission(target: 'member' | 'key', permission: string) {
  const permissions = target === 'member' ? memberPermissions : keyPermissions
  permissions.value = permissions.value.includes(permission)
    ? permissions.value.filter((value) => value !== permission)
    : [...permissions.value, permission]
}

function openMember(member?: Member) {
  if (!canWrite(member ? 'member:update' : 'member:invite')) return
  formError.value = ''
  selectedMember.value = member || null
  memberMode.value = member ? 'edit' : 'invite'
  memberEmail.value = member?.email || ''
  if (member) {
    memberPermissions.value = [...member.permissions]
  } else {
    applyPreset('member', 'viewer')
  }
  modal.value = 'member'
}

async function saveMember() {
  const permission = memberMode.value === 'invite' ? 'member:invite' : 'member:update'
  if (!canWrite(permission) || pending.value) return
  pending.value = true
  formError.value = ''
  try {
    if (memberMode.value === 'invite') {
      await $apiFetch('/auth/invite', {
        method: 'POST', body: { email: memberEmail.value, permissions: memberPermissions.value },
      })
    } else if (selectedMember.value) {
      await $apiFetch(`/v1/members/${selectedMember.value.id}`, {
        method: 'PATCH', body: { permissions: memberPermissions.value },
      })
    }
    modal.value = null
    await refresh()
  } catch (saveError) {
    formError.value = apiErrorMessage(saveError, 'Member settings could not be saved.')
  } finally {
    pending.value = false
  }
}

async function requestDomainVerification() {
  if (!canWrite('member:invite') || domainVerificationPending.value || !domainVerificationEmail.value) return
  domainVerificationPending.value = true
  domainVerificationError.value = ''
  domainVerificationNotice.value = ''
  try {
    await $apiFetch('/v1/member-domains', {
      method: 'POST', body: { email: domainVerificationEmail.value },
    })
    domainVerificationNotice.value = `Verification sent to ${domainVerificationEmail.value}.`
    domainVerificationEmail.value = ''
    await refresh()
  } catch (requestError) {
    domainVerificationError.value = apiErrorMessage(requestError, 'Domain verification could not be started.')
  } finally {
    domainVerificationPending.value = false
  }
}

function openKey() {
  if (!canWrite('api_key:create')) return
  formError.value = ''
  revealedToken.value = ''
  keyName.value = ''
  keyRateLimit.value = 1000
  keyExpiry.value = 'never'
  applyPreset('key', 'viewer')
  modal.value = 'key'
}

function expiryDate() {
  if (keyExpiry.value === 'never') return null
  const days = Number(keyExpiry.value)
  return new Date(Date.now() + days * 86_400_000).toISOString()
}

async function createKey() {
  if (!canWrite('api_key:create') || pending.value) return
  pending.value = true
  formError.value = ''
  try {
    const response = await $apiFetch<{ api_key: APIKey, token: string }>('/v1/api-keys', {
      method: 'POST',
      body: {
        name: keyName.value,
        permissions: keyPermissions.value,
        rate_limit_per_min: keyRateLimit.value,
        expires_at: expiryDate(),
      },
    })
    revealedToken.value = response.token
    await refresh()
  } catch (createError) {
    formError.value = apiErrorMessage(createError, 'API key could not be created.')
  } finally {
    pending.value = false
  }
}

async function copyToken() {
  await navigator.clipboard.writeText(revealedToken.value)
  tokenCopied.value = true
  window.setTimeout(() => { tokenCopied.value = false }, 1500)
}

async function copySecret() {
  await navigator.clipboard.writeText(revealedSecret.value)
}

async function deleteKey(key: APIKey) {
  if (!canWrite('api_key:delete') || !window.confirm(`Revoke ${key.name}? Requests using this key will fail immediately.`)) return
  try {
    await $apiFetch(`/v1/api-keys/${key.id}`, { method: 'DELETE' })
    await refresh()
  } catch (deleteError) {
    formError.value = apiErrorMessage(deleteError, 'API key could not be revoked.')
  }
}

function openIntegration() {
  if (!canWrite('integration:create')) return
  formError.value = ''
  revealedSecret.value = ''
  integrationKind.value = 'webhook'
  integrationName.value = ''
  integrationURL.value = ''
  integrationChannel.value = ''
  integrationEvents.value = ['feedback.created']
  modal.value = 'integration'
}

function toggleEvent(event: IntegrationEvent) {
  integrationEvents.value = integrationEvents.value.includes(event)
    ? integrationEvents.value.filter((value) => value !== event)
    : [...integrationEvents.value, event]
}

async function createIntegration() {
  if (!canWrite('integration:create') || pending.value) return
  pending.value = true
  formError.value = ''
  try {
    const response = await $apiFetch<{ integration: Integration, webhook_secret?: string }>('/v1/integrations', {
      method: 'POST',
      body: {
        kind: integrationKind.value,
        name: integrationName.value,
        webhook_url: integrationURL.value,
        channel: integrationKind.value === 'slack' ? integrationChannel.value : null,
        custom_headers: {},
        events: integrationEvents.value,
        is_active: true,
      },
    })
    revealedSecret.value = response.webhook_secret || ''
    await refresh()
    if (!revealedSecret.value) modal.value = null
  } catch (createError) {
    formError.value = apiErrorMessage(createError, 'Integration could not be created.')
  } finally {
    pending.value = false
  }
}

async function deleteIntegration(integration: Integration) {
  if (!canWrite('integration:delete') || !window.confirm(`Remove ${integration.name}?`)) return
  try {
    await $apiFetch(`/v1/integrations/${integration.id}`, { method: 'DELETE' })
    await refresh()
  } catch (deleteError) {
    formError.value = apiErrorMessage(deleteError, 'Integration could not be removed.')
  }
}

async function updateReporterAccess(enabled: boolean) {
  if (!canWrite('tenant:update') || !data.value || pending.value) return
  pending.value = true
  formError.value = ''
  try {
    await $apiFetch('/v1/reporter-access', { method: 'PATCH', body: { enabled } })
    data.value.configuration.tenant.reporter_access_enabled = enabled
  } catch (accessError) {
    formError.value = apiErrorMessage(accessError, 'Reporter access policy could not be updated.')
  } finally {
    pending.value = false
  }
}

async function updateWidgetAppearance() {
  if (!canWrite('tenant:update') || !data.value || pending.value) return
  pending.value = true
  formError.value = ''
  appearanceNotice.value = ''
  const appearance = cleanAppearanceOverrides(workspaceAppearance.value)
  try {
    await $apiFetch('/v1/widget-appearance', { method: 'PATCH', body: appearance })
    data.value.configuration.tenant.widget_appearance_defaults = appearance
    appearanceNotice.value = 'Widget appearance saved.'
  } catch (appearanceError) {
    formError.value = apiErrorMessage(appearanceError, 'Widget appearance could not be updated.')
  } finally {
    pending.value = false
  }
}

function isExpired(value?: string | null) {
  return value ? new Date(value).getTime() <= Date.now() : false
}

useHead({ title: 'Settings - Fidibeki' })
</script>

<template>
  <div class="app-page settings-page">
    <header class="app-page-head">
      <div><p class="app-eyebrow">Administration</p><h1>Settings</h1><span>Workspace access, credentials, and delivery connections.</span></div>
    </header>

    <nav class="settings-tabs" aria-label="Settings sections">
      <NuxtLink v-for="tab in tabs" :key="tab.id" :to="{ path: '/settings', query: tab.id === 'workspace' ? {} : { tab: tab.id } }" :data-active="activeTab === tab.id">{{ tab.label }}</NuxtLink>
    </nav>

    <section v-if="status === 'pending'" class="app-surface app-state"><div><span class="app-spinner" /><h2>Loading settings</h2></div></section>
    <section v-else-if="error" class="app-surface app-state"><div><AlertTriangle :size="24" /><h2>Settings unavailable</h2><p>{{ apiErrorMessage(error) }}</p><button class="app-button" @click="refresh()">Try again</button></div></section>

    <template v-else-if="data">
      <section v-if="activeTab === 'workspace'" class="settings-workspace">
        <div class="app-surface settings-section">
          <header><div><p class="app-eyebrow">Workspace</p><h2>{{ data.configuration.tenant.name }}</h2></div><span class="settings-state"><i />{{ data.configuration.tenant.status }}</span></header>
          <dl class="settings-ledger">
            <div><dt>Subdomain</dt><dd>{{ workspaceHost }}</dd></div>
            <div><dt>Plan</dt><dd class="capitalize">{{ data.configuration.plan.plan }}</dd></div>
            <div><dt>Rate</dt><dd>{{ planPrice }}</dd></div>
            <div><dt>Retention</dt><dd>{{ data.configuration.plan.limits.storage_days < 0 ? 'Custom' : `${data.configuration.plan.limits.storage_days} days` }}</dd></div>
          </dl>
        </div>
        <div class="app-surface settings-section">
          <header><div><p class="app-eyebrow">Entitlements</p><h2>Capacity and capture</h2></div><NuxtLink class="app-button" to="/billing">Manage billing</NuxtLink></header>
          <div class="settings-entitlements">
            <div><b>{{ data.configuration.plan.limits.projects < 0 ? 'Unlimited' : data.configuration.plan.limits.projects }}</b><span>Projects</span></div>
            <div><b>{{ data.configuration.plan.limits.feedback_per_month < 0 ? 'Unlimited' : formatWholeNumber(data.configuration.plan.limits.feedback_per_month) }}</b><span>Feedback / month</span></div>
            <div><b>{{ data.configuration.plan.limits.users < 0 ? 'Unlimited' : data.configuration.plan.limits.users }}</b><span>Members</span></div>
          </div>
          <ul class="settings-features">
            <li v-for="(enabled, feature) in data.configuration.plan.features" :key="feature" :data-enabled="enabled"><component :is="enabled ? Check : X" :size="13" />{{ featureLabels[String(feature)] || String(feature).replaceAll('_', ' ') }}</li>
          </ul>
        </div>
        <div class="app-surface settings-section settings-reporter-policy">
          <header><div><p class="app-eyebrow">Reporter access</p><h2>Progress links</h2></div><span class="settings-domain-mode" :data-enforced="data.configuration.tenant.reporter_access_enabled">{{ data.configuration.tenant.reporter_access_enabled ? 'Enabled' : 'Disabled' }}</span></header>
          <label><span><b>Reporter progress access</b><small>Applies to every project unless a project disables access.</small></span><input type="checkbox" :checked="data.configuration.tenant.reporter_access_enabled" :disabled="pending || !canWrite('tenant:update')" aria-label="Reporter progress access" @change="updateReporterAccess(($event.target as HTMLInputElement).checked)" /></label>
          <p v-if="formError" class="settings-form-error" role="alert">{{ formError }}</p>
        </div>
        <form class="app-surface settings-section settings-widget-appearance" @submit.prevent="updateWidgetAppearance">
          <header><div><p class="app-eyebrow">Widget defaults</p><h2>Controlled appearance</h2></div><button class="app-button app-button--primary" type="submit" :disabled="pending || !canWrite('tenant:update')">Save defaults</button></header>
          <WidgetAppearanceEditor v-model="workspaceAppearance" :parent="defaultWidgetAppearance" />
          <p v-if="appearanceNotice" class="settings-domain-notice" role="status">{{ appearanceNotice }}</p>
        </form>
      </section>

      <div v-else-if="activeTab === 'members'" class="settings-members-page">
        <section class="app-surface settings-section settings-domain-policy">
          <header><div><p class="app-eyebrow">Membership policy</p><h2>Allowed email domains</h2></div><span class="settings-domain-mode" :data-enforced="hasVerifiedMemberDomains">{{ hasVerifiedMemberDomains ? 'Enforced' : 'Open policy' }}</span></header>
          <div v-if="!can('member:read')" class="settings-empty"><ShieldCheck :size="23" /><h3>Member access required</h3></div>
          <template v-else>
            <ul v-if="data.memberDomains.length" class="settings-domain-list">
              <li v-for="domain in data.memberDomains" :key="domain.id">
                <MailCheck :size="16" />
                <span><b>{{ domain.domain }}</b><small>{{ domain.status === 'verified' ? `Verified ${formatDate(domain.verified_at!)}` : `Verification sent to ${domain.verification_email}` }}</small></span>
                <span class="status-pill" :class="domain.status === 'verified' ? 'status-completed' : 'status-pending'">{{ domain.status }}</span>
              </li>
            </ul>
            <form v-if="canWrite('member:invite')" class="settings-domain-form" @submit.prevent="requestDomainVerification">
              <label>Verification email<input v-model.trim="domainVerificationEmail" class="app-input" type="email" required placeholder="security@company.com" /></label>
              <button class="app-button" type="submit" :disabled="domainVerificationPending || !domainVerificationEmail"><MailCheck :size="15" /> {{ domainVerificationPending ? 'Sending...' : 'Verify domain' }}</button>
            </form>
            <p v-if="domainVerificationNotice" class="settings-domain-notice" role="status">{{ domainVerificationNotice }}</p>
            <p v-if="domainVerificationError" class="settings-form-error" role="alert">{{ domainVerificationError }}</p>
          </template>
        </section>

        <section class="app-surface settings-section">
          <header><div><p class="app-eyebrow">Access</p><h2>Members</h2></div><button v-if="canWrite('member:invite')" class="app-button app-button--primary" @click="openMember()"><UserPlus :size="15" /> Invite member</button></header>
          <div v-if="!can('member:read')" class="settings-empty"><ShieldCheck :size="23" /><h3>Member access required</h3></div>
          <div v-else-if="!data.members.length" class="settings-empty"><Users :size="23" /><h3>No members found</h3></div>
          <div v-else class="settings-table settings-members">
            <div class="settings-row settings-row--head"><span>Member</span><span>Permissions</span><span>Access</span><span /></div>
            <div v-for="member in data.members" :key="member.id" class="settings-row">
              <span class="settings-identity"><i>{{ (member.name || member.email).slice(0, 1).toUpperCase() }}</i><span><b>{{ member.name || member.email.split('@')[0] }}</b><small>{{ member.email }}</small></span></span>
              <span>{{ member.permissions.length }} granted</span>
              <span>{{ member.id === currentMember?.id ? 'Current session' : 'Member' }}</span>
              <button v-if="canWrite('member:update')" class="app-icon-button" title="Edit permissions" @click="openMember(member)"><ShieldCheck :size="16" /></button>
            </div>
          </div>
        </section>
      </div>

      <section v-else-if="activeTab === 'keys'" class="app-surface settings-section">
        <header><div><p class="app-eyebrow">Machine access</p><h2>API keys</h2></div><button v-if="canWrite('api_key:create')" class="app-button app-button--primary" @click="openKey"><Plus :size="15" /> Create key</button></header>
        <div v-if="!can('api_key:read')" class="settings-empty"><ShieldCheck :size="23" /><h3>API key access required</h3></div>
        <div v-else-if="!data.keys.length" class="settings-empty"><KeyRound :size="23" /><h3>No API keys</h3></div>
        <div v-else class="settings-table settings-keys">
          <div class="settings-row settings-row--head"><span>Key</span><span>Scope</span><span>Last used</span><span>Expiry</span><span /></div>
          <div v-for="key in data.keys" :key="key.id" class="settings-row" :data-muted="isExpired(key.expires_at)">
            <span><b>{{ key.name }}</b><code>{{ key.token_prefix }}</code></span>
            <span>{{ key.permissions.length }} perms · {{ formatWholeNumber(key.rate_limit_per_min) }}/min</span>
            <span>{{ key.last_used_at ? formatDate(key.last_used_at) : 'Never' }}</span>
            <span>{{ key.expires_at ? formatDate(key.expires_at) : 'Never' }}</span>
            <button v-if="canWrite('api_key:delete')" class="app-icon-button settings-danger-icon" title="Revoke API key" @click="deleteKey(key)"><Trash2 :size="16" /></button>
          </div>
        </div>
      </section>

      <section v-else class="app-surface settings-section">
        <header><div><p class="app-eyebrow">Delivery</p><h2>Integrations</h2></div><button v-if="canWrite('integration:create')" class="app-button app-button--primary" @click="openIntegration"><Plus :size="15" /> Add integration</button></header>
        <div v-if="!can('integration:read')" class="settings-empty"><ShieldCheck :size="23" /><h3>Integration access required</h3></div>
        <div v-else-if="!data.integrations.length" class="settings-empty"><Link2 :size="23" /><h3>No integrations</h3></div>
        <div v-else class="settings-table settings-integrations">
          <div class="settings-row settings-row--head"><span>Integration</span><span>Endpoint</span><span>Events</span><span>Status</span><span /></div>
          <div v-for="integration in data.integrations" :key="integration.id" class="settings-row">
            <span><b>{{ integration.name }}</b><small class="capitalize">{{ integration.kind }}</small></span>
            <code>{{ integration.webhook_url }}</code>
            <span>{{ integration.events.length }} subscribed</span>
            <span class="settings-state"><i />{{ integration.is_active ? 'Active' : 'Paused' }}</span>
            <button v-if="canWrite('integration:delete')" class="app-icon-button settings-danger-icon" title="Remove integration" @click="deleteIntegration(integration)"><Trash2 :size="16" /></button>
          </div>
        </div>
      </section>
    </template>

    <AppDialog :open="Boolean(modal)" aria-labelledby="settings-modal-title" @close="modal = null">
      <section v-if="modal" class="settings-modal">
        <header><div><p class="app-eyebrow">{{ modal === 'member' ? 'Access policy' : modal === 'key' ? 'Machine credential' : 'Delivery endpoint' }}</p><h2 id="settings-modal-title">{{ modal === 'member' ? (memberMode === 'invite' ? 'Invite member' : 'Edit member') : modal === 'key' ? 'Create API key' : 'Add integration' }}</h2></div><button class="app-icon-button" title="Close" @click="modal = null"><X :size="18" /></button></header>

        <form v-if="modal === 'member'" @submit.prevent="saveMember">
          <label>Email<input v-model.trim="memberEmail" class="app-input" type="email" required :disabled="memberMode === 'edit'" autofocus /></label>
          <div class="permission-presets" role="group" aria-label="Permission preset"><button v-for="preset in (['admin', 'editor', 'viewer'] as PermissionPreset[])" :key="preset" type="button" :data-active="memberPreset === preset" @click="applyPreset('member', preset)">{{ preset }}</button></div>
          <div class="permission-grid"><fieldset v-for="group in availablePermissionGroups" :key="group.label"><legend>{{ group.label }}</legend><label v-for="permission in group.permissions" :key="permission"><input type="checkbox" :checked="memberPermissions.includes(permission)" @change="togglePermission('member', permission)" />{{ permissionLabel(permission) }}</label></fieldset></div>
          <p v-if="formError" class="settings-form-error" role="alert">{{ formError }}</p>
          <footer><button class="app-button" type="button" @click="modal = null">Cancel</button><button class="app-button app-button--primary" type="submit" :disabled="pending || !memberEmail || !memberPermissions.length">{{ pending ? 'Saving...' : memberMode === 'invite' ? 'Send invitation' : 'Save permissions' }}</button></footer>
        </form>

        <form v-else-if="modal === 'key'" @submit.prevent="createKey">
          <div v-if="revealedToken" class="settings-secret"><ShieldCheck :size="20" /><h3>API key created</h3><code>{{ revealedToken }}</code><button class="app-button" type="button" @click="copyToken"><component :is="tokenCopied ? Check : Copy" :size="15" />{{ tokenCopied ? 'Copied' : 'Copy key' }}</button></div>
          <template v-else>
            <div class="settings-form-grid"><label>Name<input v-model.trim="keyName" class="app-input" required maxlength="120" placeholder="Production exporter" autofocus /></label><label>Expires<select v-model="keyExpiry" class="app-select"><option value="never">Never</option><option value="30">30 days</option><option value="90">90 days</option><option value="365">1 year</option></select></label><label>Rate limit / minute<input v-model.number="keyRateLimit" class="app-input" type="number" min="1" max="100000" required /></label></div>
            <div class="permission-presets" role="group" aria-label="Permission preset"><button v-for="preset in (['admin', 'editor', 'viewer'] as PermissionPreset[])" :key="preset" type="button" :data-active="keyPreset === preset" @click="applyPreset('key', preset)">{{ preset }}</button></div>
            <div class="permission-grid"><fieldset v-for="group in availablePermissionGroups" :key="group.label"><legend>{{ group.label }}</legend><label v-for="permission in group.permissions" :key="permission"><input type="checkbox" :checked="keyPermissions.includes(permission)" @change="togglePermission('key', permission)" />{{ permissionLabel(permission) }}</label></fieldset></div>
            <p v-if="formError" class="settings-form-error" role="alert">{{ formError }}</p>
          </template>
          <footer><button class="app-button" type="button" @click="modal = null">{{ revealedToken ? 'Done' : 'Cancel' }}</button><button v-if="!revealedToken" class="app-button app-button--primary" type="submit" :disabled="pending || !keyName || !keyPermissions.length">{{ pending ? 'Creating...' : 'Create key' }}</button></footer>
        </form>

        <form v-else @submit.prevent="createIntegration">
          <div v-if="revealedSecret" class="settings-secret"><ShieldCheck :size="20" /><h3>Signing secret</h3><code>{{ revealedSecret }}</code><button class="app-button" type="button" @click="copySecret"><Copy :size="15" /> Copy secret</button></div>
          <template v-else>
            <div class="settings-form-grid"><label>Type<select v-model="integrationKind" class="app-select" autofocus><option value="webhook">Webhook</option><option value="slack">Slack</option><option value="teams">Teams</option></select></label><label>Name<input v-model.trim="integrationName" class="app-input" required maxlength="100" placeholder="Operations" /></label><label class="wide">Webhook URL<input v-model.trim="integrationURL" class="app-input" type="url" required placeholder="https://hooks.example.com/..." /></label><label v-if="integrationKind === 'slack'" class="wide">Channel<input v-model.trim="integrationChannel" class="app-input" required placeholder="#feedback" /></label></div>
            <fieldset class="settings-events"><legend>Events</legend><label v-for="event in (['feedback.created', 'issue.created', 'issue.updated', 'issue.resolved'] as IntegrationEvent[])" :key="event"><input type="checkbox" :checked="integrationEvents.includes(event)" @change="toggleEvent(event)" />{{ event }}</label></fieldset>
            <p v-if="formError" class="settings-form-error" role="alert">{{ formError }}</p>
          </template>
          <footer><button class="app-button" type="button" @click="modal = null">{{ revealedSecret ? 'Done' : 'Cancel' }}</button><button v-if="!revealedSecret" class="app-button app-button--primary" type="submit" :disabled="pending || !integrationName || !integrationURL || !integrationEvents.length">{{ pending ? 'Connecting...' : 'Add integration' }}</button></footer>
        </form>
      </section>
    </AppDialog>
  </div>
</template>

<style scoped>
.settings-tabs { display: flex; gap: 22px; margin: -4px 0 18px; border-bottom: 1px solid var(--line); }
.settings-tabs a { position: relative; padding: 0 1px 11px; color: var(--muted); font-size: 11px; font-weight: 750; text-decoration: none; }
.settings-tabs a[data-active='true'] { color: var(--teal); }
.settings-tabs a[data-active='true']::after { position: absolute; right: 0; bottom: -1px; left: 0; height: 2px; background: var(--teal); content: ''; }
.settings-workspace { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr); gap: 16px; }
.settings-reporter-policy { grid-column: 1 / -1; }
.settings-widget-appearance { grid-column: 1 / -1; }
.settings-widget-appearance > :deep(.appearance-editor) { padding: 16px; }
.settings-reporter-policy > label { display: flex; min-height: 66px; align-items: center; justify-content: space-between; gap: 18px; padding: 12px 16px; }
.settings-reporter-policy > label > span { display: flex; flex-direction: column; gap: 4px; }
.settings-reporter-policy b { font-size: 11px; }
.settings-reporter-policy small { color: var(--muted); font-size: 9px; }
.settings-reporter-policy input { width: 18px; height: 18px; accent-color: var(--teal); }
.settings-reporter-policy .settings-form-error { margin: 0 16px 14px; }
.settings-members-page { display: grid; gap: 16px; }
.settings-section { overflow: hidden; }
.settings-section > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; gap: 14px; padding: 11px 16px; border-bottom: 1px solid var(--line); }
.settings-section h2 { margin: 3px 0 0; font-size: 14px; }
.settings-state { display: inline-flex; align-items: center; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 700; text-transform: capitalize; }
.settings-state i { width: 7px; height: 7px; background: var(--green); border-radius: 50%; }
.settings-ledger { margin: 0; }
.settings-ledger > div { display: flex; min-height: 48px; align-items: center; justify-content: space-between; gap: 14px; padding: 0 16px; border-bottom: 1px solid var(--line); }
.settings-ledger > div:last-child { border-bottom: 0; }
.settings-ledger dt { color: var(--muted); font-size: 10px; }
.settings-ledger dd { margin: 0; color: var(--ink-soft); font-size: 11px; font-weight: 700; }
.settings-entitlements { display: grid; grid-template-columns: repeat(3, 1fr); border-bottom: 1px solid var(--line); }
.settings-entitlements > div { display: flex; min-height: 92px; align-items: center; justify-content: center; flex-direction: column; gap: 4px; border-right: 1px solid var(--line); }
.settings-entitlements > div:last-child { border-right: 0; }
.settings-entitlements b { font-size: 20px; }
.settings-entitlements span { color: var(--muted); font-size: 9px; text-transform: uppercase; }
.settings-features { display: grid; grid-template-columns: repeat(2, 1fr); gap: 0; margin: 0; padding: 8px 16px; list-style: none; }
.settings-features li { display: flex; min-height: 32px; align-items: center; gap: 7px; color: var(--muted); font-size: 10px; text-transform: capitalize; }
.settings-features li[data-enabled='true'] { color: var(--green); }
.settings-domain-mode { padding: 5px 7px; background: var(--surface-soft); color: var(--muted); border-radius: 4px; font-size: 9px; font-weight: 800; text-transform: uppercase; }
.settings-domain-mode[data-enforced='true'] { background: var(--green-soft); color: var(--green); }
.settings-domain-list { margin: 0; padding: 0; list-style: none; }
.settings-domain-list li { display: grid; min-height: 56px; grid-template-columns: 22px minmax(0, 1fr) auto; align-items: center; gap: 9px; padding: 8px 16px; border-bottom: 1px solid var(--line); color: var(--teal); }
.settings-domain-list li > span:nth-child(2) { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.settings-domain-list b { color: var(--ink-soft); font-size: 11px; }
.settings-domain-list small { overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.settings-domain-form { display: grid; grid-template-columns: minmax(220px, 1fr) auto; align-items: end; gap: 9px; padding: 14px 16px; }
.settings-domain-form label { display: grid; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.settings-domain-form .app-button { min-height: 38px; }
.settings-domain-notice, .settings-domain-policy > .settings-form-error { margin: 0 16px 14px; }
.settings-table { width: 100%; }
.settings-row { display: grid; min-height: 62px; align-items: center; gap: 14px; padding: 7px 15px; border-bottom: 1px solid var(--line); color: var(--muted); font-size: 10px; }
.settings-row:last-child { border-bottom: 0; }
.settings-row--head { min-height: 36px; padding-top: 0; padding-bottom: 0; background: #f8faf9; font-size: 9px; font-weight: 750; text-transform: uppercase; }
.settings-members .settings-row { grid-template-columns: minmax(190px, 1.5fr) minmax(110px, .7fr) minmax(110px, .7fr) 38px; }
.settings-keys .settings-row { grid-template-columns: minmax(190px, 1.3fr) minmax(160px, 1fr) 100px 100px 38px; }
.settings-integrations .settings-row { grid-template-columns: minmax(170px, .8fr) minmax(220px, 1.4fr) 100px 90px 38px; }
.settings-row > span { min-width: 0; }
.settings-row > span > b { display: block; color: var(--ink-soft); font-size: 11px; }
.settings-row small, .settings-row code { display: block; margin-top: 3px; overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.settings-row[data-muted='true'] { opacity: .55; }
.settings-identity { display: flex; align-items: center; gap: 10px; }
.settings-identity > i { display: grid; width: 32px; height: 32px; flex: 0 0 32px; place-items: center; background: var(--teal-soft); color: var(--teal); border-radius: 5px; font-style: normal; font-weight: 800; }
.settings-identity > span { min-width: 0; }
.settings-danger-icon { color: var(--red); }
.settings-empty { display: grid; min-height: 220px; place-items: center; align-content: center; gap: 8px; color: var(--muted); }
.settings-empty h3 { margin: 0; color: var(--ink-soft); font-size: 12px; }
.settings-modal { width: min(720px, 100%); max-height: min(88vh, 860px); overflow: auto; background: #fff; border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.settings-modal > header { position: sticky; top: 0; z-index: 2; display: flex; min-height: 68px; align-items: center; justify-content: space-between; padding: 10px 15px; background: #fff; border-bottom: 1px solid var(--line); }
.settings-modal h2 { margin: 3px 0 0; font-size: 15px; }
.settings-modal form { padding: 16px; }
.settings-modal form > label, .settings-form-grid label { display: flex; flex-direction: column; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.settings-form-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; }
.settings-form-grid .wide { grid-column: 1 / -1; }
.permission-presets { display: inline-flex; margin: 16px 0 12px; border: 1px solid var(--line-strong); border-radius: 5px; overflow: hidden; }
.permission-presets button { min-height: 34px; padding: 0 14px; background: #fff; color: var(--muted); border: 0; border-right: 1px solid var(--line); font-size: 10px; font-weight: 750; text-transform: capitalize; }
.permission-presets button:last-child { border-right: 0; }
.permission-presets button[data-active='true'] { background: var(--teal-soft); color: var(--teal); }
.permission-grid { display: grid; grid-template-columns: repeat(3, 1fr); border: 1px solid var(--line); border-radius: 5px; }
.permission-grid fieldset { min-width: 0; margin: 0; padding: 11px; border: 0; border-right: 1px solid var(--line); border-bottom: 1px solid var(--line); }
.permission-grid fieldset:nth-child(3n) { border-right: 0; }
.permission-grid legend, .settings-events legend { padding: 0; color: var(--ink); font-size: 10px; font-weight: 800; }
.permission-grid label, .settings-events label { display: flex; min-height: 26px; align-items: center; gap: 7px; color: var(--muted); font-size: 9px; text-transform: capitalize; }
.permission-grid input, .settings-events input { accent-color: var(--teal); }
.settings-events { display: grid; grid-template-columns: repeat(2, 1fr); gap: 2px 10px; margin: 15px 0 0; padding: 12px; border: 1px solid var(--line); border-radius: 5px; }
.settings-events legend { padding: 0 4px; }
.settings-modal footer { display: flex; justify-content: flex-end; gap: 8px; margin: 16px -16px -16px; padding: 12px 16px; background: #f8faf9; border-top: 1px solid var(--line); }
.settings-form-error { padding: 9px 10px; background: var(--red-soft); color: var(--red); border-radius: 4px; font-size: 10px; }
.settings-secret { display: grid; place-items: center; gap: 10px; padding: 25px; text-align: center; }
.settings-secret h3 { margin: 0; font-size: 14px; }
.settings-secret code { width: 100%; padding: 10px; overflow-wrap: anywhere; background: #f3f6f5; border: 1px solid var(--line); border-radius: 4px; color: var(--ink); font-size: 11px; }
.capitalize { text-transform: capitalize; }
@media (max-width: 900px) { .settings-workspace { grid-template-columns: 1fr; } .settings-row--head { display: none; } .settings-members .settings-row, .settings-keys .settings-row, .settings-integrations .settings-row { grid-template-columns: minmax(0, 1fr) 38px; } .settings-row > span:not(:first-child), .settings-row > code { display: none; } }
@media (max-width: 650px) { .settings-tabs { gap: 15px; overflow-x: auto; } .settings-entitlements { grid-template-columns: 1fr; } .settings-entitlements > div { min-height: 70px; border-right: 0; border-bottom: 1px solid var(--line); } .settings-entitlements > div:last-child { border-bottom: 0; } .settings-features, .permission-grid, .settings-form-grid, .settings-events, .settings-domain-form { grid-template-columns: 1fr; } .settings-domain-form .app-button { width: 100%; } .permission-grid fieldset, .permission-grid fieldset:nth-child(3n) { border-right: 0; } }
</style>
