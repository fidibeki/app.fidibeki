import type { ObjectiveSignals } from '../../contracts/widget-objective-policy'
import type {
  RecurrenceMode,
  TriggerType,
  WidgetPresentation,
} from '~/types/api'

export interface PolicyMessage {
  key: string
  params: Record<string, number>
  count?: number
}

const reasonKeys = new Set([
  'continuous_availability',
  'active_5s',
  'active_10s',
  'active_15s',
  'active_20s',
  'active_30s',
  'active_45s',
  'active_60s',
  'scroll_25',
  'scroll_40',
  'scroll_50',
  'scroll_75',
  'scroll_85',
  'interaction',
  'interactions_3',
  'interactions_8',
  'return_visit',
  'document_title',
  'canonical_metadata',
  'description_metadata',
  'explicit_engagement',
  'explicit_content',
  'explicit_friction',
  'repeated_activity_without_progress',
])

const guardrailKeys = new Set([
  'discreet_launcher',
  'layout_stable_inline',
  'engaged_reaction_threshold',
  'strict_prompt_threshold',
])

const legacyReasonLabels: Record<string, string> = {
  continuous_availability: 'Continuous availability',
  active_5s: 'Active for 5 seconds',
  active_10s: 'Active for 10 seconds',
  active_15s: 'Active for 15 seconds',
  active_20s: 'Active for 20 seconds',
  active_30s: 'Active for 30 seconds',
  active_45s: 'Active for 45 seconds',
  active_60s: 'Active for 60 seconds',
  scroll_25: 'Scrolled 25%',
  scroll_40: 'Scrolled 40%',
  scroll_50: 'Scrolled 50%',
  scroll_75: 'Scrolled 75%',
  scroll_85: 'Scrolled 85%',
  interaction: 'Meaningful interaction',
  interactions_3: 'Three interactions',
  interactions_8: 'Eight interactions',
  return_visit: 'Return visit',
  document_title: 'Document title present',
  canonical_metadata: 'Canonical URL present',
  description_metadata: 'Public description present',
  explicit_engagement: 'Explicit engagement signal',
  explicit_content: 'Explicit content signal',
  explicit_friction: 'Explicit friction signal',
  repeated_activity_without_progress: 'Repeated activity without progress',
}

const legacyGuardrailLabels: Record<string, string> = {
  discreet_launcher: 'Launcher stays closed until opened',
  layout_stable_inline: 'Inline placement remains layout-stable',
  engaged_reaction_threshold: 'Reaction waits for meaningful engagement',
  strict_prompt_threshold: 'Prompt uses the strictest confidence threshold',
}

export function defaultDecisionSignals(): ObjectiveSignals {
  return {
    activeTimeMs: 0,
    scrollDepth: 0,
    interactionCount: 0,
    visitCount: 1,
    hasTitle: true,
    hasCanonical: true,
    hasDescription: true,
    explicitEngagement: false,
    explicitContent: false,
    explicitFriction: false,
  }
}

/** Returns the catalog key for a decision reason, or an empty string when it is unknown. */
export function decisionReasonKey(reason: string): string {
  return reasonKeys.has(reason) ? `projects.decision.reasons.${reason}` : ''
}

/** Returns the catalog key for a policy guardrail, or an empty string when it is unknown. */
export function decisionGuardrailKey(guardrail: string): string {
  return guardrailKeys.has(guardrail) ? `projects.decision.guardrails.${guardrail}` : ''
}

export function humanizePolicyToken(token: string): string {
  return token.replaceAll('_', ' ')
}

/** @deprecated Use `decisionReasonKey` and translate the returned catalog key. */
export function decisionReasonLabel(reason: string): string {
  return legacyReasonLabels[reason] || humanizePolicyToken(reason)
}

/** @deprecated Use `decisionGuardrailKey` and translate the returned catalog key. */
export function decisionGuardrailLabel(guardrail: string): string {
  return legacyGuardrailLabels[guardrail] || humanizePolicyToken(guardrail)
}

export function triggerSummaryMessage(
  type: TriggerType,
  delaySeconds: number | null,
  scrollPercent: number | null,
): PolicyMessage {
  if (type === 'scroll') {
    return { key: 'projects.decision.triggers.scroll', params: { percent: scrollPercent ?? 100 } }
  }
  if (type === 'exit_intent') return { key: 'projects.decision.triggers.exitIntent', params: {} }
  const seconds = Math.max(0, delaySeconds ?? 0)
  return seconds === 0
    ? { key: 'projects.decision.triggers.noDelay', params: {} }
    : { key: 'projects.decision.triggers.delay', params: {}, count: seconds }
}

export function recurrenceSummaryMessage(
  mode: RecurrenceMode,
  days: number | null,
  presentation: WidgetPresentation,
): PolicyMessage {
  if (mode === 'never') return { key: 'projects.decision.recurrences.never', params: {} }
  if (presentation === 'launcher' || presentation === 'inline') {
    if (mode === 'session') return { key: 'projects.decision.recurrences.session', params: {} }
    return { key: 'projects.decision.recurrences.cooldown', params: {}, count: days ?? 30 }
  }
  const minimum = presentation === 'prompt' ? 30 : 7
  const effective = Math.max(minimum, days ?? minimum)
  return effective === minimum
    ? { key: 'projects.decision.recurrences.minimumCooldown', params: {}, count: minimum }
    : { key: 'projects.decision.recurrences.cooldown', params: {}, count: effective }
}

/** @deprecated Use `triggerSummaryMessage` and translate its catalog message. */
export function legacyTriggerLabel(
  type: TriggerType,
  delaySeconds: number | null,
  scrollPercent: number | null,
): string {
  if (type === 'scroll') return `At ${scrollPercent ?? 100}% scroll`
  if (type === 'exit_intent') return 'On exit intent'
  const seconds = Math.max(0, delaySeconds ?? 0)
  return seconds === 0 ? 'No additional delay' : `After ${seconds} second${seconds === 1 ? '' : 's'}`
}

/** @deprecated Use `recurrenceSummaryMessage` and translate its catalog message. */
export function recurrenceGuardrailLabel(
  mode: RecurrenceMode,
  days: number | null,
  presentation: WidgetPresentation,
): string {
  if (mode === 'never') return 'Never after first submission'
  if (presentation === 'launcher' || presentation === 'inline') {
    if (mode === 'session') return 'Once per browser session'
    return `${days ?? 30}-day cooldown`
  }
  const minimum = presentation === 'prompt' ? 30 : 7
  const effective = Math.max(minimum, days ?? minimum)
  return effective === minimum ? `${minimum}-day minimum cooldown` : `${effective}-day cooldown`
}
