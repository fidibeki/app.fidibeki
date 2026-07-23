import { expect, test } from '@playwright/test'

test.describe('desktop navigation', () => {
  test.skip(({ viewport }) => !viewport || viewport.width < 900, 'desktop only')

  test('header links reach every primary section', async ({ page }) => {
    await page.goto('/')
    const nav = page.getByRole('navigation', { name: 'Primary' })
    for (const [label, path] of [
      ['Features', '/features'],
      ['Integrations', '/integrations'],
      ['Pricing', '/pricing'],
      ['Docs', '/docs'],
      ['Use cases', '/use-cases'],
    ] as const) {
      await nav.getByRole('link', { name: label }).click()
      await expect(page).toHaveURL(new RegExp(`${path}$`))
      await expect(page.locator('h1')).toBeVisible()
      await page.goto('/')
    }
  })

  test('logo returns home and footer links work', async ({ page }) => {
    await page.goto('/pricing')
    await page.getByRole('link', { name: 'Fidibeki home' }).click()
    await expect(page).toHaveURL(/\/$/)

    await page.getByRole('navigation', { name: 'Trust' }).getByRole('link', { name: 'Security' }).click()
    await expect(page).toHaveURL(/\/security$/)
    await expect(page.locator('h1')).toContainText('Security')
  })

  test('skip link is the first focusable element', async ({ page }) => {
    await page.goto('/')
    await page.keyboard.press('Tab')
    const focused = page.locator(':focus')
    await expect(focused).toHaveText('Skip to content')
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(/#main$/)
  })

  test('keyboard focus is visible on nav links', async ({ page }) => {
    await page.goto('/')
    await page.keyboard.press('Tab')
    await page.keyboard.press('Tab')
    const focused = page.locator(':focus')
    await expect(focused).toBeVisible()
    const outlineWidth = await focused.evaluate(
      (element) => getComputedStyle(element).outlineWidth,
    )
    expect(parseFloat(outlineWidth)).toBeGreaterThan(0)
  })
})

test.describe('mobile navigation', () => {
  test.skip(({ viewport }) => !viewport || viewport.width >= 900, 'mobile only')

  test('menu opens, navigates, and closes', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('navigation', { name: 'Primary' })).toBeHidden()

    await page.getByRole('button', { name: 'Open menu' }).click()
    const mobileNav = page.getByRole('navigation', { name: 'Mobile' })
    await expect(mobileNav).toBeVisible()
    await expect(mobileNav.getByRole('link', { name: 'Start free trial' })).toBeVisible()
    await expect(mobileNav.getByRole('link', { name: 'Book a demo' })).toBeVisible()

    await mobileNav.getByRole('link', { name: 'Pricing' }).click()
    await expect(page).toHaveURL(/\/pricing$/)
    await expect(page.getByRole('navigation', { name: 'Mobile' })).toBeHidden()
  })

  test('primary calls to action are reachable on a small screen', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('link', { name: 'Start free trial' }).first()).toBeVisible()
    await expect(page.getByRole('link', { name: 'Book a demo' }).first()).toBeVisible()
  })
})

test.describe('landmarks and semantics', () => {
  test.skip(({ viewport }) => !viewport || viewport.width < 900, 'desktop only')

  for (const route of ['/', '/pricing', '/features/session-replay', '/docs/installation']) {
    test(`landmarks exist on ${route}`, async ({ page }) => {
      await page.goto(route)
      await expect(page.locator('header')).toBeVisible()
      await expect(page.locator('main')).toBeVisible()
      await expect(page.locator('footer')).toBeVisible()
      const images = page.locator('img')
      const count = await images.count()
      for (let index = 0; index < count; index += 1) {
        await expect(images.nth(index)).toHaveAttribute('alt', /.+/)
      }
    })
  }
})

test.describe('error page', () => {
  test.skip(({ viewport }) => !viewport || viewport.width < 900, 'desktop only')

  test('unknown route shows the branded 404', async ({ page }) => {
    const response = await page.goto('/definitely-not-a-page')
    expect(response?.status()).toBe(404)
    await expect(page.getByRole('heading', { name: 'This page does not exist' })).toBeVisible()
    await page.getByRole('link', { name: 'Back to home' }).click()
    await expect(page).toHaveURL(/\/$/)
  })
})
