<script setup lang="ts">
import {
  AlertTriangle,
  Archive,
  ArrowLeft,
  Check,
  CircleCheck,
  CircleX,
  Clipboard,
  Code2,
  Globe2,
  Plus,
  Radio,
  RefreshCw,
  Save,
  Settings2,
  Trash2,
} from '@lucide/vue'
import WidgetTargetingEditor from '~/components/projects/WidgetTargetingEditor.vue'
import type {
  Project,
  TenantConfiguration,
  WidgetAppearanceOverrides,
  WidgetLocalization,
  WidgetResponseBlock,
  WidgetRule,
  WidgetRuleInput,
} from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { formatDateTime } from '~/utils/format'
import { defaultWidgetRule, widgetEmbedCode } from '~/utils/widget-setup'
import { cleanAppearanceOverrides, resolveWidgetAppearance } from '~/utils/widget-appearance'
import {
  buildTargetingPayload,
  targetingFromRule,
  validateTargeting,
  type TargetingFormState,
  type TargetingTestDiagnostic,
} from '~/utils/widget-targeting'

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { can, canWrite, tenant, workspaceWritable } = useAuth()
const config = useRuntimeConfig()
const projectID = computed(() => String(route.params.id || ''))
const selectedRuleID = ref('')
const editingNew = ref(false)
const pending = ref(false)
const formError = ref('')
const notice = ref('')
const projectName = ref('')
const projectDomains = ref('')
const projectDefaultLocale = ref('en')
const projectReporterAccess = ref<'inherit' | 'enabled' | 'disabled'>('inherit')
const copied = ref(false)
const editingLocale = ref('en')
const newLocale = ref('')
const showTargetingErrors = ref(false)
const showArchivedRules = ref(false)
const targetingState = ref<TargetingFormState>({ mode: null, includes: [], excludes: [] })
const targetingDiagnostic = ref<TargetingTestDiagnostic | null>(null)
const projectAppearance = ref<WidgetAppearanceOverrides>({})
const ruleAppearance = ref<WidgetAppearanceOverrides>({})
const diagnosticsRefreshing = ref(false)
const previewLauncherOpen = ref(false)

const ruleForm = reactive<WidgetRuleInput>(defaultWidgetRule())

const { data, status, error, refresh } = await useAsyncData(
  () => `project:${projectID.value}`,
  async () => {
    const [project, rules, configuration] = await Promise.all([
      $apiFetch<{ project: Project }>(`/v1/projects/${projectID.value}`),
      can('widget_rule:read')
        ? $apiFetch<{ widget_rules: WidgetRule[] }>(`/v1/projects/${projectID.value}/widget-rules`)
        : Promise.resolve({ widget_rules: [] as WidgetRule[] }),
      $apiFetch<TenantConfiguration>('/v1/config'),
    ])
    return { project: project.project, rules: rules.widget_rules, configuration }
  },
  { watch: [projectID] },
)

watch(data, (value) => {
  if (!value) return
  projectName.value = value.project.name
  projectDomains.value = value.project.allowed_domains.join('\n')
  projectDefaultLocale.value = value.project.default_locale || 'en'
  projectReporterAccess.value = value.project.reporter_access_enabled == null
    ? 'inherit'
    : value.project.reporter_access_enabled ? 'enabled' : 'disabled'
  projectAppearance.value = { ...(value.project.widget_appearance_overrides || {}) }
  if (!editingNew.value) {
    const selected = value.rules.find((rule) => rule.id === selectedRuleID.value)
      || value.rules.find((rule) => rule.definition_status !== 'archived')
    if (selected) selectRule(selected)
    else startNewRule()
  }
}, { immediate: true })

const selectedRule = computed(() => data.value?.rules.find((rule) => rule.id === selectedRuleID.value))
const visibleRules = computed(() => (data.value?.rules || []).filter(rule =>
  showArchivedRules.value || rule.definition_status !== 'archived'))
const supportsAudio = computed(() => tenant.value?.plan !== 'starter')
const supportsDOM = computed(() => tenant.value?.plan !== 'starter')
const supportsReplay = computed(() => tenant.value?.plan !== 'starter')
const supportsVideo = computed(() => tenant.value?.plan === 'scale' || tenant.value?.plan === 'enterprise')
const embedCode = computed(() => widgetEmbedCode(
  data.value?.project.public_key || '',
  String(config.public.baseDomain || 'fidibeki.com'),
))
const connectedAt = computed(() => data.value?.project.widget_last_seen_at || '')
const activeDefinitionCount = computed(() => (data.value?.rules || []).filter(rule =>
  (rule.definition_status || (rule.enabled ? 'active' : 'paused')) === 'active').length)
const workspaceAppearance = computed(() => resolveWidgetAppearance(
  data.value?.configuration.tenant.widget_appearance_defaults,
))
const projectEffectiveAppearance = computed(() => resolveWidgetAppearance(
  data.value?.configuration.tenant.widget_appearance_defaults,
  projectAppearance.value,
))
const effectiveAppearance = computed(() => resolveWidgetAppearance(
  data.value?.configuration.tenant.widget_appearance_defaults,
  projectAppearance.value,
  ruleAppearance.value,
))

function selectRule(rule: WidgetRule) {
  selectedRuleID.value = rule.id
  editingNew.value = false
  targetingState.value = targetingFromRule(rule)
  targetingDiagnostic.value = null
  showTargetingErrors.value = false
  const locale = data.value?.project.default_locale || 'en'
  const localizations = structuredClone(rule.localizations || {})
  const localized = localizations[locale] || fallbackLocalization(rule.greeting || 'How was your experience?')
  localizations[locale] = localized
  const legacyAppearance: WidgetAppearanceOverrides = {
    position: rule.position, theme: rule.theme, accent_color: rule.accent_color || undefined,
  }
  ruleAppearance.value = { ...(rule.appearance_overrides || legacyAppearance) }
  Object.assign(ruleForm, {
    name: rule.name, priority: rule.priority,
    enabled: (rule.definition_status || (rule.enabled ? 'active' : 'paused')) === 'active',
    matcher_type: rule.matcher_type, matcher_value: rule.matcher_value ?? null,
    trigger_type: rule.trigger_type, trigger_delay_seconds: rule.trigger_delay_seconds ?? null,
    trigger_scroll_percent: rule.trigger_scroll_percent ?? null, can_rate: rule.can_rate,
    can_write_text: rule.can_write_text, can_record_audio: rule.can_record_audio,
    can_record_video: rule.can_record_video, can_attach_element: rule.can_attach_element,
    can_attach_screenshot: rule.can_attach_screenshot, greeting: localized.prompt,
    position: rule.position, theme: rule.theme, accent_color: rule.accent_color ?? null,
    replay_mode: rule.replay_mode, replay_sampling_rate: rule.replay_sampling_rate,
    replay_max_duration_seconds: rule.replay_max_duration_seconds,
    replay_max_events: rule.replay_max_events, replay_mask_all_inputs: rule.replay_mask_all_inputs,
    replay_mask_text_selector: rule.replay_mask_text_selector ?? null,
    recurrence_mode: rule.recurrence_mode || 'cooldown',
    recurrence_days: (rule.recurrence_mode || 'cooldown') === 'cooldown' ? rule.recurrence_days ?? 30 : null,
    definition_key: rule.definition_key || `widget-${rule.id.replaceAll('-', '').slice(0, 12)}`,
    definition_status: rule.definition_status || (rule.enabled ? 'active' : 'paused'),
    presentation: rule.presentation || 'prompt', objective: rule.objective || 'continuous_feedback',
    response_blocks: rule.response_blocks || legacyResponseBlocks(rule), localizations,
    allow_comment: rule.allow_comment || false, collect_contact: rule.collect_contact || false,
  })
  editingLocale.value = locale
}

