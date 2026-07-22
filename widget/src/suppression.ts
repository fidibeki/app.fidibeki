import type { RecurrenceMode } from './types'
import { canonicalPage } from './targeting'

export interface StorageLike {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

export type WidgetState = 'new' | 'submitted' | 'minimized' | 'dismissed' | 'snoozed'

interface WidgetStateRecord {
  version: 1
  state: WidgetState
  session_id: string
  occurred_at: string
  expires_at?: string
}

export interface WidgetStateIdentity {
  projectID: string
  ruleID: string
  rawURL: string
}

export interface RecordWidgetStateOptions extends WidgetStateIdentity {
  state: WidgetState
  sessionID: string
  occurredAt?: number
  expiresAt?: number
}

export interface SuppressionOptions extends WidgetStateIdentity {
  sessionID: string
  recurrenceMode: RecurrenceMode
  recurrenceDays: number | null
  now?: number
}

const sessionKey = 'fidibeki:session:v1'
const dayMilliseconds = 86_400_000
const widgetStates = new Set<WidgetState>(['new', 'submitted', 'minimized', 'dismissed', 'snoozed'])

export function pageIdentity(rawURL: string): string {
  const page = canonicalPage(rawURL)
  if (!page) throw new TypeError('Widget state requires an absolute HTTP(S) URL')
  return page.pathname
}

export function widgetStateKey(projectID: string, ruleID: string, rawURL: string): string {
  return `fidibeki:state:v1:${projectID}:${ruleID}:${pageIdentity(rawURL)}`
}

export function getSessionID(store: StorageLike, createID: () => string = randomID): string {
  try {
    const existing = store.getItem(sessionKey)
    if (existing) return existing
    const created = createID()
    store.setItem(sessionKey, created)
    return created
  } catch {
    return createID()
  }
}

export function recordWidgetState(store: StorageLike, options: RecordWidgetStateOptions): void {
  const occurredAt = options.occurredAt ?? Date.now()
  const record: WidgetStateRecord = {
    version: 1,
    state: options.state,
    session_id: options.sessionID,
    occurred_at: new Date(occurredAt).toISOString(),
    ...(options.expiresAt === undefined ? {} : { expires_at: new Date(options.expiresAt).toISOString() }),
  }
  try {
    store.setItem(widgetStateKey(options.projectID, options.ruleID, options.rawURL), JSON.stringify(record))
  } catch {
    // Storage restrictions must not break the host page or feedback flow.
  }
}

export function isSuppressed(store: StorageLike, options: SuppressionOptions): boolean {
  const key = widgetStateKey(options.projectID, options.ruleID, options.rawURL)
  const record = readRecord(store, key)
  if (!record) return false

  const now = options.now ?? Date.now()
  if (record.state === 'dismissed') return record.session_id === options.sessionID
  if (record.state === 'snoozed') {
    const expiresAt = parseTimestamp(record.expires_at)
    if (expiresAt !== null && now < expiresAt) return true
    removeRecord(store, key)
    return false
  }
  if (record.state !== 'submitted') return false

  if (options.recurrenceMode === 'never') return true
  if (options.recurrenceMode === 'session') return record.session_id === options.sessionID

  const occurredAt = parseTimestamp(record.occurred_at)
  if (occurredAt === null) {
    removeRecord(store, key)
    return false
  }
  const days = validCooldownDays(options.recurrenceDays)
  if (now < occurredAt + days * dayMilliseconds) return true
  removeRecord(store, key)
  return false
}

function readRecord(store: StorageLike, key: string): WidgetStateRecord | null {
  try {
    const stored = store.getItem(key)
    if (!stored) return null
    const value = JSON.parse(stored) as Partial<WidgetStateRecord>
    if (
      value.version !== 1
      || typeof value.state !== 'string'
      || !widgetStates.has(value.state as WidgetState)
      || typeof value.session_id !== 'string'
      || typeof value.occurred_at !== 'string'
    ) {
      removeRecord(store, key)
      return null
    }
    return value as WidgetStateRecord
  } catch {
    removeRecord(store, key)
    return null
  }
}

function removeRecord(store: StorageLike, key: string): void {
  try {
    store.removeItem(key)
  } catch {
    // Storage restrictions are handled as an eligible widget state.
  }
}

function parseTimestamp(value: string | undefined): number | null {
  if (!value) return null
  const timestamp = Date.parse(value)
  return Number.isFinite(timestamp) ? timestamp : null
}

function validCooldownDays(value: number | null): number {
  return value !== null && Number.isInteger(value) && value >= 1 && value <= 3650 ? value : 30
}

function randomID(): string {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}
