import { canonicalPage } from './targeting'
import type { MatcherType, TargetCondition, WidgetRule } from './types'

export type PrivacyLocationSource = 'exact' | 'prefix' | 'origin'

export interface PrivacySafeLocation {
  target: string
  source: PrivacyLocationSource
}

type PrivacyRule = Pick<
  WidgetRule,
  'matcher_type' | 'matcher_value' | 'include_conditions'
>

export function privacySafeLocation(
  rawURL: string,
  rule: PrivacyRule,
): PrivacySafeLocation | null {
  const page = canonicalPage(rawURL)
  if (!page) return null
  const origin = new URL(page.target).origin
  const conditions = privacyIncludeConditions(rule)
  let exact: string | null = null
  let prefix: string | null = null

  for (const condition of conditions) {
    if (
      condition.matcher_type !== 'url_exact' &&
      condition.matcher_type !== 'url_prefix'
    ) continue
    if (!condition.matcher_value) continue
    const candidate = canonicalPage(condition.matcher_value)
    if (!candidate || new URL(candidate.target).origin !== origin) continue
    if (condition.matcher_type === 'url_exact' && candidate.target === page.target) {
      exact = candidate.target
      continue
    }
    if (
      condition.matcher_type === 'url_prefix' &&
      page.target.startsWith(candidate.target) &&
      (prefix === null || candidate.target.length > prefix.length)
    ) {
      prefix = candidate.target
    }
  }
  if (exact !== null) return { target: exact, source: 'exact' }
  if (prefix !== null) return { target: prefix, source: 'prefix' }
  return { target: `${origin}/`, source: 'origin' }
}

function privacyIncludeConditions(rule: PrivacyRule): Array<Pick<TargetCondition, 'matcher_type' | 'matcher_value'>> {
  if ((rule.include_conditions?.length ?? 0) > 0) return rule.include_conditions ?? []
  return [{
    matcher_type: rule.matcher_type as MatcherType,
    matcher_value: rule.matcher_value ?? null,
  }]
}
