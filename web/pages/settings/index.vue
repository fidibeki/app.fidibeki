<script setup lang="ts">
import {
  AlertTriangle,
  ArrowRightLeft,
  Check,
  Copy,
  KeyRound,
  Link2,
  MailCheck,
  Plus,
  ShieldCheck,
  SlidersHorizontal,
  Trash2,
  UserCheck,
  UserPlus,
  UserX,
  Users,
  X,
} from '@lucide/vue'
import {
  permissionPresets,
  tenantPermissionGroups,
  type PermissionPreset,
} from '~/data/permissions'
import type {
  APIKey,
  Integration,
  IntegrationEvent,
  IntegrationKind,
  IntegrationPrivacyField,
  Member,
  MemberDomain,
  OrganizationProfile,
  Project,
  TenantConfiguration,
  WidgetAppearanceOverrides,
} from '~/types/api'
import { apiErrorMessage, apiFieldErrors } from '~/utils/api-fetch'
import { cleanAppearanceOverrides, defaultWidgetAppearance } from '~/utils/widget-appearance'

const route = useRoute()
const runtimeConfig = useRuntimeConfig()
const { $apiFetch } = useNuxtApp()
const { can, canWrite, member: currentMember } = useAuth()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { currency, date, integer, plural } = useRegionalFormat()
const { success: showSuccessToast } = useAppToast()

const tabIDs = ['workspace', 'members', 'keys', 'integrations', 'health', 'privacy', 'audit'] as const
type SettingsTab = typeof tabIDs[number]
const tabs = computed(() => tabIDs.map(id => ({
  id,
  label: t(`admin.settings.tabs.${id}`),
})))

const activeTab = computed<SettingsTab>(() => {
  const value = typeof route.query.tab === 'string' ? route.query.tab : 'workspace'
  return tabIDs.includes(value as SettingsTab) ? value as SettingsTab : 'workspace'
})

const { data, status, error, refresh } = await useAsyncData('settings-data', async () => {
  const [configuration, members, memberDomains, keys, integrations, projects] = await Promise.all([
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
    can('project:read')
      ? $apiFetch<{ projects: Project[] }>('/v1/projects')
      : Promise.resolve({ projects: [] }),
  ])
  return {
    configuration,
    members: members.members,
    memberDomains: memberDomains.member_domains,
    keys: keys.api_keys,
    integrations: integrations.integrations,
    projects: projects.projects,
  }
})

const modal = ref<'member' | 'member-lifecycle' | 'transfer' | 'key' | 'integration' | 'integration-privacy' | null>(null)
const pending = ref(false)
const formError = ref('')
const reporterAccessError = ref('')
const appearanceFieldErrors = ref<Record<string, string>>({})
const appearanceFormError = ref('')
const memberEmailError = ref('')
const memberEmailInput = ref<HTMLInputElement | null>(null)
const memberMode = ref<'invite' | 'edit'>('invite')
const selectedMember = ref<Member | null>(null)
const memberAction = ref<'deactivate' | 'reactivate' | 'remove' | null>(null)
const memberActionError = ref('')
const transferTargetID = ref('')
const transferConfirmation = ref('')
const transferError = ref('')
const memberEmail = ref('')
type PermissionSelection = PermissionPreset | 'custom'
const memberPreset = ref<PermissionSelection>('viewer')
const memberPermissions = ref<string[]>([])
const domainVerificationEmail = ref('')
const domainVerificationPending = ref(false)
const domainVerificationError = ref('')
const domainVerificationNotice = ref('')
const keyName = ref('')
const keyRateLimit = ref(1000)
const keyExpiry = ref('never')
const keyPreset = ref<PermissionSelection>('viewer')
const keyPermissions = ref<string[]>([])
const revealedToken = ref('')
const tokenCopied = ref(false)
const integrationKind = ref<IntegrationKind>('webhook')
const integrationName = ref('')
const integrationURL = ref('')
const integrationChannel = ref('')
const integrationEvents = ref<IntegrationEvent[]>(['feedback.created'])
const integrationPrivacyFields = ref<IntegrationPrivacyField[]>([])
const selectedIntegration = ref<Integration | null>(null)
const workspaceAppearance = ref<WidgetAppearanceOverrides>({})
const revealedSecret = ref('')
type DestructiveSettingAction =
  | { kind: 'key', resource: APIKey }
  | { kind: 'integration', resource: Integration }
const destructiveAction = ref<DestructiveSettingAction | null>(null)
const destructivePending = ref(false)
const destructiveError = ref('')
const workspaceHost = computed(() => {
  const slug = data.value?.configuration.tenant.slug
  if (!slug) return ''
  return `${slug}.${String(runtimeConfig.public.baseDomain || 'fidibeki.com')}`
})
const hasVerifiedMemberDomains = computed(() => data.value?.memberDomains.some(domain => domain.status === 'verified') === true)
const currentWorkspaceMember = computed(() => data.value?.members.find(member => member.id === currentMember.value?.id))
const isCurrentOwner = computed(() => currentWorkspaceMember.value?.role === 'owner')
const transferCandidates = computed(() => (data.value?.members || []).filter(member =>
  member.id !== currentMember.value?.id && member.status === 'active' && member.verified && member.role !== 'owner'))

function updatePrivacyMember(member: Member) {
  const index = data.value?.members.findIndex(candidate => candidate.id === member.id) ?? -1
  if (index >= 0) data.value?.members.splice(index, 1, member)
}

const modalTitle = computed(() => {
  if (modal.value === 'member') return memberMode.value === 'invite'
    ? t('admin.settings.modal.inviteMember')
    : t('admin.settings.modal.editMember')
  if (modal.value === 'member-lifecycle') {
    if (memberAction.value === 'deactivate') return t('admin.settings.modal.deactivateMember')
    if (memberAction.value === 'reactivate') return t('admin.settings.modal.reactivateMember')
    return t('admin.settings.modal.removeMember')
  }
  if (modal.value === 'transfer') return t('admin.settings.modal.transferOwnership')
  if (modal.value === 'key') return t('admin.settings.modal.createKey')
  if (modal.value === 'integration-privacy') return t('admin.settings.modal.integrationPrivacy')
  return t('admin.settings.modal.addIntegration')
})
const modalEyebrow = computed(() => {
  if (modal.value === 'member') return t('admin.settings.modal.accessPolicy')
  if (modal.value === 'member-lifecycle') return t('admin.settings.modal.memberLifecycle')
  if (modal.value === 'transfer') return t('admin.settings.modal.workspaceOwner')
  if (modal.value === 'key') return t('admin.settings.modal.machineCredential')
  if (modal.value === 'integration-privacy') return t('admin.settings.modal.dataDisclosure')
  return t('admin.settings.modal.deliveryEndpoint')
})
const destructiveTitle = computed(() => {
  const action = destructiveAction.value
  if (!action) return ''
  return action.kind === 'key'
    ? t('admin.settings.keys.revokeTitle', { name: action.resource.name })
    : t('admin.settings.integrations.removeTitle', { name: action.resource.name })
})
const destructiveDescription = computed(() => destructiveAction.value?.kind === 'key'
  ? t('admin.settings.keys.revokeDescription')
  : t('admin.settings.integrations.removeDescription'))
