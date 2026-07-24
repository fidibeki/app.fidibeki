import { createAnalyticsOutbox, FunnelDeliveryError } from '../../src/analytics-outbox'
import type { FunnelEvent } from '../../src/analytics'

class MemoryStorage implements Storage {
  readonly values = new Map<string, string>()
  get length() { return this.values.size }
  clear() { this.values.clear() }
  getItem(key: string) { return this.values.get(key) ?? null }
  key(index: number) { return [...this.values.keys()][index] ?? null }
  removeItem(key: string) { this.values.delete(key) }
  setItem(key: string, value: string) { this.values.set(key, value) }
}

const now = Date.parse('2026-07-22T12:00:00Z')

function event(
  index: number,
  occurredAt = new Date(now).toISOString(),
  pageURL = 'https://example.com/chart',
): FunnelEvent {
  const suffix = index.toString(16).padStart(12, '0')
  return {
    event_id: `00000000-0000-4000-8000-${suffix}`,
    event_type: 'matched',
    visitor_id: '10000000-0000-4000-8000-000000000001',
    page_view_id: `20000000-0000-4000-8000-${suffix}`,
    rule_id: '30000000-0000-4000-8000-000000000001',
    page_url: pageURL,
    occurred_at: occurredAt,
    consent_token: 'consent-token',
  }
}

function acceptedResponse(events: FunnelEvent[]): Response {
  return new Response(JSON.stringify({
    accepted_event_ids: events.map(item => item.event_id),
    duplicate_event_ids: [],
  }), { status: 202, headers: { 'Content-Type': 'application/json' } })
}

