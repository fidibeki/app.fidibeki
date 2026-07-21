import { conceptSlugs, viewSlugs } from '~/types/design'
import type { ConceptSlug, FeedbackFilters, FeedbackItem, ViewSlug } from '~/types/design'

export const defaultFilters = (): FeedbackFilters => ({
  project: 'all',
  status: 'all',
  rating: 'all',
  attachment: 'all',
  moderation: 'all',
  issue: 'all',
  date: '30d',
  sentiment: 'all',
  sort: 'newest',
  query: '',
})

export function isConceptSlug(value: unknown): value is ConceptSlug {
  return typeof value === 'string' && conceptSlugs.includes(value as ConceptSlug)
}

export function isViewSlug(value: unknown): value is ViewSlug {
  return typeof value === 'string' && viewSlugs.includes(value as ViewSlug)
}

export function filterFeedback(items: FeedbackItem[], filters: FeedbackFilters): FeedbackItem[] {
  const query = filters.query.trim().toLowerCase()
  const filtered = items.filter((item) => {
    if (filters.project !== 'all' && item.projectSlug !== filters.project) return false
    if (filters.status !== 'all' && item.status !== filters.status) return false
    if (filters.rating !== 'all' && item.rating !== Number(filters.rating)) return false
    if (filters.attachment !== 'all' && !item.attachments.includes(filters.attachment as never)) return false
    if (filters.moderation === 'flagged' && item.moderationFlags.length === 0) return false
    if (filters.moderation === 'clear' && item.moderationFlags.length > 0) return false
    if (filters.issue === 'linked' && item.issue === null) return false
    if (filters.issue === 'unlinked' && item.issue !== null) return false
    if (filters.sentiment !== 'all' && item.sentiment !== filters.sentiment) return false
    if (query && !`${item.id} ${item.excerpt} ${item.project} ${item.topic}`.toLowerCase().includes(query)) return false
    return true
  })

  return [...filtered].sort((left, right) => {
    if (filters.sort === 'oldest') return left.receivedAt.localeCompare(right.receivedAt)
    if (filters.sort === 'rating-low') return left.rating - right.rating
    if (filters.sort === 'rating-high') return right.rating - left.rating
    return right.receivedAt.localeCompare(left.receivedAt)
  })
}

export function conceptPath(concept: ConceptSlug, view: ViewSlug): string {
  return `/designs/${concept}/${view}`
}
