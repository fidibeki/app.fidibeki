import { describe, expect, it } from 'vitest'
import { emptyChat, exportChat, reconcileChat, type ChatThread } from '../../../shared/website-chat'

const thread = (sequences: number[], last = Math.max(...sequences), more = false): ChatThread => ({
  conversation: { id: 'one', project_id: 'project', name: 'Visitor', last_sequence: last, visitor_read_sequence: 0, operator_read_sequence: 0, updated_at: '', expires_at: '', unread: 0, preview: '' },
  messages: sequences.map(sequence => ({ id: String(sequence), sequence, client_id: String(sequence), sender_type: 'operator', sender_name: 'Team', body: 'Hello', created_at: '' })), has_more: more,
})
describe('website conversation recovery', () => {
  it('replaces disconnected history so a missing middle remains loadable', () => {
    expect(reconcileChat(thread([1, 2]), thread([80, 81], 81, true))).toEqual(thread([80, 81], 81, true))
    expect(reconcileChat(thread([1, 2, 81], 81), thread([80, 81], 81, true))).toEqual(thread([80, 81], 81, true))
  })
  it('keeps newer snapshots and clears an unavailable conversation', () => {
    expect(reconcileChat(thread([1, 2]), thread([1]))).toEqual(thread([1, 2]))
    expect(reconcileChat(thread([1]), emptyChat())).toEqual(emptyChat())
  })
  it('downloads a complete fixed-cutoff transcript and refuses gaps', async () => {
    const blob = await exportChat(async before => before ? thread([1], 3) : thread([2, 3], 3, true))
    expect(JSON.parse(await blob.text()).messages.map((row: { sequence: number }) => row.sequence)).toEqual([1, 2, 3])
    await expect(exportChat(async () => thread([2], 2))).rejects.toThrow('Incomplete')
  })
})

import { mountChat } from '../../src/chat'
import { MemoryStorage } from '../../src/storage'
import type { WidgetConfiguration } from '../../src/types'
import { vi } from 'vitest'

describe('website chat widget', () => {
  it('only enables replies after the tenant initiates and keeps feedback in the same panel', async () => {
    const language = vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['pt-AO', 'en'])
    let current = emptyChat()
    let stream: ReadableStreamDefaultController<Uint8Array> | undefined
    const sent: unknown[] = []
    const visitor = '11111111-1111-4111-8111-111111111111'
    const fetcher = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input))
      if (url.pathname.endsWith('/session')) return Response.json({ token: `${visitor}.${'a'.repeat(43)}`, visitor_id: visitor, expires_at: new Date(Date.now() + 86_400_000).toISOString() }, { status: 201 })
      if (url.pathname.endsWith('/events')) return new Response(new ReadableStream<Uint8Array>({ start(controller) { stream = controller; controller.enqueue(new TextEncoder().encode('event: change\ndata: {}\n\n')); init?.signal?.addEventListener('abort', () => controller.close(), { once: true }) } }), { headers: { 'Content-Type': 'text/event-stream' } })
      if (url.pathname.endsWith('/conversation')) return Response.json(current)
      if (url.pathname.endsWith('/messages')) { sent.push(JSON.parse(String(init?.body))); return Response.json({ message: current.messages[0] }) }
      return new Response(null, { status: 204 })
    })
    const configuration = { project_id: 'project', default_locale: 'en', chat: { enabled: true, name: 'Shop team' } } as WidgetConfiguration
    const feedback = vi.fn(async (target: HTMLElement) => { target.textContent = 'Existing feedback form'; return { mounted: true, unmount() { target.textContent = '' } } })
    const storage = new MemoryStorage()
    const chat = mountChat({ configuration, rule: null, publicKey: 'public', apiBase: new URL('https://widget.example/v1/'), storage, fetcher: fetcher as typeof fetch, mountFeedback: feedback })
    try {
      const shadow = document.querySelector('#fidibeki-chat')!.shadowRoot!
      shadow.querySelector<HTMLButtonElement>('.launcher')!.click()
      await vi.waitFor(() => expect(fetcher).toHaveBeenCalled())
      expect(fetcher.mock.calls.find(([url]) => String(url).includes('/session'))?.[1]?.body).toBe(JSON.stringify({ locale: 'pt-AO' }))
      await vi.waitFor(() => expect(fetcher.mock.calls.some(([url, init]) => String(url).includes('/presence') && JSON.parse(String(init?.body)).locale === 'pt-AO')).toBe(true))
      expect(shadow.querySelector<HTMLElement>('.compose')!.hidden).toBe(true)
      current = thread([1]); current.conversation!.unread = 1
      await vi.waitFor(() => expect(stream).toBeDefined())
      stream!.enqueue(new TextEncoder().encode('event: change\ndata: {}\n\n'))
      await vi.waitFor(() => expect(shadow.querySelector<HTMLElement>('.compose')!.hidden).toBe(false))
      expect(shadow.querySelector('.messages')!.textContent).toContain('Hello')
      const input = shadow.querySelector('textarea')!; input.value = 'Thanks for checking in'; input.dispatchEvent(new Event('input'))
      shadow.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }))
      await vi.waitFor(() => expect(sent).toHaveLength(1))
      expect(sent[0]).toMatchObject({ body: 'Thanks for checking in' })
      shadow.querySelector<HTMLButtonElement>('.tab-feedback')!.click()
      await vi.waitFor(() => expect(feedback).toHaveBeenCalled())
      expect(shadow.querySelector('.feedback')!.textContent).toBe('Existing feedback form')
      expect(document.querySelector('#fidibeki-chat')!.hasAttribute('data-fidibeki-block')).toBe(true)
    } finally { chat.destroy(); language.mockRestore() }
    expect(document.querySelector('#fidibeki-chat')).toBeNull()
  })
})
