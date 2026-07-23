export type WidgetPolicyObjective =
  | 'continuous_feedback'
  | 'after_engagement'
  | 'validate_content'
  | 'detect_friction'

export type WidgetPolicyPresentation = 'launcher' | 'inline' | 'reaction' | 'prompt'

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

const maximumScore = 100

export function evaluateObjectivePolicy(
  objective: WidgetPolicyObjective | undefined,
  presentation: WidgetPolicyPresentation | undefined,
  signals: ObjectiveSignals,
): ObjectivePolicyDecision {
  const resolvedObjective = objective ?? 'continuous_feedback'
  const resolvedPresentation = presentation ?? 'prompt'
  const reasons: string[] = []
  const score = Math.min(maximumScore, scoreObjective(resolvedObjective, signals, reasons))

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

function scoreObjective(
  objective: WidgetPolicyObjective,
  signals: ObjectiveSignals,
  reasons: string[],
): number {
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

function reactionThreshold(objective: WidgetPolicyObjective): number {
  if (objective === 'continuous_feedback') return 40
  if (objective === 'detect_friction') return 60
  return 55
}

function promptThreshold(objective: WidgetPolicyObjective): number {
  if (objective === 'detect_friction') return 80
  if (objective === 'continuous_feedback') return 70
  return 75
}

function addReason(reasons: string[], reason: string, points: number): number {
  reasons.push(reason)
  return points
}
