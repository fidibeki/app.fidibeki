<script setup lang="ts">
import { Check, Database, KeyRound, Lock, ShieldCheck, Timer, CloudUpload } from '@lucide/vue'

const links = useAppLinks()

useSiteMeta({
  title: 'Security',
  description:
    'How Fidibeki is secured: strict workspace isolation, passwordless auth with rotating tokens, quarantined uploads, encrypted secrets, and rate-limited public endpoints.',
  path: '/security',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Security', path: '/security' },
  ],
})

const pillars = [
  {
    icon: Database,
    title: 'Strict workspace isolation',
    body: 'Each workspace’s data is fully separated from every other customer’s. Isolation is enforced at the data layer itself — not just in application code — so a request from one workspace can never read another workspace’s records.',
  },
  {
    icon: KeyRound,
    title: 'Passwordless authentication',
    body: 'Sign-in uses emailed magic links — there are no passwords to phish or leak. Sessions use short-lived, audience-bound access tokens with hashed, rotating refresh-token cookies. Token reuse outside a narrow rotation window revokes the entire session family.',
  },
  {
    icon: ShieldCheck,
    title: 'Least-privilege permissions',
    body: 'Workspace membership is governed by explicit per-route permissions covering projects, widget rules, feedback, issues, sessions, members, API keys, integrations, and account operations. Invitations can be restricted to verified company email domains.',
  },
  {
    icon: CloudUpload,
    title: 'Quarantined upload pipeline',
    body: 'Attachments upload through short-lived presigned URLs and are verified for byte size and MIME type. Files land quarantined and only become visible after signature (magic-byte) verification. Storage objects stay private — downloads and replay playback stream through the authenticated API, never as direct storage links.',
  },
  {
    icon: Lock,
    title: 'Encrypted integration secrets',
    body: 'Webhook secrets and custom headers are encrypted at rest with AES-256-GCM and displayed exactly once at creation. Outbound webhook deliveries are signed with HMAC-SHA256 so your systems can verify every payload.',
  },
  {
    icon: Timer,
    title: 'Abuse-resistant public endpoints',
    body: 'Public submission endpoints enforce per-IP and per-project rate ceilings, verify the browser origin against your allowed domains, and apply daily AI cost budgets. Limiter identities are short-lived HMAC hashes — diagnostics never persist raw network or visitor data.',
  },
]

const widgetPoints = [
  'Runs in an isolated shadow root — your page styles and the widget never collide',
  'Never reads input values, private page text, or network payloads',
  'No personal fingerprinting — targeting signals are scored locally in the browser',
  'Session replay masks inputs by default and honors per-rule masking policies',
  'Replays are never sent to any AI provider',
]
</script>

<template>
  <div>
    <PageHero
      eyebrow="Trust"
      title="Security"
      lede="Fidibeki holds your customers’ words and your product’s context. Here is precisely how it is protected — no vague badges, just implemented behavior."
    >
      <template #breadcrumbs>
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Security', path: '/security' }]" />
      </template>
    </PageHero>

    <section class="section">
      <div class="container">
        <div class="pillar-grid">
          <div v-for="pillar in pillars" :key="pillar.title" class="card pillar-card">
            <span class="pillar-icon"><component :is="pillar.icon" :size="22" aria-hidden="true" /></span>
            <h2>{{ pillar.title }}</h2>
            <p>{{ pillar.body }}</p>
          </div>
        </div>
      </div>
    </section>

    <section class="section widget-section">
      <div class="container">
        <div class="section-head">
          <h2>The widget on your page</h2>
          <p class="lede">
            The embed script runs on your site, so its behavior is a security question too.
          </p>
        </div>
        <ul class="widget-list">
          <li v-for="point in widgetPoints" :key="point">
            <Check :size="17" aria-hidden="true" class="widget-check" />
            <span>{{ point }}</span>
          </li>
        </ul>
      </div>
    </section>

    <section class="section disclosure-section">
      <div class="container disclosure-inner">
        <h2>What we do not claim</h2>
        <p>
          Fidibeki does not currently hold SOC 2, ISO 27001, or similar certifications, and we
          will not imply otherwise. If your security review needs specific answers, questionnaire
          responses, or roadmap commitments,
          <a :href="links.demo" rel="noopener" target="_blank">talk to us directly</a> — you will get engineering
          answers, not marketing ones.
        </p>
      </div>
    </section>

    <CtaSection
      title="Have a security question?"
      body="Book a demo and bring your security checklist — we would rather answer it before you buy than after."
    />
  </div>
</template>

<style scoped>
.pillar-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1rem;
}

.pillar-card h2 {
  font-size: 1.15rem;
  margin: 0.6rem 0 0.35rem;
}

.pillar-card p {
  margin: 0;
  color: var(--muted);
  font-size: 0.94rem;
}

.pillar-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: 10px;
  background: var(--accent-soft);
  color: var(--accent-text);
}

.widget-section {
  padding-top: 0;
}

.widget-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.8rem;
  max-width: 44rem;
}

.widget-list li {
  display: flex;
  gap: 0.6rem;
  align-items: flex-start;
  color: var(--ink-soft);
  font-size: 0.98rem;
}

.widget-check {
  color: var(--accent);
  flex-shrink: 0;
  margin-top: 0.2rem;
}

.disclosure-section {
  padding-top: 0;
}

.disclosure-inner {
  border-top: 1px solid var(--line);
  padding-top: 2.5rem;
}

.disclosure-inner h2 {
  font-size: 1.15rem;
}

.disclosure-inner p {
  margin: 0;
  max-width: 44rem;
  color: var(--muted);
}

@media (min-width: 760px) {
  .pillar-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (min-width: 1080px) {
  .pillar-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}
</style>
