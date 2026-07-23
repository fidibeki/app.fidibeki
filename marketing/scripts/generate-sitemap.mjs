// Generates sitemap.xml from the prerendered static output. Run after
// `nuxt generate`; the build script wires this automatically.
import { readdir, stat, writeFile } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.dirname(fileURLToPath(import.meta.url))
const outputDir = path.resolve(root, '..', '.output', 'public')
const siteUrl = (process.env.NUXT_PUBLIC_SITE_URL || 'https://fidibeki.com').replace(/\/$/, '')

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

export function buildSitemap(routes, origin) {
  const urls = routes
    .filter((route) => !excluded.has(route))
    .sort()
    .map((route) => `  <url><loc>${origin}${route === '/' ? '/' : route}</loc></url>`)
    .join('\n')
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls}\n</urlset>\n`
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
  console.log(`Wrote ${target} with ${routes.filter((route) => !excluded.has(route)).length} URLs.`)
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  await main()
}
