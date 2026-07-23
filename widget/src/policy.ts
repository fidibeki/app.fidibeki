import { canonicalPage } from './targeting'
import { scrollPercent } from './triggers'
import type { StorageLike } from './storage'
import type { WidgetObjective, WidgetPresentation, WidgetRule } from './types'

export interface ObjectiveSignals {
  activeTimeMs: number
  scrollDepth: number
  interactionCount: number
  visitCount: number
  hasTitle: boolean
  hasCanonical: boolean
  hasDescription: boolean
  explicitEngagement: boolean
  explicitContent: boolean
  explicitFriction: boolean
}

export interface ObjectivePolicyDecision {
  eligible: boolean
  score: number
  requiredScore: number
  reasons: string[]
  guardrails: string[]
}

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

const maximumScore = 100
const policyIntervalMilliseconds = 1_000

export function evaluateObjectivePolicy(
  objective: WidgetObjective | undefined,
  presentation: WidgetPresentation | undefined,
  signals: ObjectiveSignals,
): ObjectivePolicyDecision {
  const resolvedObjective = objective ?? 'continuous_feedback'
  const resolvedPresentation = presentation ?? 'prompt'
  const reasons: string[] = []
  let score = scoreObjective(resolvedObjective, signals, reasons)
  score = Math.min(maximumScore, score)

  if (resolvedPresentation === 'launcher' || resolvedPresentation === 'inline') {
    return {
      eligible: true,
      score,
      requiredScore: 0,
      reasons,
      guardrails: [resolvedPresentation === 'launcher' ? 'discreet_launcher' : 'layout_stable_inline'],
    }
  }

  const requiredScore = resolvedPresentation === 'prompt'
    ? promptThreshold(resolvedObjective)
    : reactionThreshold(resolvedObjective)
  return {
    eligible: score >= requiredScore,
    score,
    requiredScore,
    reasons,
    guardrails: [resolvedPresentation === 'prompt' ? 'strict_prompt_threshold' : 'engaged_reaction_threshold'],
  }
}

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

function scoreObjective(objective: WidgetObjective, signals: ObjectiveSignals, reasons: string[]): number {
  switch (objective) {
    case 'after_engagement':
      return scoreAfterEngagement(signals, reasons)
    case 'validate_content':
      return scoreContentValidation(signals, reasons)
    case 'detect_friction':
      return scoreFriction(signals, reasons)
    default:
      return scoreContinuousFeedback(signals, reasons)
  }
}

function scoreContinuousFeedback(signals: ObjectiveSignals, reasons: string[]): number {
  let score = addReason(reasons, 'continuous_availability', 10)
  if (signals.activeTimeMs >= 5_000) score += addReason(reasons, 'active_5s', 30)
  if (signals.activeTimeMs >= 15_000) score += addReason(reasons, 'active_15s', 20)
  if (signals.scrollDepth >= 25) score += addReason(reasons, 'scroll_25', 20)
  if (signals.interactionCount >= 1) score += addReason(reasons, 'interaction', 20)
  if (signals.visitCount >= 2) score += addReason(reasons, 'return_visit', 10)
  if (signals.explicitEngagement) score += addReason(reasons, 'explicit_engagement', 50)
  return score
}

function scoreAfterEngagement(signals: ObjectiveSignals, reasons: string[]): number {
  let score = 0
  if (signals.activeTimeMs >= 10_000) score += addReason(reasons, 'active_10s', 25)
  if (signals.activeTimeMs >= 30_000) score += addReason(reasons, 'active_30s', 20)
  if (signals.scrollDepth >= 40) score += addReason(reasons, 'scroll_40', 25)
  if (signals.scrollDepth >= 75) score += addReason(reasons, 'scroll_75', 15)
  if (signals.interactionCount >= 1) score += addReason(reasons, 'interaction', 20)
  if (signals.interactionCount >= 3) score += addReason(reasons, 'interactions_3', 10)
  if (signals.visitCount >= 2) score += addReason(reasons, 'return_visit', 10)
  if (signals.explicitEngagement) score += addReason(reasons, 'explicit_engagement', 50)
  return score
}

function scoreContentValidation(signals: ObjectiveSignals, reasons: string[]): number {
  let score = 0
  if (signals.hasTitle) score += addReason(reasons, 'document_title', 10)
  if (signals.hasCanonical) score += addReason(reasons, 'canonical_metadata', 10)
  if (signals.hasDescription) score += addReason(reasons, 'description_metadata', 15)
  if (signals.activeTimeMs >= 15_000) score += addReason(reasons, 'active_15s', 20)
  if (signals.activeTimeMs >= 45_000) score += addReason(reasons, 'active_45s', 15)
  if (signals.scrollDepth >= 50) score += addReason(reasons, 'scroll_50', 25)
  if (signals.scrollDepth >= 85) score += addReason(reasons, 'scroll_85', 15)
  if (signals.interactionCount >= 1) score += addReason(reasons, 'interaction', 10)
  if (signals.explicitContent) score += addReason(reasons, 'explicit_content', 50)
  return score
}

function scoreFriction(signals: ObjectiveSignals, reasons: string[]): number {
  let score = 0
  if (signals.activeTimeMs >= 20_000) score += addReason(reasons, 'active_20s', 15)
  if (signals.activeTimeMs >= 60_000) score += addReason(reasons, 'active_60s', 15)
  if (signals.interactionCount >= 3) score += addReason(reasons, 'interactions_3', 25)
  if (signals.interactionCount >= 8) score += addReason(reasons, 'interactions_8', 20)
  if (signals.visitCount >= 2) score += addReason(reasons, 'return_visit', 10)
  if (signals.activeTimeMs >= 45_000 && signals.interactionCount >= 5 && signals.scrollDepth < 20) {
    score += addReason(reasons, 'repeated_activity_without_progress', 15)
  }
  if (signals.explicitFriction) score += addReason(reasons, 'explicit_friction', 80)
  return score
}

function reactionThreshold(objective: WidgetObjective): number {
  if (objective === 'continuous_feedback') return 40
  if (objective === 'detect_friction') return 60
  return 55
}

function promptThreshold(objective: WidgetObjective): number {
  if (objective === 'detect_friction') return 80
  if (objective === 'continuous_feedback') return 70
  return 75
}

function addReason(reasons: string[], reason: string, points: number): number {
  reasons.push(reason)
  return points
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
