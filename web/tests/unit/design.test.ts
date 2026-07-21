import { describe, expect, it } from 'vitest'
import { feedbackItems } from '../../data/design-fixtures'
import { conceptPath, defaultFilters, filterFeedback, isConceptSlug, isViewSlug } from '../../utils/design'

describe('design study metadata', () => {
  it('recognizes only supported concepts and views', () => {
    expect(isConceptSlug('clearspace')).toBe(true)
    expect(isConceptSlug('signal-observatory')).toBe(true)
    expect(isConceptSlug('terminal-dashboard')).toBe(false)
    expect(isViewSlug('feedback-detail')).toBe(true)
    expect(isViewSlug('settings')).toBe(false)
  })

  it('creates canonical design routes', () => {
    expect(conceptPath('dispatch', 'feedback')).toBe('/designs/dispatch/feedback')
  })
})

describe('feedback filtering', () => {
  it('returns independent default filter objects', () => {
    const first = defaultFilters()
    const second = defaultFilters()
    first.project = 'storefront'
    expect(second.project).toBe('all')
  })

  it('filters across project, state, evidence, and issue relationship', () => {
    const filters = defaultFilters()
    filters.project = 'storefront'
    filters.status = 'new'
    filters.attachment = 'replay'
    filters.issue = 'unlinked'

    expect(filterFeedback(feedbackItems, filters).map((item) => item.id)).toEqual(['FDB-1048'])
  })

  it('filters moderation, sentiment, rating, and search text', () => {
    const filters = defaultFilters()
    filters.moderation = 'flagged'
    filters.sentiment = 'negative'
    filters.rating = '1'
    filters.query = 'promotions'

    expect(filterFeedback(feedbackItems, filters).map((item) => item.id)).toEqual(['FDB-1045'])
  })

  it('sorts results without mutating fixture order', () => {
    const original = feedbackItems.map((item) => item.id)
    const filters = defaultFilters()
    filters.sort = 'rating-low'

    const results = filterFeedback(feedbackItems, filters)
    expect(results[0]?.rating).toBe(1)
    expect(results.at(-1)?.rating).toBe(5)
    expect(feedbackItems.map((item) => item.id)).toEqual(original)
  })
})
