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
        body: 'Each Slack destination chooses exactly which events it cares about — new feedback, new issues, updates, or resolutions. Messages use Slack Block Kit so activity is scannable at a glance, and every delivery attempt is retried automatically and recorded in delivery logs.',
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
      'Teams destinations deliver MessageCard payloads for the feedback and issue events you subscribe to, with the same retry and delivery-log guarantees as every destination.',
    icon: 'users',
    availability: 'Scale & Enterprise',
    events: destinationEvents,
    sections: [
      {
        heading: 'Native-feeling cards',
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
    slug: 'slack-actions',
    name: 'Interactive Slack & Teams actions',
    status: 'planned',
    tagline: 'Triage from the channel — planned, not yet available.',
    summary:
      'Acting on feedback directly from Slack or Teams messages — changing status or claiming an issue without opening Fidibeki — is on the roadmap but not shipped yet.',
    icon: 'wand-sparkles',
    availability: 'Planned',
    sections: [
      {
        heading: 'What we are considering',
        body: 'Today’s integrations are notification destinations. Interactive actions inside chat messages are a documented later extension — we would rather ship them properly than half-way. If this matters to your team, tell us when you book a demo.',
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
