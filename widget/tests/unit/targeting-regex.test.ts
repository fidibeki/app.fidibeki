import targetingContract from '../../../contracts/widget-targeting.json'
import { matches } from '../../src/modules/targeting-regex'

describe('safe targeting regex', () => {
  it.each(targetingContract.condition_matches.filter(testCase => testCase.condition.matcher_type === 'url_regex'))(
    'matches shared condition: $name',
    (testCase) => {
      const url = new URL(testCase.url)
      const pathname = url.pathname === '/' ? '/' : url.pathname.replace(/\/+$/, '') || '/'
      expect(matches(testCase.condition.matcher_value ?? '', `${url.origin}${pathname}`)).toBe(testCase.matches)
    },
  )

  it('rejects invalid and unsupported backtracking syntax', () => {
    expect(() => matches('[', 'https://example.com')).toThrow()
    expect(() => matches('(?<=example)\\.com', 'https://example.com')).toThrow()
  })
})
