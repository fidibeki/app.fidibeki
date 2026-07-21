import type { ErrorEvent } from '@sentry/browser'
import { scrubEvent } from '../../src/modules/error-reporting'

describe('Sentry event scrubbing', () => {
  it('removes user, request payload, auth headers, and sensitive extra fields', () => {
    const event: ErrorEvent = {
      type: undefined,
      user: { email: 'person@example.com', id: 'user-id' },
      request: {
        url: 'https://example.com/checkout?token=secret',
        data: { text: 'private feedback' },
        headers: { Authorization: 'Bearer secret' },
        cookies: { session: 'secret' },
      },
      extra: { project_id: 'project', email: 'person@example.com', nested: { token: 'secret' } },
    }
    const scrubbed = scrubEvent(event)
    expect(scrubbed.user).toBeUndefined()
    expect(scrubbed.request).toEqual({ url: 'https://example.com/checkout' })
    expect(scrubbed.extra).toEqual({ project_id: 'project', email: '[Filtered]', nested: { token: '[Filtered]' } })
  })
})
