import type { MobileWidgetDefinition, StorageAdapter } from './types'
import { createUUID, type RandomBytes } from './uuid'

export const BACKGROUND_SESSION_RESET_MS = 30 * 60 * 1000
const STORAGE_PREFIX = '@fidibeki/mobile/v1'

export type RecurrenceState = 'submitted' | 'dismissed' | 'snoozed'

export interface RecurrenceRecord {
  state: RecurrenceState
  recordedAt: number
  appSessionId?: string
  snoozedUntil?: number
}

let processAppSessionId: string | undefined

export function normalizeScreen(screen: string): string {
  const withoutQuery = screen.trim().split(/[?#]/, 1)[0] ?? ''
  const normalized = `/${withoutQuery}`.replace(/\/+/g, '/').replace(/\/+$/, '')
  return normalized === '' ? '/' : normalized
}

export function recurrenceStorageKey(
  projectId: string,
  definitionKey: string,
  screen: string,
): string {
  return [
    STORAGE_PREFIX,
    encodeURIComponent(projectId),
    encodeURIComponent(definitionKey),
    encodeURIComponent(normalizeScreen(screen)),
  ].join(':')
}

export function currentProcessAppSessionId(randomBytes?: RandomBytes): string {
  processAppSessionId ??= createUUID(randomBytes)
  return processAppSessionId
}

export function resetProcessAppSessionForTesting(): void {
  processAppSessionId = undefined
}

function parseRecord(value: string | null): RecurrenceRecord | null {
  if (!value) return null
  try {
    const record = JSON.parse(value) as Partial<RecurrenceRecord>
    if (
      !['submitted', 'dismissed', 'snoozed'].includes(record.state ?? '') ||
      !Number.isFinite(record.recordedAt)
    ) {
      return null
    }
    return record as RecurrenceRecord
  } catch {
    return null
  }
}

export class RecurrenceEngine {
  private appSessionId: string
  private backgroundedAt: number | null = null

  constructor(
    private readonly storage: StorageAdapter,
    options: {
      appSessionId?: string
      now?: () => number
      randomBytes?: RandomBytes
    } = {},
  ) {
    this.appSessionId =
      options.appSessionId ?? currentProcessAppSessionId(options.randomBytes)
    this.now = options.now ?? Date.now
    this.randomBytes = options.randomBytes
    this.hostOwnsSession = options.appSessionId !== undefined
  }

  private readonly now: () => number
  private readonly randomBytes: RandomBytes | undefined
  private readonly hostOwnsSession: boolean

  get currentAppSessionId(): string {
    return this.appSessionId
  }

  onBackground(): void {
    if (!this.hostOwnsSession && this.backgroundedAt === null) {
      this.backgroundedAt = this.now()
    }
  }

  onForeground(): boolean {
    if (this.hostOwnsSession || this.backgroundedAt === null) return false
    const elapsed = this.now() - this.backgroundedAt
    this.backgroundedAt = null
    if (elapsed < BACKGROUND_SESSION_RESET_MS) return false
    this.appSessionId = createUUID(this.randomBytes)
    processAppSessionId = this.appSessionId
    return true
  }

  async isEligible(
    projectId: string,
    definition: MobileWidgetDefinition,
    screen: string,
  ): Promise<boolean> {
    const key = recurrenceStorageKey(projectId, definition.definition_key, screen)
    const record = parseRecord(await this.storage.getItem(key))
    if (!record) return true
    if (record.state === 'snoozed') {
      return record.snoozedUntil === undefined || this.now() >= record.snoozedUntil
    }
    if (definition.recurrence_mode === 'session') {
      return record.appSessionId !== this.appSessionId
    }
    if (definition.recurrence_mode === 'never') return false
    const days = definition.recurrence_days ?? 30
    return this.now() >= record.recordedAt + days * 24 * 60 * 60 * 1000
  }

  async record(
    projectId: string,
    definition: MobileWidgetDefinition,
    screen: string,
    state: Exclude<RecurrenceState, 'snoozed'>,
  ): Promise<void> {
    const record: RecurrenceRecord = {
      state,
      recordedAt: this.now(),
      ...(definition.recurrence_mode === 'session'
        ? { appSessionId: this.appSessionId }
        : {}),
    }
    await this.storage.setItem(
      recurrenceStorageKey(projectId, definition.definition_key, screen),
      JSON.stringify(record),
    )
  }

  async snooze(
    projectId: string,
    definition: MobileWidgetDefinition,
    screen: string,
    until: number,
  ): Promise<void> {
    await this.storage.setItem(
      recurrenceStorageKey(projectId, definition.definition_key, screen),
      JSON.stringify({
        state: 'snoozed',
        recordedAt: this.now(),
        snoozedUntil: until,
      } satisfies RecurrenceRecord),
    )
  }
}
