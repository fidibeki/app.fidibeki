const exactPaths = new Set([
  '/admin/auth/login',
  '/admin/auth/verify',
  '/admin/auth/refresh',
  '/admin/auth/logout',
  '/admin/backoffice/auth/login',
  '/admin/breach-incidents',
  '/admin/tenants',
])

const prefixes = [
  '/admin/backoffice/',
  '/admin/breach-incidents/',
  '/admin/tenants/',
  '/admin/queues/',
  '/admin/operations/',
]

// This is an allow-list, not a broad /admin prefix check: the Nuxt process
// must never become an accidental proxy for unrelated platform routes.
export function isBackofficeProxyPath(path: string) {
  return exactPaths.has(path) || prefixes.some(prefix => path.startsWith(prefix))
}
