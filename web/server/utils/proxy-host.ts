const platformRequests = new Set([
  'GET /v1/plans',
  'POST /v1/signup',
])

export function proxyHostForRequest(method: string, path: string, incomingHost: string, baseDomain: string) {
  if (!platformRequests.has(`${method.toUpperCase()} ${path}`)) return incomingHost
  if (!baseDomain) throw new Error('Base domain is not configured')
  return `api.${baseDomain}`
}
