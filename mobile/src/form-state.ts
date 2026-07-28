import type {
  FeedbackFormState,
  FormResponses,
  MobileCaptureSession,
  MobileResponseBlock,
  ReporterConsentInput,
} from './types'

export const EMPTY_FORM_STATE: FeedbackFormState = {
  responses: {},
  comment: '',
  reporterEmail: '',
  consentGranted: false,
}

export const MAX_TEXT_LENGTH = 10_000

export interface FormValidation {
  valid: boolean
  fields: Record<string, string>
}

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

function blockValueIsValid(block: MobileResponseBlock, value: unknown): boolean {
  if (block.type === 'rating') {
    return Number.isInteger(value) && Number(value) >= 1 && Number(value) <= 5
  }
  if (block.type === 'free_text') {
    return (
      typeof value === 'string' &&
      value.trim().length > 0 &&
      value.trim().length <= MAX_TEXT_LENGTH
    )
  }
  return (
    typeof value === 'string' &&
    (block.options ?? []).some((option) => option.value === value)
  )
}

export function reconcileResponses(
  previousBlocks: readonly MobileResponseBlock[],
  nextBlocks: readonly MobileResponseBlock[],
  responses: FormResponses,
): FormResponses {
  const previousByKey = new Map(previousBlocks.map((block) => [block.key, block]))
  const next: FormResponses = {}
  for (const block of nextBlocks) {
    const previous = previousByKey.get(block.key)
    const value = responses[block.key]
    if (previous?.type === block.type && value !== undefined && blockValueIsValid(block, value)) {
      next[block.key] = value
    }
  }
  return next
}

export function validateForm(
  definition: MobileCaptureSession['configuration']['definition'],
  state: FeedbackFormState,
): FormValidation {
  const fields: Record<string, string> = {}
  for (const block of definition.response_blocks) {
    const value = state.responses[block.key]
    if (value === undefined || value === '') {
      if (block.required) fields[`responses.${block.key}`] = 'required'
      continue
    }
    if (!blockValueIsValid(block, value)) fields[`responses.${block.key}`] = 'invalid'
  }
  if (state.comment.trim().length > MAX_TEXT_LENGTH) fields.comment = 'too_long'
  if (definition.collect_contact && state.reporterEmail.trim() !== '') {
    if (!EMAIL_PATTERN.test(state.reporterEmail.trim()) || state.reporterEmail.trim().length > 320) {
      fields.reporter_email = 'invalid'
    }
    if (!state.consentGranted) fields.consent = 'required'
  }
  return { valid: Object.keys(fields).length === 0, fields }
}

export function createConsentInput(
  session: MobileCaptureSession,
  state: FeedbackFormState,
): ReporterConsentInput | undefined {
  const notice = session.configuration.contact_notice
  if (!notice || !state.consentGranted || state.reporterEmail.trim() === '') return undefined
  return {
    purpose: 'feedback_updates',
    notice_version: notice.notice_version,
    controller_profile_version: notice.controller_profile_version,
    widget_definition_version: session.configuration.definition.definition_version,
    locale: notice.locale,
    granted: true,
  }
}
