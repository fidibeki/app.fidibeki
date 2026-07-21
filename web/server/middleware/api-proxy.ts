import { getRequestHost, getRequestURL, proxyRequest } from 'h3'
import { proxyHostForRequest } from '../utils/proxy-host'

const apiPrefixes = ['/auth/', '/v1/', '/healthz', '/readyz']

export default defineEventHandler(async (event) => {
  const path = getRequestURL(event).pathname
  if (path === '/auth/verify' && (event.method === 'GET' || event.method === 'HEAD')) return
  if (!apiPrefixes.some((prefix) => path === prefix || path.startsWith(prefix))) return

  const config = useRuntimeConfig(event)
  if (!config.apiUpstream) return

  const target = new URL(event.node.req.url || '/', String(config.apiUpstream))
  const proxySecret = String(config.internalProxySecret || '')
  if (!proxySecret) {
    throw createError({ statusCode: 503, statusMessage: 'API proxy is not configured' })
  }
  let proxyHost = ''
  try {
    proxyHost = proxyHostForRequest(
      event.method,
      path,
      getRequestHost(event, { xForwardedHost: false }),
      String(config.baseDomain || ''),
    )
  } catch {
    throw createError({ statusCode: 503, statusMessage: 'Base domain is not configured' })
  }
  return proxyRequest(event, target.toString(), {
    headers: {
      'x-fidibeki-proxy-host': proxyHost,
      'x-fidibeki-proxy-secret': proxySecret,
    },
  })
})
