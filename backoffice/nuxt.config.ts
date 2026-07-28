const developmentBaseDomain = process.env.NODE_ENV === 'development' ? 'localhost' : ''

export default defineNuxtConfig({
  compatibilityDate: '2026-07-21',
  // The strict refresh cookie is intentionally scoped to /admin/auth, so it
  // is unavailable to SSR document requests. Rendering this private console
  // as a client application prevents an unauthenticated document response
  // from containing an administration shell or data; the browser guard then
  // refreshes using its HTTP-only cookie.
  ssr: false,
  devtools: { enabled: false },
  css: ['~/assets/css/main.css'],
  app: { head: { title: 'Fidibeki Control Room', htmlAttrs: { lang: 'en' }, meta: [{ name: 'robots', content: 'noindex, nofollow' }] } },
  routeRules: {
    '/**': {
      headers: {
        'cache-control': 'no-store, max-age=0',
        'referrer-policy': 'no-referrer',
        'x-content-type-options': 'nosniff',
        'x-frame-options': 'DENY',
        // Nuxt's serialized page payload is an inline script. No external
        // scripts or eval are permitted, but this allowance is required for
        // client hydration and the browser-only session guard.
        'content-security-policy': "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; font-src 'self'; object-src 'none'",
      },
    },
  },
  runtimeConfig: {
    apiUpstream: process.env.BACKOFFICE_API_UPSTREAM || (process.env.NODE_ENV === 'development' ? 'http://127.0.0.1:8088' : ''),
    internalProxySecret: process.env.BACKOFFICE_INTERNAL_PROXY_SECRET || (process.env.NODE_ENV === 'development' ? 'local-only-internal-proxy-secret-change-me' : ''),
    baseDomain: process.env.BACKOFFICE_BASE_DOMAIN || developmentBaseDomain,
    trustedProxyCIDRs: process.env.BACKOFFICE_TRUSTED_PROXY_CIDRS || (process.env.NODE_ENV === 'development' ? '127.0.0.0/8,::1/128' : ''),
  },
  typescript: { strict: true, typeCheck: true },
})
