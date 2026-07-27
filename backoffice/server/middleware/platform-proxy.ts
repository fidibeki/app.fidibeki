import { getRequestIP, getRequestURL, proxyRequest, setResponseStatus, type H3Event } from 'h3'
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
  try {
    return await proxyRequest(event, target.toString(), {
      headers: {
        // Overwrite these trusted headers; accepting a browser-supplied value
        // would let an admin browser target a tenant host through the API proxy.
        'x-fidibeki-proxy-host': `api.${config.baseDomain}`,
        'x-fidibeki-proxy-secret': String(config.internalProxySecret),
		'x-fidibeki-backoffice-client-ip': getRequestIP(event, { xForwardedFor: true }) || '',
      },
      fetchOptions: { redirect: 'manual' },
    })
  } catch { return unavailable(event) }
})
