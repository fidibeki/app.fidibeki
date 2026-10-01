// Local counterpart of the widget asset service: real built assets and the
// real Go public API on the same widget origin. Never sends requests externally.
import { createServer, request as requestHTTP } from 'node:http'
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'

const root = fileURLToPath(new URL('../widget/dist', import.meta.url))
createServer(async (request, response) => {
  const url = new URL(request.url || '/', 'http://localhost')
  if (url.pathname === '/website-chat-fixture.html') {
    const key = url.searchParams.get('key') || ''
    if (!/^pk_live_[A-Za-z0-9]{22}$/.test(key)) { response.writeHead(400).end(); return }
    response.writeHead(200, { 'Content-Type': 'text/html' })
    response.end(`<!doctype html><html lang="en"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Shop website fixture</title><style>body{font:16px system-ui;background:#f6f4ef;color:#203d37;margin:0;padding:40px}main{max-width:760px;margin:auto}article{background:white;padding:32px;border-radius:12px}button{padding:12px 20px}</style><main><p>FIELD STORE</p><article><h1>A quieter workday.</h1><p>A synthetic customer website for testing tenant conversations.</p><button>View the collection</button></article></main><script async src="http://widget.localhost:${process.env.SUPPORT_WIDGET_PORT || '4180'}/v1/loader.js" data-fidibeki-key="${key}"></script></html>`)
    return
  }
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
