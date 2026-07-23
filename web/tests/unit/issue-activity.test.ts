import { describe, expect, it } from 'vitest'
import type { IssueActivity } from '../../types/api'
import {
  issueActivityChanges,
  issueActivityReporterResponse,
  issueActivitySummaryKey,
  issueActivityUsesActor,
  issueStatusKey,
} from '../../utils/issue-activity'

function activity(overrides: Partial<IssueActivity> = {}): IssueActivity {
  return {
    id: 'activity-1',
    issue_id: 'issue-1',
    kind: 'reporter.responded',
    visibility: 'reporter',
    data: {},
    created_at: '2026-07-20T10:00:00.000Z',
    ...overrides,
  }
}

describe('issueActivityReporterResponse', () => {
  it('returns the complete response text, newlines included, for reporter.responded activities', () => {
    const text = 'iOS 17.5, Safari 17.4.\nStill failing on the same device.'
    expect(issueActivityReporterResponse(activity({ data: { request_id: 'req-1', response: text } })))
      .toBe(text)
  })

  it('returns an empty string when the activity is not a reporter response', () => {
    expect(issueActivityReporterResponse(activity({ kind: 'issue.status_changed', data: { response: 'x' } })))
      .toBe('')
  })

  it('returns an empty string when the response field is missing or not a string', () => {
    expect(issueActivityReporterResponse(activity({ data: {} }))).toBe('')
    expect(issueActivityReporterResponse(activity({ data: { response: 42 } }))).toBe('')
  })

  it('does not interpret the response as a summary or attribute it to a member actor', () => {
    expect(issueActivitySummaryKey('reporter.responded'))
      .toBe('issues.detail.activity.summaries.reporterResponded')
    expect(issueActivityUsesActor('reporter.responded')).toBe(false)
  })

  it('leaves unrelated extractors unaffected by response-bearing data', () => {
    const withResponse = activity({ data: { response: 'context', changes: undefined } })
    expect(issueActivityChanges(withResponse)).toEqual([])
    expect(issueStatusKey('needs_info')).toBe('issues.statuses.needs_info')
  })
})
