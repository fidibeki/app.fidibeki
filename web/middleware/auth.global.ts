export default defineNuxtRouteMiddleware(async (to) => {
  const config = useRuntimeConfig()
  if (to.path === '/' &&
    isApplicationHost(
      useRequestURL().hostname,
      String(config.public.baseDomain || ''),
      String(config.public.appUrl || ''),
    )) {
    return navigateTo('/signup')
  }
  const regional = useRegionalContext()
  if (to.path.startsWith('/designs') || to.path === '/login' || to.path === '/signup' ||
    to.path === '/auth/verify' || to.path === '/auth/domain-verify' || to.path === '/auth/two-factor' ||
    to.path.startsWith('/reporter/') || to.path.startsWith('/privacy/') ||
    // The person answering a testimonial request is not a member and never will
    // be, so these pages have to sit outside the session entirely. Their token
    // is the credential, checked server-side.
    to.path.startsWith('/testimonials/collect/') ||
    to.path.startsWith('/testimonials/revoke/')) {
    await regional.apply()
    return
  }

  const { authenticated, member } = useAuth()
  // The two-factor requirement lives outside the session: a challenge is owed
  // before any token exists, and a required-policy enrolment is owed even
  // though the session already holds one.
  const pendingTwoFactor = useState<string | null>('auth:two-factor', () => null)

  if (!authenticated.value) {
    if (import.meta.server && !config.apiUpstream) {
      return navigateTo({ path: '/login', query: { return_to: to.fullPath } })
    }
    const { $refreshSession } = useNuxtApp()
    try {
      await $refreshSession()
    } catch {
      if (pendingTwoFactor.value === 'challenge_required') {
        return navigateTo({ path: '/auth/two-factor', query: { return_to: to.fullPath } })
      }
      return navigateTo({ path: '/login', query: { return_to: to.fullPath } })
    }
  }

  if (pendingTwoFactor.value === 'challenge_required') {
    return navigateTo({ path: '/auth/two-factor', query: { return_to: to.fullPath } })
  }
  await regional.apply()
  if (pendingTwoFactor.value === 'enrollment_required') {
    return to.path === '/auth/two-factor-setup' ? undefined : navigateTo('/auth/two-factor-setup')
  }

  const permissions = member.value?.permissions || []
  const home = firstAccessibleWorkspacePath(permissions)
  if (to.path === '/auth/two-factor-setup') return navigateTo(home)
  if (to.path === '/getting-started' && !canActivateWorkspace(permissions)) return navigateTo(home)
  if (to.path === '/access-denied' && home !== '/access-denied') return navigateTo(home)
  if (!canAccessWorkspacePath(to.path, permissions)) return navigateTo(home)
})
