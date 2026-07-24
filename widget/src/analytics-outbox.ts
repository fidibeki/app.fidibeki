import type { FunnelEvent } from './analytics'

export interface FunnelDeliveryResult {
  accepted_event_ids: string[]
  duplicate_event_ids: string[]
}

export interface FunnelEventRequest {
  endpoint: URL
  publicKey: string
  events: FunnelEvent[]
  fetchImpl?: typeof fetch
}

export class FunnelDeliveryError extends Error {
  constructor(message: string, readonly retryable: boolean) {
    super(message)
    this.name = 'FunnelDeliveryError'
  }
}

interface FunnelBatch {
  events: FunnelEvent[]
  body: string
}

export async function reportFunnelEvents(request: FunnelEventRequest): Promise<FunnelDeliveryResult> {
  if (request.events.length === 0) return { accepted_event_ids: [], duplicate_event_ids: [] }
  return deliverBatch(
    request.endpoint,
    request.publicKey,
    buildFunnelBatch(request.events),
    request.fetchImpl ?? fetch,
  )
}

interface StorageLike {
  readonly length: number
  getItem(key: string): string | null
  key(index: number): string | null
  removeItem(key: string): void
  setItem(key: string, value: string): void
}

interface AnalyticsOutboxOptions {
  endpoint: URL
  publicKey: string
  ownerID: string
  storage?: StorageLike | undefined
  fetchImpl?: typeof fetch
  sendBeacon?: ((url: string, data?: BodyInit | null) => boolean) | undefined
  now?: () => number
  setTimeout?: (callback: () => void, delay: number) => unknown
  clearTimeout?: (timer: unknown) => void
  onError?: (error: unknown) => void
}

export interface AnalyticsOutbox {
  readonly size: number
  enqueue(event: FunnelEvent): void
  flush(): Promise<void>
  flushOnHide(): void
  /**
   * Aborts any in-flight delivery, discards the in-memory queue, and removes every persisted
   * event for this outbox owner, then permanently stops the outbox. Used for consent withdrawal,
   * where queued analytics and any in-flight request must never reach the backend (issue #58).
   */
  destroy(): void
}

interface QueuedEvent {
  event: FunnelEvent
  storageKey: string
}

const maximumEvents = 100
const maximumBatchEvents = 20
const maximumBatchBytes = 60 << 10
const flushThreshold = 10
const flushDelayMilliseconds = 5_000
const maximumAgeMilliseconds = 24 * 60 * 60 * 1_000
const outboxPrefix = 'fidibeki:analytics-outbox:v2:'

