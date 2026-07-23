import type { SubmissionHealthCategory, SubmissionHealthPoint } from '~/types/api'

export interface SubmissionHealthCategoryDefinition {
  id: SubmissionHealthCategory
  label: string
  description: string
  tone: 'red' | 'amber' | 'blue' | 'violet' | 'teal' | 'rose'
}

export interface SubmissionHealthTrendBucket {
  startAt: string
  label: string
  count: number
  heightPercent: number
}

export const submissionHealthCategories: readonly SubmissionHealthCategoryDefinition[] = [
  {
    id: 'rate_limit',
    label: 'Rate limits',
    description: 'Submission, upload, or replay requests blocked by coordinated limits.',
    tone: 'red',
  },
  {
    id: 'spam',
    label: 'Spam signals',
    description: 'Neutral rejections triggered by automated-input checks.',
    tone: 'rose',
  },
  {
    id: 'plan_quota',
    label: 'Plan quota',
    description: 'Feedback intake blocked at the workspace plan boundary.',
    tone: 'amber',
  },
  {
    id: 'upload_safety',
    label: 'Upload safety',
    description: 'Attachment or replay objects rejected or expired during verification.',
    tone: 'violet',
  },
  {
    id: 'ai_budget',
    label: 'AI budget',
    description: 'Accepted feedback deferred by tenant or project analysis budgets.',
    tone: 'teal',
  },
  {
    id: 'provider',
    label: 'Provider pressure',
    description: 'AI provider authentication, timeout, rate, or availability failures.',
    tone: 'blue',
  },
] as const

export function buildSubmissionHealthTrend(
  points: readonly SubmissionHealthPoint[],
  startAt: string,
  endAt: string,
  bucketCount = 12,
): SubmissionHealthTrendBucket[] {
  const start = Date.parse(startAt)
  const end = Date.parse(endAt)
  if (!Number.isFinite(start) || !Number.isFinite(end) || start >= end || bucketCount < 1) return []
  const width = (end - start) / bucketCount
  const counts = Array.from({ length: bucketCount }, () => 0)
  for (const point of points) {
    const occurredAt = Date.parse(point.bucket_start)
    if (!Number.isFinite(occurredAt) || occurredAt < start || occurredAt >= end) continue
    const index = Math.min(bucketCount - 1, Math.floor((occurredAt - start) / width))
    counts[index] = (counts[index] ?? 0) + Math.max(0, point.count)
  }
  const maximum = Math.max(0, ...counts)
  const showTime = end - start <= 48 * 60 * 60 * 1000
  return counts.map((count, index) => {
    const bucketStart = new Date(start + width * index)
    return {
      startAt: bucketStart.toISOString(),
      label: showTime
        ? `${bucketStart.toISOString().slice(11, 16)} UTC`
        : bucketStart.toISOString().slice(0, 10),
      count,
      heightPercent: maximum === 0 ? 0 : Math.round((count / maximum) * 100),
    }
  })
}

export function submissionHealthReasonLabel(reason: string): string {
  const normalized = reason.trim().replaceAll('_', ' ')
  if (!normalized) return 'Unclassified'
  const words = normalized.split(/\s+/).map((word, index) => {
    if (word.toLowerCase() === 'ai') return 'AI'
    return index === 0 ? word.charAt(0).toUpperCase() + word.slice(1) : word.toLowerCase()
  })
  return words.join(' ')
}
