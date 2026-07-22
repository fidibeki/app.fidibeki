import { createServer } from 'node:http'

const webPort = Number(process.env.PLAYWRIGHT_PORT || '4321')
const apiPort = webPort + 1000
const activeConfig = {
  tenant: { id: 'tenant-1', name: 'Acme Studio', slug: 'acme', plan: 'growth', status: 'active' },
  plan: {
    plan: 'growth',
    limits: { projects: 4, feedback_per_month: 2500, users: 3, storage_days: 90 },
    features: {
      ai: true,
      session_replay: true,
      slack: false,
      video: false,
      audio: true,
      dom_capture: true,
      custom_domain: false,
    },
  },
}

const api = createServer((request, response) => {
  response.setHeader('content-type', 'application/json')
  if (request.url !== '/v1/config') {
    response.statusCode = 404
    response.end(JSON.stringify({ error: { code: 'not_found', message: 'resource not found' } }))
    return
  }

  const host = String(request.headers['x-fidibeki-proxy-host'] || '')
  if (host.startsWith('missing.localhost')) {
    response.statusCode = 404
    response.end(JSON.stringify({ error: { code: 'unknown_tenant', message: 'unknown tenant' } }))
    return
  }
  if (host.startsWith('suspended.localhost')) {
    response.statusCode = 403
    response.end(JSON.stringify({ error: { code: 'tenant_suspended', message: 'tenant is not active' } }))
    return
  }
  if (host.startsWith('error.localhost')) {
    response.statusCode = 503
    response.end(JSON.stringify({ error: { code: 'service_unavailable', message: 'service unavailable' } }))
    return
  }

  response.end(JSON.stringify(activeConfig))
})

await new Promise((resolve, reject) => {
  api.once('error', reject)
  api.listen(apiPort, '127.0.0.1', resolve)
})

process.env.HOST = '127.0.0.1'
process.env.PORT = String(webPort)
process.env.NUXT_API_UPSTREAM = `http://127.0.0.1:${apiPort}`
process.env.NUXT_BASE_DOMAIN = 'localhost'
process.env.NUXT_PUBLIC_BASE_DOMAIN = 'localhost'
process.env.NUXT_PUBLIC_TURNSTILE_SITE_KEY = '1x00000000000000000000AA'
process.env.NUXT_INTERNAL_PROXY_SECRET = '0123456789abcdef0123456789abcdef'

await import('../../.output/server/index.mjs')
