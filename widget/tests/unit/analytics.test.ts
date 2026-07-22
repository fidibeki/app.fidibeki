import { createAnalyticsTracker } from '../../src/analytics'
import { reportFunnelEvents } from '../../src/analytics-outbox'

class MemoryStorage implements Storage {
  private readonly values = new Map<string, string>()
  get length() { return this.values.size }
  clear() { this.values.clear() }
  getItem(key: string) { return this.values.get(key) ?? null }
  key(index: number) { return [...this.values.keys()][index] ?? null }
  removeItem(key: string) { this.values.delete(key) }
  setItem(key: string, value: string) { this.values.set(key, value) }
}

const ids = [
  '00000000-0000-4000-8000-000000000001',
  '00000000-0000-4000-8000-000000000002',
  '00000000-0000-4000-8000-000000000003',
  '00000000-0000-4000-8000-000000000004',
  '00000000-0000-4000-8000-000000000005',
  '00000000-0000-4000-8000-000000000006',
]

function idSequence() {
  let index = 0
  return () => ids[index++]!
}

describe('widget funnel identity', () => {
  it('persists one anonymous visitor without deriving browser characteristics', () => {
    const storage = new MemoryStorage()
    const first = createAnalyticsTracker({ storage, randomUUID: idSequence() })
    const second = createAnalyticsTracker({ storage, randomUUID: idSequence() })

    expect(first.visitorID).toBe(ids[0])
    expect(second.visitorID).toBe(ids[0])
    expect(storage.length).toBe(1)
  })

  it('uses an ephemeral visitor when persistent storage is unavailable', () => {
    const storage = {
      getItem() { throw new Error('blocked') },
      setItem() { throw new Error('blocked') },
      removeItem() {},
    }
    const tracker = createAnalyticsTracker({ storage, randomUUID: idSequence() })
    expect(tracker.visitorID).toBe(ids[0])
  })

  it('keeps one page view for query, hash, and trailing slash changes', () => {
    const tracker = createAnalyticsTracker({ storage: new MemoryStorage(), randomUUID: idSequence() })

    const first = tracker.visit('https://example.com/chart/?asset=BAI#price')
    const equivalent = tracker.visit('https://example.com/chart?asset=BIC#details')
    const distinct = tracker.visit('https://example.com/portfolio')
    const returned = tracker.visit('https://example.com/chart')

    expect(first).toEqual({ pageURL: 'https://example.com/chart', pageViewID: ids[1] })
    expect(equivalent).toEqual(first)
    expect(distinct.pageViewID).toBe(ids[2])
    expect(returned.pageViewID).toBe(ids[3])
  })

  it('emits matched once per rule and canonical page view', () => {
    const tracker = createAnalyticsTracker({
      storage: new MemoryStorage(), randomUUID: idSequence(),
      now: () => new Date('2026-07-22T12:00:00Z'),
    })
    tracker.visit('https://example.com/chart?asset=BAI')

    const first = tracker.record('matched', '11111111-1111-4111-8111-111111111111')
    const duplicate = tracker.record('matched', '11111111-1111-4111-8111-111111111111')
    tracker.visit('https://example.com/other')
    const nextPage = tracker.record('matched', '11111111-1111-4111-8111-111111111111')

    expect(first).toMatchObject({
      event_id: ids[2], event_type: 'matched', visitor_id: ids[0], page_view_id: ids[1],
      rule_id: '11111111-1111-4111-8111-111111111111',
      page_url: 'https://example.com/chart', occurred_at: '2026-07-22T12:00:00.000Z',
    })
    expect(duplicate).toBeNull()
    expect(nextPage?.page_view_id).toBe(ids[3])
  })

  it('reports a bounded event payload without credentials', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(new Response(
      '{"accepted_event_ids":["00000000-0000-4000-8000-000000000003"],"duplicate_event_ids":[]}',
      { status: 202, headers: { 'Content-Type': 'application/json' } },
    ))
    const tracker = createAnalyticsTracker({
      storage: new MemoryStorage(), randomUUID: idSequence(),
      now: () => new Date('2026-07-22T12:00:00Z'),
    })
    tracker.visit('https://example.com/chart?asset=BAI')
    const event = tracker.record('matched', '11111111-1111-4111-8111-111111111111')!

    await reportFunnelEvents({
      endpoint: new URL('https://widget.test/v1/widget/events'),
      publicKey: 'pk_live_test', events: [event], fetchImpl,
    })

    const [url, options] = fetchImpl.mock.calls[0] ?? []
    expect(String(url)).toBe('https://widget.test/v1/widget/events?key=pk_live_test')
    expect(options).toMatchObject({ method: 'POST', mode: 'cors', credentials: 'omit', keepalive: true })
    expect(new Headers(options?.headers).get('X-Fidibeki-Public-Key')).toBe('pk_live_test')
    expect(JSON.parse(String(options?.body))).toEqual({ events: [event] })
  })
})
