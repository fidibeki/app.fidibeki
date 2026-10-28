import { expect, test } from '@playwright/test'
import { marketingRoutes } from '../../data/site'
import {
  localizedPath,
  marketingLocales,
  markdownPath,
} from '../../utils/locale'

const origin = 'https://fidibeki.com'
for (const locale of marketingLocales) {
  test(`all ${locale.code} pages expose localized content and reciprocal SEO links`, async ({
    request,
  }) => {
    const titles = new Set<string>()
    const descriptions = new Set<string>()
    for (const route of marketingRoutes) {
      const url = localizedPath(route, locale.code)
      const response = await request.get(url)
      expect(response.status(), url).toBe(200)
      const html = await response.text()
      expect(html, url).toContain(`lang="${locale.language}"`)
      expect([...html.matchAll(/<h1[\s>]/g)], url).toHaveLength(1)
      const canonical = [...html.matchAll(/<link\b[^>]*rel="canonical"[^>]*>/g)]
      expect(canonical, url).toHaveLength(1)
      expect(canonical[0]?.[0] ?? '', url).toContain(`href="${origin}${url}"`)
      for (const other of marketingLocales) {
        const alternate = [
          ...html.matchAll(/<link\b[^>]*hreflang="([^"]+)"[^>]*>/g),
        ].find((match) => match[1] === other.language)?.[0]
        expect(alternate, `${url} -> ${other.code}`).toContain(
          `href="${origin}${localizedPath(route, other.code)}"`,
        )
      }
      expect(html, url).toContain('hreflang="x-default"')
      expect(html, url).toContain(`href="${origin}${markdownPath(url)}"`)
      const title = html.match(/<title>(.*?)<\/title>/)?.[1] ?? ''
      const description =
        html.match(/<meta name="description" content="([^"]+)"/)?.[1] ?? ''
      expect(title.length, url).toBeGreaterThan(8)
      expect(description.length, url).toBeGreaterThan(40)
      expect(titles.has(title), `duplicate ${title}`).toBe(false)
      expect(descriptions.has(description), `duplicate ${url}`).toBe(false)
      titles.add(title)
      descriptions.add(description)
      if (locale.code !== 'en') {
        const english = await (await request.get(route)).text()
        // “Contact” and “Documentation” have the same spelling in French.
        if (!(locale.code === 'fr' && ['/contact', '/docs'].includes(route))) {
          expect(title, url).not.toBe(english.match(/<title>(.*?)<\/title>/)?.[1])
        }
        expect(description, url).not.toBe(
          english.match(/<meta name="description" content="([^"]+)"/)?.[1],
        )
      }
      for (const block of html.matchAll(
        /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
      )) {
        const data = JSON.parse(block[1] ?? '{}')
        if (data['@type'] === 'BreadcrumbList') {
          for (const crumb of data.itemListElement) {
            expect(new URL(crumb.item).pathname, url).toBe(
              localizedPath(new URL(crumb.item).pathname, locale.code),
            )
          }
        }
      }
      const markdown = await request.get(markdownPath(url))
      expect(markdown.status(), url).toBe(200)
      expect(markdown.headers()['content-type']).toContain('text/markdown')
      const text = await markdown.text()
      expect(text, url).toContain(`Source: ${origin}${url}`)
      expect(text.length, url).toBeGreaterThan(200)
      // Installation examples intentionally retain script tags inside fenced code.
      expect(text.replace(/```[^\n]*\n[\s\S]*?```/g, ''), url).not.toMatch(
        /<script|__NUXT__/,
      )
    }
  })
}

test('llms.txt has a concise index of real Markdown documents', async ({
  request,
}) => {
  const response = await request.get('/llms.txt')
  expect(response.status()).toBe(200)
  const text = await response.text()
  expect(text).toMatch(/^# Fidibeki\n\n> /)
  const links = [...text.matchAll(/\]\((https:\/\/fidibeki\.com[^)]+)\)/g)]
  expect(links.length).toBeGreaterThan(10)
  for (const [, href] of links) {
    expect(href).toBeTruthy()
    const page = await request.get(new URL(href!).pathname)
    expect(page.status(), href).toBe(200)
    expect(page.headers()['content-type'], href).toContain('text/markdown')
  }
  const pricing = await (await request.get('/pricing.md')).text()
  expect(pricing).toContain(
    '| Projects | 1 project | 4 projects | 10 projects | Unlimited projects |',
  )
  expect(pricing).toContain('$9.99')
})
