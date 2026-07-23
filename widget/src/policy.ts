import { canonicalPage } from './targeting'
import { scrollPercent } from './triggers'
import type { StorageLike } from './storage'
import type { WidgetRule } from './types'
import {
  evaluateObjectivePolicy,
  type ObjectivePolicyDecision,
  type ObjectiveSignals,
} from '../../contracts/widget-objective-policy'

export { evaluateObjectivePolicy }
export type { ObjectivePolicyDecision, ObjectiveSignals }

export interface ObjectivePolicyEnvironment {
  window: Window
  document: Document
  now(): number
  setTimeout: typeof globalThis.setTimeout
  clearTimeout: typeof globalThis.clearTimeout
}

interface VisitOptions {
  projectID: string
  rawURL: string
  sessionID: string
}

interface VisitRecord {
  version: 1
  count: number
  session_id: string
}

type DynamicSignals = Pick<ObjectiveSignals, 'activeTimeMs' | 'scrollDepth' | 'interactionCount' | 'visitCount'>

const policyIntervalMilliseconds = 1_000

export function collectObjectiveSignals(
  documentObject: Document,
  dynamic: Partial<DynamicSignals> = {},
): ObjectiveSignals {
  const root = documentObject.documentElement
  return {
    activeTimeMs: finiteNonNegative(dynamic.activeTimeMs),
    scrollDepth: Math.min(100, finiteNonNegative(dynamic.scrollDepth)),
    interactionCount: Math.floor(finiteNonNegative(dynamic.interactionCount)),
    visitCount: Math.max(1, Math.floor(finiteNonNegative(dynamic.visitCount) || 1)),
    hasTitle: documentObject.title.trim() !== '',
    hasCanonical: hasNonEmptyAttribute(documentObject, 'link[rel~="canonical"][href]', 'href'),
    hasDescription: hasNonEmptyAttribute(
      documentObject,
      'meta[name="description"][content], meta[property="og:description"][content]',
      'content',
    ),
    explicitEngagement: publicBoolean(root.dataset.fidibekiEngaged),
    explicitContent: publicBoolean(root.dataset.fidibekiContent),
    explicitFriction: publicBoolean(root.dataset.fidibekiFriction),
  }
}

export function recordObjectiveVisit(store: StorageLike, options: VisitOptions): number {
  const page = canonicalPage(options.rawURL)
  if (!page) return 1
  const key = `fidibeki:policy-visit:v1:${options.projectID}:${page.target}`
  try {
    const current = readVisitRecord(store.getItem(key))
    if (current?.session_id === options.sessionID) return current.count
    const count = Math.min(100, (current?.count ?? 0) + 1)
    const next: VisitRecord = { version: 1, count, session_id: options.sessionID }
    store.setItem(key, JSON.stringify(next))
    return count
  } catch {
    return 1
  }
}

export function armObjectivePolicy(
  rule: WidgetRule,
  visitCount: number,
  onEligible: () => void,
  environment: ObjectivePolicyEnvironment = browserEnvironment(),
): () => void {
  let activeTimeMs = 0
  let activeSince = environment.document.visibilityState === 'visible' ? environment.now() : null
  let interactionCount = 0
  let highestScrollDepth = scrollPercent(environment.window, environment.document)
  let timer: ReturnType<typeof globalThis.setTimeout> | null = null
  let stopped = false

  const clearTimer = () => {
    if (timer === null) return
    environment.clearTimeout(timer)
    timer = null
  }
  const cleanup = () => {
    if (stopped) return
    stopped = true
    clearTimer()
    environment.window.removeEventListener('scroll', onScroll)
    environment.document.removeEventListener('pointerdown', onInteraction, true)
    environment.document.removeEventListener('keydown', onInteraction, true)
    environment.document.removeEventListener('visibilitychange', onVisibility)
  }
  const currentActiveTime = () => activeTimeMs + (
    activeSince === null ? 0 : Math.max(0, environment.now() - activeSince)
  )
  const evaluate = () => {
    if (stopped) return
    clearTimer()
    const signals = collectObjectiveSignals(environment.document, {
      activeTimeMs: currentActiveTime(),
      scrollDepth: highestScrollDepth,
      interactionCount,
      visitCount,
    })
    if (evaluateObjectivePolicy(rule.objective, rule.presentation, signals).eligible) {
      cleanup()
      onEligible()
      return
    }
    if (environment.document.visibilityState === 'visible') {
      timer = environment.setTimeout(evaluate, policyIntervalMilliseconds)
    }
  }
  const onScroll = () => {
    highestScrollDepth = Math.max(highestScrollDepth, scrollPercent(environment.window, environment.document))
    evaluate()
  }
  const onInteraction = (event: Event) => {
    if (event instanceof KeyboardEvent && event.key !== 'Enter' && event.key !== ' ') return
    interactionCount = Math.min(20, interactionCount + 1)
    evaluate()
  }
  const onVisibility = () => {
    if (environment.document.visibilityState === 'hidden') {
      if (activeSince !== null) activeTimeMs += Math.max(0, environment.now() - activeSince)
      activeSince = null
    } else if (activeSince === null) {
      activeSince = environment.now()
    }
    evaluate()
  }

  environment.window.addEventListener('scroll', onScroll, { passive: true })
  environment.document.addEventListener('pointerdown', onInteraction, true)
  environment.document.addEventListener('keydown', onInteraction, true)
  environment.document.addEventListener('visibilitychange', onVisibility)
  evaluate()
  return cleanup
}

function finiteNonNegative(value: number | undefined): number {
  return value !== undefined && Number.isFinite(value) && value > 0 ? value : 0
}

function publicBoolean(value: string | undefined): boolean {
  return value === 'true' || value === '1'
}

function hasNonEmptyAttribute(documentObject: Document, selector: string, attribute: string): boolean {
  return Array.from(documentObject.querySelectorAll(selector)).some(element =>
    (element.getAttribute(attribute) ?? '').trim() !== '')
}

function readVisitRecord(raw: string | null): VisitRecord | null {
  if (!raw) return null
  try {
    const value = JSON.parse(raw) as Partial<VisitRecord>
    if (value.version !== 1 || !Number.isInteger(value.count) || (value.count ?? 0) < 1 ||
      typeof value.session_id !== 'string') return null
    return value as VisitRecord
  } catch {
    return null
  }
}

function browserEnvironment(): ObjectivePolicyEnvironment {
  return {
    window,
    document,
    now: () => performance.now(),
    setTimeout: globalThis.setTimeout.bind(globalThis),
    clearTimeout: globalThis.clearTimeout.bind(globalThis),
  }
}