function startNewRule() {
  selectedRuleID.value = ''
  editingNew.value = true
  targetingState.value = { mode: null, includes: [], excludes: [] }
  targetingDiagnostic.value = null
  showTargetingErrors.value = false
  Object.assign(ruleForm, defaultWidgetRule())
  ruleAppearance.value = {}
  editingLocale.value = projectDefaultLocale.value || 'en'
}

function normalizedRule(): WidgetRuleInput {
  const locale = projectDefaultLocale.value.trim().toLowerCase() || 'en'
  const defaultPrompt = ruleForm.localizations[locale]?.prompt?.trim()
    || ruleForm.greeting?.trim() || 'How was your experience?'
  const localized = {
    ...fallbackLocalization(defaultPrompt),
    ...(ruleForm.localizations[locale] || {}),
    prompt: defaultPrompt,
  }
  const responseBlocks = synchronizedResponseBlocks(ruleForm)
  return {
    ...ruleForm,
    enabled: ruleForm.definition_status === 'active',
    ...buildTargetingPayload(targetingState.value),
    trigger_delay_seconds: ruleForm.trigger_type === 'delay' ? Number(ruleForm.trigger_delay_seconds || 0) : null,
    trigger_scroll_percent: ruleForm.trigger_type === 'scroll' ? Number(ruleForm.trigger_scroll_percent || 1) : null,
    greeting: defaultPrompt,
    accent_color: ruleForm.accent_color?.trim() || null,
    replay_sampling_rate: ruleForm.replay_mode === 'sampling' ? Number(ruleForm.replay_sampling_rate || 1) : 0,
    replay_mask_text_selector: ruleForm.replay_mask_text_selector?.trim() || null,
    recurrence_days: ruleForm.recurrence_mode === 'cooldown' ? Number(ruleForm.recurrence_days || 30) : null,
    definition_key: ruleForm.definition_key.trim().toLowerCase(),
    definition_status: ruleForm.definition_status,
    appearance_overrides: cleanAppearanceOverrides(ruleAppearance.value, ruleAppearance.value.logo_url === ''),
    response_blocks: responseBlocks,
    localizations: { ...ruleForm.localizations, [locale]: localized },
  }
}

function fallbackLocalization(prompt: string): WidgetLocalization {
  return {
    launcher_label: 'Feedback', prompt, comment_placeholder: 'Tell us more',
    contact_label: 'Email address', consent_label: 'Email me about this feedback',
    submit_label: 'Send feedback', success_message: 'Your feedback was sent.',
    block_labels: {}, option_labels: {},
  }
}

function legacyResponseBlocks(rule: Pick<WidgetRule, 'can_rate' | 'can_write_text'>): WidgetResponseBlock[] {
  return [
    ...(rule.can_rate ? [{ key: 'rating', type: 'rating', required: false } as WidgetResponseBlock] : []),
    ...(rule.can_write_text ? [{ key: 'feedback', type: 'free_text', required: false } as WidgetResponseBlock] : []),
  ]
}

function synchronizedResponseBlocks(rule: WidgetRuleInput): WidgetResponseBlock[] {
  const rating = rule.response_blocks.find(block => block.type === 'rating')
  const freeText = rule.response_blocks.find(block => block.type === 'free_text')
  const extended = rule.response_blocks.filter(block => block.type !== 'rating' && block.type !== 'free_text')
  return [
    ...(rule.can_rate ? [rating || { key: 'rating', type: 'rating' as const, required: false }] : []),
    ...(rule.can_write_text ? [freeText || { key: 'feedback', type: 'free_text' as const, required: false }] : []),
    ...extended,
  ].slice(0, 4)
}

watch(() => [ruleForm.can_rate, ruleForm.can_write_text], () => {
  ruleForm.response_blocks = synchronizedResponseBlocks(ruleForm)
})

const editingLocalization = computed(() => {
  const locale = editingLocale.value || projectDefaultLocale.value || 'en'
  if (!ruleForm.localizations[locale]) {
    ruleForm.localizations[locale] = fallbackLocalization(ruleForm.greeting || 'How was your experience?')
  }
  return ruleForm.localizations[locale]!
})
const previewLocalization = computed(() => ruleForm.localizations[projectDefaultLocale.value]
  || editingLocalization.value)

function hasResponseType(type: WidgetResponseBlock['type']) {
  return ruleForm.response_blocks.some(block => block.type === type)
}

function setResponseType(type: WidgetResponseBlock['type'], enabled: boolean) {
  if (!enabled) {
    ruleForm.response_blocks = ruleForm.response_blocks.filter(block => block.type !== type)
    return
  }
  if (hasResponseType(type) || ruleForm.response_blocks.length >= 4) return
  if (type === 'reaction') {
    ruleForm.response_blocks.push({
      key: 'reaction', type, required: false,
      options: [{ value: 'helpful' }, { value: 'not_helpful' }],
    })
  } else if (type === 'single_choice') {
    ruleForm.response_blocks.push({
      key: 'choice', type, required: false,
      options: [{ value: 'yes' }, { value: 'no' }],
    })
  }
}

function removeResponseBlock(type: WidgetResponseBlock['type']) {
  if (type === 'rating') ruleForm.can_rate = false
  else if (type === 'free_text') ruleForm.can_write_text = false
  else setResponseType(type, false)
}

function addResponseOption(block: WidgetResponseBlock) {
  const maximum = block.type === 'reaction' ? 5 : 10
  block.options ||= []
  if (block.options.length >= maximum) return
  let index = block.options.length + 1
  while (block.options.some(option => option.value === `option_${index}`)) index++
  block.options.push({ value: `option_${index}` })
}

