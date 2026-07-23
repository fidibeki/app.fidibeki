import { describe, expect, it } from 'vitest'
import { createRegionalFormatters } from '../../utils/regional-format'
import { buildWidgetAnalyticsQuery, formatAnalyticsRate } from '../../utils/widget-analytics'

describe('widget analytics presentation', () => {
  it('distinguishes unavailable rates and delegates locale-aware percent formatting', () => {
    const english = createRegionalFormatters({ locale: 'en', timezone: 'UTC' })
    const angola = createRegionalFormatters({ locale: 'pt-AO', timezone: 'Africa/Luanda' })

    expect(formatAnalyticsRate(null, english.percent)).toBe('—')
    expect(formatAnalyticsRate(0, english.percent)).toBe('0%')
    expect(formatAnalyticsRate(0.124, english.percent)).toBe('12.4%')
    expect(formatAnalyticsRate(0.124, angola.percent)).toBe('12,4%')
  })

  it('serializes only active filters and preserves canonical page URLs', () => {
    expect(buildWidgetAnalyticsQuery({
      range: '7d',
      projectId: 'project-1',
      ruleId: '',
      page: 'https://shop.example.com/chart',
    })).toBe('range=7d&project_id=project-1&page=https%3A%2F%2Fshop.example.com%2Fchart')
  })
})
