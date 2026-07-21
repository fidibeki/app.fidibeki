import { cacheIsFresh, fetchConfiguration, matchRule, ruleMatches } from '../../src/config'
import type { WidgetConfiguration } from '../../src/types'
import { rule } from './fixtures'

describe('rule matching', () => {
  it.each([
    [rule({ matcher_type: 'all_pages' }), 'https://example.com/anything', true],
    [rule({ matcher_type: 'url_exact', matcher_value: 'https://example.com/docs' }), 'https://example.com/docs', true],
    [rule({ matcher_type: 'url_exact', matcher_value: 'https://example.com/docs' }), 'https://example.com/docs/one', false],
    [rule({ matcher_type: 'url_prefix', matcher_value: 'https://example.com/docs' }), 'https://example.com/docs/one', true],
    [rule({ matcher_type: 'url_regex', matcher_value: '^https://example\\.com/(docs|help)' }), 'https://example.com/help', true],
    [rule({ matcher_type: 'url_regex', matcher_value: '[' }), 'https://example.com/help', false],
    [rule({ matcher_type: 'url_regex', matcher_value: null }), 'https://example.com/help', false],
  ])('matches %#j against %s', (candidate, url, want) => {
    expect(ruleMatches(candidate, url)).toBe(want)
  })

  it('selects the highest priority matching rule without mutating input', () => {
    const rules = [
      rule({ id: 'fallback', priority: 1 }),
      rule({ id: 'checkout', priority: 20, matcher_type: 'url_prefix', matcher_value: 'https://example.com/checkout' }),
      rule({ id: 'other', priority: 50, matcher_type: 'url_exact', matcher_value: 'https://example.com/other' }),
    ]
    expect(matchRule(rules, 'https://example.com/checkout/confirm')?.id).toBe('checkout')
    expect(rules.map((candidate) => candidate.id)).toEqual(['fallback', 'checkout', 'other'])
  })

  it('returns null when no rules match', () => {
    expect(matchRule([rule({ matcher_type: 'url_exact', matcher_value: 'https://other.test' })], 'https://example.com')).toBeNull()
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
    expect(options).toMatchObject({ credentials: 'omit', mode: 'cors' })
    expect(new Headers(options?.headers).get('X-Fidibeki-Public-Key')).toBe('pk_live_test')
  })

  it('throws for denied configuration', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(new Response('', { status: 403 }))
    await expect(fetchConfiguration({
      endpoint: new URL('https://widget.test/v1/widget/config'), publicKey: 'denied', fetchImpl,
    })).rejects.toThrow('status 403')
  })

  it('uses the server refresh interval', () => {
    const cache = { value: configuration, fetchedAt: 1_000 }
    expect(cacheIsFresh(cache, 300_999)).toBe(true)
    expect(cacheIsFresh(cache, 301_000)).toBe(false)
    expect(cacheIsFresh(null, 1_000)).toBe(false)
  })
})
