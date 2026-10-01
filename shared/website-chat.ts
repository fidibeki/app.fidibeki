export { watchSupport, type LiveState } from './live-support'

export interface ChatMessage {
  id: string
  sequence: number
  client_id: string
  sender_type: 'operator' | 'visitor'
  sender_name: string
  body: string
  created_at: string
}
export interface ChatConversation {
  id: string
  project_id: string
  name: string
  last_sequence: number
  visitor_read_sequence: number
  operator_read_sequence: number
  updated_at: string
  expires_at: string
  unread: number
  preview: string
}
export interface ChatThread { allow_visitor_messages?: boolean; conversation: ChatConversation | null; messages: ChatMessage[]; has_more: boolean }
export interface ChatVisitor { id: string; name: string; page_url: string; state: 'active' | 'idle'; last_seen: string }
export const emptyChat = (): ChatThread => ({ conversation: null, messages: [], has_more: false })
export function mergeChatMessages(current: ChatMessage[], incoming: ChatMessage[]) {
  return [...new Map([...current, ...incoming].map(message => [message.id, message])).values()].sort((a, b) => a.sequence - b.sequence)
}
export function reconcileChat(current: ChatThread, incoming: ChatThread): ChatThread {
  if (!incoming.conversation || current.conversation?.id !== incoming.conversation.id || !current.messages.length) return incoming
  if (incoming.conversation.last_sequence < current.conversation.last_sequence) return current
  const oldest = incoming.messages[0]?.sequence
  if (oldest === undefined || oldest > current.messages[current.messages.length - 1]!.sequence + 1) return incoming
  const messages = mergeChatMessages(current.messages, incoming.messages)
  if (messages.some((message, index) => index > 0 && message.sequence !== messages[index - 1]!.sequence + 1)) return incoming
  return { ...incoming, messages, has_more: current.has_more }
}
export function prependChat(current: ChatThread, older: ChatThread): ChatThread {
  if (!current.conversation || current.conversation.id !== older.conversation?.id) return current
  const end = older.messages[older.messages.length - 1]?.sequence
  if (end === undefined || end + 1 < (current.messages[0]?.sequence ?? 0)) return current
  return { ...current, messages: mergeChatMessages(older.messages, current.messages), has_more: older.has_more }
}

export async function exportChat(load: (before: number | undefined, signal: AbortSignal) => Promise<ChatThread>, signal?: AbortSignal): Promise<Blob> {
  const controller = new AbortController()
  const abort = () => controller.abort()
  signal?.addEventListener('abort', abort, { once: true })
  if (signal?.aborted) abort()
  const timeout = setTimeout(abort, 20_000)
  let before: number | undefined
  let conversation: ChatConversation | null = null
  let messages: ChatMessage[] = []
  let bytes = 0
  const encoder = new TextEncoder()
  try {
    for (;;) {
      if (controller.signal.aborted) throw new Error('Download interrupted')
      const page = await load(before, controller.signal)
      if (controller.signal.aborted || !page.conversation) throw new Error('Conversation unavailable')
      if (!conversation) conversation = page.conversation
      if (conversation.id !== page.conversation.id || conversation.project_id !== page.conversation.project_id) throw new Error('Conversation changed')
      const rows = page.messages.filter(message => message.sequence <= conversation!.last_sequence)
      bytes += encoder.encode(JSON.stringify(rows)).length
      if (bytes > 20 * 1024 * 1024) throw new Error('Conversation exceeds download limit')
      messages = [...rows, ...messages]
      if (!page.has_more) break
      const next = page.messages[0]?.sequence
      if (!next || (before !== undefined && next >= before)) throw new Error('Conversation changed')
      before = next
    }
    if (messages.length !== conversation.last_sequence || messages.some((message, index) => message.sequence !== index + 1)) throw new Error('Incomplete conversation')
    return new Blob([JSON.stringify({ schema_version: 'fidibeki.website-chat.v1', conversation_id: conversation.id, project_id: conversation.project_id, exported_at: new Date().toISOString(), messages }, null, 2)], { type: 'application/json' })
  } finally { clearTimeout(timeout); signal?.removeEventListener('abort', abort) }
}
