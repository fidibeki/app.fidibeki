/** Published-document comparisons. Review source links whenever facts or pricing change. */
export interface ComparisonRow {
  topic: string
  fidibeki: string
  competitor: string
  source: string
  detail: string
  productPath: string
}
export interface CompetitorEntry {
  slug: string
  name: string
  focus: string
  reviewedAt: string
  sources: { label: string; url: string }[]
  alternative: {
    title: string
    description: string
    introduction: string
    fit: string
    keep: string
    steps: string[]
    question: string
    answer: string
  }
  versus: {
    description: string
    verdict: string
    scenario: string
    rows: ComparisonRow[]
  }
  pricing: {
    plan: string
    usd: number
    cadence: 'month' | 'year'
    note: string
  }[]
  pricingNote: string
  featureSlugs: string[]
}
const reviewedAt = '2026-10-02'
export const competitors: CompetitorEntry[] = [
  {
    slug: 'marker-io',
    name: 'Marker.io',
    reviewedAt,
    focus: 'Visual bug reporting and website QA',
    sources: [
      { label: 'Product overview', url: 'https://marker.io/' },
      { label: 'Pricing', url: 'https://marker.io/pricing' },
      {
        label: 'Session replay',
        url: 'https://marker.io/features/session-replay',
      },
    ],
    alternative: {
      title: 'A Marker.io alternative for ongoing product feedback',
      description:
        'Considering a Marker.io alternative? See where Fidibeki fits, what to keep in your QA workflow, and how to test a switch before moving your team.',
      introduction:
        'If you need a place for visitors to report problems, suggest improvements and get an answer, consider Fidibeki. It brings feedback, website conversations and issue follow-up into one workspace.',
      fit: 'Fidibeki fits a product team collecting feedback after launch. Start with an embedded widget, ask for context when needed, link related reports to an issue, and update the reporters who opted in. Visitors can also reply to your team in the website conversation.',
      keep: 'Keep Marker.io on your shortlist when annotated screenshots and an established QA process are central to your work. A lower entry price is not a reason to remove tools your reviewers rely on.',
      steps: [
        'Pick one live product flow, such as checkout. List the details engineers need from a report before installing a second tool.',
        'Create a Fidibeki project, allow the test domain and configure the feedback widget. Test screenshots and, on Growth or above, consented replay.',
        'Route a sample report to an issue and test the reporter update. Keep historical Marker.io tickets and their links accessible while you evaluate new intake.',
      ],
      question: 'Will my existing Marker.io tickets move automatically?',
      answer:
        'No. Fidibeki has no dedicated Marker.io importer. Plan access to old tickets separately, recreate the rules and integrations you need, and switch new feedback only after your team has tested the whole flow.',
    },
    versus: {
      description:
        'Compare Fidibeki vs Marker.io for website feedback, visual QA, session replay, team workflow and pricing, with official sources and clear trade-offs.',
      verdict:
        'Choose Fidibeki when the job continues from visitor feedback to a conversation and an issue update. Choose Marker.io when visual bug reporting and QA are the main workflow. Both can collect website reports; evaluate the evidence and handoff your team actually needs.',
      scenario:
        'Report a checkout problem from a phone. Check the screenshot, the steps needed to reproduce it, the destination issue and the message the reporter receives. Then repeat with a feature suggestion. That second report helps reveal whether you need a QA tool or broader product feedback intake.',
      rows: [
        {
          topic: 'Reporting on the page',
          fidibeki:
            'Embedded feedback widget with optional screenshots and other context.',
          competitor: 'Screenshots, annotations and browser details.',
          source: 'https://marker.io/pricing',
          productPath: '/features/feedback-widget',
          detail:
            'If reviewers need to draw directly on screenshots, include that action in your trial. Capturing an image and annotating one are different requirements.',
        },
        {
          topic: 'Session replay',
          fidibeki:
            'Growth and above, when enabled and explicitly consented to.',
          competitor: 'Replay of activity before a report.',
          source: 'https://marker.io/features/session-replay',
          productPath: '/features/session-replay',
          detail:
            'For Fidibeki, verify the consent prompt, masking and playback permissions on your test site. A replay is useful only when the report contains the relevant interaction.',
        },
        {
          topic: 'Where the work happens',
          fidibeki: 'Triage in Fidibeki; connect issues to Jira or Linear.',
          competitor: 'QA workflow with task-management integrations.',
          source: 'https://marker.io/',
          productPath: '/features/issue-tracking',
          detail:
            'Choose the place your team will maintain status. Test the actual integration and its plan requirements instead of treating every integration as identical.',
        },
      ],
    },
    pricing: [
      {
        plan: 'Starter',
        usd: 39,
        cadence: 'year',
        note: '3 users; unlimited feedback.',
      },
      {
        plan: 'Team',
        usd: 149,
        cadence: 'year',
        note: '15 users; session replay and developer tools.',
      },
    ],
    pricingNote:
      'These Marker.io prices are monthly equivalents with annual billing. User allowances and feedback limits differ from Fidibeki; compare the plan your team would actually use.',
    featureSlugs: ['feedback-widget', 'session-replay'],
  },
  {
    slug: 'userback',
    name: 'Userback',
    reviewedAt,
    focus: 'Visual feedback, surveys and a feature portal',
    sources: [
      { label: 'Product overview', url: 'https://userback.io/' },
      { label: 'Pricing', url: 'https://userback.io/pricing/' },
      {
        label: 'Feature portal',
        url: 'https://userback.io/feature/feature-portal/',
      },
      {
        label: 'Session replay',
        url: 'https://userback.io/feature/session-replay/',
      },
      {
        label: 'Screen annotation',
        url: 'https://userback.io/feature/screen-annotation/',
      },
    ],
    alternative: {
      title: 'A Userback alternative for feedback and conversations',
      description:
        'Looking for a Userback alternative? Evaluate Fidibeki for website conversations, feedback triage and reporter updates, with an honest look at what differs.',
      introduction:
        'Fidibeki is worth evaluating when you want to collect a report, talk to the visitor and track the resulting work together. Choose it around that workflow, rather than assuming every feedback product includes the same tools.',
      fit: 'Use Fidibeki for direct, in-product feedback and private issue follow-up. Ask a visitor a question while they are on your site, let them reply in the widget, and decide per project whether visitors can start conversations. Keep the resulting feedback with the work it informs.',
      keep: 'Userback deserves a closer look if a public feature portal or screenshot annotation is essential. Fidibeki does not offer a public voting board. Keep a portal your users depend on until you have a replacement for that specific job.',
      steps: [
        'Separate the workflows you use today: reporting, surveys, a public portal and conversations. Decide which one you want Fidibeki to replace.',
        'Pilot the embedded widget on one domain. Set visitor-started conversations on or off and check the experience on a phone.',
        'Review a report with your team, link it to an issue and send an opted-in update. Keep the existing portal and historical feedback available during the pilot.',
      ],
      question: 'Is Fidibeki a free replacement for Userback?',
      answer:
        'Fidibeki offers a 14-day trial followed by a paid plan, not an ongoing free plan. Compare your required workflow, storage window and team size before moving just to reduce the subscription cost.',
    },
    versus: {
      description:
        'Fidibeki vs Userback: compare feedback capture, feature portals, session replay, conversations and plan limits. See where each product fits your team.',
      verdict:
        'Fidibeki suits teams that want a direct conversation alongside their feedback and issue workflow. Userback also covers visual feedback, with a broader set of collection tools. A public portal, annotation requirements and retention needs can matter more than the starting subscription price.',
      scenario:
        'Ask a user to report a broken form, then submit a product idea. Follow both through triage and an update. If the idea needs public votes, test that separately. If the report needs a private conversation on your site, make that part of your evaluation too.',
      rows: [
        {
          topic: 'Visual feedback',
          fidibeki:
            'Reporter-chosen screenshots and context through the website widget.',
          competitor: 'On-screen annotation tools.',
          source: 'https://userback.io/feature/screen-annotation/',
          productPath: '/features/feedback-widget',
          detail:
            'Decide whether your team needs an attached screenshot or a reviewer drawing on it. Test the exact interaction you expect your users to perform.',
        },
        {
          topic: 'Feature requests',
          fidibeki:
            'Private issues can group related requests; no public voting board.',
          competitor: 'Feature portal with voting and a public roadmap.',
          source: 'https://userback.io/feature/feature-portal/',
          productPath: '/use-cases/feature-requests',
          detail:
            'Fidibeki keeps individual reports linked to the issue your team decides to work on. A community that expects public voting needs a portal workflow as well.',
        },
        {
          topic: 'Session replay',
          fidibeki: 'Growth and above; explicit consent is required.',
          competitor: 'Replay can accompany feedback.',
          source: 'https://userback.io/feature/session-replay/',
          productPath: '/features/session-replay',
          detail:
            'Check the plan, recording configuration and retention window for your expected report volume. Do not compare a basic capture plan with a replay-enabled plan as though their scope were equal.',
        },
      ],
    },
    pricing: [
      {
        plan: 'Free',
        usd: 0,
        cadence: 'month',
        note: '2 seats; 2 projects; 7-day feedback availability.',
      },
      { plan: 'Team', usd: 39, cadence: 'month', note: '5 seats; 5 projects.' },
      {
        plan: 'Business',
        usd: 99,
        cadence: 'month',
        note: 'Unlimited seats; 25 projects; session replay.',
      },
    ],
    pricingNote:
      'Userback also sells a Feature Portal add-on. Confirm its cost if public voting is part of your requirements. The Free plan exists, so Fidibeki is not automatically the cheaper option.',
    featureSlugs: ['feedback-widget', 'issue-tracking'],
  },
  {
    slug: 'bugherd',
    name: 'BugHerd',
    reviewedAt,
    focus: 'Client review of websites and design assets',
    sources: [
      { label: 'Product overview', url: 'https://bugherd.com/' },
      { label: 'Pricing', url: 'https://bugherd.com/pricing' },
      { label: 'Features', url: 'https://bugherd.com/features' },
    ],
    alternative: {
      title: 'A BugHerd alternative for feedback after launch',
      description:
        'Evaluate Fidibeki as a BugHerd alternative for ongoing website feedback. Understand the difference from client design review before planning a switch.',
      introduction:
        'A site review and a live product have different feedback needs. Consider Fidibeki when visitors need to report a problem, ask a question and hear what changed after the release is already in their hands.',
      fit: 'Fidibeki gives your team an embedded intake point, a feedback inbox and tracked issues. Link multiple reports to the same problem and notify the reporters who chose to receive updates. Add consented session replay on Growth or above when written context is not enough.',
      keep: 'BugHerd is a strong option for agencies reviewing websites and design assets with clients. Fidibeki is built around the running website. It does not replace a Figma or PDF review workflow.',
      steps: [
        'List what arrives before launch and what arrives after it. Keep formal design review separate from ongoing visitor support.',
        'Install Fidibeki on a staging domain and test a mobile bug report. Confirm which attachments your selected plan and widget rule allow.',
        'Run new visitor feedback through one project first. Keep client review history in its existing system until everyone knows where to find it.',
      ],
      question: 'Can Fidibeki replace feedback on PDFs and Figma files?',
      answer:
        'No. Fidibeki captures feedback in a website or app. If your approval process depends on reviewing design files, keep a tool that supports those files and evaluate Fidibeki for the live product instead.',
    },
    versus: {
      description:
        'Compare Fidibeki vs BugHerd for live website feedback, client reviews, task handoff and pricing. See the differences that matter before choosing.',
      verdict:
        'Choose Fidibeki for ongoing visitor feedback, website conversations and updates tied to tracked issues. Choose BugHerd when client review is the centre of the job. The useful comparison is what your reviewers need to review, not how many features fit in a checklist.',
      scenario:
        'Use two examples: a client requesting a design revision before launch, and a visitor reporting a broken button after launch. Test how each person submits context, who sees it and how they learn the work is finished. Include a PDF if files are part of your review process.',
      rows: [
        {
          topic: 'What people review',
          fidibeki: 'A running website or app through its embedded widget.',
          competitor: 'Websites, Figma designs, PDFs and images.',
          source: 'https://bugherd.com/pricing',
          productPath: '/features/feedback-widget',
          detail:
            'Fidibeki is not a design-file review tool. If one approval must include both a mockup and the finished site, keep that requirement explicit.',
        },
        {
          topic: 'Feedback location',
          fidibeki:
            'Reports can include screenshots and, on eligible plans, a selected page element.',
          competitor: 'Feedback pinned to the page.',
          source: 'https://bugherd.com/',
          productPath: '/use-cases/bug-reports',
          detail:
            'Test whether the next person can find the affected element on mobile. A screenshot, an element attachment and a pinned review each give a different kind of context.',
        },
        {
          topic: 'Managing the work',
          fidibeki:
            'A feedback inbox linked to issues, priorities and reporter updates.',
          competitor: 'Task board for reviewing and organising feedback.',
          source: 'https://bugherd.com/features',
          productPath: '/features/issue-tracking',
          detail:
            'Keep the decision to create work separate from the original report. In Fidibeki, several reports can support one issue without deleting the individual feedback.',
        },
      ],
    },
    pricing: [
      {
        plan: 'Standard',
        usd: 50,
        cadence: 'month',
        note: '5 members; unlimited projects and client users.',
      },
    ],
    pricingNote:
      'BugHerd prices additional members separately and also offers annual billing. Its unlimited-project allowance is different from Fidibeki’s project limits; count your active client sites before choosing.',
    featureSlugs: ['feedback-widget', 'reporter-loop'],
  },
  {
    slug: 'canny',
    name: 'Canny',
    reviewedAt,
    focus: 'Feature voting, prioritisation and public roadmaps',
    sources: [
      { label: 'Product overview', url: 'https://canny.io/' },
      { label: 'Pricing', url: 'https://canny.io/pricing' },
      { label: 'Features', url: 'https://canny.io/features' },
    ],
    alternative: {
      title: 'A Canny alternative for private product feedback',
      description:
        'Looking for a Canny alternative? Consider Fidibeki for private feedback, bug reports and conversations. Understand what changes if you rely on public voting.',
      introduction:
        'Consider Fidibeki when your next problem is understanding an individual report and answering its author. A public request board is a different job, and moving away from one changes how your users participate.',
      fit: 'Fidibeki fits teams collecting contextual reports directly in their product. Link repeated requests to an issue, choose a priority and keep an internal history. Reporters who opt in can receive progress updates without creating an account or joining a public board.',
      keep: 'Keep Canny on your shortlist when users expect to vote on requests and follow a public roadmap. Fidibeki does not provide a public voting board, a public roadmap or a changelog publisher.',
      steps: [
        'Decide whether you still need public voting. If you do, keep that workflow; adding private feedback intake does not replace it.',
        'Pilot Fidibeki for one flow that needs more context, such as a bug report. Test an attachment, issue linking and an opted-in reporter update.',
        'Plan how users will find old requests, votes and announcements. There is no automatic transfer of those records into Fidibeki.',
      ],
      question: 'Can I move Canny votes and roadmap posts into Fidibeki?',
      answer:
        'Fidibeki has no dedicated Canny importer or equivalent public voting board. Keep the original records accessible. If voting history and public commitments are essential, a full replacement is not the right choice.',
    },
    versus: {
      description:
        'Fidibeki vs Canny: compare private feedback intake with public voting and roadmaps, understand plan limits, and choose the workflow your product needs.',
      verdict:
        'Fidibeki focuses on individual feedback, conversations and issue follow-up. Canny is a better fit for a public feature-request programme. Both can keep users informed, but a private update to one reporter is not the same as a public roadmap or release announcement.',
      scenario:
        'Evaluate a recurring feature request and a private bug report separately. For the request, decide whether you need public voting and a published commitment. For the bug, check how to capture context and answer one person. You may need both workflows rather than one replacement.',
      rows: [
        {
          topic: 'Collecting feedback',
          fidibeki:
            'Website widget with optional screenshots, audio and consented replay, depending on plan.',
          competitor:
            'Feedback boards and automatic capture from connected conversations.',
          source: 'https://canny.io/',
          productPath: '/features/feedback-widget',
          detail:
            'Choose where feedback should originate. In-product reporting gives the visitor an immediate place to explain what happened; collecting existing conversations serves a different intake pattern.',
        },
        {
          topic: 'Prioritising requests',
          fidibeki: 'Link reports to private issues with priorities and tags.',
          competitor: 'Voting, segmentation and roadmap prioritisation.',
          source: 'https://canny.io/features',
          productPath: '/use-cases/feature-requests',
          detail:
            'Fidibeki preserves the evidence behind an issue but does not offer a public voting board. Decide whether votes are part of your decision process before replacing that experience.',
        },
        {
          topic: 'Keeping people informed',
          fidibeki:
            'Opted-in email updates and a secure individual progress view.',
          competitor: 'Status updates, roadmaps and a changelog.',
          source: 'https://canny.io/features',
          productPath: '/features/reporter-loop',
          detail:
            'Use private updates when a person needs an answer about their report. Use a roadmap or changelog when the goal is a shared view of future plans or releases.',
        },
      ],
    },
    pricing: [
      {
        plan: 'Free',
        usd: 0,
        cadence: 'month',
        note: '25 tracked users; unlimited feedback.',
      },
      {
        plan: 'Pro',
        usd: 79,
        cadence: 'year',
        note: 'Starts at 100 tracked users.',
      },
    ],
    pricingNote:
      'Canny counts users associated with feedback. Pro starts at the listed annual-billing price and scales with tracked users. Fidibeki limits projects, monthly feedback and team members instead. Model your own usage rather than comparing the two starting prices alone.',
    featureSlugs: ['issue-tracking', 'reporter-loop'],
  },
]
export function alternativePath(slug: string) {
  return `/${slug}-alternative`
}
export function versusPath(slug: string) {
  return `/fidibeki-vs-${slug}`
}
export const comparisonRoutes = competitors.flatMap((entry) => [
  alternativePath(entry.slug),
  versusPath(entry.slug),
])
export function comparisonForPath(path: string) {
  const competitor = competitors.find(
    (entry) =>
      alternativePath(entry.slug) === path || versusPath(entry.slug) === path,
  )
  return competitor
    ? {
        competitor,
        kind:
          path === alternativePath(competitor.slug)
            ? ('alternative' as const)
            : ('versus' as const),
      }
    : undefined
}
