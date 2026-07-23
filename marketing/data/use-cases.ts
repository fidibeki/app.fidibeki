import type { UseCaseEntry } from './types'

export const useCases: UseCaseEntry[] = [
  {
    slug: 'bug-reports',
    name: 'Bug reports with full context',
    audience: 'Engineering teams',
    tagline: 'Stop asking for screenshots. Reporters can attach them when the rule allows it.',
    summary:
      'Reporters can file bugs with screenshots, error reports, element attachments, and session replay when those tools are enabled for the rule and the plan supports them. AI can transcribe and summarize on Growth and above, and needs-info questions reach reporters who opted in without another tool.',
    icon: 'bug',
    featureSlugs: ['feedback-widget', 'session-replay', 'ai-analysis', 'reporter-loop'],
    sections: [
      {
        heading: 'Reproduction steps that write themselves',
        body: 'A bug report filed through the widget can carry a screenshot, a captured JavaScript error, an element attachment, and a session replay of the minutes before the report, when those tools are enabled. Engineers watch what actually happened instead of guessing from a one-line description.',
      },
      {
        heading: 'Get missing details without chasing people',
        body: 'When a report is missing detail, move the issue to “needs info”. Opted-in reporters get an email and a secure page to answer on, no account required. When the fix ships, resolving the issue tells everyone who opted in to hear about it.',
      },
    ],
  },
  {
    slug: 'feature-requests',
    name: 'Feature request triage',
    audience: 'Product teams',
    tagline: 'See real demand, not duplicate requests.',
    summary:
      'Collect requests in-product. On eligible plans, AI can clean and score reports that enter the pipeline, link duplicates to one issue, and close the loop with requesters who opted in when you ship.',
    icon: 'lightbulb',
    featureSlugs: ['feedback-widget', 'ai-analysis', 'issue-tracking', 'reporter-loop'],
    sections: [
      {
        heading: 'One issue, all its evidence',
        body: 'Every request stays an independent feedback record until you decide it is work. Link twenty duplicate requests to a single issue and the issue carries all twenty voices, with sentiment, when you argue for it in planning.',
      },
      {
        heading: 'Close the loop when you ship',
        body: 'When the issue resolves, requesters who opted in hear about it automatically. The people most invested in a feature learn you shipped it from you, not from a changelog they never read.',
      },
    ],
  },
  {
    slug: 'customer-support',
    name: 'Support & success workflows',
    audience: 'Support & CX teams',
    tagline: 'A triage queue your whole team can operate, not another shared inbox.',
    summary:
      'Filter and bulk-triage incoming feedback, escalate real problems to issues with priorities and tags. On Scale and Enterprise plans, Slack and Teams destinations can announce events to the channels you configure.',
    icon: 'headset',
    featureSlugs: ['issue-tracking', 'reporter-loop', 'feedback-widget'],
    sections: [
      {
        heading: 'Volume without chaos',
        body: 'The feedback inbox filters by project, status, and more, and bulk operations let one person clear a morning’s intake in minutes. Priorities from none to urgent and workspace tags keep escalations explicit.',
      },
      {
        heading: 'Notify the right channel automatically',
        body: 'On Scale and Enterprise plans, Slack and Teams destinations announce new feedback and issue movement to the channels that should care. Delivery is logged, retried, and never blocking intake.',
      },
    ],
  },
  {
    slug: 'in-product-research',
    name: 'In-product research',
    audience: 'Research & design teams',
    tagline: 'Ask at the moment of truth, not in next Tuesday’s survey email.',
    summary:
      'Objective-based targeting shows prompts only after real engagement, content consumption, or visible friction, and a funnel of impressions to submissions tells you it worked. AI scoring is available on eligible plans and only for reports that enter the AI pipeline.',
    icon: 'telescope',
    featureSlugs: ['feedback-widget', 'ai-analysis', 'issue-tracking'],
    sections: [
      {
        heading: 'Evidence-gated prompts',
        body: 'Pick an objective: continuous feedback, after engagement, validate content, or detect friction. The widget’s local policy then decides when a visitor has shown enough evidence to be worth interrupting. No personal fingerprinting, no reading page content: the scoring runs on interaction signals in the browser.',
      },
      {
        heading: 'Measure the ask itself',
        body: 'Widget analytics record the funnel from matched to impression, interaction, submission, or dismissal, so you can tell a prompt that converts from a prompt that annoys, and pause the second kind.',
      },
    ],
  },
]

export const useCaseSlugs = useCases.map((useCase) => useCase.slug)

export function useCaseBySlug(slug: string) {
  return useCases.find((useCase) => useCase.slug === slug)
}
