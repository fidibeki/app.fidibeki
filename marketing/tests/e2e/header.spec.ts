import { expect, test } from '@playwright/test'

// The header starts full-bleed and transparent, then floats as a rounded,
// blurred pill once the visitor scrolls. Runs on the desktop project only.
test.skip(({ viewport }) => !viewport || viewport.width < 900, 'desktop only')

test('header floats into a pill on scroll and returns at top', async ({ page }) => {
  await page.goto('/')
  const header = page.locator('header.site-header')
  const shell = page.locator('.header-shell')
  // The frosted fill lives on .header-shell::before so its blur radius can stay
  // constant and fade in with opacity, so read the pill's visibility from there.
  const fill = () =>
    shell.evaluate((element) => Number(getComputedStyle(element, '::before').opacity))

  await expect(header).not.toHaveClass(/scrolled/)
  expect(await fill()).toBe(0)

  await page.evaluate(() => window.scrollTo({ top: 600, behavior: 'instant' }))
  await expect(header).toHaveClass(/scrolled/)
  await expect.poll(fill).toBe(1)
  await expect
    .poll(async () => (await shell.boundingBox())!.width)
    .toBeLessThanOrEqual(1200)

  await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }))
  await expect(header).not.toHaveClass(/scrolled/)
  await expect.poll(fill).toBe(0)
})

// The blur radius must never animate — that per-frame re-blur is what made the
// collapse stutter. Guards both ends: no filter at rest, one fixed radius when lit.
test('header blur radius is constant, not animated', async ({ page }) => {
  await page.goto('/')
  const shell = page.locator('.header-shell')

  expect(await shell.evaluate((element) => getComputedStyle(element).backdropFilter)).toBe('none')
  const radius = () =>
    shell.evaluate((element) => getComputedStyle(element, '::before').backdropFilter)
  expect(await radius()).toBe('blur(16px)')

  await page.evaluate(() => window.scrollTo({ top: 600, behavior: 'instant' }))
  await expect(page.locator('header.site-header')).toHaveClass(/scrolled/)
  expect(await radius()).toBe('blur(16px)')
  expect(
    await shell.evaluate((element) => getComputedStyle(element).transitionProperty),
  ).not.toContain('backdrop-filter')
})

test('header stays usable while scrolled', async ({ page }) => {
  await page.goto('/')
  await page.evaluate(() => window.scrollTo({ top: 1200, behavior: 'instant' }))
  const nav = page.getByRole('navigation', { name: 'Primary' })
  await expect(nav.getByRole('link', { name: 'Pricing' })).toBeVisible()
  await nav.getByRole('link', { name: 'Pricing' }).click()
  await expect(page).toHaveURL(/\/pricing$/)
})
