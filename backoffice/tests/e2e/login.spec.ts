import { expect, test } from '@playwright/test'

test('exposes a production health check without rendering the application shell', async ({ request }) => {
  const response = await request.get('/backoffice-healthz')

  expect(response.status()).toBe(200)
  expect(response.headers()['content-type']).toContain('application/json')
  await expect(response.json()).resolves.toEqual({ status: 'ok' })
})

test('renders the passwordless platform-admin entry point', async ({ page }) => {
  const response = await page.goto('/login')

  await expect(page).toHaveTitle('Fidibeki Control Room')
  await expect(page.getByRole('heading', { name: 'Keep the signal clear.' })).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Work email' })).toHaveAttribute('type', 'email')
  await expect(page.getByRole('button', { name: 'Email secure link' })).toBeVisible()
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute('content', 'noindex, nofollow')
  expect(response?.headers()['referrer-policy']).toBe('no-referrer')
  expect(response?.headers()['content-security-policy']).toContain("default-src 'self'")
})

test('does not expose a protected console route without a browser refresh session', async ({ page }) => {
  const response = await page.goto('/')

  expect(await response?.text()).not.toContain('The platform, at a glance.')
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('heading', { name: 'Keep the signal clear.' })).toBeVisible()
})
