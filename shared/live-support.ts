export interface SupportMessage {
  id: string
  sequence: number
  client_id: string
  sender_type: 'member' | 'admin'
  sender_name: string
  body: string
  created_at: string
}

export interface SupportConversation {
  id: string
  tenant_id: string
  member_id: string
  member_name: string
  member_email?: string
  workspace_name?: string
  last_sequence: number
  member_read_sequence: number
  admin_read_sequence: number
  updated_at: string
  unread: number
  preview: string
}

export interface SupportThread {
  conversation: SupportConversation | null
  messages: SupportMessage[]
  has_more: boolean
}

export interface OnlineMember {
  tenant_id: string
  member_id: string
  name: string
  email: string
  workspace: string
  section: string
  state: 'active' | 'idle'
  last_seen: string
}

export type LiveState = 'connecting' | 'connected' | 'offline' | 'forbidden'

export function mergeMessages(current: SupportMessage[], incoming: SupportMessage[]) {
  const messages = new Map(current.map(message => [message.id, message]))
  for (const message of incoming) messages.set(message.id, message)
  return [...messages.values()].sort((a, b) => a.sequence - b.sequence)
}

// Keep one contiguous window. A reconnect can skip more than a page; replace
// that window so Load earlier messages can retrieve the missing middle.
export function reconcileThread(current: SupportThread, incoming: SupportThread): SupportThread {
  if (!incoming.conversation || current.conversation?.id !== incoming.conversation.id || !current.messages.length) return incoming
  const latest = current.messages.at(-1)!.sequence
  const oldestIncoming = incoming.messages[0]?.sequence
  if (oldestIncoming === undefined || oldestIncoming > latest + 1) return incoming
  // A slower snapshot must not undo a newer one.
  if (incoming.conversation.last_sequence < (current.conversation?.last_sequence ?? 0)) return current
  const messages = mergeMessages(current.messages, incoming.messages)
  if (messages.some((message, index) => index > 0 && message.sequence !== messages[index - 1]!.sequence + 1)) return incoming
  return { ...incoming, messages, has_more: current.has_more }
}

export function prependHistory(current: SupportThread, older: SupportThread): SupportThread {
  if (!current.conversation || current.conversation.id !== older.conversation?.id) return current
  const end = older.messages.at(-1)?.sequence
  if (end === undefined || end + 1 < (current.messages[0]?.sequence ?? 0)) return current
  return { ...current, messages: mergeMessages(older.messages, current.messages), has_more: older.has_more }
}

export function supportSection(path: string) {
  const section = path.split('/')[1] || 'overview'
  return ['overview', 'feedback', 'replays', 'issues', 'notifications', 'testimonials', 'analytics', 'projects', 'settings', 'profile'].includes(section)
    ? section : 'other'
}

function pause(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve) => {
    if (signal.aborted) { resolve(); return }
    const done = () => { clearTimeout(timer); signal.removeEventListener('abort', done); resolve() }
    const timer = setTimeout(done, ms)
    signal.addEventListener('abort', done, { once: true })
  })
}

/** Authorization stays in a header, never a URL or browser storage. The server
 * rotates streams every 20 seconds; every connection triggers a fresh snapshot. */
