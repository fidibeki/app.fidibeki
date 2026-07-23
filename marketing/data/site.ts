import { featureSlugs } from './features'
import { integrationSlugs } from './integrations'
import { useCaseSlugs } from './use-cases'
import { docSlugs } from './docs'

export const siteName = 'Fidibeki'

export const siteTagline = 'Product feedback, closed-loop.'

export const siteDescription =
  'Fidibeki captures in-product feedback with full context, analyzes and triages it, tracks every decision to resolution, and tells reporters how it ended.'

export const loopStages = [
  { key: 'capture', label: 'Capture', blurb: 'Feedback arrives in-product with screenshots, replay, and context.' },
  { key: 'analyze', label: 'Analyze', blurb: 'AI cleans, transcribes, and scores every report before you read it.' },
  { key: 'decide', label: 'Decide', blurb: 'Triage the inbox, link reports to issues, merge duplicates.' },
  { key: 'track', label: 'Track', blurb: 'Statuses, priorities, and immutable history to resolution.' },
  { key: 'communicate', label: 'Communicate', blurb: 'Reporters hear the outcome — automatically.' },
] as const

/** Real customers only — never add a logo without the founder's confirmation. */
export const customers = [
  {
    name: 'Tauview',
    url: 'https://tauview.app',
    logoLight: '/customers/tauview-light.png',
    logoDark: '/customers/tauview-dark.png',
  },
]

export const primaryNav = [
  { label: 'Features', to: '/features' },
  { label: 'Integrations', to: '/integrations' },
  { label: 'Pricing', to: '/pricing' },
  { label: 'Docs', to: '/docs' },
  { label: 'Use cases', to: '/use-cases' },
]

export const footerGroups = [
  {
    heading: 'Product',
    links: [
      { label: 'Features', to: '/features' },
      { label: 'Integrations', to: '/integrations' },
      { label: 'Pricing', to: '/pricing' },
      { label: 'Use cases', to: '/use-cases' },
    ],
  },
  {
    heading: 'Resources',
    links: [
      { label: 'Documentation', to: '/docs' },
      { label: 'Install the widget', to: '/docs/installation' },
      { label: 'Widget rules', to: '/docs/widget-rules' },
      { label: 'Webhooks', to: '/docs/webhooks' },
    ],
  },
  {
    heading: 'Trust',
    links: [
      { label: 'Security', to: '/security' },
      { label: 'Privacy', to: '/privacy' },
    ],
  },
  {
    heading: 'Get started',
    links: [{ label: 'Sign up', to: '/sign-up' }],
  },
]

/**
 * Every indexable route on the marketing site. Used for prerendering,
 * sitemap validation, and internal-link tests.
 */
export const marketingRoutes: string[] = [
  '/',
  '/features',
  ...featureSlugs.map((slug) => `/features/${slug}`),
  '/integrations',
  ...integrationSlugs.map((slug) => `/integrations/${slug}`),
  '/pricing',
  '/docs',
  ...docSlugs.map((slug) => `/docs/${slug}`),
  '/use-cases',
  ...useCaseSlugs.map((slug) => `/use-cases/${slug}`),
  '/privacy',
  '/security',
  '/sign-up',
]
