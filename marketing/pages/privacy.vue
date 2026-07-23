<script setup lang="ts">
useSiteMeta({
  title: 'Privacy',
  description:
    'How Fidibeki treats data in plain language: what the widget collects and refuses to collect, retention windows, reporter rights, exports, and deletion.',
  path: '/privacy',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Privacy', path: '/privacy' },
  ],
})

const sections = [
  {
    heading: 'What the widget collects — and refuses to',
    body: 'The embedded widget collects what a reporter deliberately submits: their message, optional rating or reaction, and any attachments they choose to add. Targeting decisions are computed locally in the browser from interaction signals like active time and scroll depth.',
    bullets: [
      'Never reads input values, private page text, or network payloads',
      'Never constructs a personal fingerprint of a visitor',
      'Session replay, where enabled, masks inputs by default',
      'Replays are never sent to any AI provider',
    ],
  },
  {
    heading: 'Retention is enforced, not aspirational',
    body: 'Data ages out on a schedule that runs daily. Replays and attachments follow your plan’s storage window (30, 90, or 365 days). Raw analytics events are retained for 90 days; daily aggregates for two years. Cleanup jobs delete both database records and the underlying storage objects.',
  },
  {
    heading: 'Reporters have real rights',
    body: 'People who submit feedback can act on their own data without creating an account. Reporter access links are random tokens stored only as hashes — rate-limited, non-enumerable, and expiring.',
    bullets: [
      'Opted-in email updates carry a working unsubscribe link in every message',
      'Reporters can request an export of their own submissions via a verified flow',
      'Secure progress pages show issue status without exposing workspace internals',
    ],
  },
  {
    heading: 'Workspace exports omit the dangerous stuff',
    body: 'Workspace data exports are built from explicit allowlists. They omit credentials, token hashes, API keys, session records, magic links, billing identifiers, queue payloads, and raw binaries. Export download links are signed and expire after ten minutes.',
  },
  {
    heading: 'Deletion is deliberate and complete',
    body: 'Deleting a workspace requires typing its name, then starts a seven-day read-only grace period during which the deletion can be cancelled. Execution proceeds only after billing cancellation and storage cleanup succeed, so a deleted workspace is actually gone — not soft-hidden.',
  },
  {
    heading: 'Who processes data on our behalf',
    body: 'Fidibeki runs on managed infrastructure and uses a small set of processors to operate: payment processing (Stripe), transactional email (Resend), error telemetry (Sentry), bot protection on signup (Cloudflare Turnstile), and — only on plans with AI analysis enabled, and never for session replays — AI processing (Google Gemini).',
  },
  {
    heading: 'Analytics on this website',
    body: 'This marketing site uses Umami, a privacy-friendly analytics tool: no cookies, no personal data, no cross-site tracking. It only runs if you allow it in the consent banner, and declining is remembered just as well as accepting.',
  },
]
</script>

<template>
  <div>
    <PageHero
      eyebrow="Trust"
      title="Privacy"
      lede="A plain-language account of how Fidibeki treats data — what is collected, how long it lives, and who can get it out."
    >
      <template #breadcrumbs>
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Privacy', path: '/privacy' }]" />
      </template>
    </PageHero>

    <section class="section">
      <div class="container">
        <DetailSections :sections="sections" />
      </div>
    </section>

    <CtaSection
      title="Questions about data handling?"
      body="Ask us directly — the people answering are the people who built the retention jobs."
    />
  </div>
</template>

