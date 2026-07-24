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
  ShieldCheck,
  Trash2,
} from '@lucide/vue'
import WidgetTargetingEditor from '~/components/projects/WidgetTargetingEditor.vue'
import type {
  AIProcessingConfig,
  AnalyticsConsentConfig,
  Project,
  ReplayConsentConfig,
  ReplayMode,
  TenantConfiguration,
  WidgetAppearanceOverrides,
  WidgetLocalization,
  WidgetResponseBlock,
  WidgetRule,
  WidgetRuleInput,
} from '~/types/api'
import { defaultWidgetRule, widgetEmbedCode } from '~/utils/widget-setup'
import { cleanAppearanceOverrides, resolveWidgetAppearance } from '~/utils/widget-appearance'
import { cloneReactive } from '~/utils/reactive-clone'
import {
  buildTargetingPayload,
  targetingErrorCatalogKey,
  targetingFromRule,
  targetingTestResultMessage,
  validateTargeting,
  type TargetingFormState,
  type TargetingTestDiagnostic,
} from '~/utils/widget-targeting'
import { apiFieldErrors } from '~/utils/api-fetch'

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { can, canWrite, tenant, workspaceWritable } = useAuth()
const { t } = useI18n()
const { message: apiErrorMessage, fields: localizedFieldErrors } = useLocalizedAPIError()
const { dateTime: formatDateTime, integer, plural } = useRegionalFormat()
const config = useRuntimeConfig()
const projectID = computed(() => String(route.params.id || ''))
const selectedRuleID = ref('')
const editingNew = ref(false)
const pending = ref(false)
const formError = ref('')
const ruleFormError = ref('')
const ruleFieldErrors = ref<Record<string, string>>({})
const ruleFormElement = ref<HTMLFormElement | null>(null)
const ruleValidationSummary = ref<HTMLElement | null>(null)
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
const replayConsentDialogOpen = ref(false)
const aiProcessingDialogOpen = ref(false)
const analyticsConsentDialogOpen = ref(false)
type ProjectDestructiveAction =
  | { kind: 'rule', id: string, name: string }
  | { kind: 'project', id: string, name: string }
const destructiveAction = ref<ProjectDestructiveAction | null>(null)
const destructivePending = ref(false)
const destructiveError = ref('')

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
const supportsAI = computed(() => data.value?.configuration.plan.features.ai === true)
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
const positionKeys: Record<string, string> = { 'bottom-left': 'bottomLeft', 'bottom-right': 'bottomRight' }
const previewAppearanceSummary = computed(() => t('projects.preview.appearance', {
  theme: t(`projects.appearance.themes.${effectiveAppearance.value.theme}`),
  position: t(`projects.appearance.positions.${positionKeys[effectiveAppearance.value.position || ''] || ''}`),
}))
const destructiveTitle = computed(() => {
  const action = destructiveAction.value
  if (!action) return ''
  return action.kind === 'rule'
    ? t('projects.editor.confirmDelete', { name: action.name })
    : t('projects.settings.confirmDelete', { name: action.name })
})
const destructiveDescription = computed(() => destructiveAction.value?.kind === 'rule'
  ? t('projects.editor.deleteDescription')
  : t('projects.settings.confirmDeleteDescription'))
const destructiveConfirmLabel = computed(() => destructiveAction.value?.kind === 'rule'
  ? t('projects.editor.delete')
  : t('projects.settings.delete'))
const destructivePendingLabel = computed(() => destructiveAction.value?.kind === 'rule'
  ? t('projects.editor.deleting')
  : t('projects.settings.deleting'))
const ruleValidationEntries = computed(() => Object.entries(ruleFieldErrors.value))
const localizationFields = [
  'launcher_label',
  'prompt',
  'comment_placeholder',
  'contact_label',
  'consent_label',
  'submit_label',
  'success_message',
  'privacy_purpose_summary',
  'privacy_lawful_basis_summary',
  'privacy_retention_summary',
] as const
type LocalizationField = typeof localizationFields[number]
const localizationLabelKeys: Record<LocalizationField, string> = {
  launcher_label: 'launcherLabel',
  prompt: 'prompt',
  comment_placeholder: 'commentPlaceholder',
  contact_label: 'contactLabel',
  consent_label: 'consentLabel',
  submit_label: 'submitLabel',
  success_message: 'successMessage',
  privacy_purpose_summary: 'privacyPurpose',
  privacy_lawful_basis_summary: 'privacyLawfulBasis',
  privacy_retention_summary: 'privacyRetention',
}
const ruleAppearanceErrors = computed(() => Object.fromEntries(
  Object.entries(ruleFieldErrors.value).flatMap(([path, message]) => {
    if (path.startsWith('appearance_overrides.')) {
      return [[path.slice('appearance_overrides.'.length), message]]
    }
    return path === 'accent_color' ? [['accent_color', message]] : []
  }),
))

function ruleErrorID(path: string) {
  return `rule-error-${path.replaceAll(/[^a-zA-Z0-9_-]/g, '-')}`
}

function localizationLabel(field: LocalizationField) {
  return t(`projects.editor.content.${localizationLabelKeys[field]}`)
}

function localizationMaximum(field: LocalizationField) {
  if (field.startsWith('privacy_')) return 1000
  if (field === 'prompt') return 500
  if (field === 'comment_placeholder' || field === 'consent_label') return 240
  return 120
}

function localizationRequired(field: LocalizationField) {
  return !field.startsWith('privacy_') || ruleForm.collect_contact
}

function ruleFieldError(path: string) {
  return ruleFieldErrors.value[path] || ''
}

function ruleFieldAttributes(path: string) {
  const error = ruleFieldError(path)
  return {
    'data-field-path': path,
    'aria-invalid': error ? true : undefined,
    'aria-describedby': error ? ruleErrorID(path) : undefined,
  }
}

function clearRuleField(path: string) {
  const paths = path === 'appearance_overrides.accent_color'
    ? [path, 'accent_color']
    : [path]
  if (!paths.some(candidate => ruleFieldErrors.value[candidate])) return
  const next = { ...ruleFieldErrors.value }
  paths.forEach(candidate => delete next[candidate])
  ruleFieldErrors.value = next
}

function clearRuleFieldFromEvent(event: Event) {
  const target = event.target
  if (!(target instanceof HTMLElement)) return
  const path = target.dataset.fieldPath
  if (path) clearRuleField(path)
}

function clearRuleValidation() {
  ruleFormError.value = ''
  ruleFieldErrors.value = {}
}

async function focusRuleField(path: string, fallbackToSummary = true) {
  const locale = /^localizations\.([^.]+)\./.exec(path)?.[1]
  if (locale && ruleForm.localizations[locale]) editingLocale.value = locale
  await nextTick()
  const elements = ruleFormElement.value?.querySelectorAll<HTMLElement>('[data-field-path]')
  const target = Array.from(elements || []).find(element =>
    element.dataset.fieldPath === path
      || (path === 'accent_color' && element.dataset.fieldPath === 'appearance_overrides.accent_color'))
  if (!target) {
    if (fallbackToSummary) ruleValidationSummary.value?.focus()
    return false
  }
  target.scrollIntoView({ block: 'center', behavior: 'smooth' })
  target.focus({ preventScroll: true })
  return true
}

async function focusFirstRuleField() {
  for (const path of Object.keys(ruleFieldErrors.value)) {
    if (await focusRuleField(path, false)) return
  }
  ruleValidationSummary.value?.focus()
}

function ruleStatus(rule: WidgetRule) {
  return rule.definition_status || (rule.enabled ? 'active' : 'paused')
}

function ruleSummary(rule: WidgetRule) {
  return t('projects.rules.summary', {
    status: t(`projects.enums.definitionStatus.${ruleStatus(rule)}`),
    matcher: t(`projects.enums.matcher.${rule.matcher_type}`),
    priority: integer(rule.priority),
  })
}

function responseBlockLabel(type: WidgetResponseBlock['type']) {
  return t(`projects.enums.responseBlock.${type}`)
}

function testResultMessage() {
  return targetingDiagnostic.value ? targetingTestResultMessage(t, targetingDiagnostic.value.result) : ''
}

