import { describe, expect, it, vi } from 'vitest'
import { exportSupportTranscript, reconcileThread, prependHistory, mergeMessages, supportSection, watchSupport, type SupportMessage, type SupportThread } from '../../../shared/live-support'

describe('support transport', () => {
  it('refreshes an expired token and parses a split notification without exposing credentials in the URL', async () => {
    const controller = new AbortController()
    let token = 'expired'
    const requests: Array<{ url: string; authorization: string }> = []
    let changes = 0
    const encoder = new TextEncoder()
    await watchSupport({ url: '/v1/support/events', token: () => token, signal: controller.signal,
      refreshAuth: async () => { token = 'fresh' }, onState: () => {},
      onChange: () => { changes++; controller.abort() },
      fetcher: (async (url, init) => {
        requests.push({ url: String(url), authorization: new Headers(init?.headers).get('Authorization') || '' })
        if (requests.length === 1) return new Response('', { status: 401 })
        return new Response(new ReadableStream({ start(stream) {
          stream.enqueue(encoder.encode('event: cha')); stream.enqueue(encoder.encode('nge\ndata: {}\n\n'))
        } }), { headers: { 'Content-Type': 'text/event-stream' } })
      }) as typeof fetch,
    })
    expect(changes).toBe(1)
    expect(requests).toEqual([
      { url: '/v1/support/events', authorization: 'Bearer expired' },
      { url: '/v1/support/events', authorization: 'Bearer fresh' },
    ])
  })
  it('routes a presence event without reloading conversations', async () => {
    const controller = new AbortController(); let changes = 0; let presence = 0
    await watchSupport({ url: '/events', token: () => 'token', signal: controller.signal,
      refreshAuth: async () => {}, onState: () => {}, onChange: () => { changes++ },
      onPresence: () => { presence++; controller.abort() },
      fetcher: (async () => new Response(new ReadableStream({ start(stream) { stream.enqueue(new TextEncoder().encode('event: presence\ndata: {}\n\n')) } }), { headers: { 'Content-Type': 'text/event-stream' } })) as typeof fetch,
    })
    expect(presence).toBe(1); expect(changes).toBe(0)
  })
  it('stops permanently when access is revoked', async () => {
    const states: string[] = []; let count = 0
    await watchSupport({ url: '/events', token: () => 'token', signal: new AbortController().signal,
      refreshAuth: async () => { throw new Error('must not refresh forbidden credentials') },
      onState: value => states.push(value), onChange: () => { throw new Error('must not read data') },
      fetcher: (async () => { count++; return new Response('', { status: 403 }) }) as typeof fetch,
    })
    expect(states).toEqual(['forbidden']); expect(count).toBe(1)
  })
  it('cancels a reconnect wait when the component unmounts', async () => {
    const controller = new AbortController(); let count = 0
    await watchSupport({ url: '/events', token: () => 'token', signal: controller.signal,
      refreshAuth: async () => {}, onChange: () => {}, onState: state => { if (state === 'offline') controller.abort() },
      fetcher: (async () => { count++; throw new Error('offline') }) as typeof fetch,
    })
    expect(count).toBe(1)
  })
})

