import { expect, test } from '@playwright/test'

// The header starts full-bleed and transparent, then floats as a rounded,
// blurred pill once the visitor scrolls. Runs on the desktop project only.
test.skip(({ viewport }) => !viewport || viewport.width < 900, 'desktop only')

const transparent = 'rgba(0, 0, 0, 0)'

test('header floats into a pill on scroll and returns at top', async ({ page }) => {
  await page.goto('/')
  const header = page.locator('header.site-header')
  const shell = page.locator('.header-shell')

  await expect(header).not.toHaveClass(/scrolled/)
  expect(await shell.evaluate((element) => getComputedStyle(element).backgroundColor)).toBe(
    transparent,
  )

  await page.evaluate(() => window.scrollTo({ top: 600, behavior: 'instant' }))
  await expect(header).toHaveClass(/scrolled/)
  await expect
    .poll(async () => shell.evaluate((element) => getComputedStyle(element).backgroundColor))
    .not.toBe(transparent)
  await expect
    .poll(async () => (await shell.boundingBox())!.width)
    .toBeLessThanOrEqual(1200)

  await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }))
  await expect(header).not.toHaveClass(/scrolled/)
  await expect
    .poll(async () => shell.evaluate((element) => getComputedStyle(element).backgroundColor))
    .toBe(transparent)
})

test('header stays usable while scrolled', async ({ page }) => {
  await page.goto('/')
  await page.evaluate(() => window.scrollTo({ top: 1200, behavior: 'instant' }))
  const nav = page.getByRole('navigation', { name: 'Primary' })
  await expect(nav.getByRole('link', { name: 'Pricing' })).toBeVisible()
  await nav.getByRole('link', { name: 'Pricing' }).click()
  await expect(page).toHaveURL(/\/pricing$/)
})
