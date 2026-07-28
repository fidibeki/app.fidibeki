import { expect, test } from '@playwright/test'

const hstsPolicy = 'max-age=31536000'

test('staging public hosts enforce HTTPS and the product surfaces are live', async ({ page, request }) => {
  const apiURL = process.env.STAGING_API_URL
  const appURL = process.env.STAGING_APP_URL
  const tenantURL = process.env.STAGING_TENANT_URL
  const widgetURL = process.env.STAGING_WIDGET_URL
  test.skip(
    !apiURL || !appURL || !tenantURL || !widgetURL,
    'Staging endpoints are only available after deployment',
  )
  test.setTimeout(4 * 60_000)

  const endpoints = [
    { name: 'API', url: new URL('/readyz', apiURL).href },
    { name: 'application', url: new URL('/web-healthz', appURL).href },
    { name: 'tenant application', url: new URL('/web-healthz', tenantURL).href },
    { name: 'widget', url: new URL('/healthz', widgetURL).href },
  ]

  await expect.poll(async () => {
    const failures: string[] = []
    for (const endpoint of endpoints) {
      const secureResponse = await request.get(endpoint.url, {
        failOnStatusCode: false,
        maxRedirects: 0,
      })
      if (!secureResponse.ok()) {
        failures.push(`${endpoint.name} HTTPS status ${secureResponse.status()}`)
      }
      const hsts = secureResponse.headers()['strict-transport-security']
      if (hsts !== hstsPolicy) {
        failures.push(`${endpoint.name} HSTS ${JSON.stringify(hsts || '')}`)
      }

      const httpURL = new URL(endpoint.url)
      httpURL.protocol = 'http:'
      const redirect = await request.get(httpURL.href, {
        failOnStatusCode: false,
        maxRedirects: 0,
      })
      if (![301, 302, 307, 308].includes(redirect.status())) {
        failures.push(`${endpoint.name} HTTP status ${redirect.status()}`)
        continue
      }
      const location = redirect.headers().location
      if (!location || new URL(location, httpURL).href !== endpoint.url) {
        failures.push(`${endpoint.name} redirect ${JSON.stringify(location || '')}`)
      }
    }
    return failures
  }, {
    message: 'public hosts should converge on the transport-security policy during rollout',
    timeout: 3 * 60_000,
    intervals: [5_000],
  }).toEqual([])

  const readiness = await request.get(new URL('/readyz', apiURL).href)
  expect(readiness.ok()).toBeTruthy()
  await expect(readiness.json()).resolves.toMatchObject({ status: 'ok' })

  const webHealth = await request.get(new URL('/web-healthz', tenantURL).href)
  expect(webHealth.ok()).toBeTruthy()
  await expect(webHealth.json()).resolves.toMatchObject({ status: 'ok' })

  await page.goto(new URL('/login', tenantURL).href)
  await expect(page.getByRole('heading', { name: 'Sign in to Fidibeki' })).toBeVisible()
  await expect(page.getByLabel('Work email')).toBeVisible()
})
