import type { StorageLike } from './storage'

export interface ChatCredential { token: string; visitor_id: string; expires_at: string }
export const chatStorageKey = (projectID: string) => `fidibeki:chat:${projectID}`
export function chatResetPending(storage: StorageLike, publicKey: string): boolean {
  try {
    const key = `fidibeki:chat-reset:${publicKey}`
    if (Number(storage.getItem(key)) > Date.now()) return true
    storage.removeItem(key); return false
  } catch { return false }
}
export function markChatReset(storage: StorageLike, publicKey: string, pending: boolean): void {
  try {
    const key = `fidibeki:chat-reset:${publicKey}`
    if (pending) storage.setItem(key, String(Date.now() + 86_400_000))
    else storage.removeItem(key)
  } catch { /* In-memory reset remains available when storage is unavailable. */ }
}

export function readChatCredential(storage: StorageLike, projectID: string): ChatCredential | null {
  try {
    const value = JSON.parse(storage.getItem(chatStorageKey(projectID)) || 'null') as ChatCredential | null
    if (value && typeof value.token === 'string' && /^[a-f0-9-]{36}\.[A-Za-z0-9_-]{43}$/.test(value.token)
      && typeof value.visitor_id === 'string' && Date.parse(value.expires_at) > Date.now()) return value
    storage.removeItem(chatStorageKey(projectID))
  } catch { /* Unavailable or damaged storage starts a fresh session. */ }
  return null
}

export async function forgetChatCredential(options: {
  storage: StorageLike; projectID: string; publicKey: string; apiBase: URL
  credential?: ChatCredential | null; fetcher?: typeof fetch
}): Promise<void> {
  const stored = readChatCredential(options.storage, options.projectID)
  try { options.storage.removeItem(chatStorageKey(options.projectID)) } catch { /* The view also drops its in-memory credential. */ }
  const tokens = [...new Set([stored?.token, options.credential?.token].filter((token): token is string => Boolean(token)))]
  if (!tokens.length) return
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 3000)
  const endpoint = new URL('widget/chat/session/revoke', options.apiBase)
  endpoint.searchParams.set('key', options.publicKey)
  try {
    await Promise.all(tokens.map(async token => {
      try {
        await (options.fetcher ?? fetch)(endpoint, {
          method: 'POST', headers: { 'X-Fidibeki-Public-Key': options.publicKey, 'X-Fidibeki-Chat-Token': token, 'Content-Type': 'application/json' },
          body: '{}', mode: 'cors', credentials: 'omit', cache: 'no-store', signal: controller.signal,
        })
      } catch { /* Local forgetting must succeed when revocation is unavailable. */ }
    }))
  } finally { clearTimeout(timeout) }
}
