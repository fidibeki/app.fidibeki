import { describe, expect, it } from 'vitest'
import {
  defaultDecisionSignals,
  decisionReasonLabel,
  legacyTriggerLabel,
  recurrenceGuardrailLabel,
} from '../../utils/widget-policy'
import { evaluateObjectivePolicy } from '../../../contracts/widget-objective-policy'

describe('widget decision diagnostics', () => {
  it('uses the runtime evaluator for the backoffice decision', () => {
    const decision = evaluateObjectivePolicy('after_engagement', 'reaction', {
      ...defaultDecisionSignals(), activeTimeMs: 30_000, scrollDepth: 75, interactionCount: 3,
    })
    expect(decision.eligible).toBe(true)
    expect(decision.reasons.map(decisionReasonLabel)).toContain('Active for 30 seconds')
  })

  it('formats compatible trigger and recurrence guardrails', () => {
    expect(legacyTriggerLabel('delay', 4, null)).toBe('After 4 seconds')
    expect(legacyTriggerLabel('scroll', null, 60)).toBe('At 60% scroll')
    expect(legacyTriggerLabel('exit_intent', null, null)).toBe('On exit intent')
    expect(recurrenceGuardrailLabel('cooldown', 30, 'prompt')).toBe('30-day minimum cooldown')
    expect(recurrenceGuardrailLabel('session', null, 'launcher')).toBe('Once per browser session')
  })
})
