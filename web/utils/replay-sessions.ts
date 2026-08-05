export interface ReplaySessionListFilters {
  projectId?: string
  status?: string
  feedback?: string
  cursor?: string
}

// One query builder shared by the first page and every "load more" request so
// a cursor can never be paired with different filters than the page it came from.
export function replaySessionListQuery(filters: ReplaySessionListFilters): string {
  const params = new URLSearchParams()
  if (filters.projectId) params.set('project_id', filters.projectId)
  if (filters.status) params.set('status', filters.status)
  if (filters.feedback) params.set('feedback', filters.feedback)
  params.set('limit', '50')
  if (filters.cursor) params.set('cursor', filters.cursor)
  return params.toString()
}

export function replaySessionDurationSeconds(startedAt: string, endedAt: string | null): number | null {
  if (!endedAt) return null
  const elapsed = (new Date(endedAt).getTime() - new Date(startedAt).getTime()) / 1000
  return Math.max(0, Math.round(elapsed))
}

const statusClasses: Record<string, string> = {
  ready: 'status-completed',
  recording: 'status-processing',
  processing: 'status-processing',
  failed: 'status-failed',
  expired: 'status-expired',
}

export function replaySessionStatusClass(status: string): string {
  return statusClasses[status] || 'status-processing'
}
