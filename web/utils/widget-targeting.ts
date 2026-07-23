import { RE2JS } from 're2js'
import type {
  MatcherType,
  WidgetRule,
  WidgetTargetConditionInput,
} from '~/types/api'

export type TargetingMode = 'all_pages' | 'specific_pages' | null
export type TargetingTestStatus = 'eligible' | 'excluded' | 'unmatched' | 'invalid'

export interface CanonicalPage {
  target: string
  pathname: string
}

export interface TargetingFormState {
  mode: TargetingMode
  includes: WidgetTargetConditionInput[]
  excludes: WidgetTargetConditionInput[]
}

export interface TargetingTestResult {
  status: TargetingTestStatus
  message: string
  conditionIndex?: number
}

export interface TargetingTestDiagnostic {
  url: string
  result: TargetingTestResult
}

const targetingErrorKeys: Record<string, string> = {
  'Choose where this rule should appear.': 'choose',
  'Add between 1 and 20 include conditions.': 'includeCount',
  'Use no more than 20 exclude conditions.': 'excludeCount',
  'All pages must be the only include condition.': 'allPagesOnly',
  'All pages cannot be excluded.': 'allPagesExcluded',
  'All pages cannot have a value.': 'allPagesValue',
  'Enter a matcher value.': 'matcherValue',
  'Use no more than 2048 characters.': 'valueLength',
  'Enter a valid safe regular expression.': 'invalidRegex',
  'Enter an absolute HTTP(S) URL.': 'absoluteURL',
}

/**
 * Maps a stable validation outcome from `validateTargeting` to its catalog key.
 * Unrecognized outcomes fall back to the generic per-condition message.
 */
export function targetingErrorCatalogKey(error: string): string {
  return `onboarding.targeting.errors.${targetingErrorKeys[error] || 'condition'}`
}

export function targetingTestResultMessage(
  translate: (key: string, parameters?: Record<string, unknown>) => string,
  result: TargetingTestResult,
): string {
  return translate(
    `onboarding.targeting.testResults.${result.status}`,
    result.status === 'excluded' ? { index: result.conditionIndex } : {},
  )
}

export function canonicalPage(rawURL: string): CanonicalPage | null {
  try {
    const url = new URL(rawURL)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    const pathname = url.pathname === '/' ? '/' : url.pathname.replace(/\/+$/, '') || '/'
    return { target: `${url.origin}${pathname}`, pathname }
  } catch {
    return null
  }
}

export function targetingFromRule(rule: WidgetRule): TargetingFormState {
  const hasConditionContract = rule.include_conditions !== undefined || rule.exclude_conditions !== undefined
  const includes = hasConditionContract
    ? (rule.include_conditions ?? []).map(toInputCondition)
    : [{ matcher_type: rule.matcher_type, matcher_value: rule.matcher_value ?? null }]
  const excludes = hasConditionContract
    ? (rule.exclude_conditions ?? []).map(toInputCondition)
    : []
  return {
    mode: includes.length === 1 && includes[0]?.matcher_type === 'all_pages' ? 'all_pages' : 'specific_pages',
    includes,
    excludes,
  }
}

export function validateTargeting(state: TargetingFormState): Record<string, string> {
  const fields: Record<string, string> = {}
  if (state.mode === null) {
    fields.targeting = 'Choose where this rule should appear.'
    return fields
  }
  const includes = state.mode === 'all_pages'
    ? [{ matcher_type: 'all_pages', matcher_value: null } satisfies WidgetTargetConditionInput]
    : state.includes
  if (includes.length < 1 || includes.length > 20) {
    fields.include_conditions = 'Add between 1 and 20 include conditions.'
  }
  if (state.excludes.length > 20) {
    fields.exclude_conditions = 'Use no more than 20 exclude conditions.'
  }
  if (includes.some(condition => condition.matcher_type === 'all_pages') && includes.length !== 1) {
    fields.include_conditions = 'All pages must be the only include condition.'
  }
  includes.forEach((condition, index) => validateCondition(condition, 'include_conditions', index, fields))
  state.excludes.forEach((condition, index) => {
    if (condition.matcher_type === 'all_pages') {
      fields[`exclude_conditions[${index}].matcher_type`] = 'All pages cannot be excluded.'
    }
    validateCondition(condition, 'exclude_conditions', index, fields)
  })
  return fields
}

