import { expect, test } from '@playwright/test'

test('staging API and tenant login surface are live', async ({ page, request }) => {
  const apiURL = process.env.STAGING_API_URL
  const tenantURL = process.env.STAGING_TENANT_URL
  test.skip(!apiURL || !tenantURL, 'Staging endpoints are only available after deployment')

  const readiness = await request.get(new URL('/readyz', apiURL).href)
  expect(readiness.ok()).toBeTruthy()
  await expect(readiness.json()).resolves.toMatchObject({ status: 'ok' })

  await page.goto(new URL('/login', tenantURL).href)
  await expect(page.getByRole('heading', { name: 'Sign in to Fidibeki' })).toBeVisible()
  await expect(page.getByLabel('Work email')).toBeVisible()
})
