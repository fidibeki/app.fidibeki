<script setup lang="ts">
import {
  Check,
  Database,
  KeyRound,
  Lock,
  ShieldCheck,
  Timer,
  CloudUpload,
  ScrollText,
} from '@lucide/vue'
import { onMounted, onUnmounted, ref } from 'vue'

const activeId = ref('infrastructure')
let observer: IntersectionObserver | null = null

const securityHeadings = [
  { id: 'infrastructure', label: 'Infrastructure' },
  { id: 'encryption', label: 'Encryption' },
  { id: 'application-security', label: 'Application security' },
  { id: 'incident-response', label: 'Incident response' },
  { id: 'certifications', label: 'Certifications' },
]

onMounted(() => {
  observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          activeId.value = entry.target.id
        }
      }
    },
    {
      rootMargin: '-20% 0px -70% 0px',
      threshold: 0,
    }
  )
  for (const heading of securityHeadings) {
    const element = document.getElementById(heading.id)
    if (element) observer.observe(element)
  }
})

onUnmounted(() => {
  observer?.disconnect()
})

const links = useAppLinks()

useSiteMeta({
  title: 'Security',
  description:
    'How Fidibeki is secured: strict workspace isolation, passwordless authentication with rotating tokens, quarantined uploads, encrypted secrets, rate limited public endpoints, and an honest view of what we do not yet claim.',
  path: '/security',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Security', path: '/security' },
  ],
})

const pillars = [
  {
    icon: ScrollText,
    title: 'Audit trail for sensitive access',
    body: 'Every view and download of attachments, replay, exports, and other sensitive support data is logged with actor, timestamp, and purpose. We are building this so your team can prove who accessed what, and when.',
  },
  {
    icon: KeyRound,
    title: 'Passwordless authentication',
    body: 'Sign in uses emailed magic links. There are no passwords to phish or leak. Sessions use brief tokens scoped to the audience and rotating refresh cookies with reuse detection. Token reuse outside a narrow rotation window revokes the entire session family.',
  },
  {
    icon: ShieldCheck,
    title: 'Least privilege permissions',
    body: 'Workspace membership is governed by explicit per route permissions covering projects, widget rules, feedback, issues, sessions, members, API keys, integrations, and account operations. Invitations can be restricted to verified company email domains.',
  },
  {
    icon: CloudUpload,
    title: 'Quarantined upload pipeline',
    body: 'Attachments upload through brief presigned URLs and are verified for byte size and MIME type. Files land quarantined and only become visible after signature verification. Storage objects stay private. Downloads and replay playback stream through the authenticated API, never as direct storage links.',
  },
  {
    icon: Lock,
    title: 'Encrypted integration secrets',
    body: 'Webhook secrets and custom headers are encrypted at rest with AES-256-GCM and displayed exactly once at creation. Outbound webhook deliveries are signed with HMAC-SHA256 so your systems can verify every payload.',
  },
  {
    icon: Timer,
    title: 'Abuse resistant public endpoints',
    body: 'Public submission endpoints enforce per IP and per project rate ceilings, verify the browser origin against your allowed domains, and apply daily AI cost budgets. Limiter identities are brief HMAC hashes. Diagnostics never persist raw network or visitor data.',
  },
]

const widgetPoints = [
  'Runs in an isolated shadow root. Your page styles and the widget never collide',
  'Never reads input values, private page text, or network payloads',
  'No personal fingerprinting. Targeting signals are scored locally in the browser',
  'Session replay masks inputs by default and honors per rule masking policies',
  'Replays are never sent to any AI provider',
]
</script>

