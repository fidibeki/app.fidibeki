import { featureSlugs } from './features'
import { integrationSlugs } from './integrations'
import { useCaseSlugs } from './use-cases'
import { docSlugs } from './docs'

export const siteName = 'Fidibeki'

export const siteTagline = 'Product feedback that finishes the job.'

export const siteDescription =
  'The fastest path from “I found a bug” to “we fixed it.” Fidibeki lets reporters add screenshots, audio, or session replay from inside your product, so your team can triage, track, and update them in one workflow.'

export const loopStages = [
  { key: 'capture', label: 'Collect', blurb: 'Reporters submit feedback and add context on their terms.' },
  { key: 'analyze', label: 'Understand', blurb: 'On eligible plans, AI can clean, transcribe, and score reports before your team reads them.' },
  { key: 'decide', label: 'Decide', blurb: 'Triage the inbox, link related reports to one issue, and merge duplicates.' },
  { key: 'track', label: 'Track', blurb: 'Follow statuses, priorities, and an immutable history until resolution.' },
  { key: 'communicate', label: 'Close the loop', blurb: 'Send updates to the reporters who opted in.' },
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