const destructiveConfirmLabel = computed(() => destructiveAction.value?.kind === 'key'
  ? t('admin.settings.keys.revoke')
  : t('admin.settings.integrations.remove'))
const destructivePendingLabel = computed(() => destructiveAction.value?.kind === 'key'
  ? t('admin.settings.keys.revoking')
  : t('admin.settings.integrations.removing'))

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
  return cents == null ? t('admin.common.custom') : `${currency(cents / 100, 'EUR')}${t('admin.billing.perMonth')}`
})

function statusLabel(value: string) {
  return t(`admin.statuses.${value}`)
}

function planLabel(value: string) {
  return t(`admin.plans.${value}`)
}

function featureLabel(value: string) {
  return t(`admin.features.${value}`)
}

function eventLabel(value: IntegrationEvent) {
  const key = {
    'feedback.created': 'feedbackCreated',
    'issue.created': 'issueCreated',
    'issue.updated': 'issueUpdated',
    'issue.resolved': 'issueResolved',
  }[value]
  return t(`admin.settings.integrations.event.${key}`)
}

const privacyFieldOptions: IntegrationPrivacyField[] = [
  'description',
  'page',
  'rating',
  'classification',
  'workflow',
  'attachments',
]

function privacyFieldLabel(value: IntegrationPrivacyField) {
  return t(`admin.settings.integrations.privacy.field.${value}`)
}

function privacyFieldDescription(value: IntegrationPrivacyField) {
  return t(`admin.settings.integrations.privacy.fieldDescription.${value}`)
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

function matchingPreset(permissions: string[]): PermissionSelection {
  const allowed = new Set(currentMember.value?.permissions || [])
  const selected = new Set(permissions)
  for (const preset of ['admin', 'editor', 'viewer'] as PermissionPreset[]) {
    const expected = permissionPresets[preset].filter(permission => allowed.has(permission))
    if (expected.length === selected.size && expected.every(permission => selected.has(permission))) return preset
  }
  return 'custom'
}

function togglePermission(target: 'member' | 'key', permission: string) {
  const permissions = target === 'member' ? memberPermissions : keyPermissions
  permissions.value = permissions.value.includes(permission)
    ? permissions.value.filter((value) => value !== permission)
    : [...permissions.value, permission]
  if (target === 'member') memberPreset.value = matchingPreset(memberPermissions.value)
  else keyPreset.value = matchingPreset(keyPermissions.value)
}

function openMember(member?: Member) {
  if (member?.role === 'owner') return
  if (!canWrite(member ? 'member:update' : 'member:invite')) return
  formError.value = ''
  memberEmailError.value = ''
  selectedMember.value = member || null
  memberMode.value = member ? 'edit' : 'invite'
  memberEmail.value = member?.email || ''
  if (member) {
    memberPermissions.value = [...member.permissions]
    memberPreset.value = matchingPreset(memberPermissions.value)
  } else {
    applyPreset('member', 'viewer')
  }
  modal.value = 'member'
}

function openOwnershipTransfer() {
  if (!isCurrentOwner.value || !canWrite('member:update') || !canWrite('tenant:update')) return
  transferTargetID.value = transferCandidates.value[0]?.id || ''
  transferConfirmation.value = ''
  transferError.value = ''
  modal.value = 'transfer'
}

function openMemberLifecycle(member: Member, action: 'deactivate' | 'reactivate' | 'remove') {
  if (member.role === 'owner' || !canWrite(action === 'remove' ? 'member:remove' : 'member:update')) return
  selectedMember.value = member
  memberAction.value = action
  memberActionError.value = ''
  modal.value = 'member-lifecycle'
}

async function changeMemberLifecycle() {
  if (!selectedMember.value || !memberAction.value || pending.value) return
  pending.value = true
  memberActionError.value = ''
  try {
    if (memberAction.value === 'remove') {
      await $apiFetch(`/v1/members/${selectedMember.value.id}`, { method: 'DELETE' })
    } else {
      await $apiFetch(`/v1/members/${selectedMember.value.id}/status`, {
        method: 'PATCH',
        body: { status: memberAction.value === 'deactivate' ? 'deactivated' : 'active' },
      })
    }
    modal.value = null
    await refresh()
  } catch (memberFailure) {
    memberActionError.value = localizedError.message(memberFailure, 'admin.settings.errors.memberLifecycle')
  } finally {
    pending.value = false
  }
}

async function transferOwnership() {
  if (!isCurrentOwner.value || pending.value || !transferTargetID.value || !data.value) return
  pending.value = true
  transferError.value = ''
  try {
    await $apiFetch('/v1/ownership/transfer', {
      method: 'POST',
      body: {
        new_owner_member_id: transferTargetID.value,
        confirmation: transferConfirmation.value,
      },
    })
    modal.value = null
    await refresh()
  } catch (transferFailure) {
    transferError.value = localizedError.message(transferFailure, 'admin.settings.errors.ownership')
  } finally {
    pending.value = false
  }
}

async function saveMember() {
  const permission = memberMode.value === 'invite' ? 'member:invite' : 'member:update'
  if (!canWrite(permission) || pending.value) return
  pending.value = true
  formError.value = ''
  memberEmailError.value = ''
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
    const diagnostic = apiErrorMessage(saveError, '')
    if (memberMode.value === 'invite' && /email|domain|member already exists/i.test(diagnostic)) {
      memberEmailError.value = t('admin.settings.members.inviteEmailInvalid')
      await nextTick()
      memberEmailInput.value?.focus()
    } else {
      formError.value = localizedError.message(saveError, 'admin.settings.errors.memberSave')
    }
  } finally {
    pending.value = false
  }
}

watch(memberEmail, () => { memberEmailError.value = '' })

async function requestDomainVerification() {
  if (!canWrite('member:invite') || domainVerificationPending.value || !domainVerificationEmail.value) return
  domainVerificationPending.value = true
  domainVerificationError.value = ''
  domainVerificationNotice.value = ''
  try {
    await $apiFetch('/v1/member-domains', {
      method: 'POST', body: { email: domainVerificationEmail.value },
    })
    domainVerificationNotice.value = t('admin.settings.notices.verificationSent', { email: domainVerificationEmail.value })
    domainVerificationEmail.value = ''
    await refresh()
  } catch (requestError) {
    domainVerificationError.value = localizedError.message(requestError, 'admin.settings.errors.domainVerification')
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
    formError.value = localizedError.message(createError, 'admin.settings.errors.keyCreate')
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
  if (!canWrite('api_key:delete')) return
  destructiveError.value = ''
  destructiveAction.value = { kind: 'key', resource: key }
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
  integrationPrivacyFields.value = []
  selectedIntegration.value = null
  modal.value = 'integration'
}

function toggleEvent(event: IntegrationEvent) {
  integrationEvents.value = integrationEvents.value.includes(event)
    ? integrationEvents.value.filter((value) => value !== event)
    : [...integrationEvents.value, event]
}

function togglePrivacyField(field: IntegrationPrivacyField) {
  integrationPrivacyFields.value = integrationPrivacyFields.value.includes(field)
    ? integrationPrivacyFields.value.filter(value => value !== field)
    : [...integrationPrivacyFields.value, field]
}

function openIntegrationPrivacy(integration: Integration) {
  if (!canWrite('integration:update')) return
  formError.value = ''
  selectedIntegration.value = integration
  integrationPrivacyFields.value = [...(integration.privacy_fields || [])]
  modal.value = 'integration-privacy'
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
        privacy_fields: integrationPrivacyFields.value,
        is_active: true,
      },
    })
    revealedSecret.value = response.webhook_secret || ''
    await refresh()
    if (!revealedSecret.value) modal.value = null
  } catch (createError) {
    formError.value = localizedError.message(createError, 'admin.settings.errors.integrationCreate')
  } finally {
    pending.value = false
  }
}

