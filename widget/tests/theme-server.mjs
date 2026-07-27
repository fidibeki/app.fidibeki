import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { extname, join } from 'node:path'

const root = new URL('..', import.meta.url).pathname
const types = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.map': 'application/json',
  '.json': 'application/json; charset=utf-8',
}

const mockWallPayload = {
  schema_version: 1,
  wall: 'wl_demo',
  name: 'Demo Wall',
  layout: 'grid',
  theme: { mode: 'auto' },
  show_rating: true,
  show_avatar: true,
  show_company: true,
  show_video: false,
  generated_at: '2026-07-27T12:00:00Z',
  cards: [
    {
      id: 'ts_001',
      quote: 'This product changed how our team works. The dark mode support is fantastic.',
      headline: 'Game changer',
      rating: 5,
      author: { name: 'Ana Marques', role: 'Head of Support', company: 'Acme' },
    },
    {
      id: 'ts_002',
      quote: 'We cut triage from 3 days to 20 minutes. The widget follows our site theme perfectly.',
      rating: 5,
      author: { name: 'João Silva', role: 'CTO', company: 'TechCo' },
    },
  ],
}

createServer(async (request, response) => {
  const pathname = new URL(request.url ?? '/', 'http://127.0.0.1').pathname

  // Serve the theme test page
  if (pathname === '/theme-test.html') {
    try {
      const body = await readFile(join(root, 'tests/theme-test.html'))
      response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
      response.end(body)
      return
    } catch {
      response.writeHead(404).end()
      return
    }
  }

  // Mock wall endpoint
  if (pathname === '/v1/public/walls/wl_demo') {
    response.writeHead(200, { 'Content-Type': 'application/json' })
    response.end(JSON.stringify(mockWallPayload))
    return
  }

  // Serve built widget files from dist
  const relative = pathname === '/sample.html' ? 'tests/sample.html' : `dist${pathname}`
  if (relative.includes('..')) {
    response.writeHead(400).end()
    return
  }
  try {
    const body = await readFile(join(root, relative))
    response.writeHead(200, { 'Content-Type': types[extname(relative)] ?? 'application/octet-stream' })
    response.end(body)
  } catch {
    response.writeHead(404).end()
  }
}).listen(4174, '127.0.0.1')

console.log('Theme test server running at http://127.0.0.1:4174/theme-test.html')