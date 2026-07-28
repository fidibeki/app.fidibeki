import { expect, test } from '@playwright/test'

const hstsPolicy = 'max-age=31536000'

test('adds HSTS to application, health, and proxied API responses', async ({ request }) => {
  for (const path of ['/login', '/web-healthz', '/v1/config']) {
    const response = await request.get(path)

    expect(response.headers()['strict-transport-security'], path).toBe(hstsPolicy)
  }
})
