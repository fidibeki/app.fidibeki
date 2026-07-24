import type { FeatureEntry } from './types'

export const features: FeatureEntry[] = [
  {
    slug: 'feedback-widget',
    name: 'Feedback widget',
    loopStage: 'Capture',
    tagline: 'Collect feedback where it happens, with tools reporters choose to use.',
    summary:
      'One async script tag adds launchers, inline blocks, reactions, and prompts to your site. Reporters choose what to share; you decide which tools the rule offers and where it appears.',
    icon: 'message-square-plus',
    availability: 'All plans',
    highlights: [
      'Launcher, inline, reaction, and prompt presentations',
      'Free text, rating, reaction, and single choice response blocks',
      'Reporter chosen screenshots, audio, element attachment, and error report attachments, when enabled',
      'Targeting rules with objectives like "after engagement" or "detect friction"',
      'Shadow DOM isolation so widget styles never leak into your page',
      'Drafts are preserved across guarded retries and rate limit waits',
    ],
    sections: [
      {
        heading: 'Four ways to ask, one script tag',
        body: 'The loader ships as a single async script scoped to your project key. Each widget rule chooses a presentation: a floating launcher, an inline block embedded in your page, a lightweight reaction bar, or a proactive prompt. It then composes response blocks from free text, ratings, reactions, and single choices.',
      },
      {
        heading: 'Show prompts to the right visitors',
        body: 'Rules combine URL matchers, triggers, and an objective-based policy that scores active time, scroll depth, interaction counts, and per-page visits locally in the browser. Prompts and reactions only appear once there is real evidence of engagement, content consumption, or friction. Launchers and inline blocks stay immediately available.',
        bullets: [
          'Objectives: continuous feedback, after engagement, validate content, detect friction',
          'Allowed domain lists accept exact hosts and wildcards like *.preview.example.com',
          'Recurrence and suppression state keeps prompts from nagging the same visitor',
          'Localization support for widget copy',
        ],
      },
      {
        heading: 'Rich context, attached safely',
        body: 'Reporters can add screenshots, audio notes, element attachments, or video clips if the rule enables those tools. Session replay records only when a rule turns it on and the environment supports it. Uploads go straight to object storage through brief presigned URLs, are verified for size and type, and pass a quarantine check before anyone can view them.',
      },
    ],
    related: ['session-replay', 'ai-analysis'],
  },
  {
    slug: 'session-replay',
    name: 'Session replay',
    loopStage: 'Capture',
    tagline: 'See exactly what happened, only after the end user explicitly agrees.',
    summary:
      'Replay enabled rules record the moments leading up to a report with rrweb after explicit end user consent, honor sampling and masking policies, and stream playback only to authenticated teammates.',
    icon: 'monitor-play',
    availability: 'Growth, Scale & Enterprise',
    highlights: [
      'Off by default; starts only after explicit end user consent',
      'Session level sampling and masking policy; inputs masked by default',
      'Revocation immediately stops capture and discards buffered events',
      'Hard stops: 30 minutes, 50,000 events, five minutes idle, or page hide',
      'Playback streams through your authenticated workspace. Direct storage URLs are never exposed.',
      'Retention follows your plan’s storage window, cleaned up daily',
    ],
    sections: [
      {
        heading: 'Recording that respects the page',
        body: 'Replay records only when a rule explicitly enables it, the browser environment supports rrweb, and the end user has given explicit consent. Inputs are masked by default, recording stops at strict time/event/idle limits, and the capture never reads page content or network payloads. Consent can be managed by Fidibeki or asserted by your existing consent manager through our runtime API.',
      },
      {
        heading: 'Private by construction',
        body: 'Replays upload in small batches through presigned URLs and are merged server side into a single session file. Teammates with the session read permission stream playback through the authenticated API. Fidibeki never hands out direct object storage links, and replays are never sent to any AI provider.',
      },
    ],
    related: ['feedback-widget', 'issue-tracking'],
  },
  {
    slug: 'ai-analysis',
    name: 'AI analysis',
    loopStage: 'Analyze',
    tagline: 'Reports on eligible plans arrive cleaned, transcribed, and scored before your team reads them.',
    summary:
      'Asynchronous Gemini processing can clean, transcribe, and score only the reports that enter the AI pipeline on eligible plans. It can read text, screenshots, audio, video, and element attachments, and it never touches session replays.',
    icon: 'sparkles',
    availability: 'Growth, Scale & Enterprise',
    highlights: [
      'Cleaned text plus transcription and OCR for reports that include screenshots, audio, video, or element attachments',
      'Sentiment scoring and flag only moderation for reports that run through AI',
      'Session replays are never sent to the AI provider',
      'Retry a single attachment or reprocess a whole report on demand',
      'Daily AI budgets per workspace and per project keep costs bounded',
      'Submissions keep flowing even if AI processing is delayed',
    ],
    sections: [
      {
        heading: 'Analysis that runs off the critical path',
        body: 'AI work happens in background queues after a submission lands, so reporters never wait on a model. On eligible plans and rules with AI enabled, the worker can read the reporter’s text and any attachments they chose to include, request schema constrained JSON, and store cleaned text, transcription or OCR output, sentiment, and moderation flags alongside the original feedback.',
      },
      {
        heading: 'Guardrails by default',
        body: 'Temporary AI files are deleted after each pass and a reconciler resumes any unfinished cleanup from a database ledger. Workspace level and per project daily request budgets cap spend, and only quarantine-cleared attachments are ever eligible for analysis.',
      },
    ],
    related: ['feedback-widget', 'issue-tracking'],
  },
  {
    slug: 'issue-tracking',
    name: 'Issues & triage',
    loopStage: 'Decide',
    tagline: 'Turn a noisy inbox into a deliberate queue of decisions your team makes.',
    summary:
      'Feedback lands in a filterable inbox where your team triages in bulk, links reports to issues, merges duplicates with undo, and keeps an immutable history of every decision.',
    icon: 'list-checks',
    availability: 'All plans',
    highlights: [
      'Feedback inbox with filters, cursor pagination, and bulk triage',
      'Link and unlink feedback to issues; create an issue from any report',
      'Statuses: open, in progress, resolved, closed, needs info',
      'Priorities from none to urgent, plus workspace tags',
      'Merge duplicate issues and undo the merge if you were wrong',
      'Immutable activity history records every status change',
    ],
    sections: [
      {
        heading: 'Feedback and issues stay distinct on purpose',
        body: 'Every submission is an independent feedback record. Nothing auto-converts into work. When a report deserves action, your team creates or links an issue, so your issue list reflects decisions your team actually made, and each issue aggregates all the feedback that motivated it.',
      },
      {
        heading: 'Decisions with a paper trail',
        body: 'Status changes accept an optional internal comment and append to an immutable history, so “why did we close this?” always has an answer. Merging issues keeps their feedback links intact and can be undone, and dashboards summarize what is moving.',
      },
    ],
    related: ['reporter-loop', 'ai-analysis'],
  },
  {
    slug: 'reporter-loop',
    name: 'Close the loop',
    loopStage: 'Communicate',
    tagline: 'The people who report problems can hear how their issue ended, if they choose to.',
    summary:
      'Reporters who opt in get email updates as their issue progresses, a secure progress view, and a way to answer “needs info” questions, with delivery logged end to end.',
    icon: 'mail-check',
    availability: 'All plans',
    highlights: [
      'Automatic email updates to reporters who opted in as status changes',
      'Secure, revocable reporter progress view, no account required',
      'Reporters can respond when an issue needs more information',
      'Every message carries an unsubscribe link that just works',
      'Provider delivery attempts are recorded for auditability',
      'Bounces and complaints are handled automatically',
    ],
    sections: [
      {
        heading: 'From black hole to feedback loop',
        body: 'Most feedback tools end at the inbox. Fidibeki tracks each issue to resolution and, for reporters who opt in, tells them what happened. Updates are sent asynchronously, so triage stays fast, and reporters who opted in see honest progress instead of silence.',
      },
      {
        heading: 'Respectful by default',
        body: 'Reporter access tokens are random, stored only as hashes, rate limited, and expiring. Every update includes a long lasting unsubscribe link, and delivery attempts, bounces, and complaints are logged so you can prove what was sent.',
      },
    ],
    related: ['issue-tracking', 'feedback-widget'],
  },
  {
    slug: 'workspace-permissions',
    name: 'Workspaces & permissions',
    loopStage: 'Track',
    tagline: 'A separate home for every customer, locked down by default.',
    summary:
      'Each workspace lives on its own subdomain with passwordless sign in, fine grained member permissions, verified email domains, and API keys for automation.',
    icon: 'shield-check',
    availability: 'All plans',
    highlights: [
      'Dedicated workspace at your-team.fidibeki.com',
      'Passwordless magic link sign in for every member',
      'Fine grained per route permissions across projects, feedback, issues, and settings',
      'Restrict invitations to verified company email domains',
      'API keys for programmatic access',
      'Submission health diagnostics surface rate limit, quota, and upload pressure',
    ],
    sections: [
      {
        heading: 'The right people, the right verbs',
        body: 'Membership uses explicit permissions, over thirty of them, covering projects, widget rules, feedback, issues, sessions, members, API keys, integrations, and account operations. Invitations can be limited to verified company domains, and ownership transfer is a first class, audited flow.',
      },
      {
        heading: 'Operational visibility built in',
        body: 'Submission health diagnostics show at a glance when rate limits, quotas, uploads, or AI budgets are under pressure, so a quiet widget is never a mystery. API keys give your own tooling programmatic access to the workspace.',
      },
    ],
    related: ['issue-tracking', 'session-replay'],
  },
  {
    slug: 'enterprise-authentication',
    name: 'Enterprise authentication',
    loopStage: 'Track',
    tagline: 'SAML SSO and IdP managed MFA for larger teams.',
    summary:
      'Planned support for SAML 2.0 single sign on and identity provider managed multi factor authentication, so larger teams can enforce their own access policies.',
    icon: 'shield-check',
    availability: 'Enterprise, planned',
    highlights: [
      'SAML 2.0 single sign on, planned',
      'Organization level two factor authentication policy, planned',
      'IdP managed multi factor authentication, planned',
      'Optional and required SSO modes per organization, planned',
    ],
    sections: [
      {
        heading: 'Bring your own identity provider',
        body: 'We are planning SAML SSO so members can authenticate through your existing provider. The first version will support SP initiated login, verified email claims, and organization scoped enforcement modes.',
      },
      {
        heading: 'MFA through your IdP',
        body: 'Multi factor authentication requirements will be delegated to your identity provider, keeping Fidibeki simple while letting you enforce your own assurance level.',
      },
    ],
    related: ['workspace-permissions'],
  },
]

export const featureSlugs = features.map((feature) => feature.slug)

export function featureBySlug(slug: string) {
  return features.find((feature) => feature.slug === slug)
}
