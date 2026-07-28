import { MobileClientError, type MobileCaptureSession, type MobileResponseBlock } from './types'

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.trim() !== ''
}

function invalidSession(message: string): never {
  throw new MobileClientError('invalid_response', `Invalid capture session: ${message}`)
}

function validateResponseBlock(value: unknown): MobileResponseBlock {
  if (!isObject(value)) invalidSession('response block must be an object')
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
      if (
        !isObject(option) ||
        !isNonEmptyString(option.value) ||
        optionValues.has(option.value)
      ) {
        invalidSession('invalid response option')
      }
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
  const definition = configuration.definition
  if (
    !isNonEmptyString(definition.id) ||
    !isNonEmptyString(definition.definition_key) ||
    !Number.isInteger(definition.definition_version) ||
    Number(definition.definition_version) < 1 ||
    definition.presentation !== 'launcher' ||
    !['edge_tab', 'floating_button'].includes(String(definition.launcher_variant)) ||
    ![
      'continuous_feedback',
      'after_engagement',
      'validate_content',
      'detect_friction',
    ].includes(String(definition.objective)) ||
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
  if (localizationFields.some((field) => !isNonEmptyString(localization[field]))) {
    invalidSession('incomplete localization')
  }
  if (
    definition.localization.block_labels !== undefined &&
    !isObject(definition.localization.block_labels)
  ) {
    invalidSession('invalid response localization')
  }
  if (
    definition.localization.option_labels !== undefined &&
    !isObject(definition.localization.option_labels)
  ) {
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
    if (notice.locale !== configuration.display_locale) {
      invalidSession('contact notice locale must match the display locale')
    }
    let policyURL: URL
    try {
      policyURL = new URL(notice.privacy_policy_url)
    } catch {
      invalidSession('invalid privacy policy URL')
    }
    if (policyURL.protocol !== 'https:') invalidSession('privacy policy URL must use HTTPS')
  }
  if (definition.ai_processing !== undefined) {
    const ai = definition.ai_processing
    if (
      !isObject(ai) ||
      typeof ai.enabled !== 'boolean'
    ) {
      invalidSession('invalid AI disclosure')
    }
    if (
      ai.enabled &&
      (!isNonEmptyString(ai.provider) ||
        !isNonEmptyString(ai.privacy_policy_url) ||
        !isNonEmptyString(ai.policy_version) ||
        !Array.isArray(ai.input_categories) ||
        ai.input_categories.some(
          (category) =>
            !['feedback_text', 'screenshot', 'audio', 'video'].includes(String(category)),
        ))
    ) {
      invalidSession('incomplete AI disclosure')
    }
  }
  return value as unknown as MobileCaptureSession
}
