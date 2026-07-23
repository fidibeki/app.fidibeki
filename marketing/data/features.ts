import type { FeatureEntry } from './types'

export const features: FeatureEntry[] = [
  {
    slug: 'feedback-widget',
    name: 'Feedback widget',
    loopStage: 'Capture',
    tagline: 'Capture feedback where it happens, without breaking your product.',
    summary:
      'One async script tag adds launchers, inline blocks, reactions, and prompts to your site. Targeting rules decide who sees what, and drafts survive retries so no report is lost.',
    icon: 'message-square-plus',
    availability: 'All plans',
    highlights: [
      'Launcher, inline, reaction, and prompt presentations',
      'Free text, rating, reaction, and single-choice response blocks',
      'Screenshot, audio, video, and error-report attachments',
      'Targeting rules with objectives like “after engagement” or “detect friction”',
      'Shadow-DOM isolation so widget styles never leak into your page',
      'Drafts are preserved across guarded retries and rate-limit waits',
    ],
    sections: [
      {
        heading: 'Four ways to ask, one script tag',
        body: 'The loader ships as a single async script scoped to your project key. Each widget rule chooses a presentation — a floating launcher, an inline block embedded in your page, a lightweight reaction bar, or a proactive prompt — and composes response blocks from free text, ratings, reactions, and single choices.',
      },
      {
        heading: 'Deliberate targeting, not spray and pray',
        body: 'Rules combine URL matchers, triggers, and an objective-based policy that scores active time, scroll depth, interaction counts, and per-page visits locally in the browser. Prompts and reactions only appear once there is real evidence of engagement, content consumption, or friction — launchers and inline blocks stay immediately available.',
        bullets: [
          'Objectives: continuous feedback, after engagement, validate content, detect friction',
          'Allowed-domain lists accept exact hosts and wildcards like *.preview.example.com',
          'Recurrence and suppression state keeps prompts from nagging the same visitor',
          'Localization support for widget copy',
        ],
      },
      {
        heading: 'Rich context, attached safely',
        body: 'Reporters can attach screenshots, audio notes, or video clips depending on your plan, and the widget can capture reported JavaScript errors. Uploads go straight to object storage through short-lived presigned URLs, are verified for size and type, and pass a quarantine check before anyone can view them.',
      },
    ],
    related: ['session-replay', 'ai-analysis'],
  },
  {
    slug: 'session-replay',
    name: 'Session replay',
    loopStage: 'Capture',
    tagline: 'See what the reporter saw, with privacy defaults you don’t have to configure.',
    summary:
      'Replay-enabled rules record the moments leading up to a report with rrweb, honor sampling and masking policies, and stream playback only to authenticated teammates.',
    icon: 'monitor-play',
    availability: 'Growth, Scale & Enterprise',
    highlights: [
      'Pinned rrweb recording, lazy-loaded only when a rule enables it',
      'Session-level sampling and masking policy; inputs masked by default',
      'Recording continues across same-rule SPA navigation',
      'Hard stops: 30 minutes, 50,000 events, five minutes idle, or page hide',
      'Playback streams through your authenticated workspace — direct storage URLs are never exposed',
      'Retention follows your plan’s storage window, cleaned up daily',
    ],
    sections: [
      {
        heading: 'Recording that respects the page',
        body: 'The replay module lazy-loads a pinned rrweb build only for rules that enable it, so the base widget stays light. Recording uses your rule’s sampling and masking policy, keeps rolling across single-page-app navigation within the same rule, and ends automatically at strict time, event, and idle limits.',
      },
      {
        heading: 'Private by construction',
        body: 'Replays upload in small batches through presigned URLs and are merged server-side into a single session file. Teammates with the session-read permission stream playback through the authenticated API — Fidibeki never hands out direct object-storage links, and replays are never sent to any AI provider.',
      },
    ],
    related: ['feedback-widget', 'issue-tracking'],
  },
  {
    slug: 'ai-analysis',
    name: 'AI analysis',
    loopStage: 'Analyze',
    tagline: 'Every report arrives cleaned, transcribed, and scored before you read it.',
    summary:
      'Asynchronous Gemini processing turns raw text, screenshots, audio, and video into cleaned summaries, transcriptions, sentiment, and moderation flags — without ever touching session replays.',
    icon: 'sparkles',
    availability: 'Growth, Scale & Enterprise',
    highlights: [
      'Cleaned text plus transcription and OCR for screenshots, audio, and video',
      'Sentiment scoring and flag-only moderation on every processed report',
      'Session replays are never sent to the AI provider',
      'Retry a single attachment or reprocess a whole report on demand',
      'Daily AI budgets per workspace and per project keep costs bounded',
      'Submissions keep flowing even if AI processing is delayed',
    ],
    sections: [
      {
        heading: 'Analysis that runs off the critical path',
        body: 'AI work happens in background queues after a submission lands, so reporters never wait on a model. The worker streams completed media from object storage, requests schema-constrained JSON, and stores cleaned text, transcription or OCR output, sentiment, and moderation flags alongside the original feedback.',
      },
      {
        heading: 'Guardrails by default',
        body: 'Temporary AI files are deleted after each pass and a reconciler resumes any unfinished cleanup from a database ledger. Workspace-level and per-project daily request budgets cap spend, and only quarantine-cleared attachments are ever eligible for analysis.',
      },
    ],
    related: ['feedback-widget', 'issue-tracking'],
  },
  {
    slug: 'issue-tracking',
    name: 'Issues & triage',
    loopStage: 'Decide',
    tagline: 'Turn a noisy inbox into a deliberate queue of decisions.',
    summary:
      'Feedback lands in a filterable inbox where you triage in bulk, link reports to issues, merge duplicates with undo, and keep an immutable history of every decision.',
    icon: 'list-checks',
    availability: 'All plans',
    highlights: [
      'Feedback inbox with filters, cursor pagination, and bulk triage',
      'Link and unlink feedback to issues; create an issue from any report',
      'Statuses: open, in progress, resolved, closed, needs info',
      'Priorities from none to urgent, plus workspace tags',
      'Merge duplicate issues — and undo the merge if you were wrong',
      'Immutable activity history records every status change',
    ],
    sections: [
      {
        heading: 'Feedback and issues stay distinct on purpose',
        body: 'Every submission is an independent feedback record — nothing auto-converts into work. When a report deserves action, you create or link an issue, so your issue list reflects decisions your team actually made, and each issue aggregates all the feedback that motivated it.',
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
    tagline: 'The people who report problems deserve to hear how they ended.',
    summary:
      'Opted-in reporters get email updates as their issue progresses, a secure progress view, and a way to answer “needs info” questions — with delivery logged end to end.',
    icon: 'mail-check',
    availability: 'All plans',
    highlights: [
      'Automatic email updates to opted-in reporters as status changes',
      'Secure, revocable reporter progress view — no account required',
      'Reporters can respond when an issue needs more information',
      'Every message carries an unsubscribe link that just works',
      'Provider delivery attempts are recorded for auditability',
      'Bounces and complaints are handled automatically',
    ],
    sections: [
      {
        heading: 'From black hole to feedback loop',
        body: 'Most feedback tools end at the inbox. Fidibeki tracks each issue to resolution and tells the people who reported it. Updates are sent asynchronously, so triage stays fast, and reporters who opted in see honest progress instead of silence.',
      },
      {
        heading: 'Respectful by default',
        body: 'Reporter access tokens are random, stored only as hashes, rate-limited, and expiring. Every update includes a long-lived unsubscribe link, and delivery attempts, bounces, and complaints are logged so you can prove what was sent.',
      },
    ],
    related: ['issue-tracking', 'feedback-widget'],
  },
  {
    slug: 'workspace-permissions',
    name: 'Workspaces & permissions',
    loopStage: 'Track',
    tagline: 'A multi-tenant home for your team, locked down by default.',
    summary:
      'Each workspace lives on its own subdomain with passwordless sign-in, fine-grained member permissions, verified email domains, and API keys for automation.',
    icon: 'shield-check',
    availability: 'All plans',
    highlights: [
      'Dedicated workspace at your-team.fidibeki.com',
      'Passwordless magic-link sign-in for every member',
      'Fine-grained per-route permissions across projects, feedback, issues, and settings',
      'Restrict invitations to verified company email domains',
      'API keys for programmatic access',
      'Submission-health diagnostics surface rate-limit, quota, and upload pressure',
    ],
    sections: [
      {
        heading: 'The right people, the right verbs',
        body: 'Membership uses explicit permissions — over thirty of them — covering projects, widget rules, feedback, issues, sessions, members, API keys, integrations, and account operations. Invitations can be limited to verified company domains, and ownership transfer is a first-class, audited flow.',
      },
      {
        heading: 'Operational visibility built in',
        body: 'Submission-health diagnostics show at a glance when rate limits, quotas, uploads, or AI budgets are under pressure, so a quiet widget is never a mystery. API keys give your own tooling programmatic access to the workspace.',
      },
    ],
    related: ['issue-tracking', 'session-replay'],
  },
]

export const featureSlugs = features.map((feature) => feature.slug)

export function featureBySlug(slug: string) {
  return features.find((feature) => feature.slug === slug)
}
