import type { IssueActivity } from '~/types/api'

export interface ActivityValueCopy {
  key: string | null
  text: string
  items: string[] | null
}

export interface ActivityFieldCopy {
  key: string | null
  text: string
}

export interface ActivityChangeCopy {
  field: ActivityFieldCopy
  before: ActivityValueCopy
  after: ActivityValueCopy
}

const summaryKeys: Record<string, string> = {
  'issue.created': 'created',
  'issue.comment_created': 'commentCreated',
  'issue.status_changed': 'statusChanged',
  'issue.updated': 'updated',
  'feedback.linked': 'feedbackLinked',
  'feedback.unlinked': 'feedbackUnlinked',
  'issue.merged': 'merged',
  'issue.merge_received': 'mergeReceived',
  'issue.unmerged': 'mergeRemoved',
  'issue.merge_removed': 'mergeRemoved',
  'issue.external_issue_created': 'externalIssueCreated',
  'issue.external_issue_failed': 'externalIssueFailed',
  'reporter.access_changed': 'reporterAccessChanged',
  'reporter.responded': 'reporterResponded',
  'reporter.notified': 'reporterNotified',
}

const actorlessKinds = new Set(['reporter.responded', 'reporter.notified'])

const changeFields = new Set(['status', 'title', 'description', 'priority', 'assignee', 'tags'])

const statusValues = new Set<string>([
  'open',
  'in_progress',
  'needs_info',
  'resolved',
  'closed',
])

const priorityValues = new Set<string>(['none', 'low', 'medium', 'high', 'urgent'])

const processingStatusValues = new Set<string>([
  'pending',
  'processing',
  'deferred',
  'completed',
  'failed',
  'skipped',
])

const noValueKey = 'issues.detail.activity.noValue'

function plainRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null
}

/** Catalog key for a persisted issue status, or `null` for unknown server values. */
export function issueStatusKey(status: string): string | null {
  return statusValues.has(status) ? `issues.statuses.${status}` : null
}

/** Catalog key for a persisted triage priority, or `null` for unknown server values. */
export function triagePriorityKey(priority: string | null | undefined): string | null {
  return priority && priorityValues.has(priority) ? `triage.priorities.${priority}` : null
}

/** Catalog key for a feedback AI processing status, or `null` for unknown server values. */
export function processingStatusKey(status: string): string | null {
  return processingStatusValues.has(status)
    ? `issues.detail.linkedFeedback.aiStatuses.${status}`
    : null
}

export function issueActivitySummaryKey(kind: string): string {
  return `issues.detail.activity.summaries.${summaryKeys[kind] || 'unknown'}`
}

export function issueActivityUsesActor(kind: string): boolean {
  return !actorlessKinds.has(kind)
}

/** Maps a change field to catalog copy, keeping unknown server fields readable. */
export function issueActivityChangeField(field: string): ActivityFieldCopy {
  const base = field.replace('_id', '')
  return changeFields.has(base)
    ? { key: `issues.detail.activity.fields.${base}`, text: base }
    : { key: null, text: base.replaceAll('_', ' ') }
}

/**
 * Maps a change value to catalog copy. Only stable enumerations are translated;
 * customer-authored and technical values are returned verbatim.
 */
export function issueActivityChangeValue(field: string, value: unknown): ActivityValueCopy {
  if (value == null || value === '') return { key: noValueKey, text: '', items: null }
  if (Array.isArray(value)) {
    const items = value.map(item => String(item)).filter(Boolean)
    return items.length
      ? { key: null, text: items.join(', '), items }
      : { key: noValueKey, text: '', items: null }
  }
  if (typeof value === 'boolean') {
    return {
      key: value ? 'issues.detail.activity.enabled' : 'issues.detail.activity.disabled',
      text: String(value),
      items: null,
    }
  }
  const base = field.replace('_id', '')
  const text = String(value)
  if (base === 'status' && statusValues.has(text)) {
    return { key: `issues.statuses.${text}`, text, items: null }
  }
  if (base === 'priority' && priorityValues.has(text)) {
    return { key: `triage.priorities.${text}`, text, items: null }
  }
  return { key: null, text, items: null }
}

export function issueActivityChanges(activity: IssueActivity): ActivityChangeCopy[] {
  const changes = plainRecord(activity.data.changes)
  if (!changes) return []
  return Object.entries(changes).map(([field, value]) => {
    const change = plainRecord(value)
    return {
      field: issueActivityChangeField(field),
      before: issueActivityChangeValue(field, change?.from),
      after: issueActivityChangeValue(field, change?.to),
    }
  })
}

export function issueActivityFeedbackCount(activity: IssueActivity): number | null {
  const ids = activity.data.feedback_ids
  return Array.isArray(ids) ? ids.length : null
}

export function issueActivityReporterAccess(activity: IssueActivity): boolean | null {
  const enabled = activity.data.enabled
  return typeof enabled === 'boolean' ? enabled : null
}

export function issueTransitionComment(activity: IssueActivity): string {
  return typeof activity.data.transition_comment === 'string'
    ? activity.data.transition_comment
    : ''
}

/** The reporter's complete clarification response body, or '' for other activity kinds. */
export function issueActivityReporterResponse(activity: IssueActivity): string {
  return activity.kind === 'reporter.responded' && typeof activity.data.response === 'string'
    ? activity.data.response
    : ''
}
