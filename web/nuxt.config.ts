const developmentBaseDomain = process.env.NODE_ENV === 'development' ? 'localhost' : ''
const baseDomain = process.env.NUXT_BASE_DOMAIN || developmentBaseDomain
const publicBaseDomain = process.env.NUXT_PUBLIC_BASE_DOMAIN || developmentBaseDomain
const publicAppUrl = process.env.NUXT_PUBLIC_APP_URL || ''

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
  app: {
    head: {
      meta: [
        { name: 'theme-color', content: '#f5f7f6', media: '(prefers-color-scheme: light)' },
        { name: 'theme-color', content: '#0b1210', media: '(prefers-color-scheme: dark)' },
      ],
      link: [{ rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }],
      script: [
        {
          // Applies the persisted or system theme before first paint to avoid a
          // flash of the wrong color scheme. Uses the same storage key as the
          // marketing site; see useTheme.ts on why that is per-origin.
          innerHTML: `(function(){try{var t=localStorage.getItem('fidibeki-theme');if(t!=='light'&&t!=='dark'){t=window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'}document.documentElement.dataset.theme=t}catch(e){document.documentElement.dataset.theme='light'}})()`,
          tagPriority: 'critical',
        },
      ],
    },
  },
  modules: ['@nuxtjs/i18n'],
  i18n: {
    strategy: 'no_prefix',
    defaultLocale: 'en',
    detectBrowserLanguage: false,
    langDir: 'locales',
    locales: [
      { code: 'en', language: 'en', file: 'en.ts', dir: 'ltr', name: 'English' },
      { code: 'pt', language: 'pt', file: 'pt.ts', dir: 'ltr', name: 'Português' },
      { code: 'pt-AO', language: 'pt-AO', file: 'pt-AO.ts', dir: 'ltr', name: 'Português (Angola)' },
      { code: 'pt-PT', language: 'pt-PT', file: 'pt-PT.ts', dir: 'ltr', name: 'Português (Portugal)' },
      { code: 'en-XA', language: 'en-XA', file: 'en-XA.ts', dir: 'ltr', name: 'Pseudo' },
    ],
    vueI18n: './i18n.config.ts',
  },
  routeRules: {
    '/': { ssr: true },
    '/login': { ssr: true },
    '/signup': { ssr: true },
    '/auth/verify': { ssr: false },
    '/auth/domain-verify': { ssr: false },
    '/privacy/**': {
      ssr: false,
      headers: {
        'cache-control': 'no-store',
        'referrer-policy': 'no-referrer',
        'x-robots-tag': 'noindex, nofollow',
      },
    },
    '/reporter/**': {
      ssr: false,
      headers: {
        'cache-control': 'no-store',
        'referrer-policy': 'no-referrer',
        'x-robots-tag': 'noindex, nofollow',
      },
    },
    '/designs/**': { ssr: false },
  },
  runtimeConfig: {
    internalProxySecret:
      process.env.NUXT_INTERNAL_PROXY_SECRET ||
      (process.env.NODE_ENV === 'development' ? 'local-only-internal-proxy-secret-change-me' : ''),
    apiUpstream:
      process.env.NUXT_API_UPSTREAM ||
      (process.env.NODE_ENV === 'development' ? 'http://127.0.0.1:8088' : ''),
    baseDomain,
    public: {
      appUrl: publicAppUrl,
      baseDomain: publicBaseDomain,
      turnstileSiteKey: process.env.NUXT_PUBLIC_TURNSTILE_SITE_KEY || '',
    },
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