<template>
  <div>
    <PageHero
      eyebrow="Trust"
      title="Security"
      lede="Fidibeki holds your customers’ words and your product’s context. Here is precisely how it is protected: no vague badges, just implemented behavior."
    >
      <template #breadcrumbs>
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Security', path: '/security' }]" />
      </template>
    </PageHero>

    <section class="section">
      <div class="container">
        <div class="pillar-grid">
          <div
            v-for="pillar in pillars"
            :key="pillar.title"
            class="card pillar-card"
          >
            <span class="pillar-icon"
              ><component :is="pillar.icon" :size="22" aria-hidden="true" /></span
            >
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

    <section class="section compliance-section">
      <div class="container compliance-layout">
        <nav class="compliance-nav" aria-label="Security sections">
          <div class="compliance-nav-sticky">
            <ul class="compliance-nav-list">
              <li v-for="heading in securityHeadings" :key="heading.id">
                <a :href="`#${heading.id}`" :class="{ active: activeId === heading.id }">{{ heading.label }}</a>
              </li>
            </ul>
          </div>
        </nav>

        <div class="prose compliance-notice">
          <details class="last-updated">
            <summary>Last updated: 23 July 2026</summary>
            <p>
              This is the current version of the security page. Previous versions will be
              archived here when the page changes materially.
            </p>
          </details>

          <h2 id="infrastructure">Infrastructure and access</h2>
          <p>
            Fidibeki runs on managed cloud infrastructure. We do not own or have physical access to
            the underlying data centers. Production access is limited to designated operators, uses
            multi factor authentication, and is logged. We deploy through an automated pipeline with
            required review, continuous integration, and dependency scanning.
          </p>

          <h2 id="encryption">Encryption and data protection</h2>
          <ul>
            <li>All traffic between browsers, APIs, and integrations uses TLS.</li>
            <li>Integration secrets and webhook headers are encrypted at rest with AES-256-GCM.</li>
            <li>Object storage is private by default; direct links are never exposed to users.</li>
            <li>Session replay and attachment playback stream through the authenticated API.</li>
          </ul>

          <h2 id="application-security">Application security</h2>
          <ul>
            <li>All code changes are reviewed before merging;</li>
            <li>Automated tests, linting, and vulnerability scanning run in CI;</li>
            <li>No production secrets are stored in source code;</li>
            <li>Error telemetry is scrubbed to avoid collecting PII or secrets.</li>
          </ul>

          <h2 id="incident-response">Incident response and disclosure</h2>
          <p>
            We maintain an incident response runbook. For personal data breaches, we intend to
            notify affected customer/controllers without undue delay under the approved DPA once the
            breach workflow is complete. To report a security issue or vulnerability, email us at
            <a href="mailto:security@fidibeki.com">security@fidibeki.com</a>. Please include a
            proof of concept and do not test against customer workspaces or production data.
          </p>

          <h2 id="certifications">Certifications and compliance claims</h2>
          <p>
            Fidibeki does not currently hold SOC 2, ISO 27001, or similar certifications, and we
            will not imply otherwise. If your security review needs specific answers, questionnaire
            responses, or roadmap commitments,
            <a :href="links.demo" rel="noopener" target="_blank">book a demo</a>.
          </p>
        </div>
      </div>
    </section>

    <CtaSection
      title="Have a security question?"
      body="Book a demo and bring your security checklist. We would rather answer it before you buy than after."
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

.compliance-section {
  padding-top: 2rem;
  background: var(--surface-soft);
}

.compliance-layout {
  display: grid;
  gap: 2rem;
}

.compliance-nav {
  order: -1;
}

.compliance-nav-sticky {
  position: sticky;
  top: calc(var(--header-height, 72px) + 1.5rem);
}

.compliance-nav-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.compliance-nav-list a {
  display: block;
  padding: 0.35rem 0.65rem;
  border-radius: 999px;
  background: var(--surface);
  border: 1px solid var(--line);
  color: var(--ink-soft);
  font-size: 0.85rem;
  text-decoration: none;
  white-space: nowrap;
}

.compliance-nav-list a:hover,
.compliance-nav-list a.active {
  border-color: var(--accent);
  color: var(--accent-text);
}

.compliance-notice {
  padding-top: 0.25rem;
}

.last-updated {
  font-size: 0.85rem;
  color: var(--muted);
  margin-bottom: 1.8em;
}

.last-updated summary {
  cursor: pointer;
  list-style: none;
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  font-weight: 500;
}

.last-updated summary::marker,
.last-updated summary::-webkit-details-marker {
  display: none;
}

.last-updated summary::after {
  content: '›';
  display: inline-block;
  transform: rotate(90deg);
  transition: transform 160ms ease;
  color: var(--accent-text);
}

.last-updated[open] summary::after {
  transform: rotate(-90deg);
}

.last-updated p {
  margin-top: 0.6rem;
  max-width: 44rem;
}

.compliance-notice h2 {
  font-size: 1.35rem;
  margin-top: 2.2em;
}

.compliance-notice h3 {
  font-size: 1.1rem;
  margin-top: 1.6em;
  color: var(--ink-soft);
}

.compliance-notice p,
.compliance-notice ul {
  color: var(--ink-soft);
}

.compliance-notice li {
  margin-bottom: 0.45em;
}

.compliance-notice p,
.compliance-notice ul {
  color: var(--ink-soft);
}

.compliance-notice li {
  margin-bottom: 0.45em;
}

@media (min-width: 900px) {
  .compliance-layout {
    grid-template-columns: 14rem 1fr;
    gap: 3rem;
  }

  .compliance-nav {
    order: 0;
  }

  .compliance-nav-list {
    flex-direction: column;
    gap: 0.15rem;
  }

  .compliance-nav-list a {
    background: transparent;
    border: none;
    border-radius: 0;
    padding: 0.35rem 0;
    white-space: normal;
  }

  .compliance-nav-list a.active {
    text-decoration: underline;
    text-underline-offset: 0.2em;
  }

  .compliance-nav-list a:hover {
    text-decoration: underline;
    text-underline-offset: 0.2em;
  }
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
