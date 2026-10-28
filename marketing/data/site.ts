import { guideSlugs } from './guides'
import { featureSlugs } from './features'
import { integrationSlugs } from './integrations'
import { useCaseSlugs } from './use-cases'
import { docSlugs } from './docs'
import { comparisonRoutes } from './comparisons'

export const siteName = 'Fidibeki'

export const siteTagline = 'Product feedback that finishes the job.'

export const siteDescription =
  'Collect website feedback, chat with visitors, and track the issues behind their messages. Fidibeki helps small SaaS teams follow through with a personal reply.'

export const loopStages = [
  {
    key: 'capture',
    label: 'Collect',
    blurb: 'Reporters submit feedback and add context on their terms.',
  },
  {
    key: 'analyze',
    label: 'Understand',
    blurb:
      'On eligible plans, AI can clean, transcribe, and score reports before your team reads them.',
  },
  {
    key: 'decide',
    label: 'Decide',
    blurb:
      'Triage the inbox, link related reports to one issue, and merge duplicates.',
  },
  {
    key: 'track',
    label: 'Track',
    blurb:
      'Follow statuses, priorities, and an immutable history until resolution.',
  },
  {
    key: 'communicate',
    label: 'Close the loop',
    blurb: 'Send updates to the reporters who opted in.',
  },
] as const

/** Add customer logos only with the customer's written permission. */
export const customers: Array<{
  name: string
  url: string
  logoLight: string
  logoDark: string
}> = []

export const primaryNav = [
  { label: 'Features', to: '/features' },
  { label: 'Integrations', to: '/integrations' },
  { label: 'Pricing', to: '/pricing' },
  { label: 'Docs', to: '/docs' },
  { label: 'Use cases', to: '/use-cases' },
]

/**
 * Public contact routes, shared by /contact and /support. Every address here
 * must be a real, monitored mailbox — the terms promise an answer on some of
 * them, so a dead address is a broken promise, not a cosmetic bug.
 */
export const contactChannels = [
  {
    key: 'support',
    label: 'Product support',
    email: 'support@fidibeki.com',
    blurb:
      'Setup trouble, unexpected behaviour, billing questions, or anything that looks like a bug. Start here when you are not sure who to ask.',
  },
  {
    key: 'security',
    label: 'Security reports',
    email: 'security@fidibeki.com',
    blurb:
      'Vulnerability disclosures. Include a proof of concept, and never test against customer workspaces or production data.',
  },
  {
    key: 'privacy',
    label: 'Privacy and data rights',
    email: 'privacy@fidibeki.com',
    blurb:
      'Access, correction, deletion, export, and DPA questions. If you submitted feedback through someone else’s product, contact that company first.',
  },
  {
    key: 'sales',
    label: 'Enterprise and invoicing',
    email: 'sales@fidibeki.com',
    blurb:
      'Unlimited projects, feedback, members, and retention, plus a custom domain and invoiced billing. Scoped with you rather than self-serve.',
  },
] as const

export const footerGroups = [
  {
    heading: 'Product',
    links: [
      { label: 'Features', to: '/features' },
      { label: 'Integrations', to: '/integrations' },
      { label: 'Pricing', to: '/pricing' },
      { label: 'Use cases', to: '/use-cases' },
      { label: 'Compare', to: '/compare' },
    ],
  },
  {
    heading: 'Resources',
    links: [
      { label: 'Guides', to: '/guides' },
      { label: 'Documentation', to: '/docs' },
      { label: 'Install the widget', to: '/docs/installation' },
      { label: 'Widget rules', to: '/docs/widget-rules' },
      { label: 'Webhooks', to: '/docs/webhooks' },
      { label: 'Support', to: '/support' },
      { label: 'Contact', to: '/contact' },
    ],
  },
  {
    heading: 'Trust',
    links: [
      { label: 'Security', to: '/security' },
      { label: 'Privacy', to: '/privacy' },
      { label: 'Terms', to: '/tos' },
      { label: 'DPA', to: '/dpa' },
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
  '/compare',
  ...comparisonRoutes,
  '/features',
  ...featureSlugs.map((slug) => `/features/${slug}`),
  '/integrations',
  ...integrationSlugs.map((slug) => `/integrations/${slug}`),
  '/pricing',
  '/guides',
  ...guideSlugs.map(slug => `/guides/${slug}`),
  '/docs',
  ...docSlugs.map((slug) => `/docs/${slug}`),
  '/use-cases',
  ...useCaseSlugs.map((slug) => `/use-cases/${slug}`),
  '/support',
  '/contact',
  '/privacy',
  '/security',
  '/tos',
  '/dpa',
  '/sign-up',
]
