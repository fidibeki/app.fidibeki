import { expect, test } from '@playwright/test'


test('system dark preference is respected by default', async ({ browser }) => {
  const context = await browser.newContext({ colorScheme: 'dark' })
  const page = await context.newPage()
  await page.goto('/')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await context.close()
})

test('system light preference is respected by default', async ({ browser }) => {
  const context = await browser.newContext({ colorScheme: 'light' })
  const page = await context.newPage()
  await page.goto('/')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await context.close()
})

test('explicit choice wins over system preference and persists', async ({ browser }) => {
  const context = await browser.newContext({ colorScheme: 'dark' })
  const page = await context.newPage()
  await page.goto('/')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

  await page.getByRole('button', { name: 'Switch to light theme' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')

  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')

  await page.goto('/pricing')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await context.close()
})

test('theme is applied before first paint (no flash)', async ({ browser }) => {
  const context = await browser.newContext({ colorScheme: 'dark' })
  await context.addInitScript(() => {
    try {
      localStorage.setItem('fidibeki-theme', 'light')
    } catch {
      // ignore
    }
  })
  const page = await context.newPage()
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  // The inline head script must have applied the stored theme synchronously,
  // before hydration had any chance to run.
  const theme = await page.evaluate(() => document.documentElement.dataset.theme)
  expect(theme).toBe('light')
  await context.close()
})

test('reduced motion disables reveal animations', async ({ browser }) => {
  const context = await browser.newContext({ reducedMotion: 'reduce' })
  const page = await context.newPage()
  await page.goto('/')
  const hero = page.locator('h1')
  await expect(hero).toBeVisible()
  const opacity = await hero.evaluate((element) => getComputedStyle(element).opacity)
  expect(Number(opacity)).toBe(1)
  await context.close()
})

test('both themes keep primary CTA contrast', async ({ page }) => {
  await page.goto('/')
  for (const theme of ['dark', 'light'] as const) {
    await page.evaluate((value) => {
      document.documentElement.dataset.theme = value
    }, theme)
    const cta = page.locator('.hero-actions .btn-primary').first()
    const { background, color } = await cta.evaluate((element) => {
      const style = getComputedStyle(element)
      return { background: style.backgroundColor, color: style.color }
    })
    expect(background).toBe('rgb(8, 127, 115)')
    expect(color).toBe('rgb(255, 255, 255)')
  }
})