describe('widget analytics outbox', () => {
  afterEach(() => vi.useRealTimers())

  it('flushes after five seconds and acknowledges accepted events', async () => {
    vi.useFakeTimers()
    const fetchImpl = vi.fn<typeof fetch>(async (_url, init) => {
      const payload = JSON.parse(String(init?.body)) as { events: FunnelEvent[] }
      return acceptedResponse(payload.events)
    })
    const outbox = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test',
      ownerID: 'owner-a',
      storage: new MemoryStorage(), fetchImpl, now: () => now,
    })
    outbox.enqueue(event(1))

    await vi.advanceTimersByTimeAsync(4_999)
    expect(fetchImpl).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await outbox.flush()

    expect(fetchImpl).toHaveBeenCalledTimes(1)
    expect(outbox.size).toBe(0)
    outbox.destroy()
  })

  it('flushes at ten items and caps every request at twenty events', async () => {
    const batchSizes: number[] = []
    const fetchImpl = vi.fn<typeof fetch>(async (_url, init) => {
      const payload = JSON.parse(String(init?.body)) as { events: FunnelEvent[] }
      batchSizes.push(payload.events.length)
      return acceptedResponse(payload.events)
    })
    const outbox = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test',
      ownerID: 'owner-a',
      storage: new MemoryStorage(), fetchImpl, now: () => now,
    })
    for (let index = 1; index <= 45; index++) outbox.enqueue(event(index))
    await outbox.flush()

    expect(batchSizes.every(size => size <= 20)).toBe(true)
    expect(batchSizes.reduce((sum, size) => sum + size, 0)).toBe(45)
    expect(outbox.size).toBe(0)
    outbox.destroy()
  })

  it('isolates persisted queues and acknowledgments by top-level session', async () => {
    const storage = new MemoryStorage()
    const firstEvent = event(1)
    const secondEvent = event(2)
    let acknowledgeFirst: ((response: Response) => void) | undefined
    const first = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test', ownerID: 'owner-a',
      storage, now: () => now,
      fetchImpl: vi.fn<typeof fetch>(() => new Promise<Response>(resolve => { acknowledgeFirst = resolve })),
    })
    const second = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test', ownerID: 'owner-b',
      storage, now: () => now,
      fetchImpl: vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({
        accepted_event_ids: [secondEvent.event_id], duplicate_event_ids: [firstEvent.event_id],
      }), { status: 202, headers: { 'Content-Type': 'application/json' } })),
    })

    first.enqueue(firstEvent)
    const firstFlush = first.flush()
    second.enqueue(secondEvent)
    const acknowledge = acknowledgeFirst
    if (!acknowledge) throw new Error('first outbox did not start delivery')
    acknowledge(acceptedResponse([firstEvent]))
    await firstFlush

    expect(first.size).toBe(0)
    expect(storage.getItem(`fidibeki:analytics-outbox:v2:pk_test:owner-a:${firstEvent.event_id}`)).toBeNull()
    expect(JSON.parse(storage.getItem(
      `fidibeki:analytics-outbox:v2:pk_test:owner-b:${secondEvent.event_id}`,
    )!)).toEqual(secondEvent)
    await second.flush()
    expect(second.size).toBe(0)
    first.destroy()
    second.destroy()
  })

  it('keeps fetch and beacon batches below the UTF-8 keepalive budget', async () => {
    const longPage = `https://example.com/${'é'.repeat(4_000)}`
    const fetchSizes: number[] = []
    const fetchImpl = vi.fn<typeof fetch>(async (_url, init) => {
      const body = String(init?.body)
      fetchSizes.push(new TextEncoder().encode(body).byteLength)
      const payload = JSON.parse(body) as { events: FunnelEvent[] }
      return acceptedResponse(payload.events)
    })
    const fetched = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test', ownerID: 'owner-a',
      storage: new MemoryStorage(), fetchImpl, now: () => now,
    })
    for (let index = 1; index <= 20; index++) fetched.enqueue(event(index, undefined, longPage))
    await fetched.flush()

    expect(fetchSizes.length).toBeGreaterThan(1)
    expect(fetchSizes.every(size => size <= 60 << 10)).toBe(true)
    fetched.destroy()

    const beaconBodies: string[] = []
    const beaconed = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test', ownerID: 'owner-b',
      storage: new MemoryStorage(), now: () => now,
      sendBeacon: (_url, body) => { beaconBodies.push(String(body)); return true },
    })
    for (let index = 21; index <= 29; index++) beaconed.enqueue(event(index, undefined, longPage))
    beaconed.flushOnHide()

    expect(beaconBodies.length).toBeGreaterThan(1)
    expect(beaconBodies.every(body => new TextEncoder().encode(body).byteLength <= 60 << 10)).toBe(true)
    beaconed.destroy()
  })

  it('drops and reports a singleton that cannot fit the delivery budget', async () => {
    const errors: unknown[] = []
    const fetchImpl = vi.fn<typeof fetch>()
    const outbox = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test', ownerID: 'owner-a',
      storage: new MemoryStorage(), fetchImpl, now: () => now, onError: error => errors.push(error),
    })
    outbox.enqueue(event(1, undefined, `https://example.com/${'é'.repeat(70_000)}`))

    await outbox.flush()
    expect(fetchImpl).not.toHaveBeenCalled()
    expect(outbox.size).toBe(0)
    expect(errors[0]).toMatchObject({ retryable: false })
    outbox.destroy()
  })

  it('retains at most one hundred fresh events and drops expired or malformed storage', () => {
    const storage = new MemoryStorage()
    const key = (owner: string, item: FunnelEvent) =>
      `fidibeki:analytics-outbox:v2:pk_test:${owner}:${item.event_id}`
    const expired = event(1, new Date(now - 24 * 60 * 60 * 1_000 - 1).toISOString())
    storage.setItem(key('owner-old', expired), JSON.stringify(expired))
    storage.setItem('fidibeki:analytics-outbox:v2:pk_test:owner-b:malformed', '{not-json')
    for (let index = 2; index <= 106; index++) {
      const item = event(index)
      storage.setItem(key('owner-b', item), JSON.stringify(item))
    }
    const outbox = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test',
      ownerID: 'owner-a',
      storage, fetchImpl: vi.fn<typeof fetch>(() => new Promise<Response>(() => {})), now: () => now,
    })

    expect(outbox.size).toBe(100)
    expect(storage.length).toBe(100)
    expect(storage.getItem(key('owner-b', event(6)))).toBeNull()
    expect(storage.getItem(key('owner-b', event(7)))).toBe(JSON.stringify(event(7)))
    outbox.destroy()

    storage.setItem('fidibeki:analytics-outbox:v2:pk_broken:owner-a:malformed', '{not-json')
    const recovered = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_broken',
      ownerID: 'owner-a',
      storage, fetchImpl: vi.fn<typeof fetch>(), now: () => now,
    })
    expect(recovered.size).toBe(0)
    expect(storage.getItem('fidibeki:analytics-outbox:v2:pk_broken:owner-a:malformed')).toBeNull()
    recovered.destroy()
  })

  it('removes both accepted and duplicate acknowledgments', async () => {
    const first = event(1)
    const second = event(2)
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({
      accepted_event_ids: [first.event_id], duplicate_event_ids: [second.event_id],
    }), { status: 202, headers: { 'Content-Type': 'application/json' } }))
    const outbox = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test',
      ownerID: 'owner-a',
      storage: new MemoryStorage(), fetchImpl, now: () => now,
    })
    outbox.enqueue(first)
    outbox.enqueue(second)

    await outbox.flush()
    expect(outbox.size).toBe(0)
    outbox.destroy()
  })

  it('retries transient failures but drops permanently rejected batches', async () => {
    const errors: unknown[] = []
    const queued = event(1)
    const fetchImpl = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(new Response('{}', { status: 503 }))
      .mockResolvedValueOnce(acceptedResponse([queued]))
      .mockResolvedValueOnce(new Response('{}', { status: 422 }))
    const outbox = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test',
      ownerID: 'owner-a',
      storage: new MemoryStorage(), fetchImpl, now: () => now, onError: error => errors.push(error),
    })
    outbox.enqueue(queued)

    await outbox.flush()
    expect(outbox.size).toBe(1)
    expect(errors[0]).toBeInstanceOf(FunnelDeliveryError)
    await outbox.flush()
    expect(outbox.size).toBe(0)

    outbox.enqueue(event(2))
    await outbox.flush()
    expect(outbox.size).toBe(0)
    expect(errors).toHaveLength(2)
    outbox.destroy()
  })

  it('keeps beaconed events until a later acknowledged retry', async () => {
    const sendBeacon = vi.fn(() => true)
    const fetchImpl = vi.fn<typeof fetch>(async (_url, init) => {
      const payload = JSON.parse(String(init?.body)) as { events: FunnelEvent[] }
      return new Response(JSON.stringify({
        accepted_event_ids: [], duplicate_event_ids: payload.events.map(item => item.event_id),
      }), { status: 202, headers: { 'Content-Type': 'application/json' } })
    })
    const outbox = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test',
      ownerID: 'owner-a',
      storage: new MemoryStorage(), fetchImpl, sendBeacon, now: () => now,
    })
    outbox.enqueue(event(1))
    outbox.flushOnHide()

    expect(sendBeacon).toHaveBeenCalledWith(
      'https://widget.test/v1/widget/events?key=pk_test',
      JSON.stringify({ events: [event(1)] }),
    )
    expect(outbox.size).toBe(1)
    await outbox.flush()
    expect(outbox.size).toBe(0)
    outbox.destroy()
  })

  it('falls back to keepalive fetch when beacon is unavailable or refuses the payload', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async (_url, init) => {
      const payload = JSON.parse(String(init?.body)) as { events: FunnelEvent[] }
      return acceptedResponse(payload.events)
    })
    const outbox = createAnalyticsOutbox({
      endpoint: new URL('https://widget.test/v1/widget/events'), publicKey: 'pk_test',
      ownerID: 'owner-a',
      storage: {
        length: 0, getItem: () => null, key: () => null, removeItem: () => {},
        setItem: () => { throw new Error('blocked') },
      },
      fetchImpl, sendBeacon: () => false, now: () => now,
    })
    outbox.enqueue(event(1))
    outbox.flushOnHide()
    await outbox.flush()

    expect(fetchImpl).toHaveBeenCalledTimes(1)
    expect(outbox.size).toBe(0)
    outbox.destroy()
  })
})
