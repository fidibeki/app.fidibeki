import type { StorageLike } from './storage'
import type { AnalyticsConsentMode, WidgetRule } from './types'

const storageVersion = 1
const analyticsPurpose = 'widget_analytics'

export interface AnalyticsConsentAssertion {
  consentId?: string
  purpose: typeof analyticsPurpose
  noticeVersion: string
  locale: string
  source: Exclude<AnalyticsConsentMode, 'none'>
}

export interface AnalyticsConsentGrant {
  token: string
  locale: string
  noticeVersion: string
  source: Exclude<AnalyticsConsentMode, 'none'>
  expiresAt: string
}

interface StoredAnalyticsConsent {
  version: typeof storageVersion
  state: 'active' | 'denied' | 'revoked'
  token?: string
  locale: string
  noticeVersion: string
  source: Exclude<AnalyticsConsentMode, 'none'>
  consentId?: string
  expiresAt: string
}

export interface AnalyticsConsentController {
  setRule(rule: WidgetRule | null): void
  status(): 'new' | 'active' | 'denied' | 'revoked'
  activeGrant(): AnalyticsConsentGrant | null
  grant(assertion: AnalyticsConsentAssertion): Promise<AnalyticsConsentGrant>
  deny(locale: string): Promise<void>
  revoke(): Promise<void>
}

export interface AnalyticsConsentControllerOptions {
  endpoint: URL
  publicKey: string
  projectID: string
  storage: StorageLike
  now?: () => Date
  createToken?: () => string
  fetchImpl?: typeof fetch
  onChanged?: (state: 'granted' | 'denied' | 'revoked') => void
}