export function createAnalyticsOutbox(options: AnalyticsOutboxOptions): AnalyticsOutbox {
  const now = options.now ?? Date.now
  const setTimer = options.setTimeout ?? ((callback: () => void, delay: number) => globalThis.setTimeout(callback, delay))
  const clearTimer = options.clearTimeout ?? ((value: unknown) =>
    globalThis.clearTimeout(value as ReturnType<typeof globalThis.setTimeout>))
  const projectPrefix = `${outboxPrefix}${options.publicKey}:`
  const ownerPrefix = `${projectPrefix}${options.ownerID}:`
  let events: QueuedEvent[] = []
  let timer: unknown | null = null
  let flushing: Promise<void> | null = null
  let destroyed = false
  let inFlight: AbortController | null = null

  const reportError = (error: unknown) => {
    try {
      options.onError?.(error)
    } catch {
      // Host callbacks cannot interfere with analytics or widget behavior.
    }
  }
  const removeStored = (storageKey: string) => {
    try {
      options.storage?.removeItem(storageKey)
    } catch {
      // Restricted storage falls back to this page's in-memory queue.
    }
  }
  const normalize = (queue: QueuedEvent[]) => {
    const oldest = now() - maximumAgeMilliseconds
    const unique = new Map<string, QueuedEvent>()
    for (const queued of queue) {
      const occurredAt = Date.parse(queued.event.occurred_at)
      if (!Number.isFinite(occurredAt) || occurredAt < oldest) {
        removeStored(queued.storageKey)
        continue
      }
      const existing = unique.get(queued.event.event_id)
      if (!existing) unique.set(queued.event.event_id, queued)
      else if (existing.storageKey !== queued.storageKey) removeStored(queued.storageKey)
    }
    const ordered = [...unique.values()].sort((left, right) =>
      Date.parse(left.event.occurred_at) - Date.parse(right.event.occurred_at) ||
      left.storageKey.localeCompare(right.storageKey))
    const excess = Math.max(0, ordered.length - maximumEvents)
    for (const queued of ordered.slice(0, excess)) removeStored(queued.storageKey)
    return ordered.slice(excess)
  }
  const synchronize = () => {
    const persisted = loadEvents(options.storage, projectPrefix, removeStored)
    events = normalize([...events, ...persisted])
  }
  const cancelTimer = () => {
    if (timer === null) return
    clearTimer(timer)
    timer = null
  }
  const schedule = () => {
    if (destroyed || timer !== null || events.length === 0) return
    timer = setTimer(() => {
      timer = null
      void flush()
    }, flushDelayMilliseconds)
  }
  const removeEventIDs = (eventIDs: Set<string>) => {
    const retained: QueuedEvent[] = []
    for (const queued of events) {
      if (eventIDs.has(queued.event.event_id)) removeStored(queued.storageKey)
      else retained.push(queued)
    }
    events = retained
  }
  const runFlush = async () => {
    cancelTimer()
    synchronize()
    while (events.length > 0) {
      let batch: FunnelBatch
      try {
        batch = buildFunnelBatch(events.map(queued => queued.event))
      } catch (error) {
        reportError(error)
        const oversized = events.shift()
        if (oversized) removeStored(oversized.storageKey)
        continue
      }
      const controller = new AbortController()
      inFlight = controller
      try {
        const result = await deliverBatch(
          options.endpoint,
          options.publicKey,
          batch,
          options.fetchImpl ?? fetch,
          controller.signal,
        )
        const acknowledged = new Set([...result.accepted_event_ids, ...result.duplicate_event_ids])
        const before = events.length
        removeEventIDs(acknowledged)
        if (events.length === before) break
      } catch (error) {
        if (destroyed) break
        reportError(error)
        if (error instanceof FunnelDeliveryError && !error.retryable) {
          removeEventIDs(new Set(batch.events.map(event => event.event_id)))
          continue
        }
        break
      } finally {
        if (inFlight === controller) inFlight = null
      }
    }
  }
  const flush = (): Promise<void> => {
    if (destroyed) return Promise.resolve()
    if (flushing) return flushing
    flushing = runFlush().finally(() => {
      flushing = null
      schedule()
    })
    return flushing
  }

  synchronize()
  if (events.length >= flushThreshold) void flush()
  else schedule()

  return {
    get size() { return events.length },
    enqueue(event) {
      if (destroyed || event.event_type === 'submitted') return
      synchronize()
      if (events.some(queued => queued.event.event_id === event.event_id)) return
      const queued = { event, storageKey: ownerPrefix + event.event_id }
      try {
        options.storage?.setItem(queued.storageKey, JSON.stringify(event))
      } catch {
        // The in-memory queue remains usable when storage is unavailable.
      }
      events = normalize([...events, queued])
      if (events.length >= flushThreshold) void flush()
      else schedule()
    },
    flush,
    flushOnHide() {
      if (destroyed) return
      cancelTimer()
      synchronize()
      if (events.length === 0) return
      const beacon = options.sendBeacon
      if (!beacon) {
        void flush()
        return
      }
      const url = analyticsURL(options.endpoint, options.publicKey).toString()
      let remaining = events.map(queued => queued.event)
      while (remaining.length > 0) {
        let batch: FunnelBatch
        try {
          batch = buildFunnelBatch(remaining)
        } catch (error) {
          reportError(error)
          removeEventIDs(new Set([remaining[0]!.event_id]))
          remaining = remaining.slice(1)
          continue
        }
        if (!beacon(url, batch.body)) {
          void flush()
          return
        }
        remaining = remaining.slice(batch.events.length)
      }
    },
    destroy() {
      if (destroyed) return
      destroyed = true
      cancelTimer()
      inFlight?.abort()
      inFlight = null
      for (const queued of events) removeStored(queued.storageKey)
      events = []
      for (const queued of loadEvents(options.storage, ownerPrefix, removeStored)) {
        removeStored(queued.storageKey)
      }
    },
  }
}

