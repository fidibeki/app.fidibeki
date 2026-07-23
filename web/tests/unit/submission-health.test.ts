import { describe, expect, it } from 'vitest'
import {
  buildSubmissionHealthTrend,
  submissionHealthCategories,
  submissionHealthReasonLabel,
} from '~/utils/submission-health'
import type { SubmissionHealthPoint } from '~/types/api'

describe('submission health presentation', () => {
  it('keeps all operational categories in their stable display order', () => {
    expect(submissionHealthCategories.map(category => category.id)).toEqual([
      'rate_limit',
      'spam',
      'plan_quota',
      'upload_safety',
      'ai_budget',
      'provider',
    ])
  })

  it('aggregates sparse hourly points into bounded chronological buckets', () => {
    const points: SubmissionHealthPoint[] = [
      {
        project_id: 'project-1',
        bucket_start: '2026-07-23T00:10:00.000Z',
        category: 'rate_limit',
        reason: 'submission_rate_limited',
        count: 3,
      },
      {
        project_id: 'project-1',
        bucket_start: '2026-07-23T01:59:00.000Z',
        category: 'spam',
        reason: 'spam_rejected',
        count: 1,
      },
      {
        project_id: 'project-1',
        bucket_start: '2026-07-23T04:00:00.000Z',
        category: 'provider',
        reason: 'provider_timeout',
        count: 2,
      },
      {
        project_id: 'project-1',
        bucket_start: '2026-07-23T08:00:00.000Z',
        category: 'provider',
        reason: 'outside_period',
        count: 99,
      },
    ]

    const buckets = buildSubmissionHealthTrend(
      points,
      '2026-07-23T00:00:00.000Z',
      '2026-07-23T08:00:00.000Z',
      4,
    )

    expect(buckets.map(bucket => bucket.count)).toEqual([4, 0, 2, 0])
    expect(buckets.map(bucket => bucket.heightPercent)).toEqual([100, 0, 50, 0])
    expect(buckets[0]?.label).toContain('00:00')
  })

  it('returns no buckets for invalid periods and labels stable reasons safely', () => {
    expect(buildSubmissionHealthTrend([], 'broken', '2026-07-23T08:00:00.000Z')).toEqual([])
    expect(submissionHealthReasonLabel('ai_daily_budget_exhausted')).toBe('AI daily budget exhausted')
    expect(submissionHealthReasonLabel('provider_timeout')).toBe('Provider timeout')
  })
})
