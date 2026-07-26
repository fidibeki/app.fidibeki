import { getRequestHost, getRequestURL, proxyRequest, setResponseStatus, type H3Event } from 'h3'
import { proxyHostForRequest } from '../utils/proxy-host'
import { proxyTargetForRequest } from '../utils/proxy-target'

const apiPrefixes = ['/auth/', '/v1/', '/healthz', '/readyz']
const authPagePaths = new Set(['/auth/verify', '/auth/domain-verify'])

function unavailable(event: H3Event, message: string) {
  setResponseStatus(event, 503, 'Service Unavailable')
  return { error: { code: 'service_unavailable', message } }
}

export default defineEventHandler(async (event) => {
  const requestURL = getRequestURL(event)
  const path = requestURL.pathname
  if (authPagePaths.has(path) && (event.method === 'GET' || event.method === 'HEAD')) return
  if (!apiPrefixes.some((prefix) => path === prefix || path.startsWith(prefix))) return

  const config = useRuntimeConfig(event)
  if (!config.apiUpstream) {
    return unavailable(event, 'API upstream is not configured')
  }

  let target: URL
  try {
    target = proxyTargetForRequest(String(config.apiUpstream), requestURL)
  } catch {
    return unavailable(event, 'API upstream is not configured')
  }
  const proxySecret = String(config.internalProxySecret || '')
  if (!proxySecret) {
    return unavailable(event, 'API proxy is not configured')
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
    return unavailable(event, 'Base domain is not configured')
  }
  try {
    return await proxyRequest(event, target.toString(), {
      headers: {
        'x-fidibeki-proxy-host': proxyHost,
        'x-fidibeki-proxy-secret': proxySecret,
      },
      // A redirect from the API is addressed to the browser, not to this proxy.
      // Single sign-on depends on it: the SAML start endpoint answers with a
      // redirect to the organization's identity provider, and following it here
      // would serve the provider's login page from this origin, where its form
      // would post back to us instead of to the provider.
      fetchOptions: { redirect: 'manual' },
    })
  } catch {
    return unavailable(event, 'API upstream is unavailable')
  }
})