// Mirrors the structure of replay-consent.ts (see that file for the equivalent session_replay
// flow) but is a fully independent implementation: widget_analytics grants, storage keys, and
// evidence must never be inferred from, or joined with, session_replay or reporter-contact
// consent (issue #58).
export function createAnalyticsConsentController(
  options: AnalyticsConsentControllerOptions,
): AnalyticsConsentController {
  const now = options.now ?? (() => new Date())
  const createToken = options.createToken ?? createOpaqueToken
  const fetchImpl = options.fetchImpl ?? fetch
  let currentRule: WidgetRule | null = null

  const storageKey = (rule = currentRule) => {
    if (!rule) return null
    return [
      'fidibeki:analytics-consent:v1',
      options.projectID,
      rule.id,
      rule.analytics_consent.notice_version,
    ].join(':')
  }

  const readStored = (rule = currentRule): StoredAnalyticsConsent | null => {
    const key = storageKey(rule)
    if (!key || !rule) return null
    const raw = options.storage.getItem(key)
    if (!raw) return null
    try {
      const stored = JSON.parse(raw) as Partial<StoredAnalyticsConsent>
      if (
        stored.version !== storageVersion ||
        (stored.state !== 'active' && stored.state !== 'denied' && stored.state !== 'revoked') ||
        ((stored.state === 'active' || stored.state === 'revoked') && typeof stored.token !== 'string') ||
        typeof stored.locale !== 'string' ||
        typeof stored.noticeVersion !== 'string' ||
        typeof stored.source !== 'string' ||
        (stored.source === 'customer_cmp' && typeof stored.consentId !== 'string') ||
        typeof stored.expiresAt !== 'string' ||
        stored.noticeVersion !== rule.analytics_consent.notice_version ||
        stored.source !== rule.analytics_consent.mode ||
        !rule.analytics_consent.supported_locales.some(locale =>
          locale.toLowerCase() === stored.locale?.toLowerCase()) ||
        !Number.isFinite(Date.parse(stored.expiresAt))
      ) {
        options.storage.removeItem(key)
        return null
      }
      return stored as StoredAnalyticsConsent
    } catch {
      options.storage.removeItem(key)
      return null
    }
  }

  const activeGrant = (rule = currentRule): AnalyticsConsentGrant | null => {
    const key = storageKey(rule)
    const stored = readStored(rule)
    if (!key || !stored || stored.state !== 'active') return null
    if (Date.parse(stored.expiresAt) <= now().getTime()) {
      options.storage.removeItem(key)
      return null
    }
    return {
      token: stored.token!,
      locale: stored.locale,
      noticeVersion: stored.noticeVersion,
      source: stored.source,
      expiresAt: stored.expiresAt,
    }
  }

  return {
    setRule(rule) {
      currentRule = rule
    },
    status() {
      const key = storageKey()
      const stored = readStored()
      if (!key || !stored) return 'new'
      if (Date.parse(stored.expiresAt) <= now().getTime()) {
        options.storage.removeItem(key)
        return 'new'
      }
      return stored.state
    },
    activeGrant,
    async grant(assertion) {
      const rule = currentRule
      if (!rule || rule.analytics_consent.mode === 'none') {
        throw new Error('analytics consent is not configured for the current rule')
      }
      const locale = assertion.locale.trim().toLowerCase()
      const source = rule.analytics_consent.mode
      const customerReference = assertion.consentId?.trim() ?? ''
      if (
        assertion.purpose !== analyticsPurpose ||
        assertion.noticeVersion.trim() !== rule.analytics_consent.notice_version ||
        assertion.source !== source ||
        !rule.analytics_consent.supported_locales.some(value => value.toLowerCase() === locale) ||
        (source === 'customer_cmp' && !customerReference)
      ) {
        throw new Error('analytics consent assertion does not match the current rule')
      }
      const key = storageKey(rule)!
      const existing = readStored(rule)
      if (
        existing?.state === 'active' &&
        Date.parse(existing.expiresAt) > now().getTime() &&
        existing.locale === locale &&
        existing.noticeVersion === rule.analytics_consent.notice_version &&
        existing.source === source &&
        (source !== 'customer_cmp' || existing.consentId === customerReference)
      ) {
        return activeGrant(rule)!
      }
      const token = createToken()
      const response = await analyticsConsentFetch(options.endpoint, options.publicKey, fetchImpl, {
        widget_rule_id: rule.id,
        purpose: analyticsPurpose,
        notice_version: rule.analytics_consent.notice_version,
        locale,
        source,
        consent_id: source === 'customer_cmp' ? customerReference : '',
        state: 'granted',
        grant_token: token,
      })
      const payload = await response.json() as {
        token?: string
        state?: string
        expires_at?: string
      }
      if (
        response.status !== 201 ||
        payload.token !== token ||
        payload.state !== 'active' ||
        typeof payload.expires_at !== 'string' ||
        Date.parse(payload.expires_at) <= now().getTime()
      ) {
        throw new Error('analytics consent grant response is invalid')
      }
      const stored: StoredAnalyticsConsent = {
        version: storageVersion,
        state: 'active',
        token,
        locale,
        noticeVersion: rule.analytics_consent.notice_version,
        source,
        ...(source === 'customer_cmp' ? { consentId: customerReference } : {}),
        expiresAt: payload.expires_at,
      }
      options.storage.setItem(key, JSON.stringify(stored))
      options.onChanged?.('granted')
      return {
        token,
        locale,
        noticeVersion: stored.noticeVersion,
        source,
        expiresAt: stored.expiresAt,
      }
    },
    async deny(localeValue) {
      const rule = currentRule
      if (!rule || rule.analytics_consent.mode !== 'fidibeki_managed') {
        throw new Error('Fidibeki-managed analytics consent is not configured for the current rule')
      }
      const locale = localeValue.trim().toLowerCase()
      if (!rule.analytics_consent.supported_locales.some(value => value.toLowerCase() === locale)) {
        throw new Error('analytics consent locale is not supported by the current rule')
      }
      const key = storageKey(rule)!
      const response = await analyticsConsentFetch(options.endpoint, options.publicKey, fetchImpl, {
        widget_rule_id: rule.id,
        purpose: analyticsPurpose,
        notice_version: rule.analytics_consent.notice_version,
        locale,
        source: 'fidibeki_managed',
        consent_id: '',
        state: 'denied',
        grant_token: '',
      })
      const payload = await response.json() as { state?: string, expires_at?: string }
      if (
        response.status !== 201 ||
        payload.state !== 'denied' ||
        typeof payload.expires_at !== 'string' ||
        Date.parse(payload.expires_at) <= now().getTime()
      ) {
        throw new Error('analytics consent denial response is invalid')
      }
      const stored: StoredAnalyticsConsent = {
        version: storageVersion,
        state: 'denied',
        locale,
        noticeVersion: rule.analytics_consent.notice_version,
        source: 'fidibeki_managed',
        expiresAt: payload.expires_at,
      }
      options.storage.setItem(key, JSON.stringify(stored))
      options.onChanged?.('denied')
    },
    async revoke() {
      const key = storageKey()
      const stored = readStored()
      if (!key || !stored) return
      options.storage.setItem(key, JSON.stringify({ ...stored, state: 'revoked' }))
      options.onChanged?.('revoked')
      const endpoint = new URL('revoke', ensureTrailingSlash(options.endpoint))
      const response = await analyticsConsentFetch(endpoint, options.publicKey, fetchImpl, {
        token: stored.token!,
      })
      if (!response.ok) throw new Error(`analytics consent revocation failed with status ${response.status}`)
      options.storage.removeItem(key)
    },
  }
}

async function analyticsConsentFetch(
  endpoint: URL,
  publicKey: string,
  fetchImpl: typeof fetch,
  body: Record<string, unknown>,
): Promise<Response> {
  const url = new URL(endpoint)
  url.searchParams.set('key', publicKey)
  const response = await fetchImpl(url, {
    method: 'POST',
    mode: 'cors',
    credentials: 'omit',
    headers: {
      'Content-Type': 'application/json',
      'X-Fidibeki-Public-Key': publicKey,
    },
    body: JSON.stringify(body),
  })
  if (!response.ok) throw new Error(`analytics consent request failed with status ${response.status}`)
  return response
}

function ensureTrailingSlash(value: URL): URL {
  const url = new URL(value)
  if (!url.pathname.endsWith('/')) url.pathname += '/'
  return url
}

function createOpaqueToken(): string {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}
