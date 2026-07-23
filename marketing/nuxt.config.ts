import { marketingRoutes } from './data/site'

const siteUrl = process.env.NUXT_PUBLIC_SITE_URL || 'https://fidibeki.com'
// The existing application serves signup at the base domain and sign-in on each
// workspace subdomain. Keep this configurable so deployment wiring can move the
// app without touching the marketing site.
const appUrl = process.env.NUXT_PUBLIC_APP_URL || 'https://fidibeki.com'

export default defineNuxtConfig({
  compatibilityDate: '2026-07-21',
  ssr: true,
  components: [
    {
      path: '~/components',
      pathPrefix: false,
    },
  ],
  css: ['~/assets/css/main.css'],
  devtools: { enabled: false },
  modules: [],
  app: {
    head: {
      htmlAttrs: { lang: 'en' },
      meta: [{ name: 'theme-color', content: '#0b1210' }],
      link: [{ rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }],
      script: [
        {
          // Applies the persisted or system theme before first paint to
          // avoid a flash of the wrong color scheme.
          innerHTML: `(function(){try{var t=localStorage.getItem('fidibeki-theme');if(t!=='light'&&t!=='dark'){t=window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'}document.documentElement.dataset.theme=t}catch(e){document.documentElement.dataset.theme='dark'}})()`,
          tagPriority: 'critical',
        },
      ],
    },
  },
  nitro: {
    prerender: {
      crawlLinks: true,
      routes: marketingRoutes,
      failOnError: true,
    },
  },
  runtimeConfig: {
    public: {
      siteUrl,
      appUrl,
      demoSchedulerUrl:
        process.env.NUXT_PUBLIC_DEMO_SCHEDULER_URL ||
        'https://cal.eu/acidiney.dias-fidibeki/30min',
      umamiScriptUrl:
        process.env.NUXT_PUBLIC_UMAMI_SCRIPT_URL || 'https://cloud.umami.is/script.js',
      umamiWebsiteId:
        process.env.NUXT_PUBLIC_UMAMI_WEBSITE_ID || '1d518b00-2258-4884-bf69-63f789d6f11d',
    },
  },
  typescript: {
    strict: true,
    typeCheck: true,
  },
})
