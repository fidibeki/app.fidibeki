// The payload shape the public wall endpoint serves. It mirrors
// backend/internal/testimonial.SnapshotPayload; the schema version exists so a
// stale embed on a customer's site refuses a payload it does not understand
// rather than rendering a half-broken wall.
export const SUPPORTED_SCHEMA_VERSION = 1

export interface TestimonialAuthor {
  name?: string
  role?: string
  company?: string
  company_url?: string
  avatar_url?: string
  video_url?: string
  poster_url?: string
  /** The served rendition's own shape, so the card holds the right box before
   *  the video loads. Absent on payloads written before video existed. */
  video_width?: number
  video_height?: number
}

export interface TestimonialCard {
  id: string
  quote: string
  headline?: string
  rating?: number | null
  author: TestimonialAuthor
  tags?: string[]
}

export interface WallTheme {
  accent?: string
  radius?: string
  font_family?: string
  mode?: 'auto' | 'light' | 'dark'
}

// 'custom' means the customer renders the wall from this payload themselves.
// The embed still honours it — it falls back to the grid — because a wall that
// went custom while a script tag was still on the page should degrade to a wall,
// not to a blank space.
export type WallLayout =
  | 'grid'
  | 'masonry'
  | 'carousel'
  | 'marquee'
  | 'list'
  | 'single'
  | 'custom'

export interface WallPayload {
  schema_version: number
  wall: string
  name: string
  layout: WallLayout
  theme: WallTheme
  show_rating: boolean
  show_avatar: boolean
  show_company: boolean
  show_video: boolean
  cards: TestimonialCard[]
  generated_at: string
}

export function isWallPayload(value: unknown): value is WallPayload {
  if (!value || typeof value !== 'object') return false
  const payload = value as Partial<WallPayload>
  return (
    typeof payload.schema_version === 'number' &&
    typeof payload.wall === 'string' &&
    Array.isArray(payload.cards)
  )
}
