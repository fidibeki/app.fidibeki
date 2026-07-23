import { describe, expect, it } from 'vitest'
import { createRegionalFormatters } from '../../utils/regional-format'

describe('regional formatters', () => {
  it('applies timezone boundaries and daylight saving time', () => {
    const utc = createRegionalFormatters({ locale: 'en', timezone: 'UTC' })
    const luanda = createRegionalFormatters({ locale: 'en', timezone: 'Africa/Luanda' })
    const lisbon = createRegionalFormatters({ locale: 'en', timezone: 'Europe/Lisbon' })

    expect(utc.dateTime('2026-07-23T23:30:00Z')).toBe('Jul 23, 2026, 11:30 PM')
    expect(luanda.dateTime('2026-07-23T23:30:00Z')).toBe('Jul 24, 2026, 12:30 AM')
    expect(lisbon.dateTime('2026-03-29T01:30:00Z')).toBe('Mar 29, 2026, 2:30 AM')
    expect(lisbon.dateTime('not-a-date')).toBe('—')
  })

  it('formats regional numbers, currencies, and percentages', () => {
    const angola = createRegionalFormatters({ locale: 'pt-AO', timezone: 'Africa/Luanda' })

    expect(angola.integer(1_234_567)).toBe('1\u00a0234\u00a0567')
    expect(angola.decimal(1234.5)).toBe('1\u00a0234,5')
    expect(angola.currency(1234.5, 'AOA')).toBe('1\u00a0234,50\u00a0Kz')
    expect(angola.percent(0.375)).toBe('37,5%')
  })

  it('formats relative time and lists and preserves regional plural rules', () => {
    const english = createRegionalFormatters({ locale: 'en', timezone: 'UTC' })
    const angola = createRegionalFormatters({ locale: 'pt-AO', timezone: 'Africa/Luanda' })
    const portugal = createRegionalFormatters({ locale: 'pt-PT', timezone: 'Europe/Lisbon' })

    expect(english.relativeTime(-1, 'day')).toBe('yesterday')
    expect(angola.relativeTime(-1, 'day')).toBe('ontem')
    expect(english.list(['Alpha', 'Beta', 'Gamma'])).toBe('Alpha, Beta, and Gamma')
    expect(angola.list(['Alpha', 'Beta', 'Gamma'])).toBe('Alpha, Beta e Gamma')
    expect(angola.pluralCategory(0)).toBe('one')
    expect(portugal.pluralCategory(0)).toBe('other')
    expect(angola.pluralCategory(2)).toBe('other')
  })
})
