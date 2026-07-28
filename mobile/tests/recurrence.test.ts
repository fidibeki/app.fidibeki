import {
  BACKGROUND_SESSION_RESET_MS,
  RecurrenceEngine,
  normalizeScreen,
  recurrenceStorageKey,
} from '../src'
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
    const first = new RecurrenceEngine(storage, { randomBytes: deterministicRandom(1) })
    await first.record('project', definition(), '/checkout', 'submitted')
    const remount = new RecurrenceEngine(storage, { randomBytes: deterministicRandom(2) })
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
    await engine.record('project', definition(), '/checkout', 'dismissed')
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
    const first = new RecurrenceEngine(storage, { randomBytes: deterministicRandom(4) })
    await first.record('project', definition(), '/checkout', 'submitted')
    resetProcessAppSessionForTesting()
    const cold = new RecurrenceEngine(storage, { randomBytes: deterministicRandom(5) })
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
    const cooldown = definition({ recurrence_mode: 'cooldown', recurrence_days: 2 })
    await engine.record('project', cooldown, '/checkout', 'submitted')
    now = 2 * 24 * 60 * 60 * 1000 - 1
    expect(await engine.isEligible('project', cooldown, '/checkout')).toBe(false)
    now += 1
    expect(await engine.isEligible('project', cooldown, '/checkout')).toBe(true)
    const never = definition({ recurrence_mode: 'never' })
    await engine.record('project', never, '/never', 'dismissed')
    now = Number.MAX_SAFE_INTEGER
    expect(await engine.isEligible('project', never, '/never')).toBe(false)
  })
})