export function buildTargetingPayload(state: TargetingFormState) {
  const fields = validateTargeting(state)
  if (Object.keys(fields).length > 0) throw new TypeError('Targeting configuration is invalid')
  const includes = (state.mode === 'all_pages'
    ? [{ matcher_type: 'all_pages', matcher_value: null } satisfies WidgetTargetConditionInput]
    : state.includes).map(normalizeCondition)
  const excludes = state.excludes.map(normalizeCondition)
  const first = includes[0]!
  return {
    matcher_type: first.matcher_type,
    matcher_value: first.matcher_value,
    include_conditions: includes,
    exclude_conditions: excludes,
  }
}

export function testTargetingURL(rawURL: string, state: TargetingFormState): TargetingTestResult {
  const page = canonicalPage(rawURL.trim())
  if (!page) return { status: 'invalid', message: 'Enter an absolute HTTP(S) URL.' }
  const fields = validateTargeting(state)
  if (Object.keys(fields).length > 0) {
    return { status: 'invalid', message: 'Resolve the targeting errors before testing.' }
  }
  try {
    const payload = buildTargetingPayload(state)
    const included = payload.include_conditions.some(condition => conditionMatches(condition, page.target))
    if (!included) return { status: 'unmatched', message: 'No include condition matched.' }
    const excludedAt = payload.exclude_conditions.findIndex(condition => conditionMatches(condition, page.target))
    if (excludedAt >= 0) {
      return {
        status: 'excluded',
        message: `Excluded by condition ${excludedAt + 1}.`,
        conditionIndex: excludedAt + 1,
      }
    }
    return { status: 'eligible', message: 'This rule is eligible on the tested page.' }
  } catch {
    return { status: 'invalid', message: 'A targeting condition is invalid.' }
  }
}

function validateCondition(
  condition: WidgetTargetConditionInput,
  collection: 'include_conditions' | 'exclude_conditions',
  index: number,
  fields: Record<string, string>,
) {
  const valueField = `${collection}[${index}].matcher_value`
  if (condition.matcher_type === 'all_pages') {
    if (condition.matcher_value !== null) fields[valueField] = 'All pages cannot have a value.'
    return
  }
  const value = condition.matcher_value?.trim() ?? ''
  if (!value) {
    fields[valueField] = 'Enter a matcher value.'
    return
  }
  if (value.length > 2048) {
    fields[valueField] = 'Use no more than 2048 characters.'
    return
  }
  if (condition.matcher_type === 'url_regex') {
    try {
      RE2JS.compile(value)
    } catch {
      fields[valueField] = 'Enter a valid safe regular expression.'
    }
    return
  }
  if (!canonicalPage(value)) fields[valueField] = 'Enter an absolute HTTP(S) URL.'
}

function normalizeCondition(condition: WidgetTargetConditionInput): WidgetTargetConditionInput {
  if (condition.matcher_type === 'all_pages') return { matcher_type: 'all_pages', matcher_value: null }
  const value = condition.matcher_value?.trim() ?? ''
  if (condition.matcher_type === 'url_regex') return { matcher_type: 'url_regex', matcher_value: value }
  return { matcher_type: condition.matcher_type, matcher_value: canonicalPage(value)!.target }
}

function conditionMatches(condition: WidgetTargetConditionInput, target: string): boolean {
  switch (condition.matcher_type) {
    case 'all_pages':
      return true
    case 'url_exact':
      return canonicalPage(condition.matcher_value ?? '')?.target === target
    case 'url_prefix': {
      const matcher = canonicalPage(condition.matcher_value ?? '')
      return matcher !== null && target.startsWith(matcher.target)
    }
    case 'url_regex':
      return RE2JS.compile(condition.matcher_value ?? '').test(target)
  }
}

function toInputCondition(condition: { matcher_type: MatcherType; matcher_value?: string | null }) {
  return { matcher_type: condition.matcher_type, matcher_value: condition.matcher_value ?? null }
}
