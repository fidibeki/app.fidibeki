import targetingContract from '../../../contracts/widget-targeting.json'
import { describe, expect, it } from 'vitest'
import type { WidgetRule, WidgetTargetConditionInput } from '../../types/api'
import {
  buildTargetingPayload,
  canonicalPage,
  targetingFromRule,
  testTargetingURL,
  validateTargeting,
} from '../../utils/widget-targeting'

describe('widget targeting contract', () => {
  it.each(targetingContract.canonical_pages)('canonicalizes $name', (testCase) => {
    const result = canonicalPage(testCase.input)
    if (!testCase.valid) {
      expect(result).toBeNull()
      return
    }
    expect(result).toEqual({ target: testCase.canonical, pathname: testCase.pathname })
  })

  it.each(targetingContract.rule_decisions)('tests shared rule: $name', (testCase) => {
    const result = testTargetingURL(testCase.url, {
      mode: testCase.include_conditions[0]?.matcher_type === 'all_pages' ? 'all_pages' : 'specific_pages',
      includes: testCase.include_conditions as WidgetTargetConditionInput[],
      excludes: testCase.exclude_conditions as WidgetTargetConditionInput[],
    })
    expect(result.status === 'eligible').toBe(testCase.eligible)
    expect(result.status).toBe(testCase.reason === 'included' ? 'eligible' : testCase.reason)
  })

  it.each(targetingContract.condition_matches)('tests shared condition: $name', (testCase) => {
    const result = testTargetingURL(testCase.url, {
      mode: testCase.condition.matcher_type === 'all_pages' ? 'all_pages' : 'specific_pages',
      includes: [testCase.condition as WidgetTargetConditionInput],
      excludes: [],
    })
    expect(result.status === 'eligible').toBe(testCase.matches)
  })

  it.each(targetingContract.invalid_rules)('rejects shared invalid rule: $name', (testCase) => {
    const fields = validateTargeting({
      mode: 'specific_pages',
      includes: testCase.include_conditions as WidgetTargetConditionInput[],
      excludes: testCase.exclude_conditions as WidgetTargetConditionInput[],
    })
    expect(fields[testCase.field]).toBeTruthy()
  })

  it('requires an explicit mode and canonicalizes request values', () => {
    expect(validateTargeting({ mode: null, includes: [], excludes: [] })).toHaveProperty('targeting')
    expect(buildTargetingPayload({
      mode: 'specific_pages',
      includes: [{ matcher_type: 'url_exact', matcher_value: ' HTTPS://Example.com:443/chart/?asset=BAI ' }],
      excludes: [],
    })).toMatchObject({
      matcher_type: 'url_exact',
      matcher_value: 'https://example.com/chart',
      include_conditions: [{ matcher_type: 'url_exact', matcher_value: 'https://example.com/chart' }],
    })
  })

  it('reads additive conditions and falls back to a legacy matcher', () => {
    const legacy = targetingFromRule({
      matcher_type: 'url_prefix', matcher_value: 'https://example.com/docs',
    } as WidgetRule)
    expect(legacy).toMatchObject({ mode: 'specific_pages', includes: [{ matcher_type: 'url_prefix' }] })

    const additive = targetingFromRule({
      matcher_type: 'all_pages', matcher_value: null,
      include_conditions: [{ id: 'include', matcher_type: 'url_exact', matcher_value: 'https://example.com', position: 0 }],
      exclude_conditions: [{ id: 'exclude', matcher_type: 'url_prefix', matcher_value: 'https://example.com/private', position: 0 }],
    } as WidgetRule)
    expect(additive).toMatchObject({ mode: 'specific_pages', excludes: [{ matcher_type: 'url_prefix' }] })
  })
})
