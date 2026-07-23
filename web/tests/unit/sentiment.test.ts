import { describe, expect, it } from 'vitest'
import { classifySentiment, formatSentimentScore } from '../../utils/sentiment'

describe('feedback sentiment presentation', () => {
  it.each([
    [undefined, 'unscored'],
    [null, 'unscored'],
    [-0.21, 'negative'],
    [-0.2, 'neutral'],
    [0, 'neutral'],
    [0.2, 'neutral'],
    [0.21, 'positive'],
  ] as const)('classifies %s as %s', (score, expected) => {
    expect(classifySentiment(score)).toBe(expected)
  })

  it('formats the exact score without confusing a zero with missing data', () => {
    expect(formatSentimentScore(-0.8)).toBe('-0.80')
    expect(formatSentimentScore(0)).toBe('0.00')
    expect(formatSentimentScore(null)).toBe('Not scored')
  })
})
