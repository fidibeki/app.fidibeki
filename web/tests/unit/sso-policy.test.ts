import { describe, expect, it } from 'vitest'
import { loginPathsFor, ssoErrorMessageKey } from '../../utils/sso-policy'
import type { SSOPolicy } from '../../types/api'

function policy(overrides: Partial<SSOPolicy> = {}): SSOPolicy {
  return {
    mode: 'optional',
    start_url: 'https://acme.fidibeki.test/auth/sso/saml/start',
    local_login_enabled: true,
    break_glass_available: false,
    ...overrides,
  }
}

describe('login paths for an organization SSO policy', () => {
  it('leaves a workspace without SSO exactly as it was', () => {
    expect(loginPathsFor(null)).toEqual({
      ssoAvailable: false,
      localLoginEnabled: true,
      localLoginVisible: true,
      breakGlassAvailable: false,
    })
  })

  it('keeps the email form when SSO is disabled', () => {
    const paths = loginPathsFor(policy({ mode: 'disabled', start_url: undefined }))
    expect(paths.ssoAvailable).toBe(false)
    expect(paths.localLoginVisible).toBe(true)
  })

  it('offers both paths when SSO is optional', () => {
    const paths = loginPathsFor(policy({ mode: 'optional' }))
    expect(paths.ssoAvailable).toBe(true)
    expect(paths.localLoginEnabled).toBe(true)
    expect(paths.localLoginVisible).toBe(true)
    expect(paths.breakGlassAvailable).toBe(false)
  })

  it('hides the email form when SSO is required', () => {
    const paths = loginPathsFor(policy({
      mode: 'required', local_login_enabled: false, break_glass_available: true,
    }))
    expect(paths.ssoAvailable).toBe(true)
    expect(paths.localLoginEnabled).toBe(false)
    expect(paths.localLoginVisible).toBe(false)
    expect(paths.breakGlassAvailable).toBe(true)
  })

  it('reveals the owner recovery form only when it is requested', () => {
    const required = policy({
      mode: 'required', local_login_enabled: false, break_glass_available: true,
    })
    expect(loginPathsFor(required, true).localLoginVisible).toBe(true)
    // Requesting break-glass must not change the underlying policy, so the
    // page still explains that ordinary local login is closed.
    expect(loginPathsFor(required, true).localLoginEnabled).toBe(false)
  })

  it('does not let a break-glass request reopen local login without the policy', () => {
    const required = policy({
      mode: 'required', local_login_enabled: false, break_glass_available: false,
    })
    expect(loginPathsFor(required, true).localLoginVisible).toBe(false)
  })
})

describe('single sign-on failure messages', () => {
  it('maps known failure codes to their own message', () => {
    expect(ssoErrorMessageKey('assertion_replayed')).toBe('auth.login.ssoError.assertion_replayed')
    expect(ssoErrorMessageKey('member_not_eligible')).toBe('auth.login.ssoError.member_not_eligible')
    expect(ssoErrorMessageKey('plan_limit_exceeded')).toBe('auth.login.ssoError.plan_limit_exceeded')
  })

  it('falls back to the generic message for an unrecognized code', () => {
    expect(ssoErrorMessageKey('something_new')).toBe('auth.login.ssoError.sso_failed')
    expect(ssoErrorMessageKey(42)).toBe('')
    expect(ssoErrorMessageKey('')).toBe('')
  })
})
