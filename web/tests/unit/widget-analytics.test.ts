import { describe, expect, it } from 'vitest'
import { buildWidgetAnalyticsQuery, formatAnalyticsRate } from '../../utils/widget-analytics'

describe('widget analytics presentation', () => {
  it('distinguishes unavailable rates from real zero rates', () => {
    expect(formatAnalyticsRate(null)).toBe('—')
    expect(formatAnalyticsRate(0)).toBe('0%')
    expect(formatAnalyticsRate(0.124)).toBe('12.4%')
    expect(formatAnalyticsRate(1)).toBe('100%')
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