function selectRule(rule: WidgetRule) {
  clearRuleValidation()
  selectedRuleID.value = rule.id
  editingNew.value = false
  targetingState.value = targetingFromRule(rule)
  targetingDiagnostic.value = null
  showTargetingErrors.value = false
  const locale = data.value?.project.default_locale || 'en'
  const localizations = cloneReactive(rule.localizations || {})
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
    replay_record_canvas: false,
    replay_canvas_fps: 2,
    replay_consent: cloneReactive(rule.replay_consent || defaultWidgetRule().replay_consent),
    analytics_consent: cloneReactive(rule.analytics_consent || defaultWidgetRule().analytics_consent),
    recurrence_mode: rule.recurrence_mode || 'cooldown',
    recurrence_days: (rule.recurrence_mode || 'cooldown') === 'cooldown' ? rule.recurrence_days ?? 30 : null,
    definition_key: rule.definition_key || `widget-${rule.id.replaceAll('-', '').slice(0, 12)}`,
    definition_status: rule.definition_status || (rule.enabled ? 'active' : 'paused'),
    presentation: rule.presentation || 'prompt', objective: rule.objective || 'continuous_feedback',
    response_blocks: rule.response_blocks || legacyResponseBlocks(rule), localizations,
    allow_comment: rule.allow_comment || false, collect_contact: rule.collect_contact || false,
    ai_processing: cloneReactive(rule.ai_processing || defaultWidgetRule().ai_processing),
  })
  editingLocale.value = locale
}

function startNewRule() {
  clearRuleValidation()
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
    // Input masking is an anonymity-safe default enforced by the backend and widget
    // regardless of this value; kept true here so the form never suggests it is optional.
    replay_mask_all_inputs: true,
    replay_mask_text_selector: ruleForm.replay_mask_text_selector?.trim() || null,
    replay_record_canvas: false,
    replay_canvas_fps: 2,
    recurrence_days: ruleForm.recurrence_mode === 'cooldown' ? Number(ruleForm.recurrence_days || 30) : null,
    definition_key: ruleForm.definition_key.trim().toLowerCase(),
    definition_status: ruleForm.definition_status,
    appearance_overrides: cleanAppearanceOverrides(ruleAppearance.value, ruleAppearance.value.logo_url === ''),
    response_blocks: responseBlocks,
    localizations: { ...ruleForm.localizations, [locale]: localized },
  }
}

function onReplayModeChange(event: Event) {
  const target = event.target
  if (!(target instanceof HTMLSelectElement)) return
  const mode = target.value as ReplayMode
  ruleForm.replay_mode = mode
  if (mode === 'none') {
    ruleForm.replay_consent = cloneReactive(defaultWidgetRule().replay_consent)
    replayConsentDialogOpen.value = false
    return
  }
  if (ruleForm.replay_consent.mode === 'none') replayConsentDialogOpen.value = true
}

function cancelReplayConsent() {
  replayConsentDialogOpen.value = false
  if (ruleForm.replay_consent.mode === 'none') ruleForm.replay_mode = 'none'
}

function confirmReplayConsent(configuration: ReplayConsentConfig) {
  ruleForm.replay_consent = cloneReactive(configuration)
  replayConsentDialogOpen.value = false
}

function onAIProcessingChange(event: Event) {
  const target = event.target
  if (!(target instanceof HTMLInputElement)) return
  if (!target.checked) {
    ruleForm.ai_processing = { enabled: false }
    aiProcessingDialogOpen.value = false
    return
  }
  aiProcessingDialogOpen.value = true
}

function cancelAIProcessing() {
  aiProcessingDialogOpen.value = false
}

function confirmAIProcessing(configuration: AIProcessingConfig) {
  ruleForm.ai_processing = cloneReactive(configuration)
  aiProcessingDialogOpen.value = false
}

function cancelAnalyticsConsent() {
  analyticsConsentDialogOpen.value = false
}

function confirmAnalyticsConsent(configuration: AnalyticsConsentConfig) {
  ruleForm.analytics_consent = cloneReactive(configuration)
  analyticsConsentDialogOpen.value = false
}

