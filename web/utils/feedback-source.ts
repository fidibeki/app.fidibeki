import type { FeedbackItem } from '~/types/api'
import { specificPageLocation } from '~/utils/privacy-location'

export type FeedbackSource = NonNullable<FeedbackItem['source']>

export function feedbackSource(item?: Pick<FeedbackItem, 'source'> | null): FeedbackSource {
  return item?.source === 'ios' || item?.source === 'android' || item?.source === 'email'
    ? item.source
    : 'web'
}

export function isMobileFeedbackSource(source: FeedbackSource): boolean {
  return source === 'ios' || source === 'android'
}

export function feedbackContextLabel(
  item?: Pick<FeedbackItem, 'source' | 'page_url' | 'mobile_screen' | 'mobile_application_id'> | null,
): string {
  if (!item) return ''
  if (isMobileFeedbackSource(feedbackSource(item))) {
    return item.mobile_screen || item.mobile_application_id || ''
  }
  return item.page_url
}

export function feedbackContextURL(
  item?: Pick<FeedbackItem, 'source' | 'page_url'> | null,
): string | null {
  if (!item?.page_url) return null
  const source = feedbackSource(item)
  if (source === 'web') return specificPageLocation(item.page_url)

  try {
    const parsed = new URL(item.page_url)
    if (
      parsed.protocol !== 'https:'
      || !parsed.hostname
      || parsed.username
      || parsed.password
    ) return null
    if (source === 'email' && (parsed.search || parsed.hash)) return null
    return item.page_url
  } catch {
    return null
  }
}
