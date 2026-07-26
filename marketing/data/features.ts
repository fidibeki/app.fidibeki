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
    related: ['session-replay', 'ai-analysis', 'testimonials'],
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
    related: ['issue-tracking', 'feedback-widget', 'testimonials'],
  },
  {
    slug: 'testimonials',
    name: 'Testimonials',
    loopStage: 'Communicate',
    tagline: 'Your best feedback is already praise. Publish it, with permission.',
    summary:
      'Fidibeki scores the feedback you already collected, asks those reporters for permission in their own words, and gives you a wall you can embed with one script tag. Nothing goes public until the person who wrote it says yes and you approve it.',
    icon: 'quote',
    availability: 'Growth, Scale & Enterprise',
    highlights: [
      'A ranked queue of feedback worth asking about, with the reason for each rank',
      'One link asks the author for their words, their name, their photo, and their video, each granted separately',
      'Nothing publishes until the author consents and a teammate with publish rights approves',
      'Seven arrangements: grid, masonry, carousel, marquee, list, spotlight, or your own code',
      'Video testimonials in portrait or landscape, shown in the shape they were recorded in, on Scale and above',
      'Editing a published quote suspends it until the author agrees to the new wording',
    ],
    sections: [
      {
        heading: 'The testimonial you need is already in your inbox',
        body: 'Somebody already wrote you a five star review. It is sitting in your feedback list between a broken link and a pricing question. Fidibeki scores every report on sentiment, rating, length, specificity, and whether the person can be contacted at all, then puts the strongest at the top of a Suggested queue and shows you why each one ranked where it did. You send the request in one click, to the address that reporter already left.',
      },
      {
        heading: 'Consent is a record, not a checkbox',
        body: 'The author sees their own words and chooses, field by field, what may be published: the quote, their name and role, their company, their photo, their video. Each grant is stored with the wording it was given for. Every published testimonial carries a withdrawal link that works without an account, and a withdrawn testimonial disappears from your wall on the next publish.',
        bullets: [
          'Separate consent for quote, identity, company, photo, and video',
          'Editing the quote after consent suspends it until the author approves the new wording',
          'A withdrawal link on every testimonial, honored without anyone having to be asked',
          'Approval to publish is its own permission, separate from the right to edit',
        ],
      },
      {
        heading: 'One script tag, or none at all',
        body: 'Pick an arrangement in the backoffice and paste two lines into your site. The embed renders inside a shadow root, so your styles never leak in and ours never leak out, and every part of a card can be restyled with CSS custom properties without forking anything. Prefer to build it yourself? Choose the custom layout and you get the same wall as JSON, open to any origin, containing only what each author agreed to publish.',
        code: {
          language: 'html',
          snippet: '<script async src="https://widget.fidibeki.com/v1/testimonials.js"></script>\n<fidibeki-wall wall="wl_your_wall_id"></fidibeki-wall>',
        },
      },
      {
        heading: 'Video, in the shape it was filmed',
        body: 'A phone held upright records a portrait clip, and most walls letterbox it into a shape it was never in. Fidibeki transcodes each upload to a 720p rendition, keeps its real dimensions, and sizes the player from them, so a portrait recording stays portrait and a landscape one stays landscape. A still frame stands in until playback starts, and no footage reaches a wall until a teammate has watched it.',
      },
    ],
    related: ['feedback-widget', 'reporter-loop', 'ai-analysis'],
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
    tagline: 'Sign in through your identity provider, or turn on two factor and require it.',
    summary:
      'SAML 2.0 single sign on with optional and required modes, and TOTP two factor authentication members enrol in themselves and admins can require across the workspace. Both refuse the configurations that would lock you out.',
    icon: 'shield-check',
    availability: 'Enterprise',
    highlights: [
      'SAML 2.0 SP initiated single sign on against your own provider',
      'Three modes per organization: disabled, optional, or required',
      'Required mode unlocks only after a test login actually succeeds',
      'TOTP two factor with any authenticator app, plus recovery codes',
      'Require two factor workspace wide, with a compliance view of who has enrolled',
      'Admins can reset a locked out member without disabling the policy',
    ],
    sections: [
      {
        heading: 'Bring your own identity provider',
        body: 'Members authenticate through your existing SAML 2.0 provider. Login is service provider initiated, email claims are verified, and each organization chooses its own enforcement: disabled, optional so members may use either route, or required so normal member access goes through your IdP.',
        bullets: [
          'Optional mode lets members choose SAML or a magic link',
          'Required mode routes normal member access through your provider',
          'Under required mode every workspace answers identically, so the login page reveals nothing about who has SSO',
        ],
      },
      {
        heading: 'Required mode has to be earned',
        body: 'You cannot switch an organization to required until a test assertion from your provider has actually succeeded. A typo in a certificate or an entity ID would otherwise lock every member out of a workspace at the moment the setting is saved, and the people who could undo it are the people now locked out.',
      },
      {
        heading: 'Two factor, by the member and by the organization',
        body: 'Any member can enrol a TOTP authenticator from their own security settings and gets recovery codes for the day the phone is gone. An admin can require two factor for the whole workspace: members who have not enrolled are walked through it on their next sign in, and a compliance view shows who is still pending. If required SSO is already on, MFA is left to your identity provider rather than asked for twice.',
        bullets: [
          'Enrolment is per member; enforcement is per organization',
          'Recovery codes are single use and can be regenerated',
          'The policy refuses changes that would lock out the last administrator',
          'Admins can reset one member’s enrolment without turning the policy off',
        ],
      },
    ],
    related: ['workspace-permissions'],
  },
]

export const featureSlugs = features.map((feature) => feature.slug)

export function featureBySlug(slug: string) {
  return features.find((feature) => feature.slug === slug)
}
