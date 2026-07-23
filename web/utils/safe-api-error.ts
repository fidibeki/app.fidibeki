export interface ExtractedAPIError {
  code: string
  reason: string
  retryAfterSeconds: number
  details?: unknown
}

export type APIErrorTranslator = (
  key: string,
  parameters?: Record<string, unknown>,
) => string

const knownCodes = new Set([
  'bad_request',
  'unauthenticated',
  'forbidden',
  'tenant_suspended',
  'not_found',
  'unknown_tenant',
  'conflict',
  'owner_protected',
  'last_administrator',
  'recent_authentication_required',
  'invalid_ownership_confirmation',
  'invalid_ownership_target',
  'member_already_removed',
  'validation_error',
  'rate_limit_exceeded',
  'plan_limit_exceeded',
  'plan_feature_unavailable',
  'internal_error',
  'service_unavailable',
])

const knownReasons = new Set([
  'submission_rate_limited',
  'upload_rate_limited',
  'replay_rate_limited',
  'spam_rejected',
  'plan_quota_exhausted',
  'upload_expired',
  'upload_invalid',
  'upload_unsafe',
  'provider_temporarily_unavailable',
])

const knownFieldReasons = new Set([
  'required',
  'invalid',
  'unsupported',
  'too_long',
  'too_short',
  'out_of_range',
  'not_verified',
])

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null
}

function errorBody(value: unknown) {
  const root = record(value)
  if (!root) return null
  const response = record(root.response)
  const data = record(root.data) || record(response?._data) || record(response?.data) || root
  return record(data.error)
}

function safeString(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

export function extractAPIError(value: unknown): ExtractedAPIError | null {
  const body = errorBody(value)
  if (!body) return null
  const retry = typeof body.retry_after_seconds === 'number' &&
    Number.isFinite(body.retry_after_seconds)
    ? Math.max(0, Math.floor(body.retry_after_seconds))
    : 0
  return {
    code: safeString(body.code),
    reason: safeString(body.reason),
    retryAfterSeconds: retry,
    details: body.details,
  }
}

export function apiErrorCatalogKey(value: unknown) {
  const error = extractAPIError(value)
  if (error?.reason && knownReasons.has(error.reason)) {
    return `errors.reasons.${error.reason}`
  }
  if (error?.code && knownCodes.has(error.code)) {
    return `errors.codes.${error.code}`
  }
  return 'errors.generic'
}

export function localizedAPIError(
  value: unknown,
  translate: APIErrorTranslator,
  fallbackKey = 'errors.generic',
) {
  const error = extractAPIError(value)
  const key = apiErrorCatalogKey(value)
  return translate(key === 'errors.generic' ? fallbackKey : key, {
    seconds: error?.retryAfterSeconds || 1,
  })
}

function fieldReason(value: unknown) {
  const reason = safeString(record(value)?.reason)
  return knownFieldReasons.has(reason) ? reason : ''
}

export function localizedAPIFieldErrors(
  value: unknown,
  translate: APIErrorTranslator,
): Record<string, string> {
  const details = extractAPIError(value)?.details
  if (Array.isArray(details)) {
    return Object.fromEntries(details.flatMap((item) => {
      const entry = record(item)
      const field = safeString(entry?.field)
      if (!field) return []
      const reason = fieldReason(entry)
      return [[field, translate(
        reason ? `errors.fieldReasons.${reason}` : 'errors.fieldInvalid',
      )]]
    }))
  }
  const root = record(details)
  if (!root) return {}
  const fields = record(root.fields) || root
  return Object.fromEntries(Object.entries(fields).map(([field, detail]) => {
    const reason = fieldReason(detail)
    return [field, translate(
      reason ? `errors.fieldReasons.${reason}` : 'errors.fieldInvalid',
    )]
  }))
}
