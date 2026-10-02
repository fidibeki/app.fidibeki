// Build clean, localized Markdown from the rendered pages so policies, plans,
// and translations cannot drift into a separate hand-maintained content set.
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import TurndownService from 'turndown'
import { collectRoutes } from './generate-sitemap.mjs'
import { markdownPath } from '../utils/locale.ts'

export function pageMarkdown(html, canonical, routes = new Set()) {
  const main = html.match(/<main\b[^>]*>([\s\S]*?)<\/main>/i)?.[1]
  if (!main) throw new Error(`Missing main content: ${canonical}`)
  const converter = new TurndownService({
    headingStyle: 'atx',
    codeBlockStyle: 'fenced',
    bulletListMarker: '-',
  })
  converter.remove([
    'script',
    'style',
    'svg',
    'nav',
    'aside',
    'form',
    'button',
    'textarea',
    'input',
  ])
  converter.addRule('decorative', {
    filter: (node) =>
      node.getAttribute('aria-hidden') === 'true' ||
      node.hasAttribute('hidden'),
    replacement: () => '',
  })
  converter.addRule('absoluteLinks', {
    filter: 'a',
    replacement: (content, node) => {
      const href = node.getAttribute('href')
      if (!href || !content.trim()) return content
      const url = new URL(href, canonical)
      if (!['http:', 'https:', 'mailto:'].includes(url.protocol)) return content
      if (
        url.origin === new URL(canonical).origin &&
        routes.has(url.pathname)
      ) {
        url.pathname = markdownPath(url.pathname)
      }
      return `[${content.trim()}](${url.href})`
    },
  })
  converter.addRule('absoluteImages', {
    filter: 'img',
    replacement: (_, node) => {
      const alt = node.getAttribute('alt')
      const src = node.getAttribute('src')
      return alt && src
        ? `![${alt.replace(/[\[\]]/g, '')}](${new URL(src, canonical).href})`
        : ''
    },
  })
  converter.addRule('tables', {
    filter: 'table',
    replacement: (_, node) => {
      const rows = Array.from(node.querySelectorAll('tr')).map((row) =>
        Array.from(row.querySelectorAll('th,td')).map((cell) =>
          converter
            .turndown(cell.innerHTML)
            .trim()
            .replace(/\s+/g, ' ')
            .replace(/\|/g, '\\|'),
        ),
      )
      if (!rows.length) return ''
      return `\n\n| ${rows[0].join(' | ')} |\n| ${rows[0].map(() => '---').join(' | ')} |\n${rows
        .slice(1)
        .map((row) => `| ${row.join(' | ')} |`)
        .join('\n')}\n\n`
    },
  })
  return `${converter.turndown(main).trim()}\n\nSource: ${canonical}\n`
}

async function main() {
  const marketingDir = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    '..',
  )
  const output = path.join(marketingDir, '.output/public')
  const origin = (
    process.env.NUXT_PUBLIC_SITE_URL || 'https://fidibeki.com'
  ).replace(/\/$/, '')
  const routes = new Set(
    (await collectRoutes(output)).filter((route) => !route.endsWith('.html')),
  )
  for (const route of routes) {
    const html = await readFile(
      path.join(output, route.slice(1), 'index.html'),
      'utf8',
    )
    const target = path.join(output, markdownPath(route).slice(1))
    await mkdir(path.dirname(target), { recursive: true })
    await writeFile(target, pageMarkdown(html, `${origin}${route}`, routes))
  }
  const index = await readFile(
    path.join(marketingDir, 'public/llms.txt'),
    'utf8',
  )
  await writeFile(
    path.join(output, 'llms.txt'),
    index.replaceAll('https://fidibeki.com', origin),
  )
  await writeFile(
    path.join(output, 'robots.txt'),
    `User-agent: *\nAllow: /\n\nSitemap: ${origin}/sitemap.xml\n`,
  )
  console.log(`Generated ${routes.size} Markdown pages and llms.txt.`)
}

if (process.argv[1] === fileURLToPath(import.meta.url)) await main()
