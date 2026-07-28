import type { FeedbackItem } from '~/types/api'
import { specificPageLocation } from '~/utils/privacy-location'

export type FeedbackSource = NonNullable<FeedbackItem['source']>

export function feedbackSource(item?: Pick<FeedbackItem, 'source'> | null): FeedbackSource {
  return item?.source === 'ios' || item?.source === 'android' ? item.source : 'web'
}

export function feedbackContextLabel(
  item?: Pick<FeedbackItem, 'source' | 'page_url' | 'mobile_screen' | 'mobile_application_id'> | null,
): string {
  if (!item) return ''
  if (feedbackSource(item) === 'web') return item.page_url
  return item.mobile_screen || item.mobile_application_id || ''
}

export function feedbackContextURL(
  item?: Pick<FeedbackItem, 'source' | 'page_url'> | null,
): string | null {
  if (!item?.page_url) return null
  if (feedbackSource(item) === 'web') return specificPageLocation(item.page_url)

  try {
    const parsed = new URL(item.page_url)
    if (
      parsed.protocol !== 'https:'
      || !parsed.hostname
      || parsed.username
      || parsed.password
    ) return null
    return item.page_url
  } catch {
    return null
  }
}
