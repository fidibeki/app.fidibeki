import { describe, expect, it } from 'vitest'
import type { NotificationCategory, NotificationPreferences } from '../../types/api'
import {
  preferencePatch,
  usesDigest,
  usesWeeklyDigest,
} from '../../utils/notification-preferences'

const categories: NotificationCategory[] = [
  'assignments',
  'mentions',
  'comments',
  'issue_changes',
  'feedback_summary',
]

function preferences(overrides: Partial<NotificationPreferences> = {}): NotificationPreferences {
  return {
    assignments: 'immediate',
    mentions: 'immediate',
    comments: 'off',
    issue_changes: 'daily',
    feedback_summary: 'weekly',
    digest_hour: 8,
    digest_weekday: 1,
    timezone: 'UTC',
    timezone_source: 'default',
    ...overrides,
  }
}

describe('notification preference controls', () => {
  it('enables the digest window only when a digest is actually used', () => {
    expect(usesDigest(preferences(), categories)).toBe(true)
    expect(usesWeeklyDigest(preferences(), categories)).toBe(true)

    const immediateOnly = preferences({ issue_changes: 'immediate', feedback_summary: 'off' })
    expect(usesDigest(immediateOnly, categories)).toBe(false)
    expect(usesWeeklyDigest(immediateOnly, categories)).toBe(false)

    const dailyOnly = preferences({ issue_changes: 'daily', feedback_summary: 'daily' })
    expect(usesDigest(dailyOnly, categories)).toBe(true)
    expect(usesWeeklyDigest(dailyOnly, categories)).toBe(false)
  })

  it('sends only the categories the server offered', () => {
    const offered: NotificationCategory[] = ['assignments', 'mentions']
    const patch = preferencePatch(preferences(), offered, 'Africa/Luanda', false)
    expect(patch).toEqual({
      assignments: 'immediate',
      mentions: 'immediate',
      digest_hour: 8,
      digest_weekday: 1,
      timezone: 'Africa/Luanda',
    })
    expect(patch).not.toHaveProperty('comments')
  })

  it('clears the member timezone with an explicit null when inheriting', () => {
    expect(preferencePatch(preferences(), [], 'Europe/Lisbon', true).timezone).toBeNull()
    expect(preferencePatch(preferences(), [], '   ', false).timezone).toBeNull()
    expect(preferencePatch(preferences(), [], ' Europe/Lisbon ', false).timezone)
      .toBe('Europe/Lisbon')
  })

  it('never sends a field for a mandatory message class', () => {
    const patch = preferencePatch(preferences(), categories, 'UTC', false)
    for (const mandatory of ['billing', 'security', 'authentication', 'account_deletion']) {
      expect(patch).not.toHaveProperty(mandatory)
    }
  })
})
