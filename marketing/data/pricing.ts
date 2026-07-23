import type { PlanEntry } from './types'

// Verified against backend/internal/config/plans.go — a unit test keeps these
// numbers in sync with the backend plan catalog. Do not edit one without the other.
export const plans: PlanEntry[] = [
  {
    id: 'starter',
    name: 'Starter',
    monthlyPriceCents: 499,
    yearlyDiscountPercent: 7,
    blurb: 'For a first project that deserves better than a mailto link.',
    selfServe: true,
    limits: { projects: 1, feedbackPerMonth: 500, users: 1, storageDays: 30 },
    features: {
      ai: false,
      sessionReplay: false,
      slack: false,
      video: false,
      audio: false,
      domCapture: false,
      customDomain: false,
    },
  },
  {
    id: 'growth',
    name: 'Growth',
    monthlyPriceCents: 999,
    yearlyDiscountPercent: 14,
    blurb: 'For small teams who want replay and AI doing the first pass.',
    selfServe: true,
    highlighted: true,
    limits: { projects: 4, feedbackPerMonth: 2500, users: 3, storageDays: 90 },
    features: {
      ai: true,
      sessionReplay: true,
      slack: false,
      video: false,
      audio: true,
      domCapture: true,
      customDomain: false,
    },
  },
  {
    id: 'scale',
    name: 'Scale',
    monthlyPriceCents: 2799,
    yearlyDiscountPercent: 14,
    blurb: 'For teams routing feedback into Slack, Teams, and their own systems.',
    selfServe: true,
    limits: { projects: 10, feedbackPerMonth: 10000, users: 10, storageDays: 365 },
    features: {
      ai: true,
      sessionReplay: true,
      slack: true,
      video: true,
      audio: true,
      domCapture: true,
      customDomain: false,
    },
  },
  {
    id: 'enterprise',
    name: 'Enterprise',
    monthlyPriceCents: null,
    yearlyDiscountPercent: null,
    blurb: 'For organizations that need unlimited scale and custom domains.',
    selfServe: false,
    limits: { projects: -1, feedbackPerMonth: -1, users: -1, storageDays: -1 },
    features: {
      ai: true,
      sessionReplay: true,
      slack: true,
      video: true,
      audio: true,
      domCapture: true,
      customDomain: true,
    },
  },
]

export const trialDays = 14

export function formatPrice(cents: number | null) {
  if (cents === null) return 'Custom'
  const dollars = cents / 100
  return `$${dollars.toFixed(2).replace(/\.00$/, '')}`
}

export function formatLimit(value: number, singular: string, plural?: string) {
  if (value === -1) return `Unlimited ${plural ?? `${singular}s`}`
  const label = value === 1 ? singular : (plural ?? `${singular}s`)
  return `${value.toLocaleString('en-US')} ${label}`
}

export const planComparisonRows = [
  { key: 'projects', label: 'Projects' },
  { key: 'feedbackPerMonth', label: 'Feedback / month' },
  { key: 'users', label: 'Team members' },
  { key: 'storageDays', label: 'Replay & attachment retention' },
  { key: 'ai', label: 'AI analysis' },
  { key: 'sessionReplay', label: 'Session replay' },
  { key: 'audio', label: 'Audio attachments' },
  { key: 'video', label: 'Video attachments' },
  { key: 'domCapture', label: 'DOM capture' },
  { key: 'slack', label: 'Slack, Teams & webhooks' },
  { key: 'customDomain', label: 'Custom domain' },
] as const
