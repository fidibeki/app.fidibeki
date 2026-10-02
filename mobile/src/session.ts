import { MobileClientError, type MobileCaptureSession, type MobileResponseBlock } from './types'

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.trim() !== ''
}

function rejectUnknownKeys(
  value: Record<string, unknown>,
  allowed: readonly string[],
  label: string,
): void {
  const keys = new Set(allowed)
  if (Object.keys(value).some((key) => !keys.has(key))) {
    invalidSession(`${label} contains unknown fields`)
  }
}

function validateHTTPSURL(value: string, label: string): void {
  let parsed: URL
  try {
    parsed = new URL(value)
  } catch {
    invalidSession(`invalid ${label} URL`)
  }
  if (parsed.protocol !== 'https:' || parsed.hostname === '') {
    invalidSession(`${label} URL must use HTTPS`)
  }
  if (parsed.username || parsed.password) {
    invalidSession(`${label} URL must not include credentials`)
  }
}

function invalidSession(message: string): never {
  throw new MobileClientError('invalid_response', `Invalid capture session: ${message}`)
}

function validateResponseBlock(value: unknown): MobileResponseBlock {
  if (!isObject(value)) invalidSession('response block must be an object')
  rejectUnknownKeys(value, ['key', 'type', 'required', 'options'], 'response block')
  if (!isNonEmptyString(value.key) || value.key.length > 64) invalidSession('invalid response key')
  if (!['rating', 'reaction', 'single_choice', 'free_text'].includes(String(value.type))) {
    invalidSession('unsupported response block')
  }
  if (typeof value.required !== 'boolean') invalidSession('invalid response block')
  if (value.type === 'reaction' || value.type === 'single_choice') {
    if (!Array.isArray(value.options) || value.options.length < 2 || value.options.length > 10) {
      invalidSession('invalid response options')
    }
    const optionValues = new Set<string>()
    for (const option of value.options) {
      if (!isObject(option) || !isNonEmptyString(option.value) || optionValues.has(option.value)) {
        invalidSession('invalid response option')
      }
      rejectUnknownKeys(option, ['value'], 'response option')
      optionValues.add(option.value)
    }
  }
  return value as unknown as MobileResponseBlock
}

