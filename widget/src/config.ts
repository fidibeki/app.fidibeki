import { canonicalPage as parseCanonicalPage } from './targeting'
import type { TargetCondition, WidgetConfiguration, WidgetRule } from './types'

export { canonicalPage } from './targeting'

export interface TargetingRegexModule {
  matches(pattern: string, value: string): boolean
}

export interface MatchOptions {
  loadRegex?: () => Promise<TargetingRegexModule>
  onError?: (error: unknown, rule: WidgetRule) => void
}

export interface ConfigRequest {
  endpoint: URL
  publicKey: string
  fetchImpl?: typeof fetch
}

export async function fetchConfiguration(request: ConfigRequest): Promise<WidgetConfiguration> {
  const fetchImpl = request.fetchImpl ?? fetch
  const url = new URL(request.endpoint)
  url.searchParams.set('key', request.publicKey)
  const response = await fetchImpl(url, {
    method: 'GET',
    mode: 'cors',
    credentials: 'omit',
    headers: { 'X-Fidibeki-Public-Key': request.publicKey },
  })
  if (!response.ok) {
    throw new Error(`Fidibeki configuration failed with status ${response.status}`)
  }
  const configuration = await response.json() as WidgetConfiguration
  for (const rule of [...(configuration.rules ?? []), ...(configuration.definitions ?? [])]) {
    if (!rule.replay_consent) {
      rule.replay_consent = {
        mode: 'none',
        notice_version: '',
        controller_name: '',
        privacy_policy_url: '',
        supported_locales: [],
        retention_days: 0,
        expiry_days: 0,
        notices: {},
      }
    }
    if (!rule.ai_processing) rule.ai_processing = { enabled: false }
  }
  return configuration
}

export interface ConnectionRequest {
  endpoint: URL
  publicKey: string
  ruleID: string
  fetchImpl?: typeof fetch
}

export async function reportConnection(request: ConnectionRequest): Promise<void> {
  const fetchImpl = request.fetchImpl ?? fetch
  const url = new URL(request.endpoint)
  url.searchParams.set('key', request.publicKey)
  url.searchParams.set('rule_id', request.ruleID)
  const response = await fetchImpl(url, {
    method: 'POST',
    mode: 'cors',
    credentials: 'omit',
  })
  if (!response.ok) {
    throw new Error(`Fidibeki connection report failed with status ${response.status}`)
  }
}

export async function matchRule(
  rules: WidgetRule[],
  currentURL: string,
  options: MatchOptions = {},
): Promise<WidgetRule | null> {
  const ordered = [...rules].sort((left, right) => right.priority - left.priority)
  let regexModule: Promise<TargetingRegexModule> | undefined
  const loadRegex = options.loadRegex
    ? () => (regexModule ??= options.loadRegex!())
    : undefined
  const ruleOptions: MatchOptions = { ...options }
  if (loadRegex) ruleOptions.loadRegex = loadRegex
  for (const rule of ordered) {
    if (await ruleMatches(rule, currentURL, ruleOptions)) {
      return rule
    }
  }
  return null
}

export async function ruleMatches(
  rule: WidgetRule,
  currentURL: string,
  options: MatchOptions = {},
): Promise<boolean> {
  try {
    const page = parseCanonicalPage(currentURL)
    if (!page) throw new TypeError('Targeting requires an absolute HTTP(S) URL')
    const conditions = ruleConditions(rule)
    validateConditions(conditions.includes, conditions.excludes)
    const candidates = [...conditions.includes, ...conditions.excludes]
    const regex = candidates.some(condition => condition.matcher_type === 'url_regex')
      ? await loadRegex(options)
      : undefined
    const includeMatches = conditions.includes.map(condition => conditionMatches(condition, page.target, regex))
    const excludeMatches = conditions.excludes.map(condition => conditionMatches(condition, page.target, regex))
    return includeMatches.some(Boolean) && !excludeMatches.some(Boolean)
  } catch (error) {
    options.onError?.(error, rule)
    return false
  }
}

function conditionMatches(
  condition: TargetCondition,
  canonicalTarget: string,
  regex: TargetingRegexModule | undefined,
): boolean {
  switch (condition.matcher_type) {
    case 'all_pages':
      return true
    case 'url_exact': {
      const matcher = parseCanonicalPage(condition.matcher_value ?? '')
      return matcher?.target === canonicalTarget
    }
    case 'url_prefix': {
      const matcher = parseCanonicalPage(condition.matcher_value ?? '')
      return matcher !== null && canonicalTarget.startsWith(matcher.target)
    }
    case 'url_regex':
      return regex!.matches(condition.matcher_value!, canonicalTarget)
  }
}

function ruleConditions(rule: WidgetRule): { includes: TargetCondition[]; excludes: TargetCondition[] } {
  if (rule.include_conditions !== undefined || rule.exclude_conditions !== undefined) {
    return { includes: rule.include_conditions ?? [], excludes: rule.exclude_conditions ?? [] }
  }
  return {
    includes: [{ matcher_type: rule.matcher_type, matcher_value: rule.matcher_value }],
    excludes: [],
  }
}

function validateConditions(includes: TargetCondition[], excludes: TargetCondition[]): void {
  if (includes.length < 1 || includes.length > 20) throw new TypeError('A rule requires 1-20 include conditions')
  if (excludes.length > 20) throw new TypeError('A rule allows at most 20 exclude conditions')
  if (includes.some(condition => condition.matcher_type === 'all_pages') && includes.length !== 1) {
    throw new TypeError('All pages must be the sole include condition')
  }
  if (excludes.some(condition => condition.matcher_type === 'all_pages')) {
    throw new TypeError('All pages cannot be an exclude condition')
  }
  for (const condition of [...includes, ...excludes]) validateCondition(condition)
}

function validateCondition(condition: TargetCondition): void {
  if (!['all_pages', 'url_exact', 'url_prefix', 'url_regex'].includes(condition.matcher_type)) {
    throw new TypeError('Unknown targeting matcher type')
  }
  if (condition.matcher_type === 'all_pages') {
    if (condition.matcher_value !== null) throw new TypeError('All pages cannot have a matcher value')
    return
  }
  if (typeof condition.matcher_value !== 'string' || condition.matcher_value.length < 1 || condition.matcher_value.length > 2048) {
    throw new TypeError('Targeting condition requires a matcher value of at most 2048 characters')
  }
  if (condition.matcher_type !== 'url_regex' && !parseCanonicalPage(condition.matcher_value)) {
    throw new TypeError('Exact and prefix matchers require an absolute HTTP(S) URL')
  }
}

async function loadRegex(options: MatchOptions): Promise<TargetingRegexModule> {
  if (!options.loadRegex) throw new TypeError('Safe regex evaluator is unavailable')
  return options.loadRegex()
}

export interface ConfigCache {
  value: WidgetConfiguration
  fetchedAt: number
}

export function cacheIsFresh(cache: ConfigCache | null, now: number): cache is ConfigCache {
  if (cache === null) return false
  const refreshMilliseconds = Math.max(1, cache.value.refresh_after_seconds) * 1000
  return now - cache.fetchedAt < refreshMilliseconds
}
