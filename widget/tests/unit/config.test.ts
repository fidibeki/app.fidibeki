import targetingContract from '../../../contracts/widget-targeting.json'
import { cacheIsFresh, canonicalPage, fetchConfiguration, matchRule, reportConnection, ruleMatches } from '../../src/config'
import type { TargetCondition, WidgetConfiguration } from '../../src/types'
import { rule } from './fixtures'

const loadTestRegex = async () => ({ matches: (pattern: string, value: string) => new RegExp(pattern).test(value) })

describe('rule matching', () => {
  it.each(targetingContract.canonical_pages)('canonicalizes $name', (testCase) => {
    const result = canonicalPage(testCase.input)
    if (!testCase.valid) {
      expect(result).toBeNull()
      return
    }
    expect(result).toEqual({ target: testCase.canonical, pathname: testCase.pathname })
  })

  it.each([
    [rule({ matcher_type: 'all_pages' }), 'https://example.com/anything', true],
    [rule({ matcher_type: 'url_exact', matcher_value: 'https://example.com/docs' }), 'https://example.com/docs', true],
    [rule({ matcher_type: 'url_exact', matcher_value: 'https://example.com/docs' }), 'https://example.com/docs/one', false],
    [rule({ matcher_type: 'url_prefix', matcher_value: 'https://example.com/docs' }), 'https://example.com/docs/one', true],
    [rule({ matcher_type: 'url_regex', matcher_value: '^https://example\\.com/(docs|help)' }), 'https://example.com/help', true],
    [rule({ matcher_type: 'url_regex', matcher_value: '[' }), 'https://example.com/help', false],
    [rule({ matcher_type: 'url_regex', matcher_value: null }), 'https://example.com/help', false],
  ])('matches %#j against %s', async (candidate, url, want) => {
    await expect(ruleMatches(candidate, url, { loadRegex: loadTestRegex })).resolves.toBe(want)
  })

  it.each(targetingContract.condition_matches)('matches shared condition: $name', async (testCase) => {
    const condition = testCase.condition as TargetCondition
    await expect(ruleMatches(rule({
      matcher_type: condition.matcher_type,
      matcher_value: condition.matcher_value,
    }), testCase.url, { loadRegex: loadTestRegex })).resolves.toBe(testCase.matches)
  })

  it.each(targetingContract.rule_decisions)('matches shared rule: $name', async (testCase) => {
    await expect(ruleMatches(rule({
      include_conditions: testCase.include_conditions as TargetCondition[],
      exclude_conditions: testCase.exclude_conditions as TargetCondition[],
    }), testCase.url, { loadRegex: loadTestRegex })).resolves.toBe(testCase.eligible)
  })

  it('selects the highest priority matching rule without mutating input', async () => {
    const rules = [
      rule({ id: 'fallback', priority: 1 }),
      rule({ id: 'checkout', priority: 20, matcher_type: 'url_prefix', matcher_value: 'https://example.com/checkout' }),
      rule({ id: 'other', priority: 50, matcher_type: 'url_exact', matcher_value: 'https://example.com/other' }),
    ]
    expect((await matchRule(rules, 'https://example.com/checkout/confirm'))?.id).toBe('checkout')
    expect(rules.map((candidate) => candidate.id)).toEqual(['fallback', 'checkout', 'other'])
  })

  it('returns null when no rules match', async () => {
    await expect(matchRule(
      [rule({ matcher_type: 'url_exact', matcher_value: 'https://other.test' })],
      'https://example.com',
    )).resolves.toBeNull()
  })

  it('fails malformed condition arrays closed and reports the rule', async () => {
    const onError = vi.fn()
    await expect(ruleMatches(rule({ include_conditions: [], exclude_conditions: [] }), 'https://example.com', {
      onError,
    })).resolves.toBe(false)
    expect(onError).toHaveBeenCalledOnce()
  })

  it('loads the regex evaluator once and fails the complete rule closed on regex errors', async () => {
    const loadRegex = vi.fn().mockResolvedValue({
      matches: (pattern: string, value: string) => {
        if (pattern === '[') throw new Error('invalid regex')
        return new RegExp(pattern).test(value)
      },
    })
    const onError = vi.fn()
    const candidate = rule({
      include_conditions: [
        { matcher_type: 'url_exact', matcher_value: 'https://example.com/docs' },
        { matcher_type: 'url_regex', matcher_value: '^https://example\\.com/docs' },
      ],
      exclude_conditions: [{ matcher_type: 'url_regex', matcher_value: '[' }],
    })

    await expect(ruleMatches(candidate, 'https://example.com/docs', { loadRegex, onError })).resolves.toBe(false)
    expect(loadRegex).toHaveBeenCalledOnce()
    expect(onError).toHaveBeenCalledOnce()
  })

  it('does not load the regex evaluator for exact and prefix conditions', async () => {
    const loadRegex = vi.fn()
    await expect(ruleMatches(rule({
      include_conditions: [{ matcher_type: 'url_prefix', matcher_value: 'https://example.com/docs' }],
      exclude_conditions: [{ matcher_type: 'url_exact', matcher_value: 'https://example.com/docs/private' }],
    }), 'https://example.com/docs/public', { loadRegex })).resolves.toBe(true)
    expect(loadRegex).not.toHaveBeenCalled()
  })
})

