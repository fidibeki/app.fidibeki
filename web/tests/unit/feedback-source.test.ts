import { describe, expect, it } from 'vitest'
import {
  feedbackContextLabel,
  feedbackContextURL,
  feedbackSource,
} from '../../utils/feedback-source'

describe('feedback source context', () => {
  it('keeps payloads without an explicit source compatible with web feedback', () => {
    const feedback = {
      source: undefined,
      page_url: 'https://shop.example.test/checkout',
    }

    expect(feedbackSource(feedback)).toBe('web')
    expect(feedbackContextLabel(feedback)).toBe(feedback.page_url)
    expect(feedbackContextURL(feedback)).toBe(feedback.page_url)
  })

  it('uses the mobile screen as context without inventing a page URL', () => {
    const feedback = {
      source: 'ios' as const,
      page_url: '',
      mobile_application_id: 'com.example.shop',
      mobile_screen: '/checkout/payment',
    }

    expect(feedbackContextLabel(feedback)).toBe('/checkout/payment')
    expect(feedbackContextURL(feedback)).toBeNull()
  })

  it('only links credential-free HTTPS mobile context URLs', () => {
    const mobile = { source: 'android' as const }

    expect(feedbackContextURL({
      ...mobile,
      page_url: 'https://shop.example.test/orders/123',
    })).toBe('https://shop.example.test/orders/123')
    expect(feedbackContextURL({
      ...mobile,
      page_url: 'http://shop.example.test/orders/123',
    })).toBeNull()
    expect(feedbackContextURL({
      ...mobile,
      page_url: 'https://user:secret@shop.example.test/orders/123',
    })).toBeNull()
    expect(feedbackContextURL({
      ...mobile,
      page_url: 'javascript:alert(1)',
    })).toBeNull()
  })

  it('preserves email as a distinct source and uses its captured page context', () => {
    const feedback = {
      source: 'email' as const,
      page_url: 'https://shop.example.test/orders/123',
    }

    expect(feedbackSource(feedback)).toBe('email')
    expect(feedbackContextLabel(feedback)).toBe(feedback.page_url)
    expect(feedbackContextURL(feedback)).toBe(feedback.page_url)
  })
})
