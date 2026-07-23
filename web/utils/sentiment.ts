export type SentimentClassification = 'negative' | 'neutral' | 'positive' | 'unscored'

export function classifySentiment(score: number | null | undefined): SentimentClassification {
  if (score == null || !Number.isFinite(score)) return 'unscored'
  if (score < -0.2) return 'negative'
  if (score > 0.2) return 'positive'
  return 'neutral'
}

export function formatSentimentScore(score: number | null | undefined): string {
  return classifySentiment(score) === 'unscored' ? 'Not scored' : score!.toFixed(2)
}
