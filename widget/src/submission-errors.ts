import type { WidgetSystemCopy } from './localization'

export type PublicErrorReason =
  | 'submission_rate_limited'
  | 'upload_rate_limited'
  | 'replay_rate_limited'
  | 'spam_rejected'
  | 'plan_quota_exhausted'
  | 'upload_expired'
  | 'upload_invalid'
  | 'upload_unsafe'
  | 'provider_temporarily_unavailable'

export interface PublicAPIError {
  status: number
  code: string
  reason: PublicErrorReason | ''
  retryAfterSeconds: number
}

export class PublicAPIResponseError extends Error {
  readonly failure: PublicAPIError

  constructor(failure: PublicAPIError) {
    super(`public API request failed with status ${failure.status}${failure.reason ? ` (${failure.reason})` : ''}`)
    this.name = 'PublicAPIResponseError'
    this.failure = failure
  }
}

const maxRetryAfterSeconds = 24 * 60 * 60
const stableReasons = new Set<PublicErrorReason>([
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

export async function parsePublicAPIError(response: Response): Promise<PublicAPIError> {
  let body: unknown
  try {
    body = await response.json()
  } catch {
    body = null
  }
  const envelope = recordValue(body)
  const error = recordValue(envelope?.error)
  const reason = stableReason(error?.reason)
  const bodyDelay = retryDelay(error?.retry_after_seconds)
  const headerDelay = retryAfterHeader(response.headers.get('Retry-After'))
  return {
    status: response.status,
    code: stringValue(error?.code),
    reason,
    retryAfterSeconds: bodyDelay || headerDelay,
  }
}

export function publicFailureMessage(
  failure: PublicAPIError,
  copy: WidgetSystemCopy,
  remainingSeconds = 0,
): string {
  const base = reasonMessage(failure.reason, copy)
  if (remainingSeconds <= 0) return base
  const retry = (remainingSeconds === 1 ? copy.retryInSecond : copy.retryInSeconds)
    .replace('{seconds}', String(remainingSeconds))
  return `${base} ${retry}`
}

function reasonMessage(reason: PublicAPIError['reason'], copy: WidgetSystemCopy): string {
  switch (reason) {
    case 'submission_rate_limited': return copy.submissionRateLimited
    case 'upload_rate_limited': return copy.uploadRateLimited
    case 'replay_rate_limited': return copy.replayRateLimited
    case 'spam_rejected': return copy.spamRejected
    case 'plan_quota_exhausted': return copy.planQuotaExhausted
    case 'upload_expired': return copy.uploadExpired
    case 'upload_invalid': return copy.uploadInvalid
    case 'upload_unsafe': return copy.uploadUnsafe
    case 'provider_temporarily_unavailable': return copy.providerUnavailable
    default: return copy.sendError
  }
}

function recordValue(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function stableReason(value: unknown): PublicAPIError['reason'] {
  return typeof value === 'string' && stableReasons.has(value as PublicErrorReason)
    ? value as PublicErrorReason
    : ''
}

function retryDelay(value: unknown): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return 0
  return Math.min(Math.ceil(value), maxRetryAfterSeconds)
}

function retryAfterHeader(value: string | null): number {
  if (!value) return 0
  const seconds = Number(value)
  if (Number.isFinite(seconds) && seconds > 0) return retryDelay(seconds)
  const retryAt = Date.parse(value)
  if (!Number.isFinite(retryAt)) return 0
  return retryDelay((retryAt - Date.now()) / 1000)
}
