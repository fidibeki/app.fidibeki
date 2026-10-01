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
  css: [
    '~/assets/css/main.css',
    '~/assets/css/home.css',
    '~/assets/css/reading.css',
  ],
  devtools: { enabled: false },
  modules: [],
  app: {
    head: {
      htmlAttrs: { lang: 'en' },
      meta: [{ name: 'theme-color', content: '#171717' }],
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
  vue: {
    compilerOptions: {
      isCustomElement: (tag) => tag === 'fidibeki-wall',
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
        process.env.NUXT_PUBLIC_UMAMI_SCRIPT_URL ||
        'https://cloud.umami.is/script.js',
      umamiWebsiteId:
        process.env.NUXT_PUBLIC_UMAMI_WEBSITE_ID ||
        '1d518b00-2258-4884-bf69-63f789d6f11d',
      widgetScriptUrl:
        process.env.NUXT_PUBLIC_WIDGET_SCRIPT_URL ||
        'https://widget.fidibeki.com/v1/loader.js',
      // Supplied per deployment. Unset means the consent banner has no widget to
      // load, which is the safe way to be wrong.
      widgetKey: process.env.NUXT_PUBLIC_WIDGET_KEY || '',
      widgetTestimonialsKey:
        process.env.NUXT_PUBLIC_WIDGET_TESTIMONIAL_KEY || '',
      // The banner acts as this site's CMP for the widget, so these must mirror the widget
      // rule's consent configuration: customer_cmp mode with these exact notice versions and a
      // supported locale, otherwise the grants are refused and the widget collects nothing.
      widgetConsentLocale:
        process.env.NUXT_PUBLIC_WIDGET_CONSENT_LOCALE || 'en',
      widgetAnalyticsNoticeVersion:
        process.env.NUXT_PUBLIC_WIDGET_ANALYTICS_NOTICE_VERSION || '2026-07',
      widgetReplayNoticeVersion:
        process.env.NUXT_PUBLIC_WIDGET_REPLAY_NOTICE_VERSION || '2026-07',
      // Our own status page. The badge reads its public overview endpoint from
      // the browser. The page id is supplied per deployment and never committed;
      // unset it and the badge degrades to a plain link rather than showing a
      // status it cannot verify, which is the safe way to be unconfigured.
      statusPageUrl:
        process.env.NUXT_PUBLIC_STATUS_PAGE_URL ||
        'https://status.fidibeki.com',
      statusPageId: process.env.NUXT_PUBLIC_STATUS_PAGE_ID || '',
    },
  },
  typescript: {
    strict: true,
    typeCheck: true,
  },
})
