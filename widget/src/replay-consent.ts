import type { StorageLike } from './storage'
import type { ReplayConsentMode, WidgetRule } from './types'

const storageVersion = 1
const replayPurpose = 'session_replay'

export interface ReplayConsentAssertion {
  consentId?: string
  purpose: typeof replayPurpose
  noticeVersion: string
  locale: string
  source: Exclude<ReplayConsentMode, 'none'>
}

export interface ReplayConsentGrant {
  token: string
  locale: string
  noticeVersion: string
  source: Exclude<ReplayConsentMode, 'none'>
  expiresAt: string
}

interface StoredReplayConsent {
  version: typeof storageVersion
  state: 'active' | 'denied' | 'revoked'
  token?: string
  locale: string
  noticeVersion: string
  source: Exclude<ReplayConsentMode, 'none'>
  consentId?: string
  expiresAt: string
}

export interface ReplayConsentController {
  setRule(rule: WidgetRule | null): void
  status(): 'new' | 'active' | 'denied' | 'revoked'
  activeGrant(): ReplayConsentGrant | null
  grant(assertion: ReplayConsentAssertion): Promise<ReplayConsentGrant>
  deny(locale: string): Promise<void>
  revoke(): Promise<void>
}

export interface ReplayConsentControllerOptions {
  endpoint: URL
  publicKey: string
  projectID: string
  storage: StorageLike
  now?: () => Date
  createToken?: () => string
  fetchImpl?: typeof fetch
  onChanged?: (state: 'granted' | 'denied' | 'revoked') => void
}

export function createReplayConsentController(
  options: ReplayConsentControllerOptions,
): ReplayConsentController {
  const now = options.now ?? (() => new Date())
  const createToken = options.createToken ?? createOpaqueToken
  const fetchImpl = options.fetchImpl ?? fetch
  let currentRule: WidgetRule | null = null

  const storageKey = () => {
    if (!currentRule) return null
    return [
      'fidibeki:replay-consent:v1',
      options.projectID,
      currentRule.id,
      currentRule.replay_consent.notice_version,
    ].join(':')
  }

  const readStored = (): StoredReplayConsent | null => {
    const key = storageKey()
    if (!key || !currentRule) return null
    const raw = options.storage.getItem(key)
    if (!raw) return null
    try {
      const stored = JSON.parse(raw) as Partial<StoredReplayConsent>
      if (
        stored.version !== storageVersion ||
        (stored.state !== 'active' && stored.state !== 'denied' && stored.state !== 'revoked') ||
        ((stored.state === 'active' || stored.state === 'revoked') && typeof stored.token !== 'string') ||
        typeof stored.locale !== 'string' ||
        typeof stored.noticeVersion !== 'string' ||
        typeof stored.source !== 'string' ||
        (stored.source === 'customer_cmp' && typeof stored.consentId !== 'string') ||
        typeof stored.expiresAt !== 'string' ||
        stored.noticeVersion !== currentRule.replay_consent.notice_version ||
        stored.source !== currentRule.replay_consent.mode ||
        !currentRule.replay_consent.supported_locales.some(locale =>
          locale.toLowerCase() === stored.locale?.toLowerCase()) ||
        !Number.isFinite(Date.parse(stored.expiresAt))
      ) {
        options.storage.removeItem(key)
        return null
      }
      return stored as StoredReplayConsent
    } catch {
      options.storage.removeItem(key)
      return null
    }
  }

  const activeGrant = (): ReplayConsentGrant | null => {
    const key = storageKey()
    const stored = readStored()
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
      if (!currentRule || currentRule.replay_consent.mode === 'none') {
        throw new Error('replay consent is not configured for the current rule')
      }
      const locale = assertion.locale.trim().toLowerCase()
      const source = currentRule.replay_consent.mode
      const customerReference = assertion.consentId?.trim() ?? ''
      if (
        assertion.purpose !== replayPurpose ||
        assertion.noticeVersion.trim() !== currentRule.replay_consent.notice_version ||
        assertion.source !== source ||
        !currentRule.replay_consent.supported_locales.some(value => value.toLowerCase() === locale) ||
        (source === 'customer_cmp' && !customerReference)
      ) {
        throw new Error('replay consent assertion does not match the current rule')
      }
      const existing = readStored()
      if (
        existing?.state === 'active' &&
        Date.parse(existing.expiresAt) > now().getTime() &&
        existing.locale === locale &&
        existing.noticeVersion === currentRule.replay_consent.notice_version &&
        existing.source === source &&
        (source !== 'customer_cmp' || existing.consentId === customerReference)
      ) {
        return activeGrant()!
      }
      const token = createToken()
      const response = await replayConsentFetch(options.endpoint, options.publicKey, fetchImpl, {
        widget_rule_id: currentRule.id,
        purpose: replayPurpose,
        notice_version: currentRule.replay_consent.notice_version,
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
        throw new Error('replay consent grant response is invalid')
      }
      const stored: StoredReplayConsent = {
        version: storageVersion,
        state: 'active',
        token,
        locale,
        noticeVersion: currentRule.replay_consent.notice_version,
        source,
        ...(source === 'customer_cmp' ? { consentId: customerReference } : {}),
        expiresAt: payload.expires_at,
      }
      const key = storageKey()
      if (!key) throw new Error('replay consent rule changed during the grant')
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
      if (!currentRule || currentRule.replay_consent.mode !== 'fidibeki_managed') {
        throw new Error('Fidibeki-managed replay consent is not configured for the current rule')
      }
      const locale = localeValue.trim().toLowerCase()
      if (!currentRule.replay_consent.supported_locales.some(value => value.toLowerCase() === locale)) {
        throw new Error('replay consent locale is not supported by the current rule')
      }
      const response = await replayConsentFetch(options.endpoint, options.publicKey, fetchImpl, {
        widget_rule_id: currentRule.id,
        purpose: replayPurpose,
        notice_version: currentRule.replay_consent.notice_version,
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
        throw new Error('replay consent denial response is invalid')
      }
      const key = storageKey()
      if (!key) throw new Error('replay consent rule changed during the decision')
      const stored: StoredReplayConsent = {
        version: storageVersion,
        state: 'denied',
        locale,
        noticeVersion: currentRule.replay_consent.notice_version,
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
      const response = await replayConsentFetch(endpoint, options.publicKey, fetchImpl, {
        token: stored.token!,
      })
      if (!response.ok) throw new Error(`replay consent revocation failed with status ${response.status}`)
      options.storage.removeItem(key)
    },
  }
}

async function replayConsentFetch(
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
  if (!response.ok) throw new Error(`replay consent request failed with status ${response.status}`)
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
