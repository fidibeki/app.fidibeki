export function proxyTargetForRequest(upstream: string, requestURL: URL) {
  const configuredUpstream = new URL(upstream)
  if (configuredUpstream.protocol !== 'http:' && configuredUpstream.protocol !== 'https:') {
    throw new Error('API upstream must use HTTP or HTTPS')
  }

  const target = new URL(configuredUpstream.origin)
  target.pathname = requestURL.pathname
  target.search = requestURL.search
  return target
}
