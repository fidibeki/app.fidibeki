import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { extname, join } from 'node:path'

const root = new URL('..', import.meta.url).pathname
const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.map': 'application/json' }

createServer(async (request, response) => {
  const pathname = new URL(request.url ?? '/', 'http://127.0.0.1').pathname
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
}).listen(4173, '127.0.0.1')
