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
  {
    slug: 'two-factor',
    title: 'Turn on two factor authentication',
    description:
      'Enrol a TOTP authenticator on your own account, then require two factor across the workspace without locking anyone out.',
    minutes: 5,
    sections: [
      {
        heading: 'Enrol your own account',
        body: 'Open Profile, then Security, and start enrolment. Fidibeki shows a QR code for any TOTP app — 1Password, Authy, Google Authenticator, your password manager — and asks for one code to confirm the clocks agree. Enrolment is not active until that code is accepted, so a mis-scanned code cannot lock you out.',
        bullets: [
          'The secret is shown as a QR code and as text, for devices that cannot scan',
          'Confirming with a live code proves the authenticator works before it is trusted',
          'From then on, signing in asks for a six digit code after your magic link',
        ],
      },
      {
        heading: 'Keep the recovery codes',
        body: 'Confirming enrolment produces a set of single use recovery codes. They are shown once and stored only as hashes, so nobody at Fidibeki can read them back to you. Each one signs you in exactly once when the authenticator is not available. Regenerate them from the same screen if you use several or lose the list; regenerating invalidates the old set.',
      },
      {
        heading: 'Require it for everyone',
        body: 'An administrator with tenant:update can turn the workspace policy on under Settings, then Security. Members who have not enrolled are walked through it the next time they sign in, and can do nothing else until they have. A compliance view lists every member and whether they are enrolled, so you can see who is still pending before you switch it on.',
        bullets: [
          'Enrolment is a member action; requiring it is an organization one',
          'Members mid-enrolment are held at the challenge, not signed out',
          'The compliance view is a read on members, so it takes member:read',
        ],
      },
      {
        heading: 'What the policy refuses to do',
        body: 'The policy will not accept a change that locks the workspace out of its own administration — requiring two factor when no administrator has enrolled is rejected rather than applied. If required SAML SSO is already on, Fidibeki does not ask for a second factor of its own: multi factor is your identity provider’s to enforce, and asking twice would only train people to click through it.',
      },
      {
        heading: 'When someone loses their phone',
        body: 'They can sign in with a recovery code and re-enrol. If the codes are gone too, an administrator with member:update resets that member’s enrolment from the member list. The reset clears only that person, leaving the workspace policy in force, and they are asked to enrol again on their next sign in.',
      },
    ],
  },
]

export const docSlugs = docs.map((doc) => doc.slug)

export function docBySlug(slug: string) {
  return docs.find((doc) => doc.slug === slug)
}
