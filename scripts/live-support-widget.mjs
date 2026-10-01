// Local counterpart of the widget asset service: real built assets and the
// real Go public API on the same widget origin. Never sends requests externally.
import { createServer, request as requestHTTP } from 'node:http'
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'

const root = fileURLToPath(new URL('../widget/dist', import.meta.url))
createServer(async (request, response) => {
  const url = new URL(request.url || '/', 'http://localhost')
  if (url.pathname.endsWith('.js') || url.pathname.endsWith('.map')) {
    const path = resolve(root, `.${url.pathname}`)
    if (!path.startsWith(`${root}/`)) { response.writeHead(400).end(); return }
    try { const data = await readFile(path); response.writeHead(200, { 'Content-Type': 'text/javascript', 'Access-Control-Allow-Origin': '*' }); response.end(data) }
    catch { response.writeHead(404).end() }
    return
  }
  const upstream = requestHTTP({ hostname: '127.0.0.1', port: Number(process.env.SUPPORT_API_PORT || '8088'), path: request.url, method: request.method,
    headers: { ...request.headers, host: 'widget.localhost' } }, incoming => {
    response.writeHead(incoming.statusCode || 502, incoming.headers); incoming.pipe(response)
  })
  upstream.on('error', () => response.writeHead(502).end())
  request.pipe(upstream)
}).listen(Number(process.env.SUPPORT_WIDGET_PORT || '4173'), '127.0.0.1')