describe('support state', () => {
  it('reconciles a retried send with its streamed snapshot exactly once', () => {
    const message = { id: 'a', sequence: 2, body: 'Hello' } as SupportMessage
    expect(mergeMessages([message], [{ ...message }, { id: 'b', sequence: 1 } as SupportMessage]).map(item => item.id)).toEqual(['b', 'a'])
  })
  it('keeps disconnected history pageable after more than 50 messages arrive', () => {
    const current = { conversation: { id: 'c', last_sequence: 1 }, messages: [{ id: '1', sequence: 1 }], has_more: false } as SupportThread
    const incoming = { conversation: { id: 'c', last_sequence: 61 }, messages: Array.from({ length: 50 }, (_, i) => ({ id: String(i + 12), sequence: i + 12 })), has_more: true } as SupportThread
    const result = reconcileThread(current, incoming)
    expect(result.messages[0]?.sequence).toBe(12)
    expect(result.has_more).toBe(true)
    expect(reconcileThread(result, current)).toBe(result)
  })
  it('rejects a history gap even when a send response has already appended the newest message', () => {
    const current = { conversation: { id: 'c', last_sequence: 1 }, messages: [{ id: '1', sequence: 1 }, { id: '62', sequence: 62 }], has_more: false } as SupportThread
    const incoming = { conversation: { id: 'c', last_sequence: 62 }, messages: Array.from({ length: 50 }, (_, i) => ({ id: String(i + 13), sequence: i + 13 })), has_more: true } as SupportThread
    expect(reconcileThread(current, incoming)).toBe(incoming)
    const older = { conversation: { id: 'c', last_sequence: 62 }, messages: [{ id: '2', sequence: 2 }], has_more: false } as SupportThread
    expect(prependHistory(incoming, older)).toBe(incoming)
  })
  it('preserves already loaded history when the latest page overlaps', () => {
    const current = { conversation: { id: 'c', last_sequence: 2 }, messages: [{ id: '1', sequence: 1 }, { id: '2', sequence: 2 }], has_more: false } as SupportThread
    const incoming = { conversation: { id: 'c', last_sequence: 3 }, messages: [{ id: '2', sequence: 2 }, { id: '3', sequence: 3 }], has_more: true } as SupportThread
    expect(reconcileThread(current, incoming).messages.map(message => message.sequence)).toEqual([1, 2, 3])
    expect(reconcileThread(current, incoming).has_more).toBe(false)
  })
  it('reports only known app sections and strips private route details', () => {
    expect(supportSection('/projects/private-project-id')).toBe('projects')
    expect(supportSection('/')).toBe('overview')
    expect(supportSection('/customer-secret')).toBe('other')
  })
})

describe('private transcript download', () => {
  const messages = Array.from({ length: 61 }, (_, i) => ({ id: String(i + 1), sequence: i + 1, sender_type: 'admin', sender_name: 'Ada', body: `Message ${i + 1}`, client_id: 'private-key' })) as SupportMessage[]
  const conversation = { id: 'c', tenant_id: 't', member_id: 'm', last_sequence: 61 } as SupportThread['conversation']
  it('exports every page in order, excludes retry keys and keeps the initial cutoff', async () => {
    const beforeValues: Array<number | undefined> = []
    const blob = await exportSupportTranscript(async before => {
      beforeValues.push(before)
      return before ? { conversation: { ...conversation!, last_sequence: 62 }, messages: messages.slice(0, 11), has_more: false }
        : { conversation, messages: messages.slice(11), has_more: true }
    })
    const data = JSON.parse(await blob.text())
    expect(data.messages.map((m: SupportMessage) => m.sequence)).toEqual(messages.map(m => m.sequence))
    expect(data.last_sequence).toBe(61)
    expect(await blob.text()).not.toContain('private-key')
    expect(beforeValues).toEqual([undefined, 12])
  })
  it('rejects gaps and a removed or replaced conversation', async () => {
    await expect(exportSupportTranscript(async () => ({ conversation, messages: messages.slice(1), has_more: false }))).rejects.toThrow('changed')
    let calls = 0
    await expect(exportSupportTranscript(async () => ++calls === 1
      ? { conversation, messages: messages.slice(11), has_more: true }
      : { conversation: { ...conversation!, id: 'other' }, messages: messages.slice(0, 11), has_more: false })).rejects.toThrow('changed')
    await expect(exportSupportTranscript(async () => ({ conversation: null, messages: [], has_more: false }))).rejects.toThrow('unavailable')
  })
  it('cancels on unmount without producing a partial download', async () => {
    const controller = new AbortController()
    await expect(exportSupportTranscript(async () => { controller.abort(); return { conversation, messages, has_more: false } }, controller.signal)).rejects.toThrow('interrupted')
  })
  it('aborts a stalled export after twenty seconds', async () => {
    vi.useFakeTimers()
    try {
      const download = exportSupportTranscript(async (_, signal) => new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new Error('aborted')))))
      const result = expect(download).rejects.toThrow('aborted')
      await vi.advanceTimersByTimeAsync(20_000)
      await result
    } finally { vi.useRealTimers() }
  })
  it('bounds encoded Unicode output, not just character count', async () => {
    const message = { ...messages[0]!, body: '😀'.repeat(4000) }
    await expect(exportSupportTranscript(async () => ({ conversation, messages: Array.from({ length: 1400 }, () => message), has_more: false }))).rejects.toThrow('20 MiB')
  })
})
