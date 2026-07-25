import { describe, expect, it } from 'vitest'
import { isAuthResponse, twoFactorRequirement } from '../../types/api'
import type { AuthResponse } from '../../types/api'

const session = {
  access_token: 'token',
  token_type: 'Bearer',
  expires_in: 600,
  member: { id: 'member', permissions: ['project:read'] },
  tenant: { id: 'tenant' },
} as unknown as AuthResponse

describe('two-factor authentication responses', () => {
  it('recognizes a session that carries an access token', () => {
    expect(isAuthResponse(session)).toBe(true)
    expect(twoFactorRequirement(session)).toBeNull()
  })

  it('treats a challenge response as an unusable session', () => {
    const response = { two_factor: { status: 'challenge_required' } } as const

    expect(isAuthResponse(response)).toBe(false)
    expect(twoFactorRequirement(response)).toBe('challenge_required')
  })

  it('carries the enrollment requirement alongside a usable session', () => {
    // A member caught by a newly required policy still receives a token; every
    // workspace route rejects it until the enrolment completes.
    const response = { ...session, two_factor: { status: 'enrollment_required' } } as const

    expect(isAuthResponse(response)).toBe(true)
    expect(twoFactorRequirement(response)).toBe('enrollment_required')
  })

  it('ignores an unknown or missing status', () => {
    expect(twoFactorRequirement(null)).toBeNull()
    expect(twoFactorRequirement(undefined)).toBeNull()
    expect(
      twoFactorRequirement({ two_factor: { status: 'something_else' } } as never),
    ).toBeNull()
  })
})
