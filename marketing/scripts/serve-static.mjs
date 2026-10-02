// Minimal static file server for the prerendered output. Used by Playwright
// so end-to-end tests exercise the exact HTML that ships to production.
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

const root = path.dirname(fileURLToPath(import.meta.url))
const outputDir = path.resolve(root, '..', '.output', 'public')
const port = Number(process.env.PORT || 4311)

const mimeTypes = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css',
  '.js': 'text/javascript',
  '.mjs': 'text/javascript',
  '.json': 'application/json',
  '.xml': 'application/xml',
  '.txt': 'text/plain; charset=utf-8',
  '.md': 'text/markdown; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.ico': 'image/x-icon',
  '.woff2': 'font/woff2',
  '.webmanifest': 'application/manifest+json',
}

if (!existsSync(outputDir)) {
  console.error(`No static output at ${outputDir}. Run \`pnpm build\` first.`)
  process.exit(1)
}

function resolveFile(urlPath) {
  const clean = decodeURIComponent(urlPath.split('?')[0])
  const safe = path.normalize(clean).replace(/^(\.\.[/\\])+/, '')
  let filePath = path.join(outputDir, safe)
  if (safe.endsWith('/')) {
    filePath = path.join(filePath, 'index.html')
  } else if (!path.extname(filePath)) {
    filePath = path.join(filePath, 'index.html')
  }
  if (!filePath.startsWith(outputDir)) return null
  return filePath
}

const compressible = new Set([
  '.html',
  '.css',
  '.js',
  '.mjs',
  '.json',
  '.xml',
  '.txt',
  '.md',
  '.svg',
])

const server = createServer(async (request, response) => {
  const filePath = resolveFile(request.url ?? '/')
  try {
    if (!filePath) throw new Error('forbidden')
    let body = await readFile(filePath)
    const extension = path.extname(filePath)
    const type = mimeTypes[extension] ?? 'application/octet-stream'
    const headers = { 'content-type': type }
    // Mirror production delivery: compress text and cache fingerprinted assets.
    if (
      compressible.has(extension) &&
      (request.headers['accept-encoding'] ?? '').includes('gzip')
    ) {
      body = gzipSync(body)
      headers['content-encoding'] = 'gzip'
    }
    if (
      request.url?.startsWith('/_nuxt/') ||
      extension === '.woff2' ||
      extension === '.png'
    ) {
      headers['cache-control'] = 'public, max-age=31536000, immutable'
    }
    response.writeHead(200, headers)
    response.end(body)
  } catch {
    try {
      const notFound = await readFile(path.join(outputDir, '404.html'))
      response.writeHead(404, { 'content-type': 'text/html; charset=utf-8' })
      response.end(notFound)
    } catch {
      response.writeHead(404, { 'content-type': 'text/plain' })
      response.end('Not found')
    }
  }
})

server.listen(port, () => {
  console.log(`Serving ${outputDir} at http://127.0.0.1:${port}`)
})