async function saveIntegrationPrivacy() {
  const integration = selectedIntegration.value
  if (!integration || !canWrite('integration:update') || pending.value) return
  pending.value = true
  formError.value = ''
  try {
    await $apiFetch(`/v1/integrations/${integration.id}/privacy`, {
      method: 'PATCH',
      body: { privacy_fields: integrationPrivacyFields.value },
    })
    await refresh()
    modal.value = null
    selectedIntegration.value = null
  } catch (updateError) {
    formError.value = localizedError.message(updateError, 'admin.settings.errors.integrationPrivacy')
  } finally {
    pending.value = false
  }
}

async function deleteIntegration(integration: Integration) {
  if (!canWrite('integration:delete')) return
  destructiveError.value = ''
  destructiveAction.value = { kind: 'integration', resource: integration }
}

function closeDestructiveAction() {
  if (destructivePending.value) return
  destructiveAction.value = null
  destructiveError.value = ''
}

async function confirmDestructiveAction() {
  const action = destructiveAction.value
  if (!action || destructivePending.value) return
  const permission = action.kind === 'key' ? 'api_key:delete' : 'integration:delete'
  if (!canWrite(permission)) return
  destructivePending.value = true
  destructiveError.value = ''
  try {
    const path = action.kind === 'key'
      ? `/v1/api-keys/${action.resource.id}`
      : `/v1/integrations/${action.resource.id}`
    await $apiFetch(path, { method: 'DELETE' })
    await refresh()
    destructiveAction.value = null
  } catch (deleteError) {
    destructiveError.value = localizedError.message(
      deleteError,
      action.kind === 'key'
        ? 'admin.settings.errors.keyRevoke'
        : 'admin.settings.errors.integrationRemove',
    )
  } finally {
    destructivePending.value = false
  }
}

async function updateReporterAccess(enabled: boolean) {
  if (!canWrite('tenant:update') || !data.value || pending.value) return
  pending.value = true
  reporterAccessError.value = ''
  try {
    await $apiFetch('/v1/reporter-access', { method: 'PATCH', body: { enabled } })
    data.value.configuration.tenant.reporter_access_enabled = enabled
  } catch (accessError) {
    reporterAccessError.value = localizedError.message(accessError, 'admin.settings.errors.reporterAccess')
  } finally {
    pending.value = false
  }
}

async function updateWidgetAppearance() {
  if (!canWrite('tenant:update') || !data.value || pending.value) return
  pending.value = true
  appearanceFieldErrors.value = {}
  appearanceFormError.value = ''
  const appearance = cleanAppearanceOverrides(workspaceAppearance.value)
  try {
    await $apiFetch('/v1/widget-appearance', { method: 'PATCH', body: appearance })
    data.value.configuration.tenant.widget_appearance_defaults = appearance
    showSuccessToast(t('admin.settings.notices.appearanceSaved'))
  } catch (appearanceError) {
    appearanceFieldErrors.value = apiFieldErrors(
      appearanceError,
      localizedError.fields(appearanceError),
    )
    if (!Object.keys(appearanceFieldErrors.value).length) {
      appearanceFormError.value = localizedError.message(
        appearanceError,
        'admin.settings.errors.appearance',
      )
    }
  } finally {
    pending.value = false
  }
}

function clearAppearanceField(field: string) {
  if (!appearanceFieldErrors.value[field]) return
  const next = { ...appearanceFieldErrors.value }
  delete next[field]
  appearanceFieldErrors.value = next
}

function applyOrganizationProfile(profile: OrganizationProfile) {
  if (!data.value) return
  Object.assign(data.value.configuration.tenant, {
    name: profile.name,
    short_name: profile.short_name,
    website_url: profile.website_url,
    support_email: profile.support_email,
    default_locale: profile.default_locale,
    default_timezone: profile.default_timezone,
    privacy_policy_url: profile.privacy_policy_url,
    privacy_contact_email: profile.privacy_contact_email,
    privacy_rights_email: profile.privacy_rights_email,
    privacy_profile_version: profile.privacy_profile_version,
    terms_url: profile.terms_url,
    logo_version: profile.logo_version,
  })
  if (profile.accent_color) {
    workspaceAppearance.value = {
      ...workspaceAppearance.value,
      accent_color: profile.accent_color,
    }
  } else {
    const appearance = { ...workspaceAppearance.value }
    delete appearance.accent_color
    workspaceAppearance.value = appearance
  }
}

function isExpired(value?: string | null) {
  return value ? new Date(value).getTime() <= Date.now() : false
}

useHead(() => ({ title: t('admin.settings.pageTitle') }))
</script>

