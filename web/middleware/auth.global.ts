export default defineNuxtRouteMiddleware(async (to) => {
  const config = useRuntimeConfig()
  if ((to.path === '/' || to.path === '/login') &&
    isBaseDomainHost(useRequestURL().hostname, String(config.public.baseDomain || ''))) {
    return navigateTo('/signup')
  }
  if (to.path.startsWith('/designs') || to.path === '/login' || to.path === '/signup' ||
    to.path === '/auth/verify' || to.path === '/auth/domain-verify' ||
    to.path.startsWith('/reporter/') || to.path.startsWith('/privacy/')) return

  const { authenticated, member } = useAuth()

  if (!authenticated.value) {
    if (import.meta.server && !config.apiUpstream) {
      return navigateTo({ path: '/login', query: { return_to: to.fullPath } })
    }
    const { $refreshSession } = useNuxtApp()
    try {
      await $refreshSession()
    } catch {
      return navigateTo({ path: '/login', query: { return_to: to.fullPath } })
    }
  }

  const permissions = member.value?.permissions || []
  const home = firstAccessibleWorkspacePath(permissions)
  if (to.path === '/getting-started' && !canActivateWorkspace(permissions)) return navigateTo(home)
  if (to.path === '/access-denied' && home !== '/access-denied') return navigateTo(home)
  if (!canAccessWorkspacePath(to.path, permissions)) return navigateTo(home)
})
