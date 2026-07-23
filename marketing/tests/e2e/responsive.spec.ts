import { expect, test } from '@playwright/test'

const representativeRoutes = [
  '/',
  '/features',
  '/features/feedback-widget',
  '/integrations',
  '/integrations/webhooks',
  '/pricing',
  '/docs',
  '/docs/installation',
  '/use-cases',
  '/use-cases/bug-reports',
  '/privacy',
  '/security',
  '/sign-up',
]

// Runs on all three projects (desktop, tablet, mobile viewports).
for (const route of representativeRoutes) {
  test(`no horizontal overflow on ${route}`, async ({ page }) => {
    await page.goto(route)
    await expect(page.locator('h1')).toBeVisible()
    const overflow = await page.evaluate(() => {
      const width = document.documentElement.clientWidth
      const scrollWidth = document.scrollingElement?.scrollWidth ?? 0
      return { width, scrollWidth }
    })
    expect(
      overflow.scrollWidth,
      `scrollWidth ${overflow.scrollWidth} vs viewport ${overflow.width}`,
    ).toBeLessThanOrEqual(overflow.width + 1)
  })
}
