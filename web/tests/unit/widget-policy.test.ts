import { describe, expect, it } from 'vitest'
import {
  decisionGuardrailKey,
  decisionReasonLabel,
  decisionReasonKey,
  defaultDecisionSignals,
  humanizePolicyToken,
  legacyTriggerLabel,
  recurrenceGuardrailLabel,
  recurrenceSummaryMessage,
  triggerSummaryMessage,
} from '../../utils/widget-policy'
import { evaluateObjectivePolicy } from '../../../contracts/widget-objective-policy'

describe('widget decision diagnostics', () => {
  it('uses the runtime evaluator for the backoffice decision', () => {
    const decision = evaluateObjectivePolicy('after_engagement', 'reaction', {
      ...defaultDecisionSignals(), activeTimeMs: 30_000, scrollDepth: 75, interactionCount: 3,
    })
    expect(decision.eligible).toBe(true)
    expect(decision.reasons.map(decisionReasonKey)).toContain('projects.decision.reasons.active_30s')
  })

  it('resolves unknown reasons and guardrails to an empty catalog key', () => {
    expect(decisionReasonKey('unknown_reason')).toBe('')
    expect(decisionGuardrailKey('unknown_guardrail')).toBe('')
    expect(humanizePolicyToken('unknown_reason')).toBe('unknown reason')
  })

  it('formats trigger and recurrence guardrails as catalog messages', () => {
    expect(triggerSummaryMessage('delay', 4, null)).toEqual({
      key: 'projects.decision.triggers.delay', params: {}, count: 4,
    })
    expect(triggerSummaryMessage('scroll', null, 60)).toEqual({
      key: 'projects.decision.triggers.scroll', params: { percent: 60 },
    })
    expect(triggerSummaryMessage('exit_intent', null, null)).toEqual({
      key: 'projects.decision.triggers.exitIntent', params: {},
    })
    expect(recurrenceSummaryMessage('cooldown', 30, 'prompt')).toEqual({
      key: 'projects.decision.recurrences.minimumCooldown', params: {}, count: 30,
    })
    expect(recurrenceSummaryMessage('session', null, 'launcher')).toEqual({
      key: 'projects.decision.recurrences.session', params: {},
    })
  })

  it('keeps the legacy English formatting helpers compatible', () => {
    expect(decisionReasonLabel('active_30s')).toBe('Active for 30 seconds')
    expect(legacyTriggerLabel('delay', 4, null)).toBe('After 4 seconds')
    expect(recurrenceGuardrailLabel('cooldown', 30, 'prompt')).toBe('30-day minimum cooldown')
  })
})
