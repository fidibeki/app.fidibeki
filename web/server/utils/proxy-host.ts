const platformRequests = new Set([
  'GET /v1/plans',
  'POST /v1/signup',
])
const platformGetPrefixes = [
  '/v1/public/workspace-logo/',
]

export function proxyHostForRequest(method: string, path: string, incomingHost: string, baseDomain: string) {
  const normalizedMethod = method.toUpperCase()
  const platformRequest = platformRequests.has(`${normalizedMethod} ${path}`)
    || (normalizedMethod === 'GET' && platformGetPrefixes.some(prefix => path.startsWith(prefix)))
  if (!platformRequest) return incomingHost
  if (!baseDomain) throw new Error('Base domain is not configured')
  return `api.${baseDomain}`
}