export function buildFunnelBatch(candidates: FunnelEvent[]): FunnelBatch {
  let events: FunnelEvent[] = []
  let body = JSON.stringify({ events })
  for (const event of candidates.slice(0, maximumBatchEvents)) {
    const nextEvents = [...events, event]
    const nextBody = JSON.stringify({ events: nextEvents })
    if (utf8Size(nextBody) > maximumBatchBytes) {
      if (events.length === 0) {
        throw new FunnelDeliveryError('Fidibeki analytics event exceeds the delivery size limit', false)
      }
      break
    }
    events = nextEvents
    body = nextBody
  }
  return { events, body }
}

async function deliverBatch(
  endpoint: URL,
  publicKey: string,
  batch: FunnelBatch,
  fetchImpl: typeof fetch,
  signal?: AbortSignal,
): Promise<FunnelDeliveryResult> {
  const response = await fetchImpl(analyticsURL(endpoint, publicKey), {
    method: 'POST', mode: 'cors', credentials: 'omit', keepalive: true,
    ...(signal ? { signal } : {}),
    headers: {
      'Content-Type': 'application/json',
      'X-Fidibeki-Public-Key': publicKey,
    },
    body: batch.body,
  })
  if (!response.ok) {
    const retryable = response.status === 408 || response.status === 425 ||
      response.status === 429 || response.status >= 500
    throw new FunnelDeliveryError(
      `Fidibeki analytics report failed with status ${response.status}`,
      retryable,
    )
  }
  const result: unknown = await response.json()
  if (!isDeliveryResult(result)) {
    throw new FunnelDeliveryError('Fidibeki analytics report returned an invalid response', true)
  }
  return result
}

function analyticsURL(endpoint: URL, publicKey: string): URL {
  const url = new URL(endpoint)
  url.searchParams.set('key', publicKey)
  return url
}

function loadEvents(
  storage: StorageLike | undefined,
  prefix: string,
  remove: (storageKey: string) => void,
): QueuedEvent[] {
  if (!storage) return []
  try {
    const keys: string[] = []
    for (let index = 0; index < storage.length; index++) {
      const key = storage.key(index)
      if (key?.startsWith(prefix)) keys.push(key)
    }
    const events: QueuedEvent[] = []
    for (const storageKey of keys) {
      try {
        const raw = storage.getItem(storageKey)
        const parsed: unknown = raw ? JSON.parse(raw) : null
        if (!isFunnelEvent(parsed)) {
          remove(storageKey)
          continue
        }
        events.push({ event: parsed, storageKey })
      } catch {
        remove(storageKey)
      }
    }
    return events
  } catch {
    return []
  }
}

function isFunnelEvent(value: unknown): value is FunnelEvent {
  if (!value || typeof value !== 'object') return false
  const event = value as Partial<FunnelEvent>
  return isUUID(event.event_id) &&
    ['matched', 'impression', 'interaction', 'dismissed'].includes(String(event.event_type)) &&
    isUUID(event.visitor_id) && isUUID(event.page_view_id) && isUUID(event.rule_id) &&
    typeof event.page_url === 'string' && event.page_url.length <= 4096 &&
    typeof event.occurred_at === 'string' &&
    isOpaqueToken(event.consent_token)
}

function isOpaqueToken(value: unknown): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9_-]{1,64}$/.test(value)
}

function isDeliveryResult(value: unknown): value is FunnelDeliveryResult {
  if (!value || typeof value !== 'object') return false
  const result = value as Partial<FunnelDeliveryResult>
  return Array.isArray(result.accepted_event_ids) && result.accepted_event_ids.every(id => typeof id === 'string') &&
    Array.isArray(result.duplicate_event_ids) && result.duplicate_event_ids.every(id => typeof id === 'string')
}

function isUUID(value: unknown): value is string {
  return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value)
}

function utf8Size(value: string): number {
  return new TextEncoder().encode(value).byteLength
}
