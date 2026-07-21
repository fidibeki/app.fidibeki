import type { WidgetConfiguration, WidgetRule } from './types'

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
  return response.json() as Promise<WidgetConfiguration>
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

export function matchRule(rules: WidgetRule[], currentURL: string): WidgetRule | null {
  const ordered = [...rules].sort((left, right) => right.priority - left.priority)
  for (const rule of ordered) {
    if (ruleMatches(rule, currentURL)) {
      return rule
    }
  }
  return null
}

export function ruleMatches(rule: WidgetRule, currentURL: string): boolean {
  switch (rule.matcher_type) {
    case 'all_pages':
      return true
    case 'url_exact':
      return rule.matcher_value === currentURL
    case 'url_prefix':
      return rule.matcher_value !== null && currentURL.startsWith(rule.matcher_value)
    case 'url_regex':
      if (rule.matcher_value === null) return false
      try {
        return new RegExp(rule.matcher_value).test(currentURL)
      } catch {
        return false
      }
  }
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
