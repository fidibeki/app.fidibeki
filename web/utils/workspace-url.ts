const reservedSlugs = new Set([
  'admin', 'api', 'app', 'auth', 'billing', 'demo', 'dev', 'help', 'mail', 'staging',
  'support', 'test', 'widget', 'www',
])

const slugPattern = /^[a-z0-9][a-z0-9-]{1,22}[a-z0-9]$/

export function normalizeWorkspaceSlug(value: string) {
  return value.toLowerCase().trim().replace(/[^a-z0-9-]+/g, '-').replace(/-+/g, '-').slice(0, 24)
}

export function workspaceSlugError(value: string) {
  if (!slugPattern.test(value) || reservedSlugs.has(value)) {
    return 'Use 3–24 lowercase letters, numbers, or hyphens, with a letter or number at each end.'
  }
  return ''
}

function environmentOrigin(baseDomain: string, currentURL: URL) {
  const local = baseDomain === 'localhost' || baseDomain.endsWith('.localhost')
  const protocol = local ? currentURL.protocol : 'https:'
  const port = local ? currentURL.port : ''
  return `${protocol}//${baseDomain}${port ? `:${port}` : ''}`
}

export function baseDomainURL(baseDomain: string, path: string, currentURL: URL) {
  return new URL(path, `${environmentOrigin(baseDomain, currentURL)}/`).href
}

export function workspaceLoginURL(slug: string, baseDomain: string, currentURL: URL) {
  const origin = environmentOrigin(baseDomain, currentURL)
  const baseURL = new URL(origin)
  baseURL.hostname = `${slug}.${baseDomain}`
  baseURL.pathname = '/login'
  return baseURL.href
}

export function isBaseDomainHost(hostname: string, baseDomain: string) {
  return hostname.toLowerCase().replace(/\.$/, '') === baseDomain.toLowerCase().replace(/\.$/, '')
}

export function workspaceSlugFromStandardHost(hostname: string, baseDomain: string) {
  const normalizedHost = hostname.toLowerCase().replace(/\.$/, '')
  const normalizedBase = baseDomain.toLowerCase().replace(/\.$/, '')
  if (!normalizedBase) return null
  const suffix = `.${normalizedBase}`
  if (!normalizedHost.endsWith(suffix)) return null
  const slug = normalizedHost.slice(0, -suffix.length)
  if (!slug || slug.includes('.') || workspaceSlugError(slug)) return null
  return slug
}
