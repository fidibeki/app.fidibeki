import type { Route } from '@playwright/test'

// Unrelated page journeys use an empty staff-initiated conversation. The
// dedicated live-support suite exercises real delivery and replies without mocks.
export async function mockSupportRequest(route: Route): Promise<boolean> {
  const path = new URL(route.request().url()).pathname
  if (path === '/v1/support/conversation') {
    await route.fulfill({ json: { conversation: null, messages: [], has_more: false } })
  } else if (path === '/v1/support/events') {
    await route.fulfill({ contentType: 'text/event-stream', body: ': connected\n\n' })
  } else if (path === '/v1/support/presence' || path.startsWith('/v1/support/presence/')) {
    await route.fulfill({ json: {} })
  } else {
    return false
  }
  return true
}
