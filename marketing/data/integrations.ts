import type { IntegrationEntry } from './types'

const destinationEvents = [
  'feedback.created',
  'issue.created',
  'issue.updated',
  'issue.resolved',
]

export const integrations: IntegrationEntry[] = [
  {
    slug: 'slack',
    name: 'Slack',
    status: 'available',
    tagline: 'Feedback and issue activity, posted where your team already lives.',
    summary:
      'Connect Slack destinations that receive rich Block Kit notifications whenever feedback arrives or issues move, with per-destination event subscriptions.',
    icon: 'hash',
    availability: 'Scale & Enterprise',
    events: destinationEvents,
    sections: [
      {
        heading: 'Rich messages, deliberate volume',
        body: 'Each Slack destination chooses exactly which events it cares about: new feedback, new issues, updates, or resolutions. Messages use Slack Block Kit so activity is scannable at a glance, and every delivery attempt is retried automatically and recorded in delivery logs.',
      },
      {
        heading: 'Set up in minutes',
        body: 'Create a destination from your workspace integration settings, paste the webhook URL for your channel, and pick the events to subscribe to. Secrets are encrypted at rest with AES-256-GCM and shown only once.',
      },
    ],
  },
  {
    slug: 'microsoft-teams',
    name: 'Microsoft Teams',
    status: 'available',
    tagline: 'Keep Teams channels current on what customers are telling you.',
    summary:
      'Teams destinations deliver MessageCard payloads for the feedback and issue events you subscribe to, with the same retry and delivery log guarantees as every destination.',
    icon: 'users',
    availability: 'Scale & Enterprise',
    events: destinationEvents,
    sections: [
      {
        heading: 'Native feeling cards',
        body: 'Notifications arrive as MessageCard payloads that render cleanly in Teams channels. Subscriptions are per destination, so a support channel can watch new feedback while an engineering channel only hears about resolved issues.',
      },
      {
        heading: 'Reliable delivery',
        body: 'Every HTTP attempt is isolated and retried by the background queue without ever blocking feedback ingestion. Delivery logs show each attempt, so silence is always diagnosable.',
      },
    ],
  },
  {
    slug: 'webhooks',
    name: 'Webhooks',
    status: 'available',
    tagline: 'A signed event stream for anything you want to build.',
    summary:
      'Generic webhook destinations POST signed JSON for subscribed events to any endpoint, with stable delivery IDs, custom headers, and HMAC-SHA256 signatures you can verify.',
    icon: 'webhook',
    availability: 'Scale & Enterprise',
    events: destinationEvents,
    sections: [
      {
        heading: 'Built for verification',
        body: 'Every delivery includes a stable delivery ID and is signed with your destination secret as X-Fidibeki-Signature: sha256=<hex HMAC-SHA256 of the body>. Verify the signature, deduplicate on the delivery ID, and you have an exactly-once pipeline into your own systems.',
        code: {
          language: 'text',
          snippet:
            'X-Fidibeki-Signature: sha256=1b2f…\n\nsignature = HMAC_SHA256(destination_secret, raw_request_body)',
        },
      },
      {
        heading: 'Your secret stays yours',
        body: 'Webhook secrets and custom headers are stored encrypted with AES-256-GCM and are displayed exactly once at creation. Deliveries are retried with backoff and recorded in delivery logs, independent of the submission path.',
      },
    ],
  },
  {
    slug: 'jira',
    name: 'Jira',
    status: 'available',
    tagline: 'Push a Fidibeki issue into a Jira project without leaving triage.',
    summary:
      'Connect your Jira Cloud site with an API token, pick the project and issue type, and create Jira issues from Fidibeki issues, by hand or automatically. The external key comes back and stays linked to the report it came from.',
    icon: 'hash',
    availability: 'All plans',
    sections: [
      {
        heading: 'Connect once, per workspace',
        body: 'A workspace administrator supplies the Jira Cloud site URL, the account email, and an API token. Fidibeki validates them against your site before saving, so a wrong token fails at the point you can still fix it rather than the first time somebody tries to file an issue. Credentials are stored encrypted and never returned by the API.',
        bullets: [
          'Jira Cloud REST v3, against your own site URL',
          'Credentials verified at connection time, not at first use',
          'Available projects and issue types are read from your site',
        ],
      },
      {
        heading: 'By hand, or automatically',
        body: 'From an issue, create the Jira counterpart and choose the project and issue type. Or switch automatic creation on and every new Fidibeki issue gets one, created in the background so triage never waits on Jira being slow. Either way the external key is stored against the issue, so the two stay connected.',
      },
      {
        heading: 'When Jira says no',
        body: 'Provider failures are translated into something a workspace administrator can act on — an expired token, a project that no longer exists, a rate limit — rather than a raw status code. Retryable failures are retried with backoff; the rest are surfaced with the reason.',
      },
    ],
  },
  {
    slug: 'linear',
    name: 'Linear',
    status: 'available',
    tagline: 'Turn a Fidibeki issue into a Linear issue, with the report still attached.',
    summary:
      'Connect Linear with an API key, choose the team, and create Linear issues from Fidibeki issues by hand or automatically. Priority carries across and the Linear identifier stays linked to the feedback behind it.',
    icon: 'hash',
    availability: 'All plans',
    sections: [
      {
        heading: 'Connect once, per workspace',
        body: 'A workspace administrator supplies a Linear API key. Fidibeki validates it before saving and reads back the teams it can file into, so the team picker offers what your key can actually reach rather than a list you have to type from memory.',
        bullets: [
          'Linear GraphQL API with your own API key',
          'Key verified at connection time, not at first use',
          'Teams are read from Linear rather than entered by hand',
        ],
      },
      {
        heading: 'By hand, or automatically',
        body: 'Create the Linear issue from a Fidibeki issue and pick the team, or turn automatic creation on and let every new issue get one in the background. The Linear identifier is stored against the Fidibeki issue, so the report that motivated the work is one click away from the work.',
      },
      {
        heading: 'When Linear says no',
        body: 'Provider failures are translated into something a workspace administrator can act on — a revoked key, a team that no longer exists, a rate limit — rather than a raw status code. Retryable failures are retried with backoff; the rest are surfaced with the reason.',
      },
    ],
  },
  {
    slug: 'slack-actions',
    name: 'Interactive Slack & Teams actions',
    status: 'planned',
    tagline: 'Triage from the channel. Planned, not yet available.',
    summary:
      'Acting on feedback directly from Slack or Teams messages, changing status or claiming an issue without opening Fidibeki, is on the roadmap but not shipped yet.',
    icon: 'wand-sparkles',
    availability: 'Planned',
    sections: [
      {
        heading: 'What we are considering',
        body: 'Today’s integrations are notification destinations. Interactive actions inside chat messages are a documented later extension, because we would rather ship them properly than half-way. If this matters to your team, tell us when you book a demo.',
      },
    ],
  },
]

export const integrationSlugs = integrations.map((integration) => integration.slug)

export function integrationBySlug(slug: string) {
  return integrations.find((integration) => integration.slug === slug)
}

export const availableIntegrations = integrations.filter(
  (integration) => integration.status === 'available',
)

export const plannedIntegrations = integrations.filter(
  (integration) => integration.status === 'planned',
)
