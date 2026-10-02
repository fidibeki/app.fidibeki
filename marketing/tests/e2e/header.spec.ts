import { expect, test } from '@playwright/test'

test.skip(({ viewport }) => !viewport || viewport.width < 900, 'desktop only')

test('shared header stays usable while scrolling between marketing routes', async ({
  page,
}) => {
  await page.goto('/features')
  const header = page.locator('header')
  const nav = page.getByRole('navigation', { name: 'Primary' })
  await page.evaluate(() => window.scrollTo({ top: 1200, behavior: 'instant' }))
  await expect(header).toBeInViewport()
  await nav.getByRole('link', { name: 'Pricing', exact: true }).click()
  await expect(page).toHaveURL(/\/pricing$/)
  await expect(
    nav.getByRole('link', { name: 'Pricing', exact: true }),
  ).toHaveAttribute('aria-current', 'page')
})

test('detail pages keep their parent section selected', async ({ page }) => {
  await page.goto('/docs/installation')
  const docs = page
    .getByRole('navigation', { name: 'Primary' })
    .getByRole('link', { name: 'Docs', exact: true })
  await expect(docs).toHaveAttribute('aria-current', 'location')
  await docs.click()
  await expect(page).toHaveURL(/\/docs$/)
  await expect(docs).toHaveAttribute('aria-current', 'page')
})
