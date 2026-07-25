import { expect, test } from '@playwright/test'

const bannerText = 'privacy-friendly analytics'
const umamiScript = 'script[data-website-id]'
const widgetScript = 'script[data-fidibeki-key]'

test('prerendered HTML ships no analytics, no widget, and no banner', async ({ request }) => {
  const html = await (await request.get('/')).text()
  // The runtime config mentions the URLs, but no script tag may load them.
  expect(html).not.toContain('src="https://cloud.umami.is/script.js"')
  expect(html).not.toContain('data-website-id')
  expect(html).not.toContain('data-fidibeki-key')
  expect(html).not.toContain(bannerText)
})

test('banner shows on first visit and accept loads analytics and the widget persistently', async ({
  browser,
}) => {
  const context = await browser.newContext()
  const page = await context.newPage()
  await page.goto('/')

  const banner = page.getByRole('complementary', { name: 'Privacy choices' })
  await expect(banner).toBeVisible()
  await expect(page.locator(umamiScript)).toHaveCount(0)
  await expect(page.locator(widgetScript)).toHaveCount(0)

  await banner.getByRole('button', { name: 'Accept All' }).click()
  await expect(banner).toBeHidden()
  await expect(page.locator(umamiScript)).toHaveCount(1)
  await expect(page.locator(umamiScript)).toHaveAttribute(
    'data-website-id',
    '1d518b00-2258-4884-bf69-63f789d6f11d',
  )
  await expect(page.locator(widgetScript)).toHaveCount(1)
  await expect(page.locator(widgetScript)).toHaveAttribute(
    'data-fidibeki-key',
    'pk_live_GjFasO9215CNAax7LbWhiP',
  )
  // Accepting also records the consent reference this site forwards to the widget as its
  // customer_cmp consentId for widget analytics and session replay.
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('fidibeki-consent-widget-id')))
    .not.toBeNull()

  await page.goto('/pricing')
  await expect(page.getByRole('complementary', { name: 'Privacy choices' })).toBeHidden()
  await expect(page.locator(umamiScript)).toHaveCount(1)
  await expect(page.locator(widgetScript)).toHaveCount(1)
  await context.close()
})

test('more options lets a visitor allow only site analytics', async ({ browser }) => {
  const context = await browser.newContext()
  const page = await context.newPage()
  await page.goto('/')

  const banner = page.getByRole('complementary', { name: 'Privacy choices' })
  await banner.getByRole('button', { name: 'More options' }).click()
  // The widget purposes depend on the widget itself being allowed.
  await expect(banner.getByRole('checkbox', { name: /Widget usage analytics/ })).toBeDisabled()
  await expect(banner.getByRole('checkbox', { name: /Session replay/ })).toBeDisabled()

  await banner.getByRole('checkbox', { name: /Site analytics/ }).check()
  await banner.getByRole('button', { name: 'Save choices' }).click()
  await expect(banner).toBeHidden()
  await expect(page.locator(umamiScript)).toHaveCount(1)
  await expect(page.locator(widgetScript)).toHaveCount(0)

  await page.reload()
  await expect(page.getByRole('complementary', { name: 'Privacy choices' })).toBeHidden()
  await expect(page.locator(umamiScript)).toHaveCount(1)
  await expect(page.locator(widgetScript)).toHaveCount(0)
  await context.close()
})

test('more options lets a visitor allow only the feedback widget', async ({ browser }) => {
  const context = await browser.newContext()
  const page = await context.newPage()
  await page.goto('/')

  const banner = page.getByRole('complementary', { name: 'Privacy choices' })
  await banner.getByRole('button', { name: 'More options' }).click()
  await banner.getByRole('checkbox', { name: /Feedback widget/ }).check()
  await banner.getByRole('checkbox', { name: /Widget usage analytics/ }).check()
  await banner.getByRole('button', { name: 'Save choices' }).click()
  await expect(banner).toBeHidden()
  await expect(page.locator(widgetScript)).toHaveCount(1)
  await expect(page.locator(umamiScript)).toHaveCount(0)

  await page.reload()
  await expect(page.getByRole('complementary', { name: 'Privacy choices' })).toBeHidden()
  await expect(page.locator(widgetScript)).toHaveCount(1)
  await expect(page.locator(umamiScript)).toHaveCount(0)
  await context.close()
})

test('decline is remembered and never loads analytics or the widget', async ({ browser }) => {
  const context = await browser.newContext()
  const page = await context.newPage()
  await page.goto('/')

  const banner = page.getByRole('complementary', { name: 'Privacy choices' })
  await banner.getByRole('button', { name: 'Decline' }).click()
  await expect(banner).toBeHidden()
  await expect(page.locator(umamiScript)).toHaveCount(0)
  await expect(page.locator(widgetScript)).toHaveCount(0)

  await page.reload()
  await expect(page.getByRole('complementary', { name: 'Privacy choices' })).toBeHidden()
  await expect(page.locator(umamiScript)).toHaveCount(0)
  await expect(page.locator(widgetScript)).toHaveCount(0)
  await context.close()
})
