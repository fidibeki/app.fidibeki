export default defineNuxtConfig({
  compatibilityDate: '2026-07-21',
  components: [
    {
      path: '~/components',
      pathPrefix: false,
    },
  ],
  css: ['~/assets/css/main.css', '~/assets/css/app.css'],
  devtools: { enabled: false },
  modules: [],
  routeRules: {
    '/': { ssr: true },
    '/login': { ssr: true },
    '/auth/verify': { ssr: false },
    '/designs/**': { ssr: false },
  },
  runtimeConfig: {
    internalProxySecret:
      process.env.NUXT_INTERNAL_PROXY_SECRET ||
      (process.env.NODE_ENV === 'development' ? 'local-only-internal-proxy-secret-change-me' : ''),
    apiUpstream:
      process.env.NUXT_API_UPSTREAM ||
      (process.env.NODE_ENV === 'development' ? 'http://127.0.0.1:8088' : ''),
  },
  typescript: {
    strict: true,
    typeCheck: true,
  },
  vite: {
    build: {
      sourcemap: true,
    },
  },
})
