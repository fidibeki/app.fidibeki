// Generates sitemap.xml from the prerendered static output. Run after
// `nuxt generate`; the build script wires this automatically.
import { readdir, stat, writeFile } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  marketingLocales,
  localizedPath,
  unlocalizedPath,
} from '../utils/locale.ts'

const root = path.dirname(fileURLToPath(import.meta.url))
const outputDir = path.resolve(root, '..', '.output', 'public')
const siteUrl = (
  process.env.NUXT_PUBLIC_SITE_URL || 'https://fidibeki.com'
).replace(/\/$/, '')

// Routes that exist in the output but must not be indexed.
const excluded = new Set(['/404.html', '/200.html'])

export async function collectRoutes(dir, base = '') {
  const routes = []
  const entries = await readdir(dir)
  for (const entry of entries) {
    const fullPath = path.join(dir, entry)
    const info = await stat(fullPath)
    if (info.isDirectory()) {
      routes.push(...(await collectRoutes(fullPath, `${base}/${entry}`)))
    } else if (entry === 'index.html') {
      routes.push(base === '' ? '/' : base)
    } else if (entry.endsWith('.html')) {
      routes.push(`${base}/${entry}`)
    }
  }
  return routes
}

function xmlEscape(value) {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('"', '&quot;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
}

export function buildSitemap(routes, origin) {
  const indexable = [...new Set(routes)]
    .filter((route) => !excluded.has(route))
    .sort()
  const available = new Set(indexable)
  const urls = indexable
    .map((route) => {
      const base = unlocalizedPath(route)
      const variants = marketingLocales.flatMap((locale) => {
        const translated = localizedPath(base, locale.code)
        if (!available.has(translated)) return []
        const languages =
          locale.code === 'pt' ? ['pt', locale.language] : [locale.language]
        return languages.map(
          (language) =>
            `    <xhtml:link rel="alternate" hreflang="${language}" href="${xmlEscape(origin + translated)}"/>`,
        )
      })
      if (available.has(base))
        variants.push(
          `    <xhtml:link rel="alternate" hreflang="x-default" href="${xmlEscape(origin + base)}"/>`,
        )
      return `  <url>\n    <loc>${xmlEscape(origin + route)}</loc>\n${variants.join('\n')}\n  </url>`
    })
    .join('\n')
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">\n${urls}\n</urlset>\n`
}

async function main() {
  if (!existsSync(outputDir)) {
    console.error(`No static output at ${outputDir}. Run \`pnpm build\` first.`)
    process.exit(1)
  }
  const routes = await collectRoutes(outputDir)
  const sitemap = buildSitemap(routes, siteUrl)
  const target = path.join(outputDir, 'sitemap.xml')
  await writeFile(target, sitemap)
  console.log(
    `Wrote ${target} with ${routes.filter((route) => !excluded.has(route)).length} URLs.`,
  )
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  await main()
}
