export default defineNuxtRouteMiddleware(async (to) => {
  if (to.path === '/login' || to.path === '/auth/verify') return
  const { authenticated } = usePlatformAuth()
  if (authenticated.value) return
  try { await useNuxtApp().$refreshPlatformSession() } catch { return navigateTo('/login') }
})
