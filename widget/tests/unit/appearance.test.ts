import { describe, expect, it } from 'vitest'
import { contrastRatio, resolveAccentForeground } from '../../src/appearance'

describe('widget accent foreground', () => {
  it.each([
    ['#46c754', '#171a19'],
    ['#005f56', '#ffffff'],
    ['#46C754', '#171a19'],
    ['#777777', '#000000'],
  ])('derives an accessible foreground for %s', (accent, foreground) => {
    expect(resolveAccentForeground(accent)).toBe(foreground)
    expect(contrastRatio(accent, foreground)).toBeGreaterThanOrEqual(4.5)
  })

  it('falls back safely for an invalid legacy value', () => {
    expect(resolveAccentForeground('brand-green')).toBe('#ffffff')
  })
})
