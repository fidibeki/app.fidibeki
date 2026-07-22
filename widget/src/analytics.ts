import { canonicalPage } from './targeting'

export type FunnelEventType = 'matched' | 'impression' | 'interaction' | 'submitted' | 'dismissed'

export interface FunnelEvent {
  event_id: string
  event_type: FunnelEventType
  visitor_id: string
  page_view_id: string
  rule_id: string
  page_url: string
  occurred_at: string
}

export interface PageViewIdentity {
  pageURL: string
  pageViewID: string
}

interface StorageLike {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

interface TrackerOptions {
  storage?: StorageLike | undefined
  randomUUID?: () => string
  now?: () => Date
}

export interface AnalyticsTracker {
  readonly visitorID: string
  visit(rawURL: string): PageViewIdentity
  record(eventType: FunnelEventType, ruleID: string): FunnelEvent | null
}

export interface FunnelEventRequest {
  endpoint: URL
  publicKey: string
  events: FunnelEvent[]
  fetchImpl?: typeof fetch
}

export async function reportFunnelEvents(request: FunnelEventRequest): Promise<void> {
  if (request.events.length === 0) return
  const fetchImpl = request.fetchImpl ?? fetch
  const url = new URL(request.endpoint)
  url.searchParams.set('key', request.publicKey)
  const response = await fetchImpl(url, {
    method: 'POST',
    mode: 'cors',
    credentials: 'omit',
    keepalive: true,
    headers: {
      'Content-Type': 'application/json',
      'X-Fidibeki-Public-Key': request.publicKey,
    },
    body: JSON.stringify({ events: request.events }),
  })
  if (!response.ok) {
    throw new Error(`Fidibeki analytics report failed with status ${response.status}`)
  }
}

const visitorStorageKey = 'fidibeki:visitor:v1'
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

export function createAnalyticsTracker(options: TrackerOptions = {}): AnalyticsTracker {
  const randomUUID = options.randomUUID ?? secureRandomUUID
  const now = options.now ?? (() => new Date())
  const visitorID = persistentVisitorID(options.storage, randomUUID)
  const emitted = new Set<string>()
  let current: PageViewIdentity | null = null

  return {
    visitorID,
    visit(rawURL) {
      const page = canonicalPage(rawURL)
      if (!page) throw new TypeError('Analytics requires an absolute HTTP(S) page URL')
      if (!current || current.pageURL !== page.target) {
        current = { pageURL: page.target, pageViewID: randomUUID() }
      }
      return current
    },
    record(eventType, ruleID) {
      if (!current) return null
      const lifecycleKey = `${current.pageViewID}:${ruleID}:${eventType}`
      if (emitted.has(lifecycleKey)) return null
      emitted.add(lifecycleKey)
      return {
        event_id: randomUUID(), event_type: eventType, visitor_id: visitorID,
        page_view_id: current.pageViewID, rule_id: ruleID,
        page_url: current.pageURL, occurred_at: now().toISOString(),
      }
    },
  }
}

function persistentVisitorID(storage: StorageLike | undefined, randomUUID: () => string): string {
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

function secureRandomUUID(): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') return globalThis.crypto.randomUUID()
  const bytes = new Uint8Array(16)
  globalThis.crypto.getRandomValues(bytes)
  bytes[6] = (bytes[6]! & 0x0f) | 0x40
  bytes[8] = (bytes[8]! & 0x3f) | 0x80
  const hex = [...bytes].map(value => value.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
