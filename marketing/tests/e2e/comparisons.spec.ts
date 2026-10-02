import { expect, test } from '@playwright/test'
import {
  competitors,
  alternativePath,
  versusPath,
} from '../../data/comparisons'
import { localizedPath, marketingLocales } from '../../utils/locale'

for (const locale of marketingLocales) {
  test(`comparison hub reaches distinct alternative and versus pages in ${locale.code}`, async ({
    page,
  }) => {
    await page.goto(localizedPath('/compare', locale.code))
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    for (const competitor of competitors) {
      const alternative = localizedPath(
        alternativePath(competitor.slug),
        locale.code,
      )
      const versus = localizedPath(versusPath(competitor.slug), locale.code)
      await page
        .locator(`.comparison-directory a[href="${alternative}"]`)
        .click()
      await expect(page.locator('h1')).toContainText(competitor.name)
      await expect(page.locator('#switching')).toBeVisible()
      await expect(page.locator('.comparison-table')).toHaveCount(0)
      await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
        'href',
        `https://fidibeki.com${alternative}`,
      )
      await page.locator(`.hero-actions a[href="${versus}"]`).click()
      await expect(page.locator('h1')).toContainText(competitor.name)
      await expect(page.locator('.comparison-table tbody tr')).toHaveCount(competitor.versus.rows.length)
      await expect(page.locator('#switching')).toHaveCount(0)
      await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
        'href',
        `https://fidibeki.com${versus}`,
      )
      for (const source of competitor.sources)
        await expect(
          page.locator(`#sources a[href="${source.url}"]`),
        ).toBeVisible()
      if (locale.code !== 'en') {
        await expect(page.locator('.page-hero')).not.toContainText(
          competitor.versus.verdict,
        )
        await expect(page.locator('#pricing')).not.toContainText(
          /Billed monthly|Monthly equivalent|team members|Starts at/,
        )
      }
      await page
        .locator(
          `.comparison-sources a[href="${localizedPath('/compare', locale.code)}"]`,
        )
        .click()
      await expect(page.locator('.comparison-directory-row')).toHaveCount(4)
    }
    expect(errors).toEqual([])
  })

  test(`comparison content fits small screens and both themes in ${locale.code}`, async ({
    page,
  }) => {
    test.setTimeout(60_000)
    for (const width of [320, 834, 1440]) {
      await page.setViewportSize({ width, height: 1000 })
      for (const theme of ['light', 'dark']) {
        await page.emulateMedia({ colorScheme: theme as 'light' | 'dark' })
        for (const route of [
          '/compare',
          '/userback-alternative',
          '/fidibeki-vs-canny',
        ]) {
          await page.goto(localizedPath(route, locale.code))
          const dimensions = await page.evaluate(() => ({
            viewport: document.documentElement.clientWidth,
            page: document.documentElement.scrollWidth,
          }))
          expect(
            dimensions.page,
            `${locale.code} ${route} ${width} ${theme}`,
          ).toBeLessThanOrEqual(dimensions.viewport + 1)
          await expect(page.locator('h1')).toBeVisible()
          if (route.includes('-vs-') && width === 320) {
            await expect(page.locator('.comparison-scroll-hint')).toBeVisible()
            const wrapper = page.locator('.comparison-table-wrap')
            await wrapper.focus()
            await page.keyboard.press('ArrowRight')
            await expect
              .poll(() => wrapper.evaluate((node) => node.scrollLeft))
              .toBeGreaterThan(0)
            await expect(wrapper).toBeFocused()
            expect(
              await wrapper.evaluate(
                (node) => node.scrollWidth > node.clientWidth,
              ),
            ).toBe(true)
          }
        }
      }
    }
  })
}

test('changing language keeps the competitor, content and canonical together', async ({
  page,
}) => {
  await page.goto('/fidibeki-vs-bugherd')
  await page.locator('.header-language button').click()
  await page.locator('.header-language a[lang="pt-PT"]').click()
  await expect(page).toHaveURL(/\/pt\/fidibeki-vs-bugherd$/)
  await expect(page.locator('h1')).toHaveText('Fidibeki ou BugHerd')
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
    'href',
    'https://fidibeki.com/pt/fidibeki-vs-bugherd',
  )
  await expect(page.locator('.comparison-table')).toContainText(
    'Sites, designs Figma, PDFs e imagens.',
  )
  await expect(page.locator('#pricing')).toContainText('Faturação mensal')
})

test('free plans and different billing periods remain explicit', async ({
  page,
  request,
}) => {
  await page.goto('/fidibeki-vs-marker-io')
  await expect(page.locator('#pricing')).toContainText(
    'Monthly equivalent, billed annually',
  )
  await expect(page.locator('#pricing')).toContainText('Billed monthly')
  await expect(page.locator('#pricing')).toContainText('$39/month')
  await page.goto('/fidibeki-vs-userback')
  await expect(page.locator('#pricing')).toContainText('Free · $0/month')
  await expect(page.locator('#pricing')).toContainText(
    '7-day feedback availability',
  )
  await page.goto('/fidibeki-vs-canny')
  await expect(page.locator('#pricing')).toContainText('25 tracked users')
  const missing = await request.get('/unknown-alternative')
  expect(missing.status()).toBe(404)
})