describe('configuration fetch and cache', () => {
  const configuration = { project_id: 'project-id', refresh_after_seconds: 300 } as WidgetConfiguration

  it('sends the public key without credentials', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(configuration), { status: 200 }))
    await expect(fetchConfiguration({
      endpoint: new URL('https://widget.test/v1/widget/config'),
      publicKey: 'pk_live_test',
      fetchImpl,
    })).resolves.toEqual(configuration)
    const [url, options] = fetchImpl.mock.calls[0] ?? []
    expect(String(url)).toContain('key=pk_live_test')
    expect(String(url)).not.toContain('page_url')
    expect(options).toMatchObject({ credentials: 'omit', mode: 'cors' })
    expect(new Headers(options?.headers).get('X-Fidibeki-Public-Key')).toBe('pk_live_test')
  })

  it('throws for denied configuration', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(new Response('', { status: 403 }))
    await expect(fetchConfiguration({
      endpoint: new URL('https://widget.test/v1/widget/config'), publicKey: 'denied',
      fetchImpl,
    })).rejects.toThrow('status 403')
  })

  it('fails legacy replay configuration closed when consent orchestration is absent', async () => {
    const legacyRule = rule({ replay_mode: 'full' })
    const { replay_consent: _removed, ...withoutConsent } = legacyRule
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({
      ...configuration,
      rules: [withoutConsent],
      definitions: [withoutConsent],
    }), { status: 200 }))

    const result = await fetchConfiguration({
      endpoint: new URL('https://widget.test/v1/widget/config'),
      publicKey: 'pk_live_test',
      fetchImpl,
    })

    expect(result.rules[0]?.replay_consent).toEqual(expect.objectContaining({ mode: 'none' }))
    expect(result.definitions?.[0]?.replay_consent).toEqual(expect.objectContaining({ mode: 'none' }))
    expect(result.rules[0]?.ai_processing).toEqual({ enabled: false })
    expect(result.definitions?.[0]?.ai_processing).toEqual({ enabled: false })
  })

  it('reports only the selected rule without page data', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(new Response('', { status: 204 }))
    await expect(reportConnection({
      endpoint: new URL('https://widget.test/v1/widget/connected'),
      publicKey: 'pk_live_test',
      ruleID: 'rule-test',
      fetchImpl,
    })).resolves.toBeUndefined()
    const [url, options] = fetchImpl.mock.calls[0] ?? []
    expect(String(url)).toBe('https://widget.test/v1/widget/connected?key=pk_live_test&rule_id=rule-test')
    expect(options).toEqual({ method: 'POST', mode: 'cors', credentials: 'omit' })
  })

  it('uses the server refresh interval', () => {
    const cache = { value: configuration, fetchedAt: 1_000 }
    expect(cacheIsFresh(cache, 300_999)).toBe(true)
    expect(cacheIsFresh(cache, 301_000)).toBe(false)
    expect(cacheIsFresh(null, 1_000)).toBe(false)
  })
})
