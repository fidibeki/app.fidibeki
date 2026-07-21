export default defineNuxtRouteMiddleware(async (to) => {
  if (to.path.startsWith('/designs') || to.path === '/login' || to.path === '/auth/verify') return

  const { authenticated, member } = useAuth()

  if (!authenticated.value) {
    const { $refreshSession } = useNuxtApp()
    try {
      await $refreshSession()
    } catch {
      return navigateTo({ path: '/login', query: { return_to: to.fullPath } })
    }
  }

  const permissions = member.value?.permissions || []
  const home = firstAccessibleWorkspacePath(permissions)
  if (to.path === '/access-denied' && home !== '/access-denied') return navigateTo(home)
  if (!canAccessWorkspacePath(to.path, permissions)) return navigateTo(home)
})
