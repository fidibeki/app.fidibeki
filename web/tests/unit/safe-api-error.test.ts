import { describe, expect, it } from 'vitest'
import {
  apiErrorCatalogKey,
  extractAPIError,
  localizedAPIError,
  localizedAPIFieldErrors,
} from '../../utils/safe-api-error'

const messages: Record<string, string> = {
  'errors.generic': 'Something went wrong.',
  'errors.codes.not_found': 'That resource is no longer available.',
  'errors.reasons.submission_rate_limited': 'Too many submissions. Try again in {seconds} seconds.',
  'errors.fieldReasons.required': 'This field is required.',
  'errors.fieldInvalid': 'Check this field.',
}

function translate(key: string, parameters: Record<string, unknown> = {}) {
  return (messages[key] || key).replace(
    /\{(\w+)\}/g,
    (_, name: string) => String(parameters[name] ?? ''),
  )
}

describe('safe localized API errors', () => {
  it('extracts the stable error contract from supported fetch shapes', () => {
    expect(extractAPIError({
      response: {
        _data: {
          error: {
            code: 'rate_limit_exceeded',
            message: 'provider details',
            reason: 'submission_rate_limited',
            retry_after_seconds: 7,
          },
        },
      },
    })).toMatchObject({
      code: 'rate_limit_exceeded',
      reason: 'submission_rate_limited',
      retryAfterSeconds: 7,
    })
  })

  it('prefers a known reason, then a known code, then the generic fallback', () => {
    expect(apiErrorCatalogKey({
      data: { error: { code: 'rate_limit_exceeded', reason: 'submission_rate_limited' } },
    })).toBe('errors.reasons.submission_rate_limited')
    expect(apiErrorCatalogKey({
      data: { error: { code: 'not_found' } },
    })).toBe('errors.codes.not_found')
    expect(apiErrorCatalogKey({
      data: { error: { code: 'provider_secret', message: '<script>alert(1)</script>' } },
    })).toBe('errors.generic')
  })

  it('never returns arbitrary backend text for unknown errors', () => {
    expect(localizedAPIError({
      data: {
        error: {
          code: 'rate_limit_exceeded',
          message: 'provider quota identifier 123',
          reason: 'submission_rate_limited',
          retry_after_seconds: 9,
        },
      },
    }, translate)).toBe('Too many submissions. Try again in 9 seconds.')
    expect(localizedAPIError({
      data: { error: { code: 'unknown', message: '<script>alert(1)</script>' } },
    }, translate)).toBe('Something went wrong.')
  })

  it('localizes structured field reasons and hides legacy prose', () => {
    const error = {
      data: {
        error: {
          code: 'validation_error',
          details: {
            email: { reason: 'required' },
            name: 'must contain a provider-specific constraint',
          },
        },
      },
    }
    expect(localizedAPIFieldErrors(error, translate)).toEqual({
      email: 'This field is required.',
      name: 'Check this field.',
    })
  })

  it('unwraps nested fields without exposing backend prose', () => {
    expect(localizedAPIFieldErrors({
      data: {
        error: {
          code: 'validation_error',
          details: {
            fields: {
              name: 'provider-specific validation prose',
            },
          },
        },
      },
    }, translate)).toEqual({
      name: 'Check this field.',
    })
  })
})
