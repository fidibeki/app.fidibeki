export type FeedbackQueueID = 'open' | 'moderation' | 'ai_failed' | 'replay' | 'unlinked' | 'archived'

type QueryValue = string | Array<string | null> | null | undefined

function scalar(value: QueryValue) {
  return typeof value === 'string' ? value : ''
}

export function feedbackQueueFromQuery(query: Record<string, QueryValue>): FeedbackQueueID | null {
  const matches: FeedbackQueueID[] = []
  if (scalar(query.status) === 'archived') matches.push('archived')
  if (scalar(query.moderation) === 'flagged') matches.push('moderation')
  if (scalar(query.processing) === 'failed') matches.push('ai_failed')
  if (scalar(query.attachment) === 'replay') matches.push('replay')
  if (scalar(query.issue) === 'unlinked') matches.push('unlinked')

  if (matches.length === 1) return matches[0]
  if (matches.length > 1) return null

  const status = scalar(query.status)
  return status === '' || status === 'open' ? 'open' : null
}