export function validateCaptureSession(value: unknown): MobileCaptureSession {
  if (!isObject(value)) invalidSession('expected an object')
  const configuration = value.configuration
  if (
    !isNonEmptyString(value.token) ||
    !value.token.startsWith('fmc_') ||
    !isNonEmptyString(value.submit_expires_at) ||
    Number.isNaN(Date.parse(value.submit_expires_at)) ||
    !isObject(configuration) ||
    configuration.schema_version !== 1 ||
    !isNonEmptyString(configuration.project_id) ||
    !isNonEmptyString(configuration.display_locale) ||
    !isObject(configuration.definition)
  ) {
    invalidSession('missing required fields')
  }
  rejectUnknownKeys(value, ['token', 'submit_expires_at', 'configuration'], 'capture session')
  rejectUnknownKeys(
    configuration,
    [
      'schema_version',
      'project_id',
      'display_locale',
      'definition',
      'controller_profile',
      'contact_notice',
    ],
    'configuration',
  )
  const definition = configuration.definition
  if (
    !isNonEmptyString(definition.id) ||
    !isNonEmptyString(definition.definition_key) ||
    !Number.isInteger(definition.definition_version) ||
    Number(definition.definition_version) < 1 ||
    !['launcher', 'prompt', 'inline', 'reaction'].includes(String(definition.presentation)) ||
    !['edge_tab', 'floating_button'].includes(String(definition.launcher_variant)) ||
    !['continuous_feedback', 'after_engagement', 'validate_content', 'detect_friction'].includes(
      String(definition.objective),
    ) ||
    !['session', 'cooldown', 'never'].includes(String(definition.recurrence_mode)) ||
    !Array.isArray(definition.response_blocks) ||
    definition.response_blocks.length < 1 ||
    definition.response_blocks.length > 8 ||
    !isObject(definition.localization) ||
    !isObject(definition.appearance) ||
    typeof definition.allow_comment !== 'boolean' ||
    typeof definition.collect_contact !== 'boolean' ||
    typeof definition.can_attach_screenshot !== 'boolean'
  ) {
    invalidSession('invalid widget definition')
  }
  rejectUnknownKeys(
    definition,
    [
      'id',
      'definition_key',
      'definition_version',
      'presentation',
      'launcher_variant',
      'objective',
      'recurrence_mode',
      'recurrence_days',
      'response_blocks',
      'localization',
      'allow_comment',
      'collect_contact',
      'can_attach_screenshot',
      'appearance',
      'ai_processing',
    ],
    'widget definition',
  )
  const appearance = definition.appearance
  rejectUnknownKeys(
    appearance,
    [
      'position',
      'theme',
      'accent_color',
      'accent_foreground',
      'surface_color',
      'text_color',
      'logo_url',
    ],
    'widget appearance',
  )
  if (
    (appearance.position !== undefined && !['bottom-left', 'bottom-right'].includes(String(appearance.position))) ||
    (appearance.theme !== undefined && !['light', 'dark', 'auto'].includes(String(appearance.theme)))
  ) {
    invalidSession('invalid widget appearance')
  }
  for (const field of [
    appearance.accent_color,
    appearance.accent_foreground,
    appearance.surface_color,
    appearance.text_color,
  ]) {
    if (field !== undefined && (typeof field !== 'string' || !/^#[0-9a-f]{6}$/i.test(field))) {
      invalidSession('invalid widget appearance color')
    }
  }
  if (appearance.logo_url !== undefined) {
    if (!isNonEmptyString(appearance.logo_url)) invalidSession('invalid logo URL')
    validateHTTPSURL(appearance.logo_url, 'logo')
    if (appearance.logo_url.length > 2048 || new URL(appearance.logo_url).hash !== '') {
      invalidSession('invalid logo URL')
    }
  }
  const keys = new Set<string>()
  for (const rawBlock of definition.response_blocks) {
    const block = validateResponseBlock(rawBlock)
    if (keys.has(block.key)) invalidSession('duplicate response key')
    keys.add(block.key)
  }
  const localizationFields = [
    'launcher_label',
    'prompt',
    'comment_placeholder',
    'contact_label',
    'consent_label',
    'submit_label',
    'success_message',
  ]
  const localization = definition.localization as Record<string, unknown>
  rejectUnknownKeys(
    localization,
    [
      'launcher_label',
      'prompt',
      'comment_placeholder',
      'contact_label',
      'consent_label',
      'submit_label',
      'success_message',
      'block_labels',
      'option_labels',
      'privacy_purpose_summary',
      'privacy_lawful_basis_summary',
      'privacy_retention_summary',
    ],
    'localization',
  )
  if (localizationFields.some((field) => !isNonEmptyString(localization[field]))) {
    invalidSession('incomplete localization')
  }
  if (definition.localization.block_labels !== undefined && !isObject(definition.localization.block_labels)) {
    invalidSession('invalid response localization')
  }
  if (definition.localization.option_labels !== undefined && !isObject(definition.localization.option_labels)) {
    invalidSession('invalid option localization')
  }
  if (definition.recurrence_mode === 'cooldown') {
    if (
      !Number.isInteger(definition.recurrence_days) ||
      Number(definition.recurrence_days) < 1 ||
      Number(definition.recurrence_days) > 3650
    ) {
      invalidSession('invalid recurrence cooldown')
    }
  } else if (definition.recurrence_days !== undefined && definition.recurrence_days !== null) {
    invalidSession('recurrence days are only valid for cooldown mode')
  }
  if (configuration.controller_profile !== undefined) {
    const profile = configuration.controller_profile
    if (
      !isObject(profile) ||
      !Number.isInteger(profile.version) ||
      Number(profile.version) < 1 ||
      !isNonEmptyString(profile.name) ||
      !isNonEmptyString(profile.privacy_contact_email) ||
      !isNonEmptyString(profile.privacy_rights_email) ||
      !isNonEmptyString(profile.privacy_policy_url)
    ) {
      invalidSession('invalid controller profile')
    }
    rejectUnknownKeys(
      profile,
      ['version', 'name', 'privacy_contact_email', 'privacy_rights_email', 'privacy_policy_url'],
      'controller profile',
    )
    validateHTTPSURL(profile.privacy_policy_url, 'controller privacy policy')
  }
  if (definition.collect_contact) {
    const notice = configuration.contact_notice
    if (
      !isObject(notice) ||
      !isNonEmptyString(notice.notice_version) ||
      !Number.isInteger(notice.controller_profile_version) ||
      Number(notice.controller_profile_version) < 1 ||
      notice.widget_definition_version !== definition.definition_version ||
      !isNonEmptyString(notice.controller_name) ||
      !isNonEmptyString(notice.privacy_contact_email) ||
      !isNonEmptyString(notice.privacy_rights_email) ||
      !isNonEmptyString(notice.privacy_policy_url) ||
      !isNonEmptyString(notice.locale) ||
      !isNonEmptyString(notice.purpose_summary) ||
      !isNonEmptyString(notice.lawful_basis_summary) ||
      !isNonEmptyString(notice.retention_summary)
    ) {
      invalidSession('contact collection requires a complete controller notice')
    }
    rejectUnknownKeys(
      notice,
      [
        'notice_version',
        'controller_profile_version',
        'widget_definition_version',
        'controller_name',
        'privacy_contact_email',
        'privacy_rights_email',
        'privacy_policy_url',
        'locale',
        'purpose_summary',
        'lawful_basis_summary',
        'retention_summary',
      ],
      'controller notice',
    )
    if (notice.locale !== configuration.display_locale) {
      invalidSession('contact notice locale must match the display locale')
    }
    if (
      isObject(configuration.controller_profile) &&
      notice.controller_profile_version !== configuration.controller_profile.version
    ) {
      invalidSession('contact notice must match the controller profile')
    }
    validateHTTPSURL(notice.privacy_policy_url, 'privacy policy')
  }
  if (definition.ai_processing !== undefined) {
    const ai = definition.ai_processing
    if (!isObject(ai) || typeof ai.enabled !== 'boolean') {
      invalidSession('invalid AI disclosure')
    }
    rejectUnknownKeys(
      ai,
      ['enabled', 'provider', 'provider_label', 'privacy_policy_url', 'policy_version', 'input_categories'],
      'AI disclosure',
    )
    if (
      ai.enabled &&
      (!isNonEmptyString(ai.provider) ||
        !isNonEmptyString(ai.privacy_policy_url) ||
        !isNonEmptyString(ai.policy_version) ||
        !Array.isArray(ai.input_categories) ||
        ai.input_categories.some(
          (category) => !['feedback_text', 'screenshot', 'audio', 'video'].includes(String(category)),
        ))
    ) {
      invalidSession('incomplete AI disclosure')
    }
    if (ai.provider_label !== undefined && !isNonEmptyString(ai.provider_label)) invalidSession('invalid AI provider label')
    if (ai.enabled) {
      validateHTTPSURL(ai.privacy_policy_url as string, 'AI privacy policy')
    }
  }
  return value as unknown as MobileCaptureSession
}
