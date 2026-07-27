export default defineNuxtRouteMiddleware(async (to) => {
  if (to.path === '/login' || to.path === '/auth/verify') return
  // Refresh is deliberately browser-only. During SSR the request is made by
  // Nuxt, not the administrator's browser, so its strict HTTP-only refresh
  // cookie is not available. The client middleware re-runs during hydration
  // and refreshes without ever serializing the access token into HTML.
  if (import.meta.server) return
  const { authenticated } = usePlatformAuth()
  if (authenticated.value) return
  try { await useNuxtApp().$refreshPlatformSession() } catch { return navigateTo('/login') }
})
