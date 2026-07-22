import { describe, expect, it } from 'vitest'
import { feedbackQueueFromQuery } from '../../utils/feedback-queue'

describe('feedback queue selection', () => {
  it.each([
    [{}, 'open'],
    [{ status: 'open' }, 'open'],
    [{ status: 'open', id: 'feedback-1' }, 'open'],
    [{ status: 'open', attachment: 'replay', id: 'feedback-1' }, 'replay'],
    [{ moderation: 'flagged' }, 'moderation'],
    [{ processing: 'failed' }, 'ai_failed'],
    [{ issue: 'unlinked' }, 'unlinked'],
    [{ status: 'archived' }, 'archived'],
  ])('maps %j to %s', (query, expected) => {
    expect(feedbackQueueFromQuery(query)).toBe(expected)
  })

  it('does not claim one queue for conflicting queue filters', () => {
    expect(feedbackQueueFromQuery({ attachment: 'replay', moderation: 'flagged' })).toBeNull()
    expect(feedbackQueueFromQuery({ status: 'archived', issue: 'unlinked' })).toBeNull()
  })

  it('does not treat arbitrary statuses as the open queue', () => {
    expect(feedbackQueueFromQuery({ status: 'spam' })).toBeNull()
  })
})
