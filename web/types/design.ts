export const conceptSlugs = [
  'clearspace',
  'dispatch',
  'signal-observatory',
  'project-deck',
  'flowboard',
] as const

export const viewSlugs = ['overview', 'feedback', 'feedback-detail'] as const

export type ConceptSlug = (typeof conceptSlugs)[number]
export type ViewSlug = (typeof viewSlugs)[number]
export type FeedbackStatus = 'new' | 'reviewed' | 'linked' | 'resolved' | 'spam'
export type AttachmentKind = 'screenshot' | 'audio' | 'video' | 'replay'
export type ProcessingStatus = 'ready' | 'processing' | 'failed'
export type Sentiment = 'positive' | 'neutral' | 'negative'

export interface ConceptDefinition {
  slug: ConceptSlug
  number: string
  name: string
  thesis: string
  bestFor: string
}

export interface Project {
  slug: string
  name: string
  domain: string
  status: 'collecting' | 'attention'
  feedbackCount: number
  issueCount: number
  satisfaction: number
  accent: string
}

export interface FeedbackItem {
  id: string
  project: string
  projectSlug: string
  rating: 1 | 2 | 3 | 4 | 5
  status: FeedbackStatus
  excerpt: string
  cleanedText: string
  attachments: AttachmentKind[]
  moderationFlags: string[]
  issue: string | null
  page: string
  browser: string
  device: string
  viewport: string
  reporter: string
  email: string
  receivedAt: string
  age: string
  sentiment: Sentiment
  sentimentScore: number
  topic: string
  aiStatus: ProcessingStatus
  replayReady: boolean
  transcript: string
}

export interface FeedbackFilters {
  project: string
  status: string
  rating: string
  attachment: string
  moderation: string
  issue: string
  date: string
  sentiment: string
  sort: 'newest' | 'oldest' | 'rating-low' | 'rating-high'
  query: string
}