function removeResponseOption(block: WidgetResponseBlock, index: number) {
  if ((block.options?.length || 0) <= 2) return
  block.options?.splice(index, 1)
}

function addLocalization() {
  const locale = newLocale.value.trim().replaceAll('_', '-').toLowerCase()
  if (!/^[a-z]{2,3}(?:-[a-z0-9]{2,8})*$/.test(locale)) {
    formError.value = 'Language must use a tag such as pt or pt-ao.'
    return
  }
  if (!ruleForm.localizations[locale]) {
    ruleForm.localizations[locale] = cloneLocalization(
      ruleForm.localizations[projectDefaultLocale.value] || fallbackLocalization(ruleForm.greeting || 'How was your experience?'),
    )
  }
  editingLocale.value = locale
  newLocale.value = ''
  formError.value = ''
}

function cloneLocalization(source: WidgetLocalization): WidgetLocalization {
  return {
    ...source,
    block_labels: { ...(source.block_labels || {}) },
    option_labels: Object.fromEntries(
      Object.entries(source.option_labels || {}).map(([key, labels]) => [key, { ...labels }]),
    ),
  }
}

function removeLocalization() {
  if (editingLocale.value === projectDefaultLocale.value) return
  delete ruleForm.localizations[editingLocale.value]
  editingLocale.value = projectDefaultLocale.value
}

function setBlockLabel(key: string, value: string) {
  editingLocalization.value.block_labels ||= {}
  editingLocalization.value.block_labels[key] = value
}

function setOptionLabel(blockKey: string, option: string, value: string) {
  editingLocalization.value.option_labels ||= {}
  editingLocalization.value.option_labels[blockKey] ||= {}
  editingLocalization.value.option_labels[blockKey]![option] = value
}

