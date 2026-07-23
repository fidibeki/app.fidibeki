export interface WidgetAnalyticsFilters {
  range: '7d' | '30d' | '90d'
  projectId: string
  ruleId: string
  page: string
}

export function formatAnalyticsRate(
  value: number | null,
  formatPercent: (value: number) => string,
): string {
  return value == null ? '—' : formatPercent(value)
}

export function buildWidgetAnalyticsQuery(filters: WidgetAnalyticsFilters): string {
  const query = new URLSearchParams({ range: filters.range })
  if (filters.projectId) query.set('project_id', filters.projectId)
  if (filters.ruleId) query.set('rule_id', filters.ruleId)
  if (filters.page) query.set('page', filters.page)
  return query.toString()
}
