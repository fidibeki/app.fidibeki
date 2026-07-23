import {
  armObjectivePolicy,
  collectObjectiveSignals,
  evaluateObjectivePolicy,
  recordObjectiveVisit,
  type ObjectivePolicyEnvironment,
} from '../../src/policy'
import { rule } from './fixtures'

class MemoryStore {
  values = new Map<string, string>()
  getItem(key: string): string | null { return this.values.get(key) ?? null }
  setItem(key: string, value: string): void { this.values.set(key, value) }
  removeItem(key: string): void { this.values.delete(key) }
}

const emptySignals = {
  activeTimeMs: 0,
  scrollDepth: 0,
  interactionCount: 0,
  visitCount: 1,
  hasTitle: false,
  hasCanonical: false,
  hasDescription: false,
  explicitEngagement: false,
  explicitContent: false,
  explicitFriction: false,
}

describe('objective policy', () => {
  afterEach(() => {
    document.title = ''
    document.head.innerHTML = ''
    document.body.innerHTML = ''
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    delete document.documentElement.dataset.fidibekiEngaged
    delete document.documentElement.dataset.fidibekiContent
    delete document.documentElement.dataset.fidibekiFriction
    vi.useRealTimers()
  })

  it('keeps launcher and inline presentations available without an interruption score', () => {
    expect(evaluateObjectivePolicy('detect_friction', 'launcher', emptySignals).eligible).toBe(true)
    expect(evaluateObjectivePolicy('detect_friction', 'inline', emptySignals).eligible).toBe(true)
  })

  it('requires meaningful engagement for reactions and a higher score for prompts', () => {
    const engaged = {
      ...emptySignals,
      activeTimeMs: 16_000,
      scrollDepth: 60,
      interactionCount: 1,
    }
    const reaction = evaluateObjectivePolicy('after_engagement', 'reaction', engaged)
    const prompt = evaluateObjectivePolicy('after_engagement', 'prompt', engaged)
    expect(reaction.eligible).toBe(true)
    expect(prompt.requiredScore).toBeGreaterThan(reaction.requiredScore)
    expect(prompt.eligible).toBe(false)
  })

  it('uses objective-specific explicit public signals deterministically', () => {
    expect(evaluateObjectivePolicy('validate_content', 'prompt', {
      ...emptySignals, explicitContent: true, hasTitle: true, hasCanonical: true, hasDescription: true,
    }).eligible).toBe(true)
    expect(evaluateObjectivePolicy('detect_friction', 'prompt', {
      ...emptySignals, explicitFriction: true,
    }).eligible).toBe(true)
    expect(evaluateObjectivePolicy('continuous_feedback', 'reaction', {
      ...emptySignals, activeTimeMs: 5_000,
    }).eligible).toBe(true)
  })

  it('collects only public metadata and explicit boolean data attributes', () => {
    document.title = 'Checkout'
    document.head.insertAdjacentHTML('beforeend', `
      <link rel="canonical" href="https://example.com/checkout">
      <meta name="description" content="Review your order">
    `)
    document.documentElement.dataset.fidibekiEngaged = 'true'
    document.documentElement.dataset.fidibekiFriction = '1'
    document.body.innerHTML = '<input value="private-card-value"><article>Private account content</article>'

    expect(collectObjectiveSignals(document, { visitCount: 2 })).toEqual(expect.objectContaining({
      visitCount: 2,
      hasTitle: true,
      hasCanonical: true,
      hasDescription: true,
      explicitEngagement: true,
      explicitContent: false,
      explicitFriction: true,
    }))
    expect(JSON.stringify(collectObjectiveSignals(document, { visitCount: 2 }))).not.toContain('private')
  })

  it('ignores empty metadata and ordinary typing as engagement', () => {
    document.head.innerHTML = '<link rel="canonical" href=""><meta name="description" content="">'
    expect(collectObjectiveSignals(document)).toEqual(expect.objectContaining({
      hasCanonical: false,
      hasDescription: false,
    }))

    const onEligible = vi.fn()
    const cleanup = armObjectivePolicy(
      rule({ objective: 'after_engagement', presentation: 'reaction' }),
      1,
      onEligible,
    )
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'a', bubbles: true }))
    expect(onEligible).not.toHaveBeenCalled()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(onEligible).toHaveBeenCalledOnce()
    cleanup()
  })

  it('counts one anonymous visit per project, page, and browser session', () => {
    const store = new MemoryStore()
    const options = {
      projectID: 'project', rawURL: 'https://example.com/chart?asset=BAI#price', sessionID: 'session-one',
    }
    expect(recordObjectiveVisit(store, options)).toBe(1)
    expect(recordObjectiveVisit(store, { ...options, rawURL: 'https://example.com/chart/' })).toBe(1)
    expect(recordObjectiveVisit(store, { ...options, sessionID: 'session-two' })).toBe(2)
    expect(recordObjectiveVisit(store, { ...options, rawURL: 'https://example.com/other' })).toBe(1)
  })

  it('arms once after active-time signals cross the policy threshold', () => {
    vi.useFakeTimers()
    let now = 0
    let visibility: DocumentVisibilityState = 'visible'
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility })
    const onEligible = vi.fn()
    const environment: ObjectivePolicyEnvironment = {
      window,
      document,
      now: () => now,
      setTimeout: globalThis.setTimeout.bind(globalThis),
      clearTimeout: globalThis.clearTimeout.bind(globalThis),
    }
    const cleanup = armObjectivePolicy(
      rule({ objective: 'continuous_feedback', presentation: 'reaction' }),
      1,
      onEligible,
      environment,
    )

    now = 4_000
    vi.advanceTimersByTime(4_000)
    expect(onEligible).not.toHaveBeenCalled()
    visibility = 'hidden'
    document.dispatchEvent(new Event('visibilitychange'))
    now = 20_000
    vi.advanceTimersByTime(16_000)
    expect(onEligible).not.toHaveBeenCalled()
    visibility = 'visible'
    document.dispatchEvent(new Event('visibilitychange'))
    now = 21_000
    vi.advanceTimersByTime(1_000)
    expect(onEligible).toHaveBeenCalledOnce()
    cleanup()
  })
})
