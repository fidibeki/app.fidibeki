import { getRequestURL, proxyRequest, setResponseStatus, type H3Event } from 'h3'
import { trustedClientIP } from '../utils/client-ip'
import { isBackofficeProxyPath } from '../utils/proxy-path'

function unavailable(event: H3Event) {
  setResponseStatus(event, 503, 'Service Unavailable')
  return { error: { code: 'service_unavailable', message: 'Backoffice API is unavailable' } }
}

export default defineEventHandler(async (event) => {
  const requestURL = getRequestURL(event)
  if (!isBackofficeProxyPath(requestURL.pathname)) return
  const config = useRuntimeConfig(event)
  if (!config.apiUpstream || !config.internalProxySecret || !config.baseDomain) return unavailable(event)
  let target: URL
  try { target = new URL(String(config.apiUpstream)) } catch { return unavailable(event) }
  if (!['http:', 'https:'].includes(target.protocol)) return unavailable(event)
  target.pathname = requestURL.pathname
  target.search = requestURL.search
  const forwardedFor = event.node.req.headers['x-forwarded-for']
  try {
    return await proxyRequest(event, target.toString(), {
      headers: {
        // Overwrite these trusted headers; accepting a browser-supplied value
        // would let an admin browser target a tenant host through the API proxy.
        'x-fidibeki-proxy-host': `api.${config.baseDomain}`,
        'x-fidibeki-proxy-secret': String(config.internalProxySecret),
        // Replace, rather than forward, browser-controlled forwarding data.
        // The original chain is consulted only after its direct peer matches a
        // configured ingress CIDR.
        'x-fidibeki-backoffice-client-ip': trustedClientIP(
          event.node.req.socket.remoteAddress,
          Array.isArray(forwardedFor) ? forwardedFor.join(',') : forwardedFor,
          String(config.trustedProxyCIDRs),
        ),
      },
      fetchOptions: { redirect: 'manual' },
    })
  } catch { return unavailable(event) }
})
