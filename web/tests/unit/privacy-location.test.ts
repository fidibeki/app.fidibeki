import { describe, expect, it } from 'vitest'
import { specificPageLocation } from '../../utils/privacy-location'

describe('specificPageLocation', () => {
  it.each([
    '',
    'not-a-url',
    'javascript:alert(1)',
    'https://example.com',
    'https://example.com/',
    'https://example.com/?private=query#fragment',
  ])('omits origin-only or unsafe location %s', (value) => {
    expect(specificPageLocation(value)).toBeNull()
  })

  it('keeps a privacy-safe page path link', () => {
    expect(specificPageLocation('https://example.com/chart')).toBe(
      'https://example.com/chart',
    )
  })
})
