const developmentBaseDomain = process.env.NODE_ENV === 'development' ? 'localhost' : ''

export default defineNuxtConfig({
  compatibilityDate: '2026-07-21',
  devtools: { enabled: false },
  css: ['~/assets/css/main.css'],
  app: { head: { title: 'Fidibeki Control Room', meta: [{ name: 'robots', content: 'noindex, nofollow' }] } },
  runtimeConfig: {
    apiUpstream: process.env.BACKOFFICE_API_UPSTREAM || (process.env.NODE_ENV === 'development' ? 'http://127.0.0.1:8088' : ''),
    internalProxySecret: process.env.BACKOFFICE_INTERNAL_PROXY_SECRET || (process.env.NODE_ENV === 'development' ? 'local-only-internal-proxy-secret-change-me' : ''),
    baseDomain: process.env.BACKOFFICE_BASE_DOMAIN || developmentBaseDomain,
  },
  typescript: { strict: true, typeCheck: true },
})
