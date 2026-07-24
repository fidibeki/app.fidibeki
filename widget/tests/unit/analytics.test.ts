import { createFunnelTracker, createPageViewTracker, resolveVisitorID } from '../../src/analytics'
import { reportFunnelEvents } from '../../src/analytics-outbox'
import type { WidgetRule } from '../../src/types'

class MemoryStorage implements Storage {
  private readonly values = new Map<string, string>()
  get length() { return this.values.size }
  clear() { this.values.clear() }
  getItem(key: string) { return this.values.get(key) ?? null }
  key(index: number) { return [...this.values.keys()][index] ?? null }
  removeItem(key: string) { this.values.delete(key) }
  setItem(key: string, value: string) { this.values.set(key, value) }
}

const consentToken = 'consent-token'

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
  it('persists one pseudonymous visitor without deriving browser characteristics', () => {
    const storage = new MemoryStorage()
    const first = resolveVisitorID(storage, idSequence())
    const second = resolveVisitorID(storage, idSequence())

    expect(first).toBe(ids[0])
    expect(second).toBe(ids[0])
    expect(storage.length).toBe(1)
  })

  it('uses an ephemeral visitor when persistent storage is unavailable', () => {
    const storage = {
      getItem() { throw new Error('blocked') },
      setItem() { throw new Error('blocked') },
      removeItem() {},
    }
    const visitorID = resolveVisitorID(storage, idSequence())
    expect(visitorID).toBe(ids[0])
  })

  it('keeps one page view for query, hash, and trailing slash changes', () => {
    const tracker = createPageViewTracker({ randomUUID: idSequence() })

    const first = tracker.visit('https://example.com/chart/?asset=BAI#price')
    const equivalent = tracker.visit('https://example.com/chart?asset=BIC#details')
    const distinct = tracker.visit('https://example.com/portfolio')
    const returned = tracker.visit('https://example.com/chart')

    expect(first).toEqual({ pageURL: 'https://example.com/chart', pageViewID: ids[0] })
    expect(equivalent).toEqual(first)
    expect(distinct.pageViewID).toBe(ids[1])
    expect(returned.pageViewID).toBe(ids[2])
  })

  it('emits matched once per rule and canonical page view', () => {
    const pageViews = createPageViewTracker({ randomUUID: idSequence() })
    const funnel = createFunnelTracker(ids[0]!, {
      randomUUID: idSequence(), now: () => new Date('2026-07-22T12:00:00Z'),
    })
    const page = pageViews.visit('https://example.com/chart?asset=BAI')

    const first = funnel.record('matched', exactRule('https://example.com/chart'), page, consentToken)
    const duplicate = funnel.record('matched', exactRule('https://example.com/chart'), page, consentToken)
    const nextPage = pageViews.visit('https://example.com/other')
    const nextEvent = funnel.record('matched', exactRule('https://example.com/chart'), nextPage, consentToken)

    expect(first).toMatchObject({
      event_id: ids[0], event_type: 'matched', visitor_id: ids[0], page_view_id: ids[0],
      rule_id: '11111111-1111-4111-8111-111111111111',
      page_url: 'https://example.com/chart', occurred_at: '2026-07-22T12:00:00.000Z',
      consent_token: consentToken,
    })
    expect(duplicate).toBeNull()
    expect(nextEvent?.page_view_id).toBe(ids[1])
  })

  it('reports a bounded event payload without credentials', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(new Response(
      '{"accepted_event_ids":["00000000-0000-4000-8000-000000000003"],"duplicate_event_ids":[]}',
      { status: 202, headers: { 'Content-Type': 'application/json' } },
    ))
    const pageViews = createPageViewTracker({ randomUUID: idSequence() })
    const funnel = createFunnelTracker(ids[0]!, {
      randomUUID: idSequence(), now: () => new Date('2026-07-22T12:00:00Z'),
    })
    const page = pageViews.visit('https://example.com/chart?asset=BAI')
    const event = funnel.record('matched', exactRule('https://example.com/chart'), page, consentToken)!

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

  it('never emits an unapproved personal path suffix', () => {
    const pageViews = createPageViewTracker({ randomUUID: idSequence() })
    const funnel = createFunnelTracker(ids[0]!, {
      randomUUID: idSequence(), now: () => new Date('2026-07-22T12:00:00Z'),
    })
    const page = pageViews.visit('https://example.com/accounts/jane.private@example.test/48291?token=SECRET#profile')
    const event = funnel.record('matched', {
      ...exactRule('https://example.com/accounts'),
      matcher_type: 'url_prefix',
      include_conditions: [{
        id: 'include', matcher_type: 'url_prefix',
        matcher_value: 'https://example.com/accounts', position: 0,
      }],
    }, page, consentToken)

    expect(event?.page_url).toBe('https://example.com/accounts')
    expect(JSON.stringify(event)).not.toMatch(/jane\.private|48291|SECRET/)
  })
})

function exactRule(target: string): WidgetRule {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    matcher_type: 'url_exact',
    matcher_value: target,
    include_conditions: [{
      id: 'include', matcher_type: 'url_exact', matcher_value: target, position: 0,
    }],
  } as WidgetRule
}
