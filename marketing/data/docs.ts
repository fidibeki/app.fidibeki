import type { DocEntry } from './types'

export const docs: DocEntry[] = [
  {
    slug: 'installation',
    title: 'Install the widget',
    description:
      'Create a project, add your allowed domains, and embed the Fidibeki loader with one async script tag.',
    minutes: 5,
    sections: [
      {
        heading: '1. Create a project',
        body: 'Sign in to your workspace and create a project. Each project owns its allowed domains and a public widget key starting with pk_live_. The key is safe to expose in your HTML. The API additionally checks the browser Origin against your allowed domains.',
      },
      {
        heading: '2. Allow your domains',
        body: 'Add the exact hosts your product runs on. Leading wildcards are supported for preview environments.',
        bullets: [
          'app.example.com, exact host',
          '*.preview.example.com, any preview subdomain',
          'Requests from origins outside this list are rejected',
        ],
      },
      {
        heading: '3. Embed the loader',
        body: 'Paste the loader script into your page. It loads asynchronously, renders inside a shadow root, and never blocks your page load.',
        code: {
          language: 'html',
          snippet:
            '<script src="https://widget.fidibeki.com/v1/loader.js" data-fidibeki-key="pk_live_..." async><\/script>',
        },
      },
      {
        heading: '4. Verify the connection',
        body: 'Open your site and the getting started page in your workspace. Fidibeki detects the first live widget connection automatically and confirms the installation, so you know the loader is talking to your project before you configure anything else.',
      },
    ],
  },
  {
    slug: 'widget-rules',
    title: 'Configure widget rules',
    description:
      'Presentations, targeting, objectives, response blocks, attachments, replay, and appearance. Everything a widget rule controls.',
    minutes: 8,
    sections: [
      {
        heading: 'Rules are versioned configuration',
        body: 'A project holds one or more widget rules. Each rule combines a URL matcher, a trigger, a presentation, response blocks, and appearance settings. Rules move through draft, active, paused, and archived states, and invalid settings return precise field-level errors when you save.',
      },
      {
        heading: 'Presentations',
        body: 'Choose how the widget appears for each rule.',
        bullets: [
          'Launcher, a floating button that opens the feedback panel',
          'Inline, embedded directly inside your page content',
          'Reaction, a lightweight reaction bar for quick signals',
          'Prompt, a proactive ask, shown only with enough engagement evidence',
        ],
      },
      {
        heading: 'Objectives and the local policy',
        body: 'Automatic rules pair a trigger with an objective: continuous feedback, after engagement, validate content, or detect friction. A deterministic policy running locally in the browser scores active time, scroll depth, interaction counts, and anonymous per-page visits. You can also assert signals explicitly with boolean attributes on the html element: data-fidibeki-engaged, data-fidibeki-content, and data-fidibeki-friction.',
      },
      {
        heading: 'Replay settings',
        body: 'On plans with session replay, a rule can offer recording, but the reporter’s browser must support rrweb and the reporter must be in the sampled session. Inputs are masked by default, recording stops at strict limits, and playback is only available to authenticated teammates with the session permission.',
      },
    ],
  },
  {
    slug: 'webhooks',
    title: 'Verify webhook deliveries',
    description:
      'Subscribe destinations to events and verify the HMAC SHA256 signature on every delivery.',
    minutes: 6,
    sections: [
      {
        heading: 'Events',
        body: 'Each destination subscribes to the events it needs. One deterministic delivery task is created per subscribed destination, retried with backoff, and recorded in delivery logs.',
        bullets: ['feedback.created', 'issue.created', 'issue.updated', 'issue.resolved'],
      },
      {
        heading: 'Verify the signature',
        body: 'Every generic webhook body includes a stable delivery ID and is signed with your destination secret. Compute an HMAC SHA256 of the raw request body and compare it in constant time to the header value.',
        code: {
          language: 'js',
          snippet:
            "import { createHmac, timingSafeEqual } from 'node:crypto'\n\nfunction verify(secret, rawBody, header) {\n  const expected = 'sha256=' +\n    createHmac('sha256', secret).update(rawBody).digest('hex')\n  return timingSafeEqual(Buffer.from(expected), Buffer.from(header))\n}",
        },
      },
      {
        heading: 'Secrets and headers',
        body: 'Destination secrets and custom headers are stored encrypted with AES-256-GCM and shown exactly once at creation. Rotate a secret by recreating the destination; deduplicate replays on the stable delivery ID.',
      },
    ],
  },
]

export const docSlugs = docs.map((doc) => doc.slug)

export function docBySlug(slug: string) {
  return docs.find((doc) => doc.slug === slug)
}
