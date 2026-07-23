import type { UseCaseEntry } from './types'

export const useCases: UseCaseEntry[] = [
  {
    slug: 'bug-reports',
    name: 'Bug reports with full context',
    audience: 'Engineering teams',
    tagline: 'Stop asking “can you send a screenshot?” — it’s already attached.',
    summary:
      'Reporters file bugs with screenshots, error reports, and session replay attached. AI transcribes and summarizes, and needs-info questions reach the reporter without another tool.',
    icon: 'bug',
    featureSlugs: ['feedback-widget', 'session-replay', 'ai-analysis', 'reporter-loop'],
    sections: [
      {
        heading: 'Reproduction steps that write themselves',
        body: 'A bug report filed through the widget can carry a screenshot, a captured JavaScript error, and a session replay of the minutes before the report. Engineers watch what actually happened instead of guessing from a one-line description.',
      },
      {
        heading: 'Follow-up without the ping-pong',
        body: 'When a report is missing detail, move the issue to “needs info” — the reporter gets an email and a secure page to answer on, no account required. When the fix ships, resolving the issue tells everyone who reported it.',
      },
    ],
  },
  {
    slug: 'feature-requests',
    name: 'Feature request triage',
    audience: 'Product teams',
    tagline: 'Aggregate demand honestly instead of counting the loudest voice twice.',
    summary:
      'Collect requests in-product, let AI clean and score them, link duplicates to one issue, and close the loop with every requester when you ship.',
    icon: 'lightbulb',
    featureSlugs: ['feedback-widget', 'ai-analysis', 'issue-tracking', 'reporter-loop'],
    sections: [
      {
        heading: 'One issue, all its evidence',
        body: 'Every request stays an independent feedback record until you decide it is work. Link twenty duplicate requests to a single issue and the issue carries all twenty voices — with sentiment — when you argue for it in planning.',
      },
      {
        heading: 'Shipping becomes marketing',
        body: 'When the issue resolves, opted-in requesters hear about it automatically. The people most invested in a feature learn you shipped it from you, not from a changelog they never read.',
      },
    ],
  },
  {
    slug: 'customer-support',
    name: 'Support & success workflows',
    audience: 'Support & CX teams',
    tagline: 'A triage queue your whole team can operate, not another shared inbox.',
    summary:
      'Filter and bulk-triage incoming feedback, escalate real problems to issues with priorities and tags, and let Slack or Teams tell the room when something urgent lands.',
    icon: 'headset',
    featureSlugs: ['issue-tracking', 'reporter-loop', 'feedback-widget'],
    sections: [
      {
        heading: 'Volume without chaos',
        body: 'The feedback inbox filters by project, status, and more, and bulk operations let one person clear a morning’s intake in minutes. Priorities from none to urgent and workspace tags keep escalations explicit.',
      },
      {
        heading: 'The room finds out immediately',
        body: 'On Scale and Enterprise plans, Slack and Teams destinations announce new feedback and issue movement to the channels that should care — with delivery logged, retried, and never blocking intake.',
      },
    ],
  },
  {
    slug: 'in-product-research',
    name: 'In-product research',
    audience: 'Research & design teams',
    tagline: 'Ask at the moment of truth, not in next Tuesday’s survey email.',
    summary:
      'Objective-based targeting shows prompts only after real engagement, content consumption, or visible friction — and a funnel of impressions to submissions tells you it worked.',
    icon: 'telescope',
    featureSlugs: ['feedback-widget', 'ai-analysis', 'issue-tracking'],
    sections: [
      {
        heading: 'Evidence-gated prompts',
        body: 'Pick an objective — continuous feedback, after engagement, validate content, or detect friction — and the widget’s local policy decides when a visitor has shown enough evidence to be worth interrupting. No personal fingerprinting, no reading page content: the scoring runs on interaction signals in the browser.',
      },
      {
        heading: 'Measure the ask itself',
        body: 'Widget analytics record the funnel from matched to impression, interaction, submission, or dismissal, so you can tell a prompt that converts from a prompt that annoys — and pause the second kind.',
      },
    ],
  },
]

export const useCaseSlugs = useCases.map((useCase) => useCase.slug)

export function useCaseBySlug(slug: string) {
  return useCases.find((useCase) => useCase.slug === slug)
}