async function saveRule() {
  const permission = editingNew.value ? 'widget_rule:create' : 'widget_rule:update'
  if (!canWrite(permission) || pending.value) return
  formError.value = ''
  showTargetingErrors.value = true
  const targetingErrors = validateTargeting(targetingState.value)
  if (Object.keys(targetingErrors).length > 0) {
    formError.value = Object.values(targetingErrors)[0] || 'Targeting configuration is invalid.'
    return
  }
  pending.value = true
  try {
    if (editingNew.value) {
      const response = await $apiFetch<{ widget_rule: WidgetRule }>(`/v1/projects/${projectID.value}/widget-rules`, { method: 'POST', body: normalizedRule() })
      selectedRuleID.value = response.widget_rule.id
      editingNew.value = false
    } else {
      await $apiFetch(`/v1/projects/${projectID.value}/widget-rules/${selectedRuleID.value}`, { method: 'PATCH', body: normalizedRule() })
    }
    notice.value = 'Widget rule saved.'
    await refresh()
  } catch (saveError) {
    formError.value = apiErrorMessage(saveError, 'Widget rule could not be saved.')
  } finally {
    pending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

async function deleteRule() {
  if (!canWrite('widget_rule:delete') || !selectedRule.value || pending.value || !window.confirm(`Delete “${selectedRule.value.name}”?`)) return
  pending.value = true
  formError.value = ''
  try {
    await $apiFetch(`/v1/projects/${projectID.value}/widget-rules/${selectedRule.value.id}`, { method: 'DELETE' })
    selectedRuleID.value = ''
    editingNew.value = false
    await refresh()
  } catch (deleteError) {
    formError.value = apiErrorMessage(deleteError, 'Widget rule could not be deleted.')
  } finally { pending.value = false }
}

async function saveProject() {
  if (!canWrite('project:update') || !data.value || pending.value) return
  pending.value = true
  formError.value = ''
  try {
    await $apiFetch(`/v1/projects/${projectID.value}`, { method: 'PATCH', body: {
      name: projectName.value,
      allowed_domains: projectDomains.value.split(/[\n,]+/).map((value) => value.trim()).filter(Boolean),
      default_locale: projectDefaultLocale.value.trim(),
      reporter_access_enabled: projectReporterAccess.value === 'inherit'
        ? null
        : projectReporterAccess.value === 'enabled',
      widget_appearance_overrides: cleanAppearanceOverrides(
        projectAppearance.value, projectAppearance.value.logo_url === '',
      ),
    } })
    notice.value = 'Project settings saved.'
    await refresh()
  } catch (saveError) {
    formError.value = apiErrorMessage(saveError, 'Project settings could not be saved.')
  } finally {
    pending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

async function deleteProject() {
  if (!canWrite('project:delete') || !data.value || pending.value || !window.confirm(`Delete ${data.value.project.name} and all of its feedback?`)) return
  pending.value = true
  try {
    await $apiFetch(`/v1/projects/${projectID.value}`, { method: 'DELETE' })
    await navigateTo('/projects')
  } catch (deleteError) {
    formError.value = apiErrorMessage(deleteError, 'Project could not be deleted.')
    pending.value = false
  }
}

async function copyEmbed() {
  try {
    await navigator.clipboard.writeText(embedCode.value)
    copied.value = true
    window.setTimeout(() => { copied.value = false }, 1800)
  } catch {
    formError.value = 'The install code could not be copied. Select the code and copy it manually.'
  }
}

async function refreshDiagnostics() {
  if (diagnosticsRefreshing.value) return
  diagnosticsRefreshing.value = true
  try {
    const response = await $apiFetch<{ project: Project }>(`/v1/projects/${projectID.value}`)
    if (data.value) Object.assign(data.value.project, response.project)
  } finally {
    diagnosticsRefreshing.value = false
  }
}

watch(() => ruleForm.presentation, () => { previewLauncherOpen.value = false })

useHead(() => ({ title: `${data.value?.project.name || 'Project'} · Fidibeki` }))
</script>

<template>
  <div class="project-workspace">
    <header class="project-workspace-head">
      <NuxtLink to="/projects" class="app-button"><ArrowLeft :size="15" /> Projects</NuxtLink>
      <div v-if="data"><span class="project-workspace-mark">{{ data.project.name.slice(0, 1).toUpperCase() }}</span><p><b>{{ data.project.name }}</b><span>{{ data.project.allowed_domains.join(', ') || 'No domains' }}</span></p></div>
    </header>
    <section v-if="status === 'pending'" class="app-state"><div><span class="app-spinner" /><h2>Loading project</h2></div></section>
    <section v-else-if="error" class="app-state"><div><AlertTriangle :size="24" /><h2>Project unavailable</h2><p>{{ apiErrorMessage(error) }}</p></div></section>
    <template v-else-if="data">
      <section class="project-config-band">
        <div><p class="app-eyebrow">Project configuration</p><h1>{{ data.project.name }}</h1><span>{{ data.rules.length }} widget rule{{ data.rules.length === 1 ? '' : 's' }}</span></div>
        <div class="project-embed"><Code2 :size="15" /><code>{{ embedCode }}</code><button title="Copy embed code" @click="copyEmbed"><Check v-if="copied" :size="15" /><Clipboard v-else :size="15" /></button></div>
      </section>
      <section class="project-install-diagnostics" aria-label="Installation diagnostics">
        <header><span>Installation</span><button type="button" title="Refresh diagnostics" aria-label="Refresh diagnostics" :disabled="diagnosticsRefreshing" @click="refreshDiagnostics"><RefreshCw :size="14" :class="{ spinning: diagnosticsRefreshing }" /></button></header>
        <dl>
          <div :data-status="connectedAt ? 'ready' : 'attention'"><dt><Radio :size="13" /> Loader</dt><dd>{{ connectedAt ? `Observed ${formatDateTime(connectedAt)}` : 'Not detected' }}</dd></div>
          <div :data-status="data.project.allowed_domains.length ? 'ready' : 'attention'"><dt><Globe2 :size="13" /> Domains</dt><dd>{{ data.project.allowed_domains.length ? `${data.project.allowed_domains.length} configured` : 'None configured' }}</dd></div>
          <div :data-status="activeDefinitionCount ? 'ready' : 'attention'"><dt><CircleCheck :size="13" /> Definitions</dt><dd>{{ activeDefinitionCount }} active</dd></div>
          <div :data-status="targetingDiagnostic?.result.status === 'eligible' ? 'ready' : targetingDiagnostic ? 'blocked' : 'attention'"><dt><CircleCheck v-if="targetingDiagnostic?.result.status === 'eligible'" :size="13" /><CircleX v-else :size="13" /> URL test</dt><dd>{{ targetingDiagnostic?.result.message || 'Not tested' }}</dd></div>
        </dl>
      </section>
      <p v-if="formError" class="project-config-error" role="alert"><AlertTriangle :size="15" /> {{ formError }}</p>
      <div class="project-config-grid">
        <aside class="project-rule-list">
          <header><span>Widget rules</span><div><button v-if="data.rules.some(rule => rule.definition_status === 'archived')" :title="showArchivedRules ? 'Hide archived rules' : 'Show archived rules'" :aria-pressed="showArchivedRules" @click="showArchivedRules = !showArchivedRules"><Archive :size="15" /></button><button v-if="canWrite('widget_rule:create')" title="New widget rule" @click="startNewRule"><Plus :size="16" /></button></div></header>
          <button v-for="rule in visibleRules" :key="rule.id" :data-active="rule.id === selectedRuleID" @click="selectRule(rule)"><span :data-status="rule.definition_status || (rule.enabled ? 'active' : 'paused')" /><p><b>{{ rule.name }}</b><small>{{ rule.definition_status || (rule.enabled ? 'active' : 'paused') }} · {{ rule.matcher_type.replaceAll('_', ' ') }} · priority {{ rule.priority }}</small></p></button>
          <p v-if="!visibleRules.length" class="project-rule-empty">No rules configured.</p>
          <nav><a href="#project-settings"><Settings2 :size="14" /> Project settings</a></nav>
        </aside>

        <main class="project-rule-editor">
          <header><div><p class="app-eyebrow">{{ editingNew ? 'New rule' : 'Editing rule' }}</p><h2>{{ ruleForm.name || 'Untitled rule' }}</h2></div><div><button v-if="selectedRule && canWrite('widget_rule:delete')" class="app-button project-delete-button" :disabled="pending" title="Delete rule" aria-label="Delete rule" @click="deleteRule"><Trash2 :size="14" /></button><button class="app-button app-button--primary" type="submit" form="widget-rule-form" :disabled="pending || !(editingNew ? canWrite('widget_rule:create') : canWrite('widget_rule:update'))"><Save :size="14" /> {{ pending ? 'Saving…' : 'Save rule' }}</button></div></header>
          <form id="widget-rule-form" @submit.prevent="saveRule">
            <fieldset :disabled="!workspaceWritable">
            <section><h3>Rule identity</h3><div class="project-form-grid"><label>Name<input v-model.trim="ruleForm.name" class="app-input" required maxlength="120" /></label><label>Definition key<input v-model.trim="ruleForm.definition_key" class="app-input" required maxlength="64" pattern="[a-z][a-z0-9-]{1,62}[a-z0-9]" /></label><label>Lifecycle status<select v-model="ruleForm.definition_status" class="app-select"><option value="draft">Draft</option><option value="active">Active</option><option value="paused">Paused</option><option value="archived">Archived</option></select></label><label>Presentation<select v-model="ruleForm.presentation" class="app-select"><option value="launcher">Launcher</option><option value="inline">Inline</option><option value="reaction">Reaction</option><option value="prompt">Prompt</option></select></label><label>Objective<select v-model="ruleForm.objective" class="app-select"><option value="continuous_feedback">Continuous feedback</option><option value="after_engagement">After engagement</option><option value="validate_content">Validate content</option><option value="detect_friction">Detect friction</option></select></label><label>Priority<input v-model.number="ruleForm.priority" class="app-input" type="number" /></label></div></section>
            <section><h3>Page matching</h3><WidgetTargetingEditor v-model="targetingState" :show-errors="showTargetingErrors" @tested="targetingDiagnostic = $event" /></section>
            <section><h3>Trigger</h3><div class="project-form-grid"><label>Trigger type<select v-model="ruleForm.trigger_type" class="app-select"><option value="delay">Delay</option><option value="scroll">Scroll depth</option><option value="exit_intent">Exit intent</option></select></label><label v-if="ruleForm.trigger_type === 'delay'">Delay seconds<input v-model.number="ruleForm.trigger_delay_seconds" class="app-input" type="number" min="0" max="86400" /></label><label v-if="ruleForm.trigger_type === 'scroll'">Scroll percent<input v-model.number="ruleForm.trigger_scroll_percent" class="app-input" type="number" min="1" max="100" /></label></div></section>
            <section><h3>Recurrence</h3><div class="project-form-grid"><label>Show again<select v-model="ruleForm.recurrence_mode" class="app-select"><option value="cooldown">After a period</option><option value="session">Next browser session</option><option value="never">Never show again</option></select></label><label v-if="ruleForm.recurrence_mode === 'cooldown'">Cooldown days<input v-model.number="ruleForm.recurrence_days" class="app-input" type="number" min="1" max="3650" required /></label></div></section>
            <section><WidgetDecisionDiagnostics :objective="ruleForm.objective" :presentation="ruleForm.presentation" :trigger-type="ruleForm.trigger_type" :trigger-delay-seconds="ruleForm.trigger_delay_seconds ?? null" :trigger-scroll-percent="ruleForm.trigger_scroll_percent ?? null" :recurrence-mode="ruleForm.recurrence_mode" :recurrence-days="ruleForm.recurrence_days ?? null" /></section>
            <section><h3>Feedback methods</h3><div class="project-capability-grid"><label><input v-model="ruleForm.can_rate" type="checkbox" /><span><b>Rating</b><small>Collect a 1–5 score</small></span></label><label><input v-model="ruleForm.can_write_text" type="checkbox" /><span><b>Written feedback</b><small>Allow free-form detail</small></span></label><label><input type="checkbox" :checked="hasResponseType('reaction')" :disabled="!hasResponseType('reaction') && ruleForm.response_blocks.length >= 4" @change="setResponseType('reaction', !hasResponseType('reaction'))" /><span><b>Reaction</b><small>Collect a compact sentiment</small></span></label><label><input type="checkbox" :checked="hasResponseType('single_choice')" :disabled="!hasResponseType('single_choice') && ruleForm.response_blocks.length >= 4" @change="setResponseType('single_choice', !hasResponseType('single_choice'))" /><span><b>Single choice</b><small>Offer configured options</small></span></label><label><input v-model="ruleForm.allow_comment" type="checkbox" /><span><b>Optional comment</b><small>Add context to compact responses</small></span></label><label><input v-model="ruleForm.collect_contact" type="checkbox" /><span><b>Contact with consent</b><small>Offer optional reporter email</small></span></label><label :data-locked="!supportsAudio"><input v-model="ruleForm.can_record_audio" type="checkbox" :disabled="!supportsAudio" /><span><b>Audio</b><small>{{ supportsAudio ? 'Capture a voice note' : 'Upgrade required' }}</small></span></label><label :data-locked="!supportsVideo"><input v-model="ruleForm.can_record_video" type="checkbox" :disabled="!supportsVideo" /><span><b>Video</b><small>{{ supportsVideo ? 'Capture camera video' : 'Scale plan required' }}</small></span></label><label :data-locked="!supportsDOM"><input v-model="ruleForm.can_attach_element" type="checkbox" :disabled="!supportsDOM" /><span><b>Page element</b><small>{{ supportsDOM ? 'Attach selected DOM context' : 'Upgrade required' }}</small></span></label><label :data-locked="!supportsDOM"><input v-model="ruleForm.can_attach_screenshot" type="checkbox" :disabled="!supportsDOM" /><span><b>Screenshot</b><small>{{ supportsDOM ? 'Capture the current view' : 'Upgrade required' }}</small></span></label></div></section>
            <section><h3>Response blocks</h3><div class="response-block-list"><div v-for="block in ruleForm.response_blocks" :key="block.type" class="response-block-row"><div class="response-block-head"><span>{{ block.type.replaceAll('_', ' ') }}</span><label><input v-model="block.required" type="checkbox" /> Required</label><button type="button" title="Remove response block" :aria-label="`Remove ${block.type} block`" @click="removeResponseBlock(block.type)"><Trash2 :size="13" /></button></div><div class="project-form-grid"><label>Response key<input v-model.trim="block.key" class="app-input" required maxlength="32" pattern="[a-z0-9_]{1,32}" /></label><label>Label ({{ editingLocale }})<input class="app-input" maxlength="240" :value="editingLocalization.block_labels?.[block.key] || ''" @input="setBlockLabel(block.key, ($event.target as HTMLInputElement).value)" /></label></div><div v-if="block.options" class="response-option-list"><div v-for="(option, optionIndex) in block.options" :key="optionIndex"><input v-model.trim="option.value" class="app-input" required maxlength="32" pattern="[a-z0-9_]{1,32}" aria-label="Option key" /><input class="app-input" maxlength="120" aria-label="Localized option label" :value="editingLocalization.option_labels?.[block.key]?.[option.value] || ''" @input="setOptionLabel(block.key, option.value, ($event.target as HTMLInputElement).value)" /><button type="button" title="Remove option" aria-label="Remove option" :disabled="block.options.length <= 2" @click="removeResponseOption(block, optionIndex)"><Trash2 :size="13" /></button></div><button type="button" class="app-button" :disabled="block.options.length >= (block.type === 'reaction' ? 5 : 10)" @click="addResponseOption(block)"><Plus :size="13" /> Add option</button></div></div></div></section>
            <section><h3>Content and appearance</h3><div class="localization-toolbar"><select v-model="editingLocale" class="app-select" aria-label="Editing language"><option v-for="(_, locale) in ruleForm.localizations" :key="locale" :value="locale">{{ locale }}{{ locale === projectDefaultLocale ? ' · default' : '' }}</option></select><input v-model.trim="newLocale" class="app-input" maxlength="32" placeholder="pt-ao" aria-label="New language" /><button type="button" class="app-button" @click="addLocalization"><Plus :size="13" /> Add</button><button v-if="editingLocale !== projectDefaultLocale" type="button" class="app-icon-button" title="Remove language" aria-label="Remove language" @click="removeLocalization"><Trash2 :size="13" /></button></div><div class="project-form-grid localization-fields"><label>Launcher label<input v-model="editingLocalization.launcher_label" class="app-input" required maxlength="120" /></label><label class="wide">Prompt<input v-model="editingLocalization.prompt" class="app-input" required maxlength="500" /></label><label>Comment placeholder<input v-model="editingLocalization.comment_placeholder" class="app-input" required maxlength="240" /></label><label>Contact label<input v-model="editingLocalization.contact_label" class="app-input" required maxlength="120" /></label><label class="wide">Consent label<input v-model="editingLocalization.consent_label" class="app-input" required maxlength="240" /></label><label>Submit label<input v-model="editingLocalization.submit_label" class="app-input" required maxlength="120" /></label><label>Success message<input v-model="editingLocalization.success_message" class="app-input" required maxlength="500" /></label></div><WidgetAppearanceEditor v-model="ruleAppearance" :parent="projectEffectiveAppearance" allow-logo-reset /></section>
            <section><h3>Session replay</h3><div class="project-form-grid"><label>Replay mode<select v-model="ruleForm.replay_mode" class="app-select" :disabled="!supportsReplay"><option value="none">Disabled</option><option value="full">Every session</option><option value="sampling">Sample sessions</option></select></label><label v-if="ruleForm.replay_mode === 'sampling'">Sampling percent<input v-model.number="ruleForm.replay_sampling_rate" class="app-input" type="number" min="1" max="100" /></label><label>Max duration (seconds)<input v-model.number="ruleForm.replay_max_duration_seconds" class="app-input" type="number" min="1" max="1800" /></label><label>Max events<input v-model.number="ruleForm.replay_max_events" class="app-input" type="number" min="1" max="50000" /></label><label class="project-toggle wide"><input v-model="ruleForm.replay_mask_all_inputs" type="checkbox" /><span>Mask all input values</span></label><label class="wide">Additional text mask selector<input v-model="ruleForm.replay_mask_text_selector" class="app-input" placeholder=".private, [data-sensitive]" /></label></div></section>
            </fieldset>
          </form>
        </main>

        <aside class="project-widget-preview">
          <header><span>Live preview</span><div v-if="ruleForm.presentation === 'launcher'" class="widget-preview-modes" role="group" aria-label="Launcher preview state"><button type="button" :data-active="!previewLauncherOpen" @click="previewLauncherOpen = false">Launcher</button><button type="button" :data-active="previewLauncherOpen" @click="previewLauncherOpen = true">Open</button></div><small v-else>{{ effectiveAppearance.theme }} · {{ effectiveAppearance.position }}</small></header>
          <div class="widget-preview-page" :data-theme="effectiveAppearance.theme" inert aria-hidden="true"><div class="widget-preview-site"><span /><span /><span /></div><div class="widget-preview-copy"><b>Checkout</b><span>Review your order before payment.</span><div /></div><button v-if="ruleForm.presentation === 'launcher' && !previewLauncherOpen" class="widget-preview-trigger" :style="{ background: effectiveAppearance.accent_color }">{{ previewLocalization.launcher_label }}</button><section v-show="ruleForm.presentation !== 'launcher' || previewLauncherOpen" class="widget-preview-modal" :data-presentation="ruleForm.presentation" :style="{ background: effectiveAppearance.surface_color, color: effectiveAppearance.text_color, borderTopColor: effectiveAppearance.accent_color }"><img v-if="effectiveAppearance.logo_url" class="widget-preview-logo" :src="effectiveAppearance.logo_url" alt="" referrerpolicy="no-referrer" /><p>{{ previewLocalization.prompt }}</p><div v-if="ruleForm.can_rate" class="widget-preview-ratings"><button v-for="value in 5" :key="value">{{ value }}</button></div><div v-if="hasResponseType('reaction')" class="widget-preview-ratings"><button v-for="option in ruleForm.response_blocks.find(block => block.type === 'reaction')?.options" :key="option.value">{{ previewLocalization.option_labels?.reaction?.[option.value] || option.value.replaceAll('_', ' ') }}</button></div><textarea v-if="ruleForm.can_write_text" :placeholder="previewLocalization.block_labels?.feedback || 'Tell us more'" /><div class="widget-preview-actions"><span v-if="ruleForm.can_attach_screenshot">Screenshot</span><span v-if="ruleForm.can_record_audio">Audio</span><button :style="{ background: effectiveAppearance.accent_color }">{{ previewLocalization.submit_label }}</button></div></section></div>
        </aside>
      </div>

      <section id="project-settings" class="project-settings app-surface"><header><div><h2>Project settings</h2><p>Update collection, access, and browser origins.</p></div><button class="app-button app-button--primary" type="submit" form="project-settings-form" :disabled="pending || !canWrite('project:update')"><Save :size="14" /> Save project</button></header><form id="project-settings-form" @submit.prevent="saveProject"><fieldset :disabled="!workspaceWritable"><label>Project name<input v-model.trim="projectName" class="app-input" required maxlength="120" /></label><label>Default language<input v-model.trim="projectDefaultLocale" class="app-input" required maxlength="32" placeholder="en" /></label><label>Reporter progress access<select v-model="projectReporterAccess" class="app-select"><option value="inherit">Inherit workspace</option><option value="enabled">Enabled</option><option value="disabled">Disabled</option></select></label><label class="wide">Allowed domains<textarea v-model="projectDomains" /></label><div class="wide project-appearance"><h3>Widget appearance</h3><WidgetAppearanceEditor v-model="projectAppearance" :parent="workspaceAppearance" allow-logo-reset /></div></fieldset></form><footer><div><b>Delete project</b><span>Permanently removes its rules, feedback, issues, and replay data.</span></div><button class="app-button project-danger" :disabled="pending || !canWrite('project:delete')" @click="deleteProject"><Trash2 :size="14" /> Delete project</button></footer></section>
    </template>
    <p v-if="notice" class="project-toast" role="status">{{ notice }}</p>
  </div>
</template>

<style scoped>
.project-workspace { min-height: calc(100vh - 56px); background: #f6f8f7; }
.project-workspace-head { display: flex; min-height: 64px; align-items: center; gap: 14px; padding: 9px 18px; background: #fff; border-bottom: 1px solid var(--line); }
.project-workspace-head > div { display: flex; align-items: center; gap: 9px; }
.project-workspace-mark { display: grid; width: 32px; height: 32px; place-items: center; background: var(--teal-soft); color: var(--teal); border-radius: 5px; font-size: 11px; font-weight: 800; }
.project-workspace-head p { display: flex; flex-direction: column; gap: 3px; margin: 0; }
.project-workspace-head b { font-size: 11px; }
.project-workspace-head p span { color: var(--muted); font-size: 9px; }
.project-config-band { display: flex; min-height: 92px; align-items: center; justify-content: space-between; gap: 20px; padding: 16px 20px; background: #fff; border-bottom: 1px solid var(--line); }
.project-config-band h1 { margin: 4px 0; font-size: 20px; }
.project-config-band > div:first-child > span { color: var(--muted); font-size: 9px; }
.project-embed { display: flex; width: min(520px, 52%); height: 40px; align-items: center; gap: 8px; padding-left: 10px; background: #f3f6f5; border: 1px solid var(--line); border-radius: 5px; }
.project-embed code { flex: 1; overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.project-embed button { display: grid; width: 38px; height: 38px; place-items: center; background: transparent; color: var(--teal); border: 0; border-left: 1px solid var(--line); }
.project-install-diagnostics { display: grid; grid-template-columns: 104px minmax(0, 1fr); min-height: 62px; background: #f9fbfa; border-bottom: 1px solid var(--line); }
.project-install-diagnostics > header { display: flex; align-items: center; justify-content: space-between; gap: 6px; padding: 0 10px 0 16px; color: var(--muted); border-right: 1px solid var(--line); font-size: 8px; font-weight: 800; text-transform: uppercase; }
.project-install-diagnostics header button { display: grid; width: 28px; height: 28px; place-items: center; background: transparent; color: var(--muted); border: 0; border-radius: 4px; }
.project-install-diagnostics header button:hover { background: #edf3f1; color: var(--teal); }
.project-install-diagnostics dl { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; }
.project-install-diagnostics dl > div { display: flex; min-width: 0; justify-content: center; flex-direction: column; gap: 4px; padding: 9px 13px; border-right: 1px solid var(--line); }
.project-install-diagnostics dl > div:last-child { border-right: 0; }
.project-install-diagnostics dt { display: flex; align-items: center; gap: 5px; color: var(--amber); font-size: 8px; font-weight: 800; text-transform: uppercase; }
.project-install-diagnostics [data-status='ready'] dt { color: var(--green); }
.project-install-diagnostics [data-status='blocked'] dt { color: var(--red); }
.project-install-diagnostics dd { min-width: 0; overflow: hidden; margin: 0; color: var(--ink-soft); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.project-install-diagnostics .spinning { animation: diagnostic-spin .8s linear infinite; }
@keyframes diagnostic-spin { to { transform: rotate(360deg); } }
.project-config-error { display: flex; align-items: center; gap: 7px; margin: 0; padding: 9px 18px; background: var(--red-soft); color: var(--red); border-bottom: 1px solid #efc2c6; font-size: 10px; }
.project-config-grid { display: grid; min-height: 720px; grid-template-columns: 210px minmax(480px, 1fr) minmax(300px, 34%); border-bottom: 1px solid var(--line); }
.project-rule-list { background: #fafbfa; border-right: 1px solid var(--line); }
.project-rule-list > header { display: flex; min-height: 48px; align-items: center; justify-content: space-between; padding: 0 10px 0 14px; color: var(--muted); border-bottom: 1px solid var(--line); font-size: 9px; font-weight: 800; text-transform: uppercase; }
.project-rule-list > header > div { display: flex; gap: 2px; }
.project-rule-list > header button { display: grid; width: 30px; height: 30px; place-items: center; background: transparent; border: 0; border-radius: 4px; }
.project-rule-list > header button[aria-pressed='true'] { background: #eaf3f0; color: var(--teal); }
.project-rule-list > button { display: grid; width: 100%; min-height: 56px; grid-template-columns: 8px minmax(0, 1fr); align-items: center; gap: 9px; padding: 7px 12px; background: transparent; color: var(--ink-soft); border: 0; border-bottom: 1px solid var(--line); text-align: left; }
.project-rule-list > button:hover,
.project-rule-list > button[data-active='true'] { background: #eaf3f0; }
.project-rule-list > button > span { width: 7px; height: 7px; background: var(--line-strong); border-radius: 50%; }
.project-rule-list > button > span[data-status='active'] { background: var(--green); }
.project-rule-list > button > span[data-status='draft'] { background: var(--amber); }
.project-rule-list > button > span[data-status='archived'] { background: var(--muted); }
.project-rule-list button p { display: flex; min-width: 0; flex-direction: column; gap: 3px; margin: 0; }
.project-rule-list button b,
.project-rule-list button small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.project-rule-list button b { font-size: 10px; }
.project-rule-list button small { color: var(--muted); font-size: 8px; }
.project-rule-empty { padding: 20px 12px; color: var(--muted); font-size: 9px; text-align: center; }
.project-rule-list nav { padding: 10px; border-top: 1px solid var(--line); }
.project-rule-list nav a { display: flex; min-height: 34px; align-items: center; gap: 7px; padding: 0 8px; color: var(--muted); border-radius: 4px; font-size: 9px; text-decoration: none; }
.project-rule-editor { min-width: 0; background: #fff; border-right: 1px solid var(--line); }
.project-rule-editor > header { position: sticky; top: 56px; z-index: 20; display: flex; min-height: 65px; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 16px; background: #fff; border-bottom: 1px solid var(--line); }
.project-rule-editor > header h2 { margin: 3px 0 0; font-size: 14px; }
.project-rule-editor > header > div:last-child { display: flex; gap: 6px; }
.project-delete-button { width: 38px; padding: 0; color: var(--red); }
.project-rule-editor form > fieldset { min-width: 0; margin: 0; padding: 0; border: 0; }
.project-rule-editor form > fieldset > section { padding: 17px 18px; border-bottom: 1px solid var(--line); }
.project-rule-editor form h3 { margin: 0 0 13px; font-size: 11px; }
.project-form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.project-form-grid label { display: flex; flex-direction: column; gap: 6px; color: var(--muted); font-size: 9px; font-weight: 700; }
.project-form-grid label.wide { grid-column: 1 / -1; }
.project-toggle { align-items: center; justify-content: flex-start; flex-direction: row !important; gap: 8px !important; min-height: 38px; padding-top: 17px; color: var(--ink-soft) !important; }
.project-capability-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.project-capability-grid label { display: flex; min-height: 58px; align-items: center; gap: 9px; padding: 8px 10px; border: 1px solid var(--line); border-radius: 5px; }
.project-capability-grid label[data-locked='true'] { background: #f7f8f8; opacity: .65; }
.project-capability-grid label span { display: flex; flex-direction: column; gap: 3px; }
.project-capability-grid b { font-size: 10px; }
.project-capability-grid small { color: var(--muted); font-size: 8px; }
.response-block-list { display: grid; gap: 14px; }
.response-block-row { padding-bottom: 14px; border-bottom: 1px solid var(--line); }
.response-block-row:last-child { padding-bottom: 0; border-bottom: 0; }
.response-block-head { display: flex; min-height: 32px; align-items: center; gap: 10px; margin-bottom: 8px; }
.response-block-head > span { color: var(--ink-soft); font-size: 9px; font-weight: 800; text-transform: capitalize; }
.response-block-head label { display: flex; align-items: center; gap: 5px; margin-left: auto; color: var(--muted); font-size: 8px; font-weight: 700; }
.response-block-head button,
.response-option-list button:not(.app-button) { display: grid; width: 30px; height: 30px; place-items: center; padding: 0; background: transparent; color: var(--muted); border: 0; border-radius: 4px; }
.response-option-list { display: grid; gap: 7px; margin-top: 10px; }
.response-option-list > div { display: grid; grid-template-columns: minmax(100px, .7fr) minmax(140px, 1.3fr) 30px; gap: 7px; }
.response-option-list > .app-button { justify-self: start; }
.localization-toolbar { display: grid; grid-template-columns: minmax(100px, .8fr) minmax(100px, 1fr) auto 34px; gap: 7px; margin-bottom: 13px; }
.localization-fields { margin-bottom: 16px; padding-bottom: 16px; border-bottom: 1px solid var(--line); }
.project-widget-preview { position: sticky; top: 56px; height: calc(100vh - 56px); min-height: 620px; background: #edf2f0; }
.project-widget-preview > header { display: flex; min-height: 48px; align-items: center; justify-content: space-between; padding: 0 14px; background: #f8faf9; color: var(--muted); border-bottom: 1px solid var(--line); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.project-widget-preview header small { font-size: 8px; font-weight: 500; }
.widget-preview-modes { display: inline-grid; grid-template-columns: 1fr 1fr; padding: 2px; background: #e7edeb; border-radius: 4px; }
.widget-preview-modes button { min-height: 26px; padding: 0 8px; background: transparent; color: var(--muted); border: 0; border-radius: 3px; font-size: 8px; font-weight: 750; }
.widget-preview-modes button[data-active='true'] { background: #fff; color: var(--ink-soft); box-shadow: 0 1px 2px rgb(20 42 36 / 12%); }
.widget-preview-page { position: relative; height: calc(100% - 48px); min-height: 570px; overflow: hidden; padding: 24px; background: #fff; }
.widget-preview-page[data-theme='dark'] { background: #202825; color: #fff; }
.widget-preview-site { display: flex; gap: 5px; padding-bottom: 14px; border-bottom: 1px solid var(--line); }
.widget-preview-site span { width: 7px; height: 7px; background: var(--line-strong); border-radius: 50%; }
.widget-preview-copy { display: flex; flex-direction: column; gap: 8px; margin-top: 28px; }
.widget-preview-copy b { font-size: 16px; }
.widget-preview-copy > span { color: var(--muted); font-size: 9px; }
.widget-preview-copy div { height: 170px; margin-top: 10px; background: #edf2f0; border-radius: 5px; }
.widget-preview-trigger { position: absolute; right: 18px; bottom: 18px; min-height: 38px; padding: 0 13px; color: #fff; border: 0; border-radius: 5px; font-size: 10px; font-weight: 750; }
.widget-preview-modal { position: absolute; right: 18px; bottom: 66px; left: 18px; padding: 14px; background: #fff; color: var(--ink); border: 1px solid var(--line); border-top-width: 4px; border-radius: 6px; box-shadow: var(--shadow-overlay); }
.widget-preview-modal[data-presentation='inline'] { position: relative; inset: auto; margin-top: 18px; box-shadow: none; }
.widget-preview-modal[data-presentation='reaction'] { left: auto; width: min(360px, calc(100% - 36px)); }
.widget-preview-logo { display: block; width: auto; max-width: 120px; height: 22px; margin-bottom: 8px; object-fit: contain; object-position: left center; }
.widget-preview-modal p { margin: 0 0 10px; font-size: 11px; font-weight: 700; }
.widget-preview-ratings { display: grid; grid-template-columns: repeat(5, 1fr); gap: 4px; margin-bottom: 8px; }
.widget-preview-ratings button { height: 30px; background: var(--surface-soft); border: 1px solid var(--line); border-radius: 4px; font-size: 9px; }
.widget-preview-modal textarea { width: 100%; height: 62px; padding: 7px; border: 1px solid var(--line); border-radius: 4px; resize: none; font: inherit; font-size: 9px; }
.widget-preview-actions { display: flex; align-items: center; gap: 6px; margin-top: 8px; }
.widget-preview-actions span { color: var(--muted); font-size: 8px; }
.widget-preview-actions button { min-height: 30px; margin-left: auto; padding: 0 11px; color: #fff; border: 0; border-radius: 4px; font-size: 9px; font-weight: 700; }
.project-settings { margin: 22px; overflow: hidden; }
.project-settings > header { display: flex; min-height: 66px; align-items: center; justify-content: space-between; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.project-settings h2 { margin: 0; font-size: 13px; }
.project-settings header p { margin: 4px 0 0; color: var(--muted); font-size: 9px; }
.project-settings > form { padding: 16px; }
.project-settings > form > fieldset { display: grid; min-width: 0; grid-template-columns: 1fr 1.4fr; gap: 14px; margin: 0; padding: 0; border: 0; }
.project-settings label { display: flex; flex-direction: column; gap: 6px; color: var(--muted); font-size: 9px; font-weight: 700; }
.project-settings label.wide { grid-column: 1 / -1; }
.project-settings .wide { grid-column: 1 / -1; }
.project-appearance { padding-top: 14px; border-top: 1px solid var(--line); }
.project-appearance h3 { margin: 0 0 12px; font-size: 11px; }
.project-settings textarea { min-height: 96px; padding: 9px; border: 1px solid var(--line-strong); border-radius: 5px; resize: vertical; font: inherit; font-size: 10px; }
.project-settings > footer { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 14px 16px; background: #fff8f8; border-top: 1px solid #efc2c6; }
.project-settings footer > div { display: flex; flex-direction: column; gap: 3px; }
.project-settings footer b { color: var(--red); font-size: 10px; }
.project-settings footer span { color: var(--muted); font-size: 9px; }
.project-danger { color: var(--red); border-color: #e4adb4; }
.project-toast { position: fixed; right: 20px; bottom: 20px; z-index: 90; margin: 0; padding: 10px 13px; background: #17342f; color: #fff; border-radius: 5px; box-shadow: var(--shadow-overlay); font-size: 10px; }
@media (max-width: 1180px) { .project-config-grid { grid-template-columns: 190px minmax(480px, 1fr); } .project-widget-preview { display: none; } }
@media (max-width: 800px) { .project-config-band { align-items: stretch; flex-direction: column; } .project-embed { width: 100%; } .project-install-diagnostics { grid-template-columns: 1fr; } .project-install-diagnostics > header { min-height: 42px; border-right: 0; border-bottom: 1px solid var(--line); } .project-install-diagnostics dl { grid-template-columns: repeat(2, minmax(0, 1fr)); } .project-install-diagnostics dl > div:nth-child(2) { border-right: 0; } .project-install-diagnostics dl > div:nth-child(-n+2) { border-bottom: 1px solid var(--line); } .project-config-grid { display: block; } .project-rule-list { display: flex; overflow-x: auto; border: 0; border-bottom: 1px solid var(--line); } .project-rule-list > header { min-width: 130px; } .project-rule-list > button { min-width: 180px; border-right: 1px solid var(--line); } .project-rule-list nav { display: none; } .project-rule-editor { border: 0; } .project-settings > form > fieldset { grid-template-columns: 1fr; } }
@media (max-width: 600px) { .project-install-diagnostics dl { grid-template-columns: 1fr; } .project-install-diagnostics dl > div { border-right: 0; border-bottom: 1px solid var(--line); } .project-install-diagnostics dl > div:last-child { border-bottom: 0; } .project-form-grid, .project-capability-grid { grid-template-columns: 1fr; } .project-form-grid label.wide { grid-column: auto; } .project-rule-editor > header { align-items: flex-start; flex-direction: column; } .project-settings { margin: 12px; } .project-settings > footer { align-items: stretch; flex-direction: column; } }
</style>