function fallbackLocalization(prompt: string): WidgetLocalization {
  return {
    launcher_label: 'Feedback', prompt, comment_placeholder: 'Tell us more',
    contact_label: 'Email address', consent_label: 'Email me about this feedback',
    submit_label: 'Send feedback', success_message: 'Your feedback was sent.',
    privacy_purpose_summary: '', privacy_lawful_basis_summary: '',
    privacy_retention_summary: '',
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
const controllerProfileReady = computed(() => Boolean(
  tenant.value?.name &&
  tenant.value.privacy_profile_version &&
  tenant.value.privacy_contact_email &&
  tenant.value.privacy_rights_email &&
  tenant.value.privacy_policy_url,
))
const previewControllerNoticeReady = computed(() => Boolean(
  controllerProfileReady.value &&
  previewLocalization.value.privacy_purpose_summary?.trim() &&
  previewLocalization.value.privacy_lawful_basis_summary?.trim() &&
  previewLocalization.value.privacy_retention_summary?.trim(),
))

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
    formError.value = t('projects.editor.content.invalidLocale')
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
  clearRuleValidation()
  showTargetingErrors.value = true
  const targetingErrors = validateTargeting(targetingState.value)
  if (Object.keys(targetingErrors).length > 0) {
    formError.value = t(targetingErrorCatalogKey(Object.values(targetingErrors)[0] || ''))
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
    notice.value = t('projects.editor.saved')
    await refresh()
  } catch (saveError) {
    ruleFieldErrors.value = apiFieldErrors(saveError, localizedFieldErrors(saveError))
    if (Object.keys(ruleFieldErrors.value).length) {
      await focusFirstRuleField()
    } else {
      ruleFormError.value = apiErrorMessage(saveError, 'projects.editor.errors.save')
    }
  } finally {
    pending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

function deleteRule() {
  if (!canWrite('widget_rule:delete') || !selectedRule.value || pending.value) return
  destructiveError.value = ''
  destructiveAction.value = {
    kind: 'rule',
    id: selectedRule.value.id,
    name: selectedRule.value.name,
  }
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
    notice.value = t('projects.settings.saved')
    await refresh()
  } catch (saveError) {
    formError.value = apiErrorMessage(saveError, 'projects.settings.errors.save')
  } finally {
    pending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

function deleteProject() {
  if (!canWrite('project:delete') || !data.value || pending.value) return
  destructiveError.value = ''
  destructiveAction.value = {
    kind: 'project',
    id: data.value.project.id,
    name: data.value.project.name,
  }
}

function closeDestructiveAction() {
  if (destructivePending.value) return
  destructiveAction.value = null
  destructiveError.value = ''
}

async function confirmDestructiveAction() {
  const action = destructiveAction.value
  if (!action || destructivePending.value) return
  const permission = action.kind === 'rule' ? 'widget_rule:delete' : 'project:delete'
  if (!canWrite(permission)) return
  destructivePending.value = true
  destructiveError.value = ''
  try {
    const path = action.kind === 'rule'
      ? `/v1/projects/${projectID.value}/widget-rules/${action.id}`
      : `/v1/projects/${projectID.value}`
    await $apiFetch(path, { method: 'DELETE' })
    destructiveAction.value = null
    await nextTick()
    if (action.kind === 'rule') {
      selectedRuleID.value = ''
      editingNew.value = false
      await refresh()
    } else {
      await navigateTo('/projects')
    }
  } catch (deleteError) {
    destructiveError.value = apiErrorMessage(
      deleteError,
      action.kind === 'rule'
        ? 'projects.editor.errors.delete'
        : 'projects.settings.errors.delete',
    )
  } finally {
    destructivePending.value = false
  }
}

async function copyEmbed() {
  try {
    await navigator.clipboard.writeText(embedCode.value)
    copied.value = true
    window.setTimeout(() => { copied.value = false }, 1800)
  } catch {
    formError.value = t('projects.detail.copyFailed')
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

useHead(() => ({
  title: t('projects.detail.pageTitle', { name: data.value?.project.name || t('projects.detail.fallbackName') }),
}))
</script>

<template>
  <div class="project-workspace">
    <header class="project-workspace-head">
      <NuxtLink to="/projects" class="app-button"><ArrowLeft :size="15" /> {{ t('projects.detail.back') }}</NuxtLink>
      <div v-if="data"><span class="project-workspace-mark">{{ data.project.name.slice(0, 1).toUpperCase() }}</span><p><b>{{ data.project.name }}</b><span>{{ data.project.allowed_domains.join(', ') || t('projects.detail.noDomains') }}</span></p></div>
    </header>
    <section v-if="status === 'pending'" class="app-state"><div><span class="app-spinner" /><h2>{{ t('projects.detail.loading') }}</h2></div></section>
    <section v-else-if="error" class="app-state"><div><AlertTriangle :size="24" /><h2>{{ t('projects.detail.unavailable') }}</h2><p role="alert">{{ apiErrorMessage(error, 'projects.detail.errors.load') }}</p></div></section>
    <template v-else-if="data">
      <section class="project-config-band">
        <div><p class="app-eyebrow">{{ t('projects.detail.eyebrow') }}</p><h1>{{ data.project.name }}</h1><span>{{ plural('projects.detail.ruleCount', data.rules.length) }}</span></div>
        <div class="project-embed"><Code2 :size="15" /><code>{{ embedCode }}</code><button :title="t('projects.detail.copyEmbed')" @click="copyEmbed"><Check v-if="copied" :size="15" /><Clipboard v-else :size="15" /></button></div>
      </section>
      <section class="project-install-diagnostics" :aria-label="t('projects.diagnostics.label')">
        <header><span>{{ t('projects.diagnostics.title') }}</span><button type="button" :title="t('projects.diagnostics.refresh')" :aria-label="t('projects.diagnostics.refresh')" :disabled="diagnosticsRefreshing" @click="refreshDiagnostics"><RefreshCw :size="14" :class="{ spinning: diagnosticsRefreshing }" /></button></header>
        <dl>
          <div :data-status="connectedAt ? 'ready' : 'attention'"><dt><Radio :size="13" /> {{ t('projects.diagnostics.loader') }}</dt><dd>{{ connectedAt ? t('projects.diagnostics.observed', { date: formatDateTime(connectedAt) }) : t('projects.diagnostics.notDetected') }}</dd></div>
          <div :data-status="data.project.allowed_domains.length ? 'ready' : 'attention'"><dt><Globe2 :size="13" /> {{ t('projects.diagnostics.domains') }}</dt><dd>{{ data.project.allowed_domains.length ? t('projects.diagnostics.domainsConfigured', { count: integer(data.project.allowed_domains.length) }) : t('projects.diagnostics.noDomainsConfigured') }}</dd></div>
          <div :data-status="activeDefinitionCount ? 'ready' : 'attention'"><dt><CircleCheck :size="13" /> {{ t('projects.diagnostics.definitions') }}</dt><dd>{{ t('projects.diagnostics.definitionsActive', { count: integer(activeDefinitionCount) }) }}</dd></div>
          <div :data-status="targetingDiagnostic?.result.status === 'eligible' ? 'ready' : targetingDiagnostic ? 'blocked' : 'attention'"><dt><CircleCheck v-if="targetingDiagnostic?.result.status === 'eligible'" :size="13" /><CircleX v-else :size="13" /> {{ t('projects.diagnostics.urlTest') }}</dt><dd>{{ targetingDiagnostic ? testResultMessage() : t('projects.diagnostics.notTested') }}</dd></div>
        </dl>
      </section>
      <p v-if="formError" class="project-config-error" role="alert"><AlertTriangle :size="15" /> {{ formError }}</p>
      <div class="project-config-grid">
        <aside class="project-rule-list">
          <header><span>{{ t('projects.rules.title') }}</span><div><button v-if="data.rules.some(rule => rule.definition_status === 'archived')" :title="showArchivedRules ? t('projects.rules.hideArchived') : t('projects.rules.showArchived')" :aria-pressed="showArchivedRules" @click="showArchivedRules = !showArchivedRules"><Archive :size="15" /></button><button v-if="canWrite('widget_rule:create')" :title="t('projects.rules.new')" @click="startNewRule"><Plus :size="16" /></button></div></header>
          <button v-for="rule in visibleRules" :key="rule.id" :data-active="rule.id === selectedRuleID" @click="selectRule(rule)"><span :data-status="ruleStatus(rule)" /><p><b>{{ rule.name }}</b><small>{{ ruleSummary(rule) }}</small></p></button>
          <p v-if="!visibleRules.length" class="project-rule-empty">{{ t('projects.rules.empty') }}</p>
          <nav><a href="#project-settings"><Settings2 :size="14" /> {{ t('projects.rules.settingsLink') }}</a></nav>
        </aside>

        <main class="project-rule-editor">
          <header><div><p class="app-eyebrow">{{ editingNew ? t('projects.editor.newRule') : t('projects.editor.editingRule') }}</p><h2>{{ ruleForm.name || t('projects.editor.untitled') }}</h2></div><div><button v-if="selectedRule && canWrite('widget_rule:delete')" class="app-button project-delete-button" :disabled="pending" :title="t('projects.editor.delete')" :aria-label="t('projects.editor.delete')" @click="deleteRule"><Trash2 :size="14" /></button><button class="app-button app-button--primary" type="submit" form="widget-rule-form" :disabled="pending || !(editingNew ? canWrite('widget_rule:create') : canWrite('widget_rule:update'))"><Save :size="14" /> {{ pending ? t('projects.editor.saving') : t('projects.editor.save') }}</button></div></header>
          <form
            id="widget-rule-form"
            ref="ruleFormElement"
            @submit.prevent="saveRule"
            @input="clearRuleFieldFromEvent"
            @change="clearRuleFieldFromEvent"
          >
            <section
              v-if="ruleValidationEntries.length || ruleFormError"
              ref="ruleValidationSummary"
              class="rule-validation-summary"
              role="alert"
              tabindex="-1"
            >
              <h3>{{ t('projects.editor.validation.title') }}</h3>
              <p v-if="ruleFormError">{{ ruleFormError }}</p>
              <ul v-else>
                <li v-for="[path, message] in ruleValidationEntries" :key="path">
                  <button type="button" @click="focusRuleField(path)">
                    <code>{{ path }}</code>
                    <span>{{ message }}</span>
                  </button>
                </li>
              </ul>
            </section>
            <fieldset :disabled="!workspaceWritable">
            <section>
              <h3>{{ t('projects.editor.identity.title') }}</h3>
              <div class="project-form-grid">
                <label>
                  {{ t('projects.editor.identity.name') }}
                  <input v-model.trim="ruleForm.name" class="app-input" required maxlength="120" v-bind="ruleFieldAttributes('name')" />
                  <small v-if="ruleFieldError('name')" :id="ruleErrorID('name')" class="rule-field-error">{{ ruleFieldError('name') }}</small>
                </label>
                <label>
                  {{ t('projects.editor.identity.definitionKey') }}
                  <input v-model.trim="ruleForm.definition_key" class="app-input" required maxlength="64" pattern="[a-z][a-z0-9-]{1,62}[a-z0-9]" v-bind="ruleFieldAttributes('definition_key')" />
                  <small v-if="ruleFieldError('definition_key')" :id="ruleErrorID('definition_key')" class="rule-field-error">{{ ruleFieldError('definition_key') }}</small>
                </label>
                <label>
                  {{ t('projects.editor.identity.lifecycleStatus') }}
                  <select v-model="ruleForm.definition_status" class="app-select" v-bind="ruleFieldAttributes('definition_status')">
                    <option value="draft">{{ t('projects.enums.definitionStatus.draft') }}</option>
                    <option value="active">{{ t('projects.enums.definitionStatus.active') }}</option>
                    <option value="paused">{{ t('projects.enums.definitionStatus.paused') }}</option>
                    <option value="archived">{{ t('projects.enums.definitionStatus.archived') }}</option>
                  </select>
                  <small v-if="ruleFieldError('definition_status')" :id="ruleErrorID('definition_status')" class="rule-field-error">{{ ruleFieldError('definition_status') }}</small>
                </label>
                <label>
                  {{ t('projects.editor.identity.presentation') }}
                  <select v-model="ruleForm.presentation" class="app-select" v-bind="ruleFieldAttributes('presentation')">
                    <option value="launcher">{{ t('projects.enums.presentation.launcher') }}</option>
                    <option value="inline">{{ t('projects.enums.presentation.inline') }}</option>
                    <option value="reaction">{{ t('projects.enums.presentation.reaction') }}</option>
                    <option value="prompt">{{ t('projects.enums.presentation.prompt') }}</option>
                  </select>
                  <small v-if="ruleFieldError('presentation')" :id="ruleErrorID('presentation')" class="rule-field-error">{{ ruleFieldError('presentation') }}</small>
                </label>
                <label>
                  {{ t('projects.editor.identity.objective') }}
                  <select v-model="ruleForm.objective" class="app-select" v-bind="ruleFieldAttributes('objective')">
                    <option value="continuous_feedback">{{ t('projects.enums.objective.continuous_feedback') }}</option>
                    <option value="after_engagement">{{ t('projects.enums.objective.after_engagement') }}</option>
                    <option value="validate_content">{{ t('projects.enums.objective.validate_content') }}</option>
                    <option value="detect_friction">{{ t('projects.enums.objective.detect_friction') }}</option>
                  </select>
                  <small v-if="ruleFieldError('objective')" :id="ruleErrorID('objective')" class="rule-field-error">{{ ruleFieldError('objective') }}</small>
                </label>
                <label>{{ t('projects.editor.identity.priority') }}<input v-model.number="ruleForm.priority" class="app-input" type="number" /></label>
              </div>
            </section>
            <section>
              <h3>{{ t('projects.editor.pageMatching') }}</h3>
              <WidgetTargetingEditor
                v-model="targetingState"
                :show-errors="showTargetingErrors"
                :server-errors="ruleFieldErrors"
                @field-edited="clearRuleField"
                @tested="targetingDiagnostic = $event"
              />
            </section>
            <section>
              <h3>{{ t('projects.editor.trigger.title') }}</h3>
              <div class="project-form-grid">
                <label>
                  {{ t('projects.editor.trigger.type') }}
                  <select v-model="ruleForm.trigger_type" class="app-select" v-bind="ruleFieldAttributes('trigger_type')">
                    <option value="delay">{{ t('projects.enums.trigger.delay') }}</option>
                    <option value="scroll">{{ t('projects.enums.trigger.scroll') }}</option>
                    <option value="exit_intent">{{ t('projects.enums.trigger.exit_intent') }}</option>
                  </select>
                  <small v-if="ruleFieldError('trigger_type')" :id="ruleErrorID('trigger_type')" class="rule-field-error">{{ ruleFieldError('trigger_type') }}</small>
                </label>
                <label v-if="ruleForm.trigger_type === 'delay'">
                  {{ t('projects.editor.trigger.delaySeconds') }}
                  <input v-model.number="ruleForm.trigger_delay_seconds" class="app-input" type="number" min="0" max="86400" v-bind="ruleFieldAttributes('trigger_delay_seconds')" />
                  <small v-if="ruleFieldError('trigger_delay_seconds')" :id="ruleErrorID('trigger_delay_seconds')" class="rule-field-error">{{ ruleFieldError('trigger_delay_seconds') }}</small>
                </label>
                <label v-if="ruleForm.trigger_type === 'scroll'">
                  {{ t('projects.editor.trigger.scrollPercent') }}
                  <input v-model.number="ruleForm.trigger_scroll_percent" class="app-input" type="number" min="1" max="100" v-bind="ruleFieldAttributes('trigger_scroll_percent')" />
                  <small v-if="ruleFieldError('trigger_scroll_percent')" :id="ruleErrorID('trigger_scroll_percent')" class="rule-field-error">{{ ruleFieldError('trigger_scroll_percent') }}</small>
                </label>
              </div>
            </section>
            <section>
              <h3>{{ t('projects.editor.recurrence.title') }}</h3>
              <div class="project-form-grid">
                <label>
                  {{ t('projects.editor.recurrence.showAgain') }}
                  <select v-model="ruleForm.recurrence_mode" class="app-select" v-bind="ruleFieldAttributes('recurrence_mode')">
                    <option value="cooldown">{{ t('projects.enums.recurrence.cooldown') }}</option>
                    <option value="session">{{ t('projects.enums.recurrence.session') }}</option>
                    <option value="never">{{ t('projects.enums.recurrence.never') }}</option>
                  </select>
                  <small v-if="ruleFieldError('recurrence_mode')" :id="ruleErrorID('recurrence_mode')" class="rule-field-error">{{ ruleFieldError('recurrence_mode') }}</small>
                </label>
                <label v-if="ruleForm.recurrence_mode === 'cooldown'">
                  {{ t('projects.editor.recurrence.cooldownDays') }}
                  <input v-model.number="ruleForm.recurrence_days" class="app-input" type="number" min="1" max="3650" required v-bind="ruleFieldAttributes('recurrence_days')" />
                  <small v-if="ruleFieldError('recurrence_days')" :id="ruleErrorID('recurrence_days')" class="rule-field-error">{{ ruleFieldError('recurrence_days') }}</small>
                </label>
              </div>
            </section>
            <section><WidgetDecisionDiagnostics :objective="ruleForm.objective" :presentation="ruleForm.presentation" :trigger-type="ruleForm.trigger_type" :trigger-delay-seconds="ruleForm.trigger_delay_seconds ?? null" :trigger-scroll-percent="ruleForm.trigger_scroll_percent ?? null" :recurrence-mode="ruleForm.recurrence_mode" :recurrence-days="ruleForm.recurrence_days ?? null" /></section>
            <section>
              <h3>{{ t('projects.editor.methods.title') }}</h3>
              <div class="project-capability-grid">
                <label><input v-model="ruleForm.can_rate" type="checkbox" /><span><b>{{ t('projects.editor.methods.rating') }}</b><small>{{ t('projects.editor.methods.ratingHint') }}</small></span></label>
                <label><input v-model="ruleForm.can_write_text" type="checkbox" /><span><b>{{ t('projects.editor.methods.writtenFeedback') }}</b><small>{{ t('projects.editor.methods.writtenFeedbackHint') }}</small></span></label>
                <label><input type="checkbox" :checked="hasResponseType('reaction')" :disabled="!hasResponseType('reaction') && ruleForm.response_blocks.length >= 4" @change="setResponseType('reaction', !hasResponseType('reaction'))" /><span><b>{{ t('projects.editor.methods.reaction') }}</b><small>{{ t('projects.editor.methods.reactionHint') }}</small></span></label>
                <label><input type="checkbox" :checked="hasResponseType('single_choice')" :disabled="!hasResponseType('single_choice') && ruleForm.response_blocks.length >= 4" @change="setResponseType('single_choice', !hasResponseType('single_choice'))" /><span><b>{{ t('projects.editor.methods.singleChoice') }}</b><small>{{ t('projects.editor.methods.singleChoiceHint') }}</small></span></label>
                <label><input v-model="ruleForm.allow_comment" type="checkbox" /><span><b>{{ t('projects.editor.methods.comment') }}</b><small>{{ t('projects.editor.methods.commentHint') }}</small></span></label>
                <label><input v-model="ruleForm.collect_contact" type="checkbox" /><span><b>{{ t('projects.editor.methods.contact') }}</b><small>{{ t('projects.editor.methods.contactHint') }}</small></span></label>
                <label :data-locked="!supportsAudio">
                  <input v-model="ruleForm.can_record_audio" type="checkbox" :disabled="!supportsAudio" v-bind="ruleFieldAttributes('can_record_audio')" />
                  <span><b>{{ t('projects.editor.methods.audio') }}</b><small :id="ruleFieldError('can_record_audio') ? ruleErrorID('can_record_audio') : undefined" :class="{ 'rule-field-error': ruleFieldError('can_record_audio') }">{{ ruleFieldError('can_record_audio') || (supportsAudio ? t('projects.editor.methods.audioHint') : t('projects.editor.methods.upgradeRequired')) }}</small></span>
                </label>
                <label :data-locked="!supportsVideo">
                  <input v-model="ruleForm.can_record_video" type="checkbox" :disabled="!supportsVideo" v-bind="ruleFieldAttributes('can_record_video')" />
                  <span><b>{{ t('projects.editor.methods.video') }}</b><small :id="ruleFieldError('can_record_video') ? ruleErrorID('can_record_video') : undefined" :class="{ 'rule-field-error': ruleFieldError('can_record_video') }">{{ ruleFieldError('can_record_video') || (supportsVideo ? t('projects.editor.methods.videoHint') : t('projects.editor.methods.scaleRequired')) }}</small></span>
                </label>
                <label :data-locked="!supportsDOM">
                  <input v-model="ruleForm.can_attach_element" type="checkbox" :disabled="!supportsDOM" v-bind="ruleFieldAttributes('can_attach_element')" />
                  <span><b>{{ t('projects.editor.methods.element') }}</b><small :id="ruleFieldError('can_attach_element') ? ruleErrorID('can_attach_element') : undefined" :class="{ 'rule-field-error': ruleFieldError('can_attach_element') }">{{ ruleFieldError('can_attach_element') || (supportsDOM ? t('projects.editor.methods.elementHint') : t('projects.editor.methods.upgradeRequired')) }}</small></span>
                </label>
                <label :data-locked="!supportsDOM">
                  <input v-model="ruleForm.can_attach_screenshot" type="checkbox" :disabled="!supportsDOM" v-bind="ruleFieldAttributes('can_attach_screenshot')" />
                  <span><b>{{ t('projects.editor.methods.screenshot') }}</b><small :id="ruleFieldError('can_attach_screenshot') ? ruleErrorID('can_attach_screenshot') : undefined" :class="{ 'rule-field-error': ruleFieldError('can_attach_screenshot') }">{{ ruleFieldError('can_attach_screenshot') || (supportsDOM ? t('projects.editor.methods.screenshotHint') : t('projects.editor.methods.upgradeRequired')) }}</small></span>
                </label>
              </div>
              <div v-if="ruleForm.collect_contact && !controllerProfileReady" class="contact-controller-warning" role="note">
                <AlertTriangle :size="16" aria-hidden="true" />
                <p>
                  <b>{{ t('projects.editor.methods.controllerMissingTitle') }}</b>
                  <span>{{ t('projects.editor.methods.controllerMissingDescription') }}</span>
                </p>
                <NuxtLink class="app-button" to="/settings#organization-profile">
                  {{ t('projects.editor.methods.configureController') }}
                </NuxtLink>
              </div>
            </section>
            <section>
              <h3>{{ t('projects.editor.ai.title') }}</h3>
              <div class="ai-processing-setting" :data-enabled="ruleForm.ai_processing.enabled">
                <label :data-locked="!supportsAI && !ruleForm.ai_processing.enabled">
                  <input
                    type="checkbox"
                    :checked="ruleForm.ai_processing.enabled"
                    :disabled="!supportsAI && !ruleForm.ai_processing.enabled"
                    v-bind="ruleFieldAttributes('ai_processing.enabled')"
                    @change="onAIProcessingChange"
                  />
                  <span><b>{{ t('projects.editor.ai.toggle') }}</b><small>{{ supportsAI || ruleForm.ai_processing.enabled ? t('projects.editor.ai.toggleHint') : t('projects.editor.methods.upgradeRequired') }}</small></span>
                </label>
                <button v-if="ruleForm.ai_processing.enabled && supportsAI" class="app-button" type="button" @click="aiProcessingDialogOpen = true">
                  <ShieldCheck :size="14" /> {{ t('projects.editor.ai.review') }}
                </button>
              </div>
              <small v-if="ruleFieldError('ai_processing.enabled')" :id="ruleErrorID('ai_processing.enabled')" class="rule-field-error">{{ ruleFieldError('ai_processing.enabled') }}</small>
              <small v-if="ruleFieldError('ai_processing.acknowledged')" :id="ruleErrorID('ai_processing.acknowledged')" class="rule-field-error">{{ ruleFieldError('ai_processing.acknowledged') }}</small>
            </section>
            <section>
              <h3>{{ t('projects.editor.blocks.title') }}</h3>
              <div
                class="response-block-list"
                tabindex="-1"
                v-bind="ruleFieldAttributes('response_blocks')"
              >
                <small v-if="ruleFieldError('response_blocks')" :id="ruleErrorID('response_blocks')" class="rule-field-error">{{ ruleFieldError('response_blocks') }}</small>
                <div v-for="(block, blockIndex) in ruleForm.response_blocks" :key="block.type" class="response-block-row">
                  <div class="response-block-head">
                    <span
                      tabindex="-1"
                      v-bind="ruleFieldAttributes(`response_blocks[${blockIndex}].type`)"
                    >{{ responseBlockLabel(block.type) }}</span>
                    <label><input v-model="block.required" type="checkbox" /> {{ t('projects.editor.blocks.required') }}</label>
                    <button type="button" :title="t('projects.editor.blocks.remove')" :aria-label="t('projects.editor.blocks.removeNamed', { type: responseBlockLabel(block.type) })" @click="removeResponseBlock(block.type)"><Trash2 :size="13" /></button>
                  </div>
                  <small v-if="ruleFieldError(`response_blocks[${blockIndex}].type`)" :id="ruleErrorID(`response_blocks[${blockIndex}].type`)" class="rule-field-error">{{ ruleFieldError(`response_blocks[${blockIndex}].type`) }}</small>
                  <div class="project-form-grid">
                    <label>
                      {{ t('projects.editor.blocks.key') }}
                      <input v-model.trim="block.key" class="app-input" required maxlength="32" pattern="[a-z0-9_]{1,32}" v-bind="ruleFieldAttributes(`response_blocks[${blockIndex}].key`)" />
                      <small v-if="ruleFieldError(`response_blocks[${blockIndex}].key`)" :id="ruleErrorID(`response_blocks[${blockIndex}].key`)" class="rule-field-error">{{ ruleFieldError(`response_blocks[${blockIndex}].key`) }}</small>
                    </label>
                    <label>
                      {{ t('projects.editor.blocks.label', { locale: editingLocale }) }}
                      <input
                        class="app-input"
                        maxlength="240"
                        :value="editingLocalization.block_labels?.[block.key] || ''"
                        v-bind="ruleFieldAttributes(`localizations.${editingLocale}.block_labels.${block.key}`)"
                        @input="setBlockLabel(block.key, ($event.target as HTMLInputElement).value)"
                      />
                      <small v-if="ruleFieldError(`localizations.${editingLocale}.block_labels.${block.key}`)" :id="ruleErrorID(`localizations.${editingLocale}.block_labels.${block.key}`)" class="rule-field-error">{{ ruleFieldError(`localizations.${editingLocale}.block_labels.${block.key}`) }}</small>
                    </label>
                  </div>
                  <div
                    v-if="block.options"
                    class="response-option-list"
                    tabindex="-1"
                    v-bind="ruleFieldAttributes(`response_blocks[${blockIndex}].options`)"
                  >
                    <small v-if="ruleFieldError(`response_blocks[${blockIndex}].options`)" :id="ruleErrorID(`response_blocks[${blockIndex}].options`)" class="rule-field-error">{{ ruleFieldError(`response_blocks[${blockIndex}].options`) }}</small>
                    <div v-for="(option, optionIndex) in block.options" :key="optionIndex">
                      <input
                        v-model.trim="option.value"
                        class="app-input"
                        required
                        maxlength="32"
                        pattern="[a-z0-9_]{1,32}"
                        :aria-label="t('projects.editor.blocks.optionKey')"
                        v-bind="ruleFieldAttributes(`response_blocks[${blockIndex}].options[${optionIndex}].value`)"
                      />
                      <input
                        class="app-input"
                        maxlength="120"
                        :aria-label="t('projects.editor.blocks.optionLabel')"
                        :value="editingLocalization.option_labels?.[block.key]?.[option.value] || ''"
                        v-bind="ruleFieldAttributes(`localizations.${editingLocale}.option_labels.${block.key}.${option.value}`)"
                        @input="setOptionLabel(block.key, option.value, ($event.target as HTMLInputElement).value)"
                      />
                      <button type="button" :title="t('projects.editor.blocks.removeOption')" :aria-label="t('projects.editor.blocks.removeOption')" :disabled="block.options.length <= 2" @click="removeResponseOption(block, optionIndex)"><Trash2 :size="13" /></button>
                      <small v-if="ruleFieldError(`response_blocks[${blockIndex}].options[${optionIndex}].value`)" :id="ruleErrorID(`response_blocks[${blockIndex}].options[${optionIndex}].value`)" class="rule-field-error">{{ ruleFieldError(`response_blocks[${blockIndex}].options[${optionIndex}].value`) }}</small>
                      <small v-if="ruleFieldError(`localizations.${editingLocale}.option_labels.${block.key}.${option.value}`)" :id="ruleErrorID(`localizations.${editingLocale}.option_labels.${block.key}.${option.value}`)" class="rule-field-error">{{ ruleFieldError(`localizations.${editingLocale}.option_labels.${block.key}.${option.value}`) }}</small>
                    </div>
                    <button type="button" class="app-button" :disabled="block.options.length >= (block.type === 'reaction' ? 5 : 10)" @click="addResponseOption(block)"><Plus :size="13" /> {{ t('projects.editor.blocks.addOption') }}</button>
                  </div>
                </div>
              </div>
            </section>
            <section>
              <h3>{{ t('projects.editor.content.title') }}</h3>
              <div class="localization-toolbar">
                <select v-model="editingLocale" class="app-select" :aria-label="t('projects.editor.content.editingLanguage')">
                  <option v-for="(_, locale) in ruleForm.localizations" :key="locale" :value="locale">{{ locale === projectDefaultLocale ? t('projects.editor.content.defaultLanguage', { locale }) : locale }}</option>
                </select>
                <input v-model.trim="newLocale" class="app-input" maxlength="32" :placeholder="t('projects.editor.content.newLanguagePlaceholder')" :aria-label="t('projects.editor.content.newLanguage')" />
                <button type="button" class="app-button" @click="addLocalization"><Plus :size="13" /> {{ t('projects.editor.content.add') }}</button>
                <button v-if="editingLocale !== projectDefaultLocale" type="button" class="app-icon-button" :title="t('projects.editor.content.removeLanguage')" :aria-label="t('projects.editor.content.removeLanguage')" @click="removeLocalization"><Trash2 :size="13" /></button>
              </div>
              <div class="project-form-grid localization-fields">
                <label v-for="field in localizationFields" :key="field" :class="{ wide: field === 'prompt' || field === 'consent_label' || field.startsWith('privacy_') }">
                  {{ localizationLabel(field) }}
                  <input
                    v-model="editingLocalization[field]"
                    class="app-input"
                    :required="localizationRequired(field)"
                    :maxlength="localizationMaximum(field)"
                    v-bind="ruleFieldAttributes(`localizations.${editingLocale}.${field}`)"
                  />
                  <small v-if="ruleFieldError(`localizations.${editingLocale}.${field}`)" :id="ruleErrorID(`localizations.${editingLocale}.${field}`)" class="rule-field-error">{{ ruleFieldError(`localizations.${editingLocale}.${field}`) }}</small>
                </label>
              </div>
              <WidgetAppearanceEditor
                v-model="ruleAppearance"
                :parent="projectEffectiveAppearance"
                :errors="ruleAppearanceErrors"
                field-path-prefix="appearance_overrides"
                allow-logo-reset
                @field-edited="clearRuleField"
              />
            </section>
            <section>
              <h3>{{ t('projects.editor.replay.title') }}</h3>
              <div class="project-form-grid">
                <label>
                  {{ t('projects.editor.replay.mode') }}
                  <select v-model="ruleForm.replay_mode" class="app-select" :disabled="!supportsReplay" v-bind="ruleFieldAttributes('replay_mode')" @change="onReplayModeChange">
                    <option value="none">{{ t('projects.enums.replay.none') }}</option>
                    <option value="full">{{ t('projects.enums.replay.full') }}</option>
                    <option value="sampling">{{ t('projects.enums.replay.sampling') }}</option>
                  </select>
                  <small v-if="ruleFieldError('replay_mode')" :id="ruleErrorID('replay_mode')" class="rule-field-error">{{ ruleFieldError('replay_mode') }}</small>
                </label>
                <div v-if="ruleForm.replay_mode !== 'none'" class="replay-consent-status wide" :data-configured="ruleForm.replay_consent.mode !== 'none'">
                  <div>
                    <ShieldCheck :size="16" aria-hidden="true" />
                    <p>
                      <b>{{ ruleForm.replay_consent.mode === 'none' ? t('projects.editor.replay.consent.requiredTitle') : t('projects.editor.replay.consent.configuredTitle') }}</b>
                      <span>{{ ruleForm.replay_consent.mode === 'none' ? t('projects.editor.replay.consent.requiredDescription') : t(`projects.editor.replay.consent.configuredDescription.${ruleForm.replay_consent.mode}`) }}</span>
                    </p>
                  </div>
                  <button class="app-button" type="button" @click="replayConsentDialogOpen = true">
                    <Settings2 :size="14" /> {{ t('projects.editor.replay.consent.configure') }}
                  </button>
                </div>
                <label v-if="ruleForm.replay_mode === 'sampling'">
                  {{ t('projects.editor.replay.samplingPercent') }}
                  <input v-model.number="ruleForm.replay_sampling_rate" class="app-input" type="number" min="1" max="100" v-bind="ruleFieldAttributes('replay_sampling_rate')" />
                  <small v-if="ruleFieldError('replay_sampling_rate')" :id="ruleErrorID('replay_sampling_rate')" class="rule-field-error">{{ ruleFieldError('replay_sampling_rate') }}</small>
                </label>
                <label>
                  {{ t('projects.editor.replay.maxDuration') }}
                  <input v-model.number="ruleForm.replay_max_duration_seconds" class="app-input" type="number" min="1" max="1800" v-bind="ruleFieldAttributes('replay_max_duration_seconds')" />
                  <small v-if="ruleFieldError('replay_max_duration_seconds')" :id="ruleErrorID('replay_max_duration_seconds')" class="rule-field-error">{{ ruleFieldError('replay_max_duration_seconds') }}</small>
                </label>
                <label>
                  {{ t('projects.editor.replay.maxEvents') }}
                  <input v-model.number="ruleForm.replay_max_events" class="app-input" type="number" min="1" max="50000" v-bind="ruleFieldAttributes('replay_max_events')" />
                  <small v-if="ruleFieldError('replay_max_events')" :id="ruleErrorID('replay_max_events')" class="rule-field-error">{{ ruleFieldError('replay_max_events') }}</small>
                </label>
                <label class="project-toggle wide">
                  <input :checked="true" type="checkbox" disabled />
                  <span><b>{{ t('projects.editor.replay.maskAllInputs') }}</b><small>{{ t('projects.editor.replay.maskAllInputsHint') }}</small></span>
                </label>
                <label class="wide">
                  {{ t('projects.editor.replay.maskSelector') }}
                  <input v-model="ruleForm.replay_mask_text_selector" class="app-input" placeholder=".private, [data-sensitive]" disabled v-bind="ruleFieldAttributes('replay_mask_text_selector')" />
                  <small>{{ t('projects.editor.replay.maskSelectorHint') }}</small>
                  <small v-if="ruleFieldError('replay_mask_text_selector')" :id="ruleErrorID('replay_mask_text_selector')" class="rule-field-error">{{ ruleFieldError('replay_mask_text_selector') }}</small>
                </label>
                <div class="replay-privacy-note wide" role="note">
                  <ShieldCheck :size="16" aria-hidden="true" />
                  <p><b>{{ t('projects.editor.replay.anonymityTitle') }}</b><span>{{ t('projects.editor.replay.anonymityDescription') }}</span></p>
                </div>
                <label class="project-toggle wide">
                  <input v-model="ruleForm.replay_record_canvas" type="checkbox" disabled />
                  <span><b>{{ t('projects.editor.replay.captureCanvas') }}</b><small>{{ t('projects.editor.replay.captureCanvasHint') }}</small></span>
                </label>
                <div class="replay-privacy-note wide" role="note">
                  <ShieldCheck :size="16" aria-hidden="true" />
                  <p><b>{{ t('projects.editor.replay.canvasPrivacyTitle') }}</b><span>{{ t('projects.editor.replay.canvasPrivacyDescription') }}</span></p>
                </div>
              </div>
            </section>
            <section>
              <h3>{{ t('projects.editor.analytics.title') }}</h3>
              <div class="project-form-grid">
                <div class="replay-consent-status wide" :data-configured="ruleForm.analytics_consent.mode !== 'none'">
                  <div>
                    <ShieldCheck :size="16" aria-hidden="true" />
                    <p>
                      <b>{{ ruleForm.analytics_consent.mode === 'none' ? t('projects.editor.analytics.consent.requiredTitle') : t('projects.editor.analytics.consent.configuredTitle') }}</b>
                      <span>{{ ruleForm.analytics_consent.mode === 'none' ? t('projects.editor.analytics.consent.requiredDescription') : t(`projects.editor.analytics.consent.configuredDescription.${ruleForm.analytics_consent.mode}`) }}</span>
                    </p>
                  </div>
                  <button class="app-button" type="button" @click="analyticsConsentDialogOpen = true">
                    <Settings2 :size="14" /> {{ t('projects.editor.analytics.consent.configure') }}
                  </button>
                </div>
              </div>
            </section>
            </fieldset>
          </form>
        </main>

        <aside class="project-widget-preview">
          <header><span>{{ t('projects.preview.title') }}</span><div v-if="ruleForm.presentation === 'launcher'" class="widget-preview-modes" role="group" :aria-label="t('projects.preview.launcherState')"><button type="button" :data-active="!previewLauncherOpen" @click="previewLauncherOpen = false">{{ t('projects.preview.launcher') }}</button><button type="button" :data-active="previewLauncherOpen" @click="previewLauncherOpen = true">{{ t('projects.preview.open') }}</button></div><small v-else>{{ previewAppearanceSummary }}</small></header>
          <div class="widget-preview-page" :data-theme="effectiveAppearance.theme" inert aria-hidden="true">
            <div class="widget-preview-site"><span /><span /><span /></div>
            <div class="widget-preview-copy">
              <b>{{ t('projects.preview.pageTitle') }}</b>
              <span>{{ t('projects.preview.pageDescription') }}</span>
              <div />
            </div>
            <button
              v-if="ruleForm.presentation === 'launcher' && !previewLauncherOpen"
              class="widget-preview-trigger"
              :style="{ background: effectiveAppearance.accent_color, color: effectiveAppearance.accent_foreground }"
            >
              {{ previewLocalization.launcher_label }}
            </button>
            <section
              v-show="ruleForm.presentation !== 'launcher' || previewLauncherOpen"
              class="widget-preview-modal"
              :data-presentation="ruleForm.presentation"
              :style="{ background: effectiveAppearance.surface_color, color: effectiveAppearance.text_color, borderTopColor: effectiveAppearance.accent_color }"
            >
              <img v-if="effectiveAppearance.logo_url" class="widget-preview-logo" :src="effectiveAppearance.logo_url" alt="" referrerpolicy="no-referrer" />
              <p>{{ previewLocalization.prompt }}</p>
              <div v-if="ruleForm.can_rate" class="widget-preview-ratings">
                <button v-for="value in 5" :key="value">{{ value }}</button>
              </div>
              <div v-if="hasResponseType('reaction')" class="widget-preview-ratings">
                <button v-for="option in ruleForm.response_blocks.find(block => block.type === 'reaction')?.options" :key="option.value">
                  {{ previewLocalization.option_labels?.reaction?.[option.value] || option.value.replaceAll('_', ' ') }}
                </button>
              </div>
              <textarea v-if="ruleForm.can_write_text" :placeholder="previewLocalization.block_labels?.feedback || t('projects.preview.tellUsMore')" />
              <details v-if="previewControllerNoticeReady" class="widget-preview-privacy" open>
                <summary>{{ t('projects.preview.privacyNotice', { name: tenant?.name }) }}</summary>
                <span>{{ previewLocalization.privacy_purpose_summary }}</span>
                <span>{{ t('projects.preview.lawfulBasis') }}: {{ previewLocalization.privacy_lawful_basis_summary }}</span>
                <span>{{ t('projects.preview.retention') }}: {{ previewLocalization.privacy_retention_summary }}</span>
                <p class="widget-preview-controller-links">
                  <a :href="`mailto:${tenant?.privacy_contact_email}`">{{ t('projects.preview.privacyContact') }}</a>
                  <a :href="`mailto:${tenant?.privacy_rights_email}`">{{ t('projects.preview.rightsContact') }}</a>
                  <a :href="tenant?.privacy_policy_url || '#'">{{ t('projects.preview.privacyPolicy') }}</a>
                </p>
              </details>
              <div v-if="ruleForm.collect_contact" class="widget-preview-contact">
                <label>
                  <span>{{ previewLocalization.contact_label }}</span>
                  <input type="email" :placeholder="previewLocalization.contact_label" />
                </label>
                <label><input type="checkbox" /><span>{{ previewLocalization.consent_label }}</span></label>
              </div>
              <div class="widget-preview-actions">
                <span v-if="ruleForm.can_attach_screenshot">{{ t('projects.preview.screenshot') }}</span>
                <span v-if="ruleForm.can_record_audio">{{ t('projects.preview.audio') }}</span>
                <button :style="{ background: effectiveAppearance.accent_color, color: effectiveAppearance.accent_foreground }">
                  {{ previewLocalization.submit_label }}
                </button>
              </div>
            </section>
          </div>
        </aside>
      </div>

      <section id="project-settings" class="project-settings app-surface"><header><div><h2>{{ t('projects.settings.title') }}</h2><p>{{ t('projects.settings.description') }}</p></div><button class="app-button app-button--primary" type="submit" form="project-settings-form" :disabled="pending || !canWrite('project:update')"><Save :size="14" /> {{ t('projects.settings.save') }}</button></header><form id="project-settings-form" @submit.prevent="saveProject"><fieldset :disabled="!workspaceWritable"><label>{{ t('projects.settings.name') }}<input v-model.trim="projectName" class="app-input" required maxlength="120" /></label><label>{{ t('projects.settings.defaultLanguage') }}<input v-model.trim="projectDefaultLocale" class="app-input" required maxlength="32" :placeholder="t('projects.settings.defaultLanguagePlaceholder')" /></label><label>{{ t('projects.settings.reporterAccess') }}<select v-model="projectReporterAccess" class="app-select"><option value="inherit">{{ t('projects.settings.reporterAccessOptions.inherit') }}</option><option value="enabled">{{ t('projects.settings.reporterAccessOptions.enabled') }}</option><option value="disabled">{{ t('projects.settings.reporterAccessOptions.disabled') }}</option></select></label><label class="wide">{{ t('projects.settings.allowedDomains') }}<textarea v-model="projectDomains" /></label><div class="wide project-appearance"><h3>{{ t('projects.settings.appearanceTitle') }}</h3><WidgetAppearanceEditor v-model="projectAppearance" :parent="workspaceAppearance" allow-logo-reset /></div></fieldset></form><footer><div><b>{{ t('projects.settings.deleteTitle') }}</b><span>{{ t('projects.settings.deleteDescription') }}</span></div><button class="app-button project-danger" :disabled="pending || !canWrite('project:delete')" @click="deleteProject"><Trash2 :size="14" /> {{ t('projects.settings.delete') }}</button></footer></section>
    </template>
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
    <ReplayConsentDialog
      :open="replayConsentDialogOpen"
      :initial="ruleForm.replay_consent"
      :project-name="projectName"
      :default-locale="projectDefaultLocale"
      :project-id="projectID"
      :rule-id="selectedRuleID"
      @cancel="cancelReplayConsent"
      @confirm="confirmReplayConsent"
    />
    <AIProcessingDialog
      :open="aiProcessingDialogOpen"
      :initial="ruleForm.ai_processing"
      :default-privacy-url="tenant?.privacy_policy_url"
      :screenshot="ruleForm.can_attach_screenshot"
      :audio="ruleForm.can_record_audio"
      :video="ruleForm.can_record_video"
      @cancel="cancelAIProcessing"
      @confirm="confirmAIProcessing"
    />
    <AnalyticsConsentDialog
      :open="analyticsConsentDialogOpen"
      :initial="ruleForm.analytics_consent"
      :project-name="projectName"
      :default-locale="projectDefaultLocale"
      :project-id="projectID"
      :rule-id="selectedRuleID"
      @cancel="cancelAnalyticsConsent"
      @confirm="confirmAnalyticsConsent"
    />
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
.project-embed { display: flex; width: min(520px, 52%); min-width: 0; height: 40px; overflow: hidden; align-items: center; gap: 8px; padding-left: 10px; background: #f3f6f5; border: 1px solid var(--line); border-radius: 5px; }
.project-embed code { flex: 1; min-width: 0; overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
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
.rule-validation-summary { margin: 0; padding: 13px 18px; background: var(--red-soft); border-bottom: 1px solid #efc2c6; }
.rule-validation-summary:focus { outline: 2px solid var(--red); outline-offset: -3px; }
.rule-validation-summary h3 { margin: 0 0 7px; color: var(--red); font-size: 10px; }
.rule-validation-summary > p { margin: 0; color: var(--red); font-size: 9px; line-height: 1.5; }
.rule-validation-summary ul { display: grid; gap: 5px; margin: 0; padding: 0; list-style: none; }
.rule-validation-summary button { display: grid; width: 100%; grid-template-columns: minmax(120px, .45fr) minmax(0, 1fr); gap: 8px; padding: 0; background: transparent; color: var(--red); border: 0; font: inherit; text-align: left; }
.rule-validation-summary code { overflow: hidden; font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }
.rule-validation-summary span { font-size: 9px; line-height: 1.4; }
.project-rule-editor form > fieldset { min-width: 0; margin: 0; padding: 0; border: 0; }
.project-rule-editor form > fieldset > section { padding: 17px 18px; border-bottom: 1px solid var(--line); }
.project-rule-editor form h3 { margin: 0 0 13px; font-size: 11px; }
.project-form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.project-form-grid label { display: flex; flex-direction: column; gap: 6px; color: var(--muted); font-size: 9px; font-weight: 700; }
.project-form-grid label.wide { grid-column: 1 / -1; }
.project-rule-editor :is(input, select, textarea, [tabindex])[aria-invalid='true'] { border-color: var(--red); box-shadow: 0 0 0 2px rgb(184 60 72 / 12%); }
.rule-field-error { grid-column: 1 / -1; color: var(--red) !important; font-size: 8px !important; font-weight: 650 !important; line-height: 1.4; }
.project-toggle { align-items: center; justify-content: flex-start; flex-direction: row !important; gap: 8px !important; min-height: 38px; padding-top: 17px; color: var(--ink-soft) !important; }
.project-toggle span { display: grid; gap: 2px; }
.project-toggle span b { color: var(--ink); font-size: 10px; }
.project-toggle span small { color: var(--muted); font-size: 9px; font-weight: 500; line-height: 1.4; }
.replay-privacy-note { display: grid; grid-template-columns: 18px minmax(0, 1fr); gap: 9px; align-items: start; padding: 10px 0; border-top: 1px solid var(--line); color: var(--muted); }
.replay-privacy-note svg { margin-top: 1px; color: var(--amber); }
.replay-privacy-note p { display: grid; min-width: 0; gap: 3px; margin: 0; font-size: 9px; line-height: 1.5; }
.replay-privacy-note b { color: var(--ink-soft); }
.replay-privacy-note span { overflow-wrap: anywhere; }
.replay-consent-status { display: flex; min-height: 58px; align-items: center; justify-content: space-between; gap: 14px; padding: 10px 12px; background: #fff8e8; border: 1px solid #e9cc84; border-radius: 5px; }
.replay-consent-status[data-configured='true'] { background: var(--teal-soft); border-color: #acd5ca; }
.replay-consent-status > div { display: grid; min-width: 0; grid-template-columns: 18px minmax(0, 1fr); gap: 8px; color: var(--amber); }
.replay-consent-status[data-configured='true'] > div { color: var(--teal); }
.replay-consent-status p { display: grid; gap: 3px; margin: 0; }
.replay-consent-status b { color: var(--ink-soft); font-size: 10px; }
.replay-consent-status span { color: var(--muted); font-size: 9px; line-height: 1.4; }
.replay-consent-status .app-button { flex: 0 0 auto; }
.project-capability-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.project-capability-grid label { display: flex; min-height: 58px; align-items: center; gap: 9px; padding: 8px 10px; border: 1px solid var(--line); border-radius: 5px; }
.project-capability-grid label[data-locked='true'] { background: #f7f8f8; opacity: .65; }
.project-capability-grid label span { display: flex; flex-direction: column; gap: 3px; }
.project-capability-grid b { font-size: 10px; }
.project-capability-grid small { color: var(--muted); font-size: 8px; }
.contact-controller-warning { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; margin-top: 8px; padding: 10px; background: var(--amber-soft); color: var(--amber); border: 1px solid color-mix(in srgb, var(--amber) 35%, var(--line)); border-radius: 5px; }
.contact-controller-warning p { display: grid; gap: 2px; margin: 0; }
.contact-controller-warning b { font-size: 10px; }
.contact-controller-warning span { font-size: 8px; line-height: 1.4; }
.ai-processing-setting { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px; border: 1px solid var(--line); border-radius: 5px; }
.ai-processing-setting[data-enabled='true'] { border-color: color-mix(in srgb, var(--green) 45%, var(--line)); background: color-mix(in srgb, var(--green) 5%, #fff); }
.ai-processing-setting > label { display: flex; min-width: 0; align-items: center; gap: 9px; }
.ai-processing-setting > label[data-locked='true'] { opacity: .65; }
.ai-processing-setting > label span { display: grid; min-width: 0; gap: 3px; }
.ai-processing-setting b { font-size: 10px; }
.ai-processing-setting small { color: var(--muted); font-size: 8px; line-height: 1.4; }
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
.widget-preview-trigger { position: absolute; right: 18px; bottom: 18px; min-height: 38px; padding: 0 13px; border: 0; border-radius: 5px; font-size: 10px; font-weight: 750; }
.widget-preview-modal { position: absolute; right: 18px; bottom: 66px; left: 18px; padding: 14px; background: #fff; color: var(--ink); border: 1px solid var(--line); border-top-width: 4px; border-radius: 6px; box-shadow: var(--shadow-overlay); }
.widget-preview-modal[data-presentation='inline'] { position: relative; inset: auto; margin-top: 18px; box-shadow: none; }
.widget-preview-modal[data-presentation='reaction'] { left: auto; width: min(360px, calc(100% - 36px)); }
.widget-preview-logo { display: block; width: auto; max-width: 120px; height: 22px; margin-bottom: 8px; object-fit: contain; object-position: left center; }
.widget-preview-modal p { margin: 0 0 10px; font-size: 11px; font-weight: 700; }
.widget-preview-ratings { display: grid; grid-template-columns: repeat(5, 1fr); gap: 4px; margin-bottom: 8px; }
.widget-preview-ratings button { height: 30px; background: var(--surface-soft); border: 1px solid var(--line); border-radius: 4px; font-size: 9px; }
.widget-preview-modal textarea { width: 100%; height: 62px; padding: 7px; border: 1px solid var(--line); border-radius: 4px; resize: none; font: inherit; font-size: 9px; }
.widget-preview-privacy { display: grid; gap: 4px; margin-top: 8px; padding: 7px 0; color: var(--muted); border-block: 1px solid var(--line); font-size: 8px; }
.widget-preview-privacy summary { color: inherit; font-weight: 750; }
.widget-preview-privacy span { display: block; margin-top: 3px; }
.widget-preview-controller-links { display: flex; flex-wrap: wrap; gap: 4px 8px; margin: 5px 0 0; }
.widget-preview-controller-links a { color: var(--green); }
.widget-preview-contact { display: grid; gap: 6px; margin-top: 8px; }
.widget-preview-contact label:first-child { display: grid; gap: 4px; color: var(--muted); font-size: 8px; font-weight: 700; }
.widget-preview-contact input[type='email'] { width: 100%; height: 30px; padding: 0 7px; background: transparent; color: inherit; border: 1px solid var(--line); border-radius: 4px; font: inherit; font-size: 9px; }
.widget-preview-contact label:last-child { display: flex; align-items: center; gap: 5px; color: var(--muted); font-size: 8px; }
.widget-preview-actions { display: flex; align-items: center; gap: 6px; margin-top: 8px; }
.widget-preview-actions span { color: var(--muted); font-size: 8px; }
.widget-preview-actions button { min-height: 30px; margin-left: auto; padding: 0 11px; border: 0; border-radius: 4px; font-size: 9px; font-weight: 700; }
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
@media (max-width: 600px) { .project-install-diagnostics dl { grid-template-columns: 1fr; } .project-install-diagnostics dl > div { border-right: 0; border-bottom: 1px solid var(--line); } .project-install-diagnostics dl > div:last-child { border-bottom: 0; } .project-form-grid, .project-capability-grid { grid-template-columns: 1fr; } .project-form-grid label.wide { grid-column: auto; } .project-rule-editor > header { align-items: flex-start; flex-direction: column; } .project-settings { margin: 12px; } .project-settings > footer { align-items: stretch; flex-direction: column; } .replay-consent-status, .contact-controller-warning { align-items: stretch; grid-template-columns: auto minmax(0, 1fr); } .replay-consent-status .app-button, .contact-controller-warning .app-button { grid-column: 1 / -1; width: 100%; } }
</style>
