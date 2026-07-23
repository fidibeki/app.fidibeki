import { resolveWidgetLocalization } from '../../src/localization'
import {
  parsePublicAPIError,
  publicFailureMessage,
  type PublicAPIError,
  type PublicErrorReason,
} from '../../src/submission-errors'
import type { WidgetRule } from '../../src/types'

const system = resolveWidgetLocalization({ greeting: 'Feedback' } as WidgetRule).system

describe('public submission errors', () => {
  it('prefers the stable body reason and retry delay over the header', async () => {
    const response = new Response(JSON.stringify({
      error: {
        code: 'rate_limit_exceeded',
        message: 'Too many attempts.',
        reason: 'submission_rate_limited',
        retry_after_seconds: 7,
      },
    }), {
      status: 429,
      headers: { 'Content-Type': 'application/json', 'Retry-After': '3' },
    })

    await expect(parsePublicAPIError(response)).resolves.toEqual({
      status: 429,
      code: 'rate_limit_exceeded',
      reason: 'submission_rate_limited',
      retryAfterSeconds: 7,
    })
  })

  it('uses a bounded Retry-After header when the body has no delay', async () => {
    const response = new Response(JSON.stringify({
      error: {
        code: 'service_unavailable',
        reason: 'provider_temporarily_unavailable',
      },
    }), {
      status: 503,
      headers: { 'Content-Type': 'application/json', 'Retry-After': '4.2' },
    })

    await expect(parsePublicAPIError(response)).resolves.toMatchObject({
      status: 503,
      reason: 'provider_temporarily_unavailable',
      retryAfterSeconds: 5,
    })
  })

  it('falls back safely for malformed and unknown payloads', async () => {
    const response = new Response('not-json', {
      status: 500,
      headers: { 'Retry-After': '999999' },
    })

    await expect(parsePublicAPIError(response)).resolves.toEqual({
      status: 500,
      code: '',
      reason: '',
      retryAfterSeconds: 86400,
    })
  })

  it.each<[PublicErrorReason, string]>([
    ['submission_rate_limited', 'Too many feedback attempts.'],
    ['upload_rate_limited', 'Attachments are temporarily busy.'],
    ['replay_rate_limited', 'Session recording is temporarily busy.'],
    ['spam_rejected', 'This feedback could not be accepted.'],
    ['plan_quota_exhausted', 'This workspace has reached its feedback limit.'],
    ['upload_expired', 'The attachment upload expired.'],
    ['upload_invalid', 'An attachment could not be validated.'],
    ['upload_unsafe', 'An attachment could not be accepted.'],
    ['provider_temporarily_unavailable', 'Processing is temporarily unavailable.'],
  ])('maps %s to concise operator-safe copy', (reason, expected) => {
    expect(publicFailureMessage({
      status: 429, code: 'rate_limit_exceeded', reason, retryAfterSeconds: 0,
    }, system)).toBe(expected)
  })

  it('adds a localized countdown without exposing server text', () => {
    const failure: PublicAPIError = {
      status: 429,
      code: 'rate_limit_exceeded',
      reason: 'submission_rate_limited',
      retryAfterSeconds: 2,
    }
    expect(publicFailureMessage(failure, system, 2)).toBe(
      'Too many feedback attempts. Try again in 2 seconds.',
    )
  })
})
