export interface WidgetAnalyticsFilters {
  range: '7d' | '30d' | '90d'
  projectId: string
  ruleId: string
  page: string
}

const percentageFormatter = new Intl.NumberFormat('en-US', {
  style: 'percent',
  maximumFractionDigits: 1,
})

export function formatAnalyticsRate(value: number | null): string {
  return value == null ? '—' : percentageFormatter.format(value)
}

export function buildWidgetAnalyticsQuery(filters: WidgetAnalyticsFilters): string {
  const query = new URLSearchParams({ range: filters.range })
  if (filters.projectId) query.set('project_id', filters.projectId)
  if (filters.ruleId) query.set('rule_id', filters.ruleId)
  if (filters.page) query.set('page', filters.page)
  return query.toString()
}
