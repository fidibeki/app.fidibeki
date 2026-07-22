import {
  getSessionID,
  isSuppressed,
  pageIdentity,
  presentationRecurrencePolicy,
  recordWidgetState,
  widgetStateKey,
  type StorageLike,
} from '../../src/suppression'

class MemoryStore implements StorageLike {
  values = new Map<string, string>()
  getItem(key: string): string | null { return this.values.get(key) ?? null }
  setItem(key: string, value: string): void { this.values.set(key, value) }
  removeItem(key: string): void { this.values.delete(key) }
}

const page = 'https://example.com/chart?asset=BAI#price'
const now = Date.parse('2026-07-22T12:00:00.000Z')

function suppressed(
  store: StorageLike,
  overrides: Partial<Parameters<typeof isSuppressed>[1]> = {},
): boolean {
  return isSuppressed(store, {
    projectID: 'project',
    ruleID: 'rule',
    rawURL: page,
    sessionID: 'session-one',
    recurrenceMode: 'cooldown',
    recurrenceDays: 30,
    now,
    ...overrides,
  })
}

describe('widget state suppression', () => {
  it('identifies a page by origin and pathname while ignoring query parameters and hashes', () => {
    expect(pageIdentity(page)).toBe('https://example.com/chart')
    expect(widgetStateKey('project', 'rule', page)).toContain('project:rule:https://example.com/chart')
    expect(widgetStateKey('project', 'rule', 'https://example.com/chart/')).toBe(
      widgetStateKey('project', 'rule', page),
    )
    expect(widgetStateKey('project', 'rule', 'https://other.test/chart')).not.toBe(
      widgetStateKey('project', 'rule', page),
    )
  })

  it('does not suppress a widget that was only opened', () => {
    expect(suppressed(new MemoryStore())).toBe(false)
  })

  it('suppresses an accepted submission until the cooldown expires', () => {
    const store = new MemoryStore()
    recordWidgetState(store, {
      projectID: 'project', ruleID: 'rule', rawURL: page,
      state: 'submitted', sessionID: 'session-one', occurredAt: now,
    })

    expect(suppressed(store, { rawURL: 'https://example.com/chart?asset=BIC' })).toBe(true)
    expect(suppressed(store, { now: now + 30 * 86_400_000 - 1 })).toBe(true)
    expect(suppressed(store, { now: now + 30 * 86_400_000 })).toBe(false)
    expect(store.getItem(widgetStateKey('project', 'rule', page))).toBeNull()
  })

  it('uses the current rule policy for existing submission records', () => {
    const store = new MemoryStore()
    recordWidgetState(store, {
      projectID: 'project', ruleID: 'rule', rawURL: page,
      state: 'submitted', sessionID: 'session-one', occurredAt: now,
    })

    expect(suppressed(store, { recurrenceMode: 'never', now: now + 400 * 86_400_000 })).toBe(true)
    expect(suppressed(store, { recurrenceMode: 'session', sessionID: 'session-two' })).toBe(false)
  })

  it('migrates legacy pathname-only records without extending their lifetime', () => {
    const store = new MemoryStore()
    store.setItem('fidibeki:state:v1:project:rule:/chart', JSON.stringify({
      version: 1,
      state: 'submitted',
      session_id: 'session-one',
      occurred_at: new Date(now).toISOString(),
    }))

    expect(suppressed(store)).toBe(true)
    expect(store.getItem('fidibeki:state:v1:project:rule:/chart')).toBeNull()
    expect(store.getItem(widgetStateKey('project', 'rule', page))).not.toBeNull()
  })

  it('suppresses dismissals only during the current browser session', () => {
    const store = new MemoryStore()
    recordWidgetState(store, {
      projectID: 'project', ruleID: 'rule', rawURL: page,
      state: 'dismissed', sessionID: 'session-one', occurredAt: now,
    })

    expect(suppressed(store)).toBe(true)
    expect(suppressed(store, { sessionID: 'session-two' })).toBe(false)
  })

  it('isolates state by project, rule, and page', () => {
    const store = new MemoryStore()
    recordWidgetState(store, {
      projectID: 'project', ruleID: 'rule', rawURL: page,
      state: 'submitted', sessionID: 'session-one', occurredAt: now,
    })

    expect(suppressed(store, { projectID: 'another-project' })).toBe(false)
    expect(suppressed(store, { ruleID: 'another-rule' })).toBe(false)
    expect(suppressed(store, { rawURL: 'https://example.com/another-page' })).toBe(false)
  })

  it('fails open for malformed or unavailable storage', () => {
    const malformed = new MemoryStore()
    malformed.setItem(widgetStateKey('project', 'rule', page), '{bad json')
    expect(suppressed(malformed)).toBe(false)

    const unavailable: StorageLike = {
      getItem: () => { throw new Error('blocked') },
      setItem: () => { throw new Error('blocked') },
      removeItem: () => { throw new Error('blocked') },
    }
    expect(suppressed(unavailable)).toBe(false)
    expect(() => recordWidgetState(unavailable, {
      projectID: 'project', ruleID: 'rule', rawURL: page,
      state: 'submitted', sessionID: 'session-one', occurredAt: now,
    })).not.toThrow()
  })

  it('keeps one anonymous session id for the life of sessionStorage', () => {
    const store = new MemoryStore()
    expect(getSessionID(store, () => 'generated-one')).toBe('generated-one')
    expect(getSessionID(store, () => 'generated-two')).toBe('generated-one')
  })

  it('applies stricter fatigue floors only to interruptive presentations', () => {
    expect(presentationRecurrencePolicy('prompt', 'session', null)).toEqual({ mode: 'cooldown', days: 30 })
    expect(presentationRecurrencePolicy('prompt', 'cooldown', 7)).toEqual({ mode: 'cooldown', days: 30 })
    expect(presentationRecurrencePolicy('reaction', 'session', null)).toEqual({ mode: 'cooldown', days: 7 })
    expect(presentationRecurrencePolicy('reaction', 'cooldown', 3)).toEqual({ mode: 'cooldown', days: 7 })
    expect(presentationRecurrencePolicy('launcher', 'session', null)).toEqual({ mode: 'session', days: null })
    expect(presentationRecurrencePolicy('prompt', 'never', null)).toEqual({ mode: 'never', days: null })
  })
})