<template>
  <div class="app-page settings-page">
    <header class="app-page-head">
      <div><p class="app-eyebrow">{{ t('admin.settings.eyebrow') }}</p><h1>{{ t('admin.settings.title') }}</h1><span>{{ t('admin.settings.description') }}</span></div>
    </header>

    <nav class="settings-tabs" :aria-label="t('admin.settings.sections')">
      <NuxtLink v-for="tab in tabs" :key="tab.id" :to="{ path: '/settings', query: tab.id === 'workspace' ? {} : { tab: tab.id } }" :data-active="activeTab === tab.id">{{ tab.label }}</NuxtLink>
    </nav>

    <section v-if="status === 'pending'" class="app-surface app-state"><div><span class="app-spinner" /><h2>{{ t('admin.settings.loading') }}</h2></div></section>
    <section v-else-if="error" class="app-surface app-state"><div><AlertTriangle :size="24" /><h2>{{ t('admin.settings.unavailable') }}</h2><p>{{ localizedError.message(error, 'admin.settings.errors.load') }}</p><button class="app-button" @click="refresh()">{{ t('admin.common.tryAgain') }}</button></div></section>

    <template v-else-if="data">
      <section v-if="activeTab === 'workspace'" class="settings-workspace">
        <OrganizationProfile @updated="applyOrganizationProfile" />
        <div class="app-surface settings-section">
          <header><div><p class="app-eyebrow">{{ t('admin.settings.workspace.eyebrow') }}</p><h2>{{ data.configuration.tenant.name }}</h2></div><span class="settings-state"><i />{{ statusLabel(data.configuration.tenant.status) }}</span></header>
          <dl class="settings-ledger">
            <div><dt>{{ t('admin.settings.workspace.subdomain') }}</dt><dd>{{ workspaceHost }}</dd></div>
            <div><dt>{{ t('admin.settings.workspace.plan') }}</dt><dd>{{ planLabel(data.configuration.plan.plan) }}</dd></div>
            <div><dt>{{ t('admin.settings.workspace.rate') }}</dt><dd>{{ planPrice }}</dd></div>
            <div><dt>{{ t('admin.settings.workspace.retention') }}</dt><dd>{{ data.configuration.plan.limits.storage_days < 0 ? t('admin.common.custom') : plural('admin.common.days', data.configuration.plan.limits.storage_days, { count: data.configuration.plan.limits.storage_days }) }}</dd></div>
          </dl>
        </div>
        <div class="app-surface settings-section">
          <header><div><p class="app-eyebrow">{{ t('admin.settings.workspace.entitlements') }}</p><h2>{{ t('admin.settings.workspace.capacity') }}</h2></div><NuxtLink class="app-button" to="/billing">{{ t('admin.settings.workspace.manageBilling') }}</NuxtLink></header>
          <div class="settings-entitlements">
            <div><b>{{ data.configuration.plan.limits.projects < 0 ? t('admin.common.unlimited') : integer(data.configuration.plan.limits.projects) }}</b><span>{{ t('admin.settings.workspace.projects') }}</span></div>
            <div><b>{{ data.configuration.plan.limits.feedback_per_month < 0 ? t('admin.common.unlimited') : integer(data.configuration.plan.limits.feedback_per_month) }}</b><span>{{ t('admin.settings.workspace.feedbackPerMonth') }}</span></div>
            <div><b>{{ data.configuration.plan.limits.users < 0 ? t('admin.common.unlimited') : integer(data.configuration.plan.limits.users) }}</b><span>{{ t('admin.settings.workspace.members') }}</span></div>
          </div>
          <ul class="settings-features">
            <li v-for="(enabled, feature) in data.configuration.plan.features" :key="feature" :data-enabled="enabled"><component :is="enabled ? Check : X" :size="13" />{{ featureLabel(String(feature)) }}</li>
          </ul>
        </div>
        <div class="app-surface settings-section settings-reporter-policy">
          <header><div><p class="app-eyebrow">{{ t('admin.settings.workspace.reporterEyebrow') }}</p><h2>{{ t('admin.settings.workspace.progressLinks') }}</h2></div><span class="settings-domain-mode" :data-enforced="data.configuration.tenant.reporter_access_enabled">{{ data.configuration.tenant.reporter_access_enabled ? t('admin.common.enabled') : t('admin.common.disabled') }}</span></header>
          <label><span><b>{{ t('admin.settings.workspace.reporterProgress') }}</b><small>{{ t('admin.settings.workspace.reporterDescription') }}</small></span><input type="checkbox" :checked="data.configuration.tenant.reporter_access_enabled" :disabled="pending || !canWrite('tenant:update')" :aria-label="t('admin.settings.workspace.reporterProgress')" @change="updateReporterAccess(($event.target as HTMLInputElement).checked)" /></label>
          <p v-if="reporterAccessError" class="settings-form-error" role="alert">{{ reporterAccessError }}</p>
        </div>
        <form class="app-surface settings-section settings-widget-appearance" @submit.prevent="updateWidgetAppearance">
          <header><div><p class="app-eyebrow">{{ t('admin.settings.workspace.widgetEyebrow') }}</p><h2>{{ t('admin.settings.workspace.controlledAppearance') }}</h2></div><button class="app-button app-button--primary" type="submit" :disabled="pending || !canWrite('tenant:update')">{{ t('admin.settings.workspace.saveDefaults') }}</button></header>
          <WidgetAppearanceEditor
            v-model="workspaceAppearance"
            :parent="defaultWidgetAppearance"
            :errors="appearanceFieldErrors"
            @field-edited="clearAppearanceField"
          />
          <p v-if="appearanceFormError" class="settings-form-error" role="alert">{{ appearanceFormError }}</p>
        </form>
      </section>

      <div v-else-if="activeTab === 'members'" class="settings-members-page">
        <section class="app-surface settings-section settings-domain-policy">
          <header><div><p class="app-eyebrow">{{ t('admin.settings.members.policyEyebrow') }}</p><h2>{{ t('admin.settings.members.allowedDomains') }}</h2></div><span class="settings-domain-mode" :data-enforced="hasVerifiedMemberDomains">{{ hasVerifiedMemberDomains ? t('admin.settings.members.enforced') : t('admin.settings.members.openPolicy') }}</span></header>
          <div v-if="!can('member:read')" class="settings-empty"><ShieldCheck :size="23" /><h3>{{ t('admin.settings.members.accessRequired') }}</h3></div>
          <template v-else>
            <ul v-if="data.memberDomains.length" class="settings-domain-list">
              <li v-for="domain in data.memberDomains" :key="domain.id">
                <MailCheck :size="16" />
                <span><b>{{ domain.domain }}</b><small>{{ domain.status === 'verified' ? t('admin.settings.members.verifiedAt', { date: date(domain.verified_at!) }) : t('admin.settings.members.verificationSentTo', { email: domain.verification_email }) }}</small></span>
                <span class="status-pill" :class="domain.status === 'verified' ? 'status-completed' : 'status-pending'">{{ statusLabel(domain.status) }}</span>
              </li>
            </ul>
            <form v-if="canWrite('member:invite')" class="settings-domain-form" @submit.prevent="requestDomainVerification">
              <label>{{ t('admin.settings.members.verificationEmail') }}<input v-model.trim="domainVerificationEmail" class="app-input" type="email" required placeholder="security@company.com" /></label>
              <button class="app-button" type="submit" :disabled="domainVerificationPending || !domainVerificationEmail"><MailCheck :size="15" /> {{ domainVerificationPending ? t('admin.settings.members.sending') : t('admin.settings.members.verifyDomain') }}</button>
            </form>
            <p v-if="domainVerificationNotice" class="settings-domain-notice" role="status">{{ domainVerificationNotice }}</p>
            <p v-if="domainVerificationError" class="settings-form-error" role="alert">{{ domainVerificationError }}</p>
          </template>
        </section>

        <section class="app-surface settings-section">
          <header><div><p class="app-eyebrow">{{ t('admin.settings.members.accessEyebrow') }}</p><h2>{{ t('admin.settings.members.title') }}</h2></div><button v-if="canWrite('member:invite')" class="app-button app-button--primary" @click="openMember()"><UserPlus :size="15" /> {{ t('admin.settings.members.invite') }}</button></header>
          <div v-if="!can('member:read')" class="settings-empty"><ShieldCheck :size="23" /><h3>{{ t('admin.settings.members.accessRequired') }}</h3></div>
          <div v-else-if="!data.members.length" class="settings-empty"><Users :size="23" /><h3>{{ t('admin.settings.members.none') }}</h3></div>
          <div v-else class="settings-table settings-members">
            <div class="settings-row settings-row--head"><span>{{ t('admin.settings.members.columns.member') }}</span><span>{{ t('admin.settings.members.columns.permissions') }}</span><span>{{ t('admin.settings.members.columns.access') }}</span><span /></div>
            <div v-for="member in data.members" :key="member.id" class="settings-row" :data-muted="member.status !== 'active'">
              <span class="settings-identity"><i><MemberAvatar :member-id="member.id" :name="member.name" :email="member.email" :avatar-version="member.avatar_version" /></i><span><b>{{ member.name || member.email.split('@')[0] }}</b><small>{{ member.email }}</small></span></span>
              <span>{{ t('admin.settings.members.granted', { count: integer(member.permissions.length) }) }}</span>
              <span class="settings-member-access"><b v-if="member.role === 'owner'" class="settings-owner-label">{{ t('admin.common.owner') }}</b><span v-else>{{ statusLabel(member.status) }}</span><small v-if="member.id === currentMember?.id">{{ t('admin.common.currentSession') }}</small></span>
              <span class="settings-member-actions">
                <button v-if="member.role !== 'owner' && member.status === 'active' && canWrite('member:update')" class="app-icon-button" :title="t('admin.settings.members.editPermissions')" @click="openMember(member)"><ShieldCheck :size="16" /></button>
                <button v-if="member.role !== 'owner' && member.status === 'active' && canWrite('member:update')" class="app-icon-button" :title="t('admin.settings.members.deactivate')" @click="openMemberLifecycle(member, 'deactivate')"><UserX :size="16" /></button>
                <button v-if="member.role !== 'owner' && member.status === 'deactivated' && canWrite('member:update')" class="app-icon-button" :title="t('admin.settings.members.reactivate')" @click="openMemberLifecycle(member, 'reactivate')"><UserCheck :size="16" /></button>
                <button v-if="member.role !== 'owner' && member.status !== 'removed' && canWrite('member:remove')" class="app-icon-button settings-danger-icon" :title="t('admin.settings.members.remove')" @click="openMemberLifecycle(member, 'remove')"><Trash2 :size="16" /></button>
                <button v-if="member.role === 'owner' && member.id === currentMember?.id && canWrite('member:update') && canWrite('tenant:update')" class="app-icon-button" :title="t('admin.settings.members.transfer')" @click="openOwnershipTransfer"><ArrowRightLeft :size="16" /></button>
              </span>
            </div>
          </div>
        </section>
      </div>

      <section v-else-if="activeTab === 'keys'" class="app-surface settings-section">
        <header><div><p class="app-eyebrow">{{ t('admin.settings.keys.eyebrow') }}</p><h2>{{ t('admin.settings.keys.title') }}</h2></div><button v-if="canWrite('api_key:create')" class="app-button app-button--primary" @click="openKey"><Plus :size="15" /> {{ t('admin.settings.keys.create') }}</button></header>
        <div v-if="!can('api_key:read')" class="settings-empty"><ShieldCheck :size="23" /><h3>{{ t('admin.settings.keys.accessRequired') }}</h3></div>
        <div v-else-if="!data.keys.length" class="settings-empty"><KeyRound :size="23" /><h3>{{ t('admin.settings.keys.none') }}</h3></div>
        <div v-else class="settings-table settings-keys">
          <div class="settings-row settings-row--head"><span>{{ t('admin.settings.keys.columns.key') }}</span><span>{{ t('admin.settings.keys.columns.scope') }}</span><span>{{ t('admin.settings.keys.columns.lastUsed') }}</span><span>{{ t('admin.settings.keys.columns.expiry') }}</span><span /></div>
          <div v-for="key in data.keys" :key="key.id" class="settings-row" :data-muted="isExpired(key.expires_at)">
            <span><b>{{ key.name }}</b><code>{{ key.token_prefix }}</code></span>
            <span>{{ t('admin.settings.keys.scope', { permissions: integer(key.permissions.length), limit: integer(key.rate_limit_per_min) }) }}</span>
            <span>{{ key.last_used_at ? date(key.last_used_at) : t('admin.common.never') }}</span>
            <span>{{ key.expires_at ? date(key.expires_at) : t('admin.common.never') }}</span>
            <button v-if="canWrite('api_key:delete')" class="app-icon-button settings-danger-icon" :title="t('admin.settings.keys.revoke')" @click="deleteKey(key)"><Trash2 :size="16" /></button>
          </div>
        </div>
      </section>

      <section v-else-if="activeTab === 'integrations'" class="app-surface settings-section">
        <header><div><p class="app-eyebrow">{{ t('admin.settings.integrations.eyebrow') }}</p><h2>{{ t('admin.settings.integrations.title') }}</h2></div><button v-if="canWrite('integration:create')" class="app-button app-button--primary" @click="openIntegration"><Plus :size="15" /> {{ t('admin.settings.integrations.add') }}</button></header>
        <div v-if="!can('integration:read')" class="settings-empty"><ShieldCheck :size="23" /><h3>{{ t('admin.settings.integrations.accessRequired') }}</h3></div>
        <div v-else-if="!data.integrations.length" class="settings-empty"><Link2 :size="23" /><h3>{{ t('admin.settings.integrations.none') }}</h3></div>
        <div v-else class="settings-table settings-integrations">
          <div class="settings-row settings-row--head"><span>{{ t('admin.settings.integrations.columns.integration') }}</span><span>{{ t('admin.settings.integrations.columns.endpoint') }}</span><span>{{ t('admin.settings.integrations.columns.events') }}</span><span>{{ t('admin.settings.integrations.columns.status') }}</span><span /></div>
          <div v-for="integration in data.integrations" :key="integration.id" class="settings-row">
            <span><b>{{ integration.name }}</b><small>{{ integration.kind }}</small></span>
            <code>{{ integration.webhook_url }}</code>
            <span>{{ t('admin.settings.integrations.subscribed', { count: integer(integration.events.length) }) }}</span>
            <span class="settings-state"><i /><span>{{ integration.is_active ? t('admin.common.active') : t('admin.common.paused') }}<small>{{ integration.privacy_fields?.length ? t('admin.settings.integrations.privacy.optionalCount', { count: integer(integration.privacy_fields.length) }) : t('admin.settings.integrations.privacy.minimum') }}</small></span></span>
            <span class="settings-row-actions">
              <button v-if="canWrite('integration:update')" class="app-icon-button" :title="t('admin.settings.integrations.privacy.configure')" @click="openIntegrationPrivacy(integration)"><SlidersHorizontal :size="16" /></button>
              <button v-if="canWrite('integration:delete')" class="app-icon-button settings-danger-icon" :title="t('admin.settings.integrations.remove')" @click="deleteIntegration(integration)"><Trash2 :size="16" /></button>
            </span>
          </div>
        </div>
      </section>

      <SubmissionHealth v-else-if="activeTab === 'health'" :projects="data.projects" />
      <AccessAuditLog v-else-if="activeTab === 'audit'" />
      <DataPrivacy v-else :members="data.members" @member-changed="updatePrivacyMember" />
    </template>

    <AppDialog :open="Boolean(modal)" aria-labelledby="settings-modal-title" @close="modal = null">
      <section v-if="modal" class="settings-modal">
        <header><div><p class="app-eyebrow">{{ modalEyebrow }}</p><h2 id="settings-modal-title">{{ modalTitle }}</h2></div><button class="app-icon-button" :title="t('admin.common.close')" @click="modal = null"><X :size="18" /></button></header>

        <form v-if="modal === 'member'" @submit.prevent="saveMember">
          <label class="member-email-field">{{ t('admin.settings.members.email') }}<input ref="memberEmailInput" v-model.trim="memberEmail" class="app-input" type="email" required :disabled="memberMode === 'edit'" autofocus :aria-invalid="memberEmailError ? 'true' : undefined" :aria-describedby="memberEmailError ? 'member-email-error' : undefined" /><span v-if="memberEmailError" id="member-email-error" class="settings-field-error" role="alert">{{ memberEmailError }}</span></label>
          <PermissionEditor :label="t('admin.settings.members.permissions')" :groups="availablePermissionGroups" :permissions="memberPermissions" :active-preset="memberPreset" @apply-preset="applyPreset('member', $event)" @toggle-permission="togglePermission('member', $event)" />
          <p v-if="formError" class="settings-form-error" role="alert">{{ formError }}</p>
          <footer><button class="app-button" type="button" @click="modal = null">{{ t('admin.common.cancel') }}</button><button class="app-button app-button--primary" type="submit" :disabled="pending || !memberEmail || !memberPermissions.length">{{ pending ? t('admin.common.saving') : memberMode === 'invite' ? t('admin.settings.members.sendInvitation') : t('admin.settings.members.savePermissions') }}</button></footer>
        </form>

        <form v-else-if="modal === 'member-lifecycle'" class="settings-lifecycle-form" @submit.prevent="changeMemberLifecycle">
          <div v-if="selectedMember" class="settings-action-subject"><span>{{ (selectedMember.name || selectedMember.email).slice(0, 1).toUpperCase() }}</span><div><b>{{ selectedMember.name || selectedMember.email }}</b><small>{{ selectedMember.email }}</small></div></div>
          <p v-if="memberAction === 'deactivate'">{{ t('admin.settings.members.deactivateDescription') }}</p>
          <p v-else-if="memberAction === 'reactivate'">{{ t('admin.settings.members.reactivateDescription') }}</p>
          <p v-else>{{ t('admin.settings.members.removeDescription') }}</p>
          <p v-if="memberActionError" class="settings-form-error" role="alert">{{ memberActionError }}</p>
          <footer><button class="app-button" type="button" @click="modal = null">{{ t('admin.common.cancel') }}</button><button class="app-button" :class="memberAction === 'remove' ? 'settings-danger-button' : 'app-button--primary'" type="submit" :disabled="pending">{{ pending ? t('admin.common.saving') : modalTitle }}</button></footer>
        </form>

        <form v-else-if="modal === 'transfer'" class="settings-transfer-form" @submit.prevent="transferOwnership">
          <p>{{ t('admin.settings.members.transferDescription') }}</p>
          <label>{{ t('admin.settings.members.newOwner') }}<select v-model="transferTargetID" class="app-select" required autofocus><option v-for="member in transferCandidates" :key="member.id" :value="member.id">{{ member.name || member.email }} · {{ member.email }}</option></select></label>
          <label><span>{{ t('admin.settings.members.typeToConfirm', { value: data?.configuration.tenant.slug }) }}</span><input v-model.trim="transferConfirmation" class="app-input" required autocomplete="off" /></label>
          <p v-if="!transferCandidates.length" class="settings-form-error" role="alert">{{ t('admin.settings.members.transferUnavailable') }}</p>
          <p v-else-if="transferError" class="settings-form-error" role="alert">{{ transferError }}</p>
          <footer><button class="app-button" type="button" @click="modal = null">{{ t('admin.common.cancel') }}</button><button class="app-button app-button--primary" type="submit" :disabled="pending || !transferTargetID || transferConfirmation !== data?.configuration.tenant.slug">{{ pending ? t('admin.settings.members.transferring') : t('admin.settings.members.transfer') }}</button></footer>
        </form>

        <form v-else-if="modal === 'key'" @submit.prevent="createKey">
          <div v-if="revealedToken" class="settings-secret"><ShieldCheck :size="20" /><h3>{{ t('admin.settings.keys.created') }}</h3><code>{{ revealedToken }}</code><button class="app-button" type="button" @click="copyToken"><component :is="tokenCopied ? Check : Copy" :size="15" />{{ tokenCopied ? t('admin.common.copied') : t('admin.settings.keys.copy') }}</button></div>
          <template v-else>
            <div class="settings-form-grid"><label>{{ t('admin.settings.keys.name') }}<input v-model.trim="keyName" class="app-input" required maxlength="120" :placeholder="t('admin.settings.keys.placeholder')" autofocus /></label><label>{{ t('admin.settings.keys.expires') }}<select v-model="keyExpiry" class="app-select"><option value="never">{{ t('admin.common.never') }}</option><option value="30">{{ t('admin.settings.keys.expiry30') }}</option><option value="90">{{ t('admin.settings.keys.expiry90') }}</option><option value="365">{{ t('admin.settings.keys.expiryYear') }}</option></select></label><label>{{ t('admin.settings.keys.rateLimit') }}<input v-model.number="keyRateLimit" class="app-input" type="number" min="1" max="100000" required /></label></div>
            <PermissionEditor :label="t('admin.settings.keys.permissions')" :groups="availablePermissionGroups" :permissions="keyPermissions" :active-preset="keyPreset" @apply-preset="applyPreset('key', $event)" @toggle-permission="togglePermission('key', $event)" />
            <p v-if="formError" class="settings-form-error" role="alert">{{ formError }}</p>
          </template>
          <footer><button class="app-button" type="button" @click="modal = null">{{ revealedToken ? t('admin.common.done') : t('admin.common.cancel') }}</button><button v-if="!revealedToken" class="app-button app-button--primary" type="submit" :disabled="pending || !keyName || !keyPermissions.length">{{ pending ? t('admin.common.creating') : t('admin.settings.keys.create') }}</button></footer>
        </form>

        <form v-else-if="modal === 'integration'" @submit.prevent="createIntegration">
          <div v-if="revealedSecret" class="settings-secret"><ShieldCheck :size="20" /><h3>{{ t('admin.settings.integrations.signingSecret') }}</h3><code>{{ revealedSecret }}</code><button class="app-button" type="button" @click="copySecret"><Copy :size="15" /> {{ t('admin.settings.integrations.copySecret') }}</button></div>
          <template v-else>
            <div class="settings-form-grid"><label>{{ t('admin.settings.integrations.type') }}<select v-model="integrationKind" class="app-select" autofocus><option value="webhook">Webhook</option><option value="slack">Slack</option><option value="teams">Teams</option></select></label><label>{{ t('admin.settings.integrations.name') }}<input v-model.trim="integrationName" class="app-input" required maxlength="100" :placeholder="t('admin.settings.integrations.namePlaceholder')" /></label><label class="wide">{{ t('admin.settings.integrations.webhookURL') }}<input v-model.trim="integrationURL" class="app-input" type="url" required placeholder="https://hooks.example.com/..." /></label><label v-if="integrationKind === 'slack'" class="wide">{{ t('admin.settings.integrations.channel') }}<input v-model.trim="integrationChannel" class="app-input" required placeholder="#feedback" /></label></div>
            <fieldset class="settings-events"><legend>{{ t('admin.settings.integrations.events') }}</legend><label v-for="event in (['feedback.created', 'issue.created', 'issue.updated', 'issue.resolved'] as IntegrationEvent[])" :key="event"><input type="checkbox" :checked="integrationEvents.includes(event)" @change="toggleEvent(event)" />{{ eventLabel(event) }}</label></fieldset>
            <section class="settings-disclosure-warning"><AlertTriangle :size="17" /><div><b>{{ t('admin.settings.integrations.privacy.destinationTitle') }}</b><p>{{ t('admin.settings.integrations.privacy.destinationWarning') }}</p></div></section>
            <fieldset class="settings-privacy-fields">
              <legend>{{ t('admin.settings.integrations.privacy.optionalFields') }}</legend>
              <p>{{ t('admin.settings.integrations.privacy.minimumIncluded') }}</p>
              <label v-for="field in privacyFieldOptions" :key="field">
                <input type="checkbox" :checked="integrationPrivacyFields.includes(field)" @change="togglePrivacyField(field)" />
                <span><b>{{ privacyFieldLabel(field) }}</b><small>{{ privacyFieldDescription(field) }}</small></span>
              </label>
            </fieldset>
            <section class="settings-disclosure-preview" aria-live="polite">
              <span>{{ t('admin.settings.integrations.privacy.syntheticPreview') }}</span>
              <b>fb_EXAMPLE · {{ t('admin.settings.integrations.privacy.previewTitle') }}</b>
              <small>{{ t('admin.settings.integrations.privacy.previewMinimum') }}</small>
              <small v-if="integrationPrivacyFields.length">{{ integrationPrivacyFields.map(privacyFieldLabel).join(' · ') }}</small>
            </section>
            <p v-if="formError" class="settings-form-error" role="alert">{{ formError }}</p>
          </template>
          <footer><button class="app-button" type="button" @click="modal = null">{{ revealedSecret ? t('admin.common.done') : t('admin.common.cancel') }}</button><button v-if="!revealedSecret" class="app-button app-button--primary" type="submit" :disabled="pending || !integrationName || !integrationURL || !integrationEvents.length">{{ pending ? t('admin.common.connecting') : t('admin.settings.integrations.add') }}</button></footer>
        </form>

        <form v-else class="settings-integration-privacy-form" @submit.prevent="saveIntegrationPrivacy">
          <section class="settings-disclosure-warning"><AlertTriangle :size="17" /><div><b>{{ t('admin.settings.integrations.privacy.destinationTitle') }}</b><p>{{ t('admin.settings.integrations.privacy.destinationWarning') }}</p></div></section>
          <fieldset class="settings-privacy-fields">
            <legend>{{ t('admin.settings.integrations.privacy.optionalFields') }}</legend>
            <p>{{ t('admin.settings.integrations.privacy.minimumIncluded') }}</p>
            <label v-for="field in privacyFieldOptions" :key="field">
              <input type="checkbox" :checked="integrationPrivacyFields.includes(field)" @change="togglePrivacyField(field)" />
              <span><b>{{ privacyFieldLabel(field) }}</b><small>{{ privacyFieldDescription(field) }}</small></span>
            </label>
          </fieldset>
          <section class="settings-disclosure-preview" aria-live="polite">
            <span>{{ t('admin.settings.integrations.privacy.syntheticPreview') }}</span>
            <b>fb_EXAMPLE · {{ t('admin.settings.integrations.privacy.previewTitle') }}</b>
            <small>{{ t('admin.settings.integrations.privacy.previewMinimum') }}</small>
            <small v-if="integrationPrivacyFields.length">{{ integrationPrivacyFields.map(privacyFieldLabel).join(' · ') }}</small>
          </section>
          <p v-if="formError" class="settings-form-error" role="alert">{{ formError }}</p>
          <footer><button class="app-button" type="button" @click="modal = null">{{ t('admin.common.cancel') }}</button><button class="app-button app-button--primary" type="submit" :disabled="pending">{{ pending ? t('admin.common.saving') : t('admin.settings.integrations.privacy.save') }}</button></footer>
        </form>
      </section>
    </AppDialog>
    <AppConfirmationDialog
      :open="Boolean(destructiveAction)"
      :title="destructiveTitle"
      :description="destructiveDescription"
      :confirm-label="destructiveConfirmLabel"
      :pending-label="destructivePendingLabel"
      :cancel-label="t('admin.common.cancel')"
      :pending="destructivePending"
      :error="destructiveError"
      @cancel="closeDestructiveAction"
      @confirm="confirmDestructiveAction"
    />
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
.settings-widget-appearance > .settings-form-error { margin: 0 16px 16px; }
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
.settings-members .settings-row { grid-template-columns: minmax(190px, 1.5fr) minmax(110px, .7fr) minmax(110px, .7fr) minmax(38px, auto); }
.settings-keys .settings-row { grid-template-columns: minmax(190px, 1.3fr) minmax(160px, 1fr) 100px 100px 38px; }
.settings-integrations .settings-row { grid-template-columns: minmax(170px, .8fr) minmax(220px, 1.4fr) 100px 110px 76px; }
.settings-row > span { min-width: 0; }
.settings-row > span > b { display: block; color: var(--ink-soft); font-size: 11px; }
.settings-row small, .settings-row code { display: block; margin-top: 3px; overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.settings-row[data-muted='true'] { opacity: .55; }
.settings-identity { display: flex; align-items: center; gap: 10px; }
.settings-identity > i { display: block; width: 32px; height: 32px; flex: 0 0 32px; overflow: hidden; border-radius: 5px; font-size: 9px; font-style: normal; }
.settings-identity > span { min-width: 0; }
.settings-member-access { display: flex; align-items: flex-start; flex-direction: column; gap: 3px; }
.settings-member-access small { margin: 0; }
.settings-member-access > b.settings-owner-label { display: inline-flex; height: 21px; align-items: center; justify-content: center; padding: 0 7px; background: var(--teal-soft); color: var(--teal); border-radius: 4px; font-size: 9px; line-height: 1; text-transform: uppercase; }
.settings-member-actions { display: flex; justify-content: flex-end; gap: 4px; }
.settings-row-actions { display: flex; justify-content: flex-end; gap: 4px; }
.settings-integrations .settings-state { align-items: flex-start; text-transform: none; }
.settings-integrations .settings-state > i { margin-top: 3px; }
.settings-integrations .settings-state > span { min-width: 0; }
.settings-danger-icon { color: var(--red); }
.settings-empty { display: grid; min-height: 220px; place-items: center; align-content: center; gap: 8px; color: var(--muted); }
.settings-empty h3 { margin: 0; color: var(--ink-soft); font-size: 12px; }
.settings-modal { width: min(720px, 100%); max-height: min(88vh, 860px); overflow: auto; background: #fff; border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.settings-modal > header { position: sticky; top: 0; z-index: 2; display: flex; min-height: 68px; align-items: center; justify-content: space-between; padding: 10px 15px; background: #fff; border-bottom: 1px solid var(--line); }
.settings-modal h2 { margin: 3px 0 0; font-size: 15px; }
.settings-modal form { padding: 16px; }
.settings-modal form > label, .settings-form-grid label { display: flex; flex-direction: column; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.settings-transfer-form { display: grid; gap: 14px; }
.settings-transfer-form > p:first-child { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.6; }
.settings-transfer-form label b { color: var(--teal); }
.settings-lifecycle-form { display: grid; gap: 14px; }
.settings-lifecycle-form > p { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.6; }
.settings-action-subject { display: flex; align-items: center; gap: 10px; padding: 11px; background: var(--surface-soft); border: 1px solid var(--line); border-radius: 5px; }
.settings-action-subject > span { display: grid; width: 34px; height: 34px; flex: 0 0 34px; place-items: center; background: var(--teal-soft); color: var(--teal); border-radius: 5px; font-size: 11px; font-weight: 800; }
.settings-action-subject > div { min-width: 0; }
.settings-action-subject b, .settings-action-subject small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.settings-action-subject b { color: var(--ink); font-size: 11px; }
.settings-action-subject small { margin-top: 3px; color: var(--muted); font-size: 9px; }
.settings-danger-button { background: var(--red); color: #fff; border-color: var(--red); }
.settings-form-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; }
.settings-form-grid .wide { grid-column: 1 / -1; }
.settings-events legend { padding: 0; color: var(--ink); font-size: 10px; font-weight: 800; }
.settings-events label { display: flex; min-height: 26px; align-items: center; gap: 7px; color: var(--muted); font-size: 9px; text-transform: capitalize; }
.settings-events input { accent-color: var(--teal); }
.settings-events { display: grid; grid-template-columns: repeat(2, 1fr); gap: 2px 10px; margin: 15px 0 0; padding: 12px; border: 1px solid var(--line); border-radius: 5px; }
.settings-events legend { padding: 0 4px; }
.settings-disclosure-warning { display: flex; align-items: flex-start; gap: 9px; margin: 15px 0 0; padding: 11px 12px; background: #fff9eb; border: 1px solid #ead9ad; border-radius: 5px; color: #795d1d; }
.settings-disclosure-warning svg { flex: 0 0 auto; margin-top: 1px; }
.settings-disclosure-warning b { display: block; color: #604912; font-size: 10px; }
.settings-disclosure-warning p { margin: 3px 0 0; font-size: 9px; line-height: 1.55; }
.settings-privacy-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 12px; margin: 15px 0 0; padding: 12px; border: 1px solid var(--line); border-radius: 5px; }
.settings-privacy-fields legend { padding: 0 4px; color: var(--ink); font-size: 10px; font-weight: 800; }
.settings-privacy-fields > p { grid-column: 1 / -1; margin: 0 0 8px; color: var(--muted); font-size: 9px; line-height: 1.5; }
.settings-privacy-fields label { display: flex; min-height: 46px; align-items: flex-start; gap: 8px; padding: 7px 0; color: var(--ink-soft); }
.settings-privacy-fields input { width: 15px; height: 15px; flex: 0 0 auto; margin: 1px 0 0; accent-color: var(--teal); }
.settings-privacy-fields label span { min-width: 0; }
.settings-privacy-fields label b, .settings-privacy-fields label small { display: block; }
.settings-privacy-fields label b { font-size: 10px; }
.settings-privacy-fields label small { margin-top: 3px; color: var(--muted); font-size: 9px; font-weight: 450; line-height: 1.45; }
.settings-disclosure-preview { display: grid; gap: 4px; margin-top: 15px; padding: 11px 12px; background: var(--surface-soft); border-left: 3px solid var(--teal); }
.settings-disclosure-preview > span { color: var(--teal); font-size: 8px; font-weight: 800; text-transform: uppercase; }
.settings-disclosure-preview > b { color: var(--ink); font-size: 11px; }
.settings-disclosure-preview > small { color: var(--muted); font-size: 9px; line-height: 1.45; }
.settings-modal footer { display: flex; justify-content: flex-end; gap: 8px; margin: 16px -16px -16px; padding: 12px 16px; background: #f8faf9; border-top: 1px solid var(--line); }
.settings-form-error { padding: 9px 10px; background: var(--red-soft); color: var(--red); border-radius: 4px; font-size: 10px; }
.settings-field-error { color: var(--red); font-size: 9px; font-weight: 650; line-height: 1.4; }
.member-email-field input[aria-invalid='true'] { border-color: var(--red); box-shadow: 0 0 0 2px rgb(184 60 72 / 12%); }
.settings-secret { display: grid; place-items: center; gap: 10px; padding: 25px; text-align: center; }
.settings-secret h3 { margin: 0; font-size: 14px; }
.settings-secret code { width: 100%; padding: 10px; overflow-wrap: anywhere; background: #f3f6f5; border: 1px solid var(--line); border-radius: 4px; color: var(--ink); font-size: 11px; }
.capitalize { text-transform: capitalize; }
@media (max-width: 900px) { .settings-workspace { grid-template-columns: 1fr; } .settings-row--head { display: none; } .settings-members .settings-row { grid-template-columns: minmax(0, 1fr) auto minmax(38px, auto); } .settings-keys .settings-row, .settings-integrations .settings-row { grid-template-columns: minmax(0, 1fr) auto; } .settings-row > span:not(:first-child), .settings-row > code { display: none; } .settings-members .settings-row > .settings-member-access, .settings-members .settings-row > .settings-member-actions, .settings-integrations .settings-row > .settings-row-actions { display: flex; } }
@media (max-width: 650px) { .settings-tabs { flex-wrap: wrap; gap: 8px 15px; overflow-x: visible; } .settings-entitlements { grid-template-columns: 1fr; } .settings-entitlements > div { min-height: 70px; border-right: 0; border-bottom: 1px solid var(--line); } .settings-entitlements > div:last-child { border-bottom: 0; } .settings-features, .settings-form-grid, .settings-events, .settings-privacy-fields, .settings-domain-form { grid-template-columns: 1fr; } .settings-domain-form .app-button { width: 100%; } }
</style>
