import { describe, expect, it } from 'vitest'
import { formatDate, formatDateTime, formatMonthlyUSD, formatWholeNumber } from '../../utils/format'

describe('deterministic display formatting', () => {
  it('uses the product locale for SSR-safe currency and counts', () => {
    expect(formatMonthlyUSD(1000)).toBe('$10/month')
    expect(formatMonthlyUSD(null)).toBe('Managed contract')
    expect(formatWholeNumber(2500)).toBe('2,500')
    expect(formatDate('2026-07-21T22:30:00-01:00')).toBe('Jul 21, 2026')
    expect(formatDateTime('2026-07-21T22:30:00-01:00')).toBe('Jul 21, 2026, 11:30 PM')
  })
})
