import type { SSOPolicy } from '~/types/api'

/** What the login page should offer for one organization's SSO policy. */
export interface LoginPaths {
  /** A "Continue with single sign-on" button is available. */
  ssoAvailable: boolean
  /** Ordinary members may sign in with a Fidibeki link. */
  localLoginEnabled: boolean
  /** The email form should be rendered right now. */
  localLoginVisible: boolean
  /** The restricted owner recovery path may be revealed. */
  breakGlassAvailable: boolean
}

/**
 * Derives the login page's shape from the organization's SSO policy. A
 * workspace with no policy — no SAML configured, or the status request failed —
 * keeps exactly the behaviour it had before SSO existed.
 *
 * Under required SSO the email form is hidden, but the workspace owner can
 * reveal it as the audited break-glass path, which is what keeps a certificate
 * expiry or identity-provider outage from locking the workspace out.
 */
export function loginPathsFor(
  policy: SSOPolicy | null,
  breakGlassRequested = false,
): LoginPaths {
  if (!policy) {
    return {
      ssoAvailable: false,
      localLoginEnabled: true,
      localLoginVisible: true,
      breakGlassAvailable: false,
    }
  }
  const localLoginEnabled = policy.local_login_enabled !== false
  const breakGlassAvailable = Boolean(policy.break_glass_available) && !localLoginEnabled
  return {
    ssoAvailable: Boolean(policy.start_url),
    localLoginEnabled,
    localLoginVisible: localLoginEnabled || (breakGlassAvailable && breakGlassRequested),
    breakGlassAvailable,
  }
}

// Failure codes the assertion consumer redirects back with. Anything outside
// this set falls back to the generic message, so a new backend code can never
// render a raw identifier to a member.
const ssoErrorCodes = new Set([
  'assertion_invalid',
  'assertion_replayed',
  'request_not_correlated',
  'member_not_eligible',
  'plan_limit_exceeded',
  'sso_unavailable',
  'start_failed',
  'malformed_response',
  'missing_response',
])

/** Maps a redirect's sso_error code to its translation key. */
export function ssoErrorMessageKey(code: unknown): string {
  const value = typeof code === 'string' ? code : ''
  if (!value) return ''
  return ssoErrorCodes.has(value)
    ? `auth.login.ssoError.${value}`
    : 'auth.login.ssoError.sso_failed'
}
