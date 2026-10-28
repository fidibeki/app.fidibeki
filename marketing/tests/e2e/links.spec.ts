import { expect, test } from '@playwright/test'
import { marketingRoutes } from '../../data/site'
import { localizedPath, marketingLocales } from '../../utils/locale'

const siteUrl = 'https://fidibeki.com'
const appUrl = process.env.NUXT_PUBLIC_APP_URL || siteUrl

test('no internal link is broken', async ({ request }) => {
  const internalLinks = new Map<string, string>()

  for (const route of marketingRoutes) {
    const response = await request.get(route)
    expect(response.status(), route).toBe(200)
    const html = await response.text()
    for (const match of html.matchAll(/href="([^"#]+)(#[^"]*)?"/g)) {
      const href = match[1]
      if (
        href.startsWith('/') &&
        !href.startsWith('//') &&
        !href.startsWith('/_')
      ) {
        if (!internalLinks.has(href)) internalLinks.set(href, route)
      }
    }
  }

  expect(internalLinks.size).toBeGreaterThan(10)

  for (const [href, foundOn] of internalLinks) {
    if (href.match(/\.(css|js|mjs|png|svg|ico|woff2|xml|txt)$/)) {
      const assetResponse = await request.get(href)
      expect(
        assetResponse.status(),
        `asset ${href} linked from ${foundOn}`,
      ).toBe(200)
      continue
    }
    const response = await request.get(href)
    expect(response.status(), `link ${href} linked from ${foundOn}`).toBe(200)
  }
})

test('sitemap covers exactly the marketing routes', async ({ request }) => {
  const response = await request.get('/sitemap.xml')
  expect(response.status()).toBe(200)
  const xml = await response.text()
  const locations = [...xml.matchAll(/<loc>([^<]+)<\/loc>/g)].map(
    (match) => match[1],
  )
  const expected = marketingLocales.flatMap((locale) =>
    marketingRoutes.map(
      (route) => `${siteUrl}${localizedPath(route, locale.code)}`,
    ),
  )
  expect(locations.sort()).toEqual(expected.sort())
})

test('robots.txt allows crawling and names the sitemap', async ({
  request,
}) => {
  const response = await request.get('/robots.txt')
  expect(response.status()).toBe(200)
  const body = await response.text()
  expect(body).toContain('User-agent: *')
  expect(body).toContain('Allow: /')
  expect(body).toContain(`Sitemap: ${siteUrl}/sitemap.xml`)
})

test('external app links point at the application', async ({ request }) => {
  const html = await (await request.get('/')).text()
  expect(html).toContain(`href="${appUrl}/login"`)
  expect(html).toContain(`href="${appUrl}/signup"`)
})

test('support email is reachable from every page footer', async ({
  request,
}) => {
  const html = await (await request.get('/')).text()
  expect(html).toContain('href="mailto:support@fidibeki.com"')
})

test('book a demo buttons link directly to the scheduling page', async ({
  request,
}) => {
  for (const route of ['/', '/pricing']) {
    const html = await (await request.get(route)).text()
    expect(html, route).toContain(
      'href="https://example.test/demo"',
    )
  }
})
