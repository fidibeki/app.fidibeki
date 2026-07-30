import { BACKGROUND_SESSION_RESET_MS, RecurrenceEngine, normalizeScreen, recurrenceStorageKey } from '../src'
import { resetProcessAppSessionForTesting } from '../src/recurrence'
import { definition, MemoryStorage } from './fixtures'

function deterministicRandom(seed: number) {
  let current = seed
  return (target: Uint8Array) => {
    target.fill(current)
    current += 1
    return target
  }
}

describe('recurrence engine', () => {
  beforeEach(() => resetProcessAppSessionForTesting())

  it('normalizes query-like screen data without fabricating a URL', () => {
    expect(normalizeScreen('checkout//review/?token=secret#section')).toBe('/checkout/review')
    expect(normalizeScreen('/')).toBe('/')
    expect(recurrenceStorageKey('project', 'definition', '/a?private=1')).not.toContain('private')
  })

  it('suppresses provider remounts in the same process session', async () => {
    const storage = new MemoryStorage()
    const first = new RecurrenceEngine(storage, {
      randomBytes: deterministicRandom(1),
    })
    await first.record('project', definition(), '/checkout', 'submitted')
    const remount = new RecurrenceEngine(storage, {
      randomBytes: deterministicRandom(2),
    })
    expect(remount.currentAppSessionId).toBe(first.currentAppSessionId)
    expect(await remount.isEligible('project', definition(), '/checkout')).toBe(false)
  })

  it('keeps a brief background in the same session and resets at 30 minutes', async () => {
    const storage = new MemoryStorage()
    let now = 1_000
    const engine = new RecurrenceEngine(storage, {
      now: () => now,
      randomBytes: deterministicRandom(3),
    })
    await engine.record('project', definition(), '/checkout', 'submitted')
    engine.onBackground()
    now += BACKGROUND_SESSION_RESET_MS - 1
    expect(engine.onForeground()).toBe(false)
    expect(await engine.isEligible('project', definition(), '/checkout')).toBe(false)
    engine.onBackground()
    now += BACKGROUND_SESSION_RESET_MS
    expect(engine.onForeground()).toBe(true)
    expect(await engine.isEligible('project', definition(), '/checkout')).toBe(true)
  })

  it('treats a cold JS restart as a new app session', async () => {
    const storage = new MemoryStorage()
    const first = new RecurrenceEngine(storage, {
      randomBytes: deterministicRandom(4),
    })
    await first.record('project', definition(), '/checkout', 'submitted')
    resetProcessAppSessionForTesting()
    const cold = new RecurrenceEngine(storage, {
      randomBytes: deterministicRandom(5),
    })
    expect(cold.currentAppSessionId).not.toBe(first.currentAppSessionId)
    expect(await cold.isEligible('project', definition(), '/checkout')).toBe(true)
  })

  it('applies cooldown timestamps and persistent never recurrence', async () => {
    const storage = new MemoryStorage()
    let now = 0
    const engine = new RecurrenceEngine(storage, {
      appSessionId: 'host-session',
      now: () => now,
    })
    const cooldown = definition({
      recurrence_mode: 'cooldown',
      recurrence_days: 2,
    })
    await engine.record('project', cooldown, '/checkout', 'submitted')
    now = 2 * 24 * 60 * 60 * 1000 - 1
    expect(await engine.isEligible('project', cooldown, '/checkout')).toBe(false)
    expect(await engine.eligibility('project', cooldown, '/checkout')).toEqual({
      eligible: false,
      nextEligibleAt: 2 * 24 * 60 * 60 * 1000,
    })
    now += 1
    expect(await engine.eligibility('project', cooldown, '/checkout')).toEqual({
      eligible: true,
      nextEligibleAt: null,
    })
    const never = definition({ recurrence_mode: 'never' })
    await engine.record('project', never, '/never', 'submitted')
    now = Number.MAX_SAFE_INTEGER
    expect(await engine.eligibility('project', never, '/never')).toEqual({
      eligible: false,
      nextEligibleAt: null,
    })
  })

  it.each(['session', 'cooldown', 'never'] as const)(
    'clears legacy dismissed records for %s recurrence',
    async (recurrenceMode) => {
      const storage = new MemoryStorage()
      const current = definition({
        recurrence_mode: recurrenceMode,
        ...(recurrenceMode === 'cooldown' ? { recurrence_days: 30 } : {}),
      })
      const key = recurrenceStorageKey('project', current.definition_key, '/checkout')
      storage.values.set(
        key,
        JSON.stringify({
          state: 'dismissed',
          recordedAt: Date.now(),
          appSessionId: 'host-session',
        }),
      )
      const removeItem = vi.spyOn(storage, 'removeItem')
      const engine = new RecurrenceEngine(storage, { appSessionId: 'host-session' })

      expect(await engine.eligibility('project', current, '/checkout')).toEqual({
        eligible: true,
        nextEligibleAt: null,
      })
      expect(removeItem).toHaveBeenCalledWith(key)
      expect(storage.values.has(key)).toBe(false)
    },
  )

  it('ignores malformed snooze timestamps instead of scheduling them', async () => {
    const storage = new MemoryStorage()
    const current = definition()
    storage.values.set(
      recurrenceStorageKey('project', current.definition_key, '/checkout'),
      JSON.stringify({
        state: 'snoozed',
        recordedAt: 1,
        snoozedUntil: 'not-a-timestamp',
      }),
    )
    const engine = new RecurrenceEngine(storage, { appSessionId: 'host-session' })
    expect(await engine.eligibility('project', current, '/checkout')).toEqual({
      eligible: true,
      nextEligibleAt: null,
    })
  })
})