export async function watchSupport(options: {
  url: string
  token(): string | null
  refreshAuth(): Promise<void>
  signal: AbortSignal
  onChange(): void
  onPresence?(): void
  onState(state: LiveState): void
  fetcher?: typeof fetch
}) {
  let failures = 0
  const fetcher = options.fetcher ?? fetch
  while (!options.signal.aborted) {
    const controller = new AbortController()
    const abort = () => controller.abort()
    options.signal.addEventListener('abort', abort, { once: true })
    const watchdog = setTimeout(abort, 35_000)
    let retry = 250
    try {
      const token = options.token()
      if (!token) return
      const response = await fetcher(options.url, {
        headers: { Authorization: `Bearer ${token}`, Accept: 'text/event-stream' },
        credentials: 'same-origin', cache: 'no-store', signal: controller.signal,
      })
      if (response.status === 401) {
        await response.body?.cancel()
        await options.refreshAuth()
        continue
      }
      if (response.status === 403) {
        await response.body?.cancel()
        options.onState('forbidden')
        return
      }
      if (!response.ok || !response.body || !response.headers.get('content-type')?.includes('text/event-stream')) {
        await response.body?.cancel()
        throw new Error('Live connection unavailable')
      }
      options.onState('connected')
      failures = 0
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      try {
        while (!options.signal.aborted) {
          const { value, done } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true }).replace(/\r/g, '')
          if (buffer.length > 65_536) throw new Error('Invalid live event')
          let boundary: number
          while ((boundary = buffer.indexOf('\n\n')) >= 0) {
            const event = buffer.slice(0, boundary)
            buffer = buffer.slice(boundary + 2)
            const lines = event.split('\n')
            if (lines.includes('event: change')) options.onChange()
            else if (lines.includes('event: presence')) options.onPresence?.()
          }
        }
      } finally { await reader.cancel().catch(() => {}); reader.releaseLock() }
    } catch {
      if (options.signal.aborted) return
      options.onState('offline')
      retry = Math.min(15_000, 1000 * 2 ** Math.min(failures++, 4)) + Math.random() * 500
    } finally {
      clearTimeout(watchdog)
      controller.abort()
      options.signal.removeEventListener('abort', abort)
    }
    await pause(retry, options.signal)
  }
}

/** Export only the authenticated participant's pages, with a fixed cutoff.
 * Fail closed on a gap, changed conversation, cancellation, or oversized file. */
export async function exportSupportTranscript(
  loadPage: (before: number | undefined, signal: AbortSignal) => Promise<SupportThread>,
  signal?: AbortSignal,
): Promise<Blob> {
  const controller = new AbortController()
  const cancel = () => controller.abort()
  signal?.addEventListener('abort', cancel, { once: true })
  if (signal?.aborted) cancel()
  const timer = setTimeout(cancel, 20_000)
  const encoder = new TextEncoder()
  const pages: Array<Array<{ sequence: number; json: string }>> = []
  let before: number | undefined
  let conversationID: string | undefined
  let cutoff = 0
  let prefix = ''
  const suffix = ']}\n'
  let bytes = 0
  let count = 0
  try {
    for (;;) {
      if (controller.signal.aborted) throw new Error('export interrupted')
      const page = await loadPage(before, controller.signal)
      if (controller.signal.aborted) throw new Error('export interrupted')
      const conversation = page.conversation
      if (!conversation) throw new Error('conversation unavailable')
      if (!conversationID) {
        conversationID = conversation.id
        cutoff = conversation.last_sequence
        const metadata = JSON.stringify({ schema_version: 'fidibeki.support-export.v1', conversation_id: conversation.id,
          tenant_id: conversation.tenant_id, member_id: conversation.member_id,
          exported_at: new Date().toISOString(), last_sequence: cutoff })
        prefix = metadata.slice(0, -1) + ',"messages":['
        bytes = encoder.encode(prefix + suffix).length
      } else if (conversation.id !== conversationID) throw new Error('conversation changed')
      const rows: Array<{ sequence: number; json: string }> = []
      for (const message of page.messages) {
        if (message.sequence > cutoff) continue
        const json = JSON.stringify({ id: message.id, sequence: message.sequence,
          sender_type: message.sender_type, sender_name: message.sender_name,
          body: message.body, created_at: message.created_at })
        bytes += encoder.encode(json).length + (count ? 1 : 0)
        if (bytes > 20 * 1024 * 1024) throw new Error('export exceeds 20 MiB')
        count++
        rows.push({ sequence: message.sequence, json })
      }
      pages.push(rows)
      if (!page.has_more) break
      const oldest = page.messages[0]?.sequence
      if (!oldest || (before !== undefined && oldest >= before)) throw new Error('conversation changed')
      before = oldest
    }
    const rows = pages.reverse().flat()
    if (rows.length !== cutoff || rows.some((row, index) => row.sequence !== index + 1)) throw new Error('conversation changed')
    if (controller.signal.aborted) throw new Error('export interrupted')
    return new Blob([prefix, rows.map(row => row.json).join(','), suffix], { type: 'application/json;charset=utf-8' })
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener('abort', cancel)
    controller.abort()
  }
}
