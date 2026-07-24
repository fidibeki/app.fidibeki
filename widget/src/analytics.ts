import { canonicalPage } from './targeting'
import { privacySafeLocation } from './privacy-location'
import type { WidgetRule } from './types'

export type FunnelEventType = 'matched' | 'impression' | 'interaction' | 'submitted' | 'dismissed'

export interface FunnelEvent {
  event_id: string
  event_type: FunnelEventType
  visitor_id: string
  page_view_id: string
  rule_id: string
  page_url: string
  occurred_at: string
  consent_token: string
}

export interface PageViewIdentity {
  pageURL: string
  pageViewID: string
}

interface StorageLike {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

interface PageViewTrackerOptions {
  randomUUID?: () => string
}

// PageViewTracker only holds an in-memory page identity. It never touches storage or the
// network, so it is safe to create unconditionally at bootstrap regardless of consent state.
export interface PageViewTracker {
  visit(rawURL: string): PageViewIdentity
}

export function createPageViewTracker(options: PageViewTrackerOptions = {}): PageViewTracker {
  const randomUUID = options.randomUUID ?? secureRandomUUID
  let current: PageViewIdentity | null = null
  return {
    visit(rawURL) {
      const page = canonicalPage(rawURL)
      if (!page) throw new TypeError('Analytics requires an absolute HTTP(S) page URL')
      if (!current || current.pageURL !== page.target) {
        current = { pageURL: page.target, pageViewID: randomUUID() }
      }
      return current
    },
  }
}

interface FunnelTrackerOptions {
  randomUUID?: () => string
  now?: () => Date
}

// FunnelTracker owns the persistent, pseudonymous visitor identifier and emits funnel events.
// It must only ever be constructed by the caller after an active widget_analytics grant exists
// for the current rule (see loader.ts) — construction is the sole gate that determines whether
// `fidibeki:visitor:*` is ever created or read (issue #58).
export interface FunnelTracker {
  readonly visitorID: string
  record(
    eventType: FunnelEventType,
    rule: WidgetRule,
    page: PageViewIdentity,
    consentToken: string,
  ): FunnelEvent | null
}

const visitorStorageKey = 'fidibeki:visitor:v1'
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

export function createFunnelTracker(visitorID: string, options: FunnelTrackerOptions = {}): FunnelTracker {
  const randomUUID = options.randomUUID ?? secureRandomUUID
  const now = options.now ?? (() => new Date())
  const emitted = new Set<string>()

  return {
    visitorID,
    record(eventType, rule, page, consentToken) {
      const location = privacySafeLocation(page.pageURL, rule)
      if (!location) return null
      const lifecycleKey = `${page.pageViewID}:${rule.id}:${eventType}`
      if (emitted.has(lifecycleKey)) return null
      emitted.add(lifecycleKey)
      return {
        event_id: randomUUID(), event_type: eventType, visitor_id: visitorID,
        page_view_id: page.pageViewID, rule_id: rule.id,
        page_url: location.target, occurred_at: now().toISOString(),
        consent_token: consentToken,
      }
    },
  }
}

// resolveVisitorID is the only function in the widget that reads or writes the persistent
// analytics visitor identifier. Callers must only invoke it once an active widget_analytics
// grant has been confirmed for the current rule.
export function resolveVisitorID(storage: StorageLike | undefined, randomUUID: () => string = secureRandomUUID): string {
  try {
    const existing = storage?.getItem(visitorStorageKey)
    if (existing && uuidPattern.test(existing)) return existing
    const created = randomUUID()
    storage?.setItem(visitorStorageKey, created)
    return created
  } catch {
    return randomUUID()
  }
}

export function forgetVisitorID(storage: StorageLike | undefined): void {
  try {
    storage?.removeItem(visitorStorageKey)
  } catch {
    // Storage restrictions must not break withdrawal.
  }
}

function secureRandomUUID(): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') return globalThis.crypto.randomUUID()
  const bytes = new Uint8Array(16)
  globalThis.crypto.getRandomValues(bytes)
  bytes[6] = (bytes[6]! & 0x0f) | 0x40
  bytes[8] = (bytes[8]! & 0x3f) | 0x80
  const hex = [...bytes].map(value => value.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
