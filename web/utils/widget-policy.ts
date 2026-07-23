import type { ObjectiveSignals } from '../../contracts/widget-objective-policy'
import type {
  RecurrenceMode,
  TriggerType,
  WidgetPresentation,
} from '~/types/api'

const reasonLabels: Record<string, string> = {
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

const guardrailLabels: Record<string, string> = {
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

export function decisionReasonLabel(reason: string): string {
  return reasonLabels[reason] || reason.replaceAll('_', ' ')
}

export function decisionGuardrailLabel(guardrail: string): string {
  return guardrailLabels[guardrail] || guardrail.replaceAll('_', ' ')
}

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
