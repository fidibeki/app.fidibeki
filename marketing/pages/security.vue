<script setup lang="ts">
const { copy } = useMarketingI18n()

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
    },
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
      :eyebrow="copy('Trust')"
      :title="copy('Security')"
      :lede="
        copy(
          'Fidibeki holds your customers’ words and your product’s context. Here is precisely how it is protected: no vague badges, just implemented behavior.',
        )
      "
    >
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Security', path: '/security' },
          ]"
        />
      </template>
    </PageHero>

    <section class="section">
      <div class="container">
        <div class="pillar-grid">
          <div
            v-for="pillar in pillars"
            :key="pillar.title"
            class="pillar-card"
          >
            <span class="pillar-icon"
              ><component :is="pillar.icon" :size="22" aria-hidden="true"
            /></span>
            <h2>{{ copy(pillar.title) }}</h2>
            <p>{{ copy(pillar.body) }}</p>
          </div>
        </div>
      </div>
    </section>

    <section class="section widget-section">
      <div class="container">
        <div class="section-head">
          <h2>{{ copy('The widget on your page') }}</h2>
          <p class="lede">
            {{
              copy(
                'The embed script runs on your site, so its behavior is a security question too.',
              )
            }}
          </p>
        </div>
        <ul class="widget-list">
          <li v-for="point in widgetPoints" :key="point">
            <Check :size="17" aria-hidden="true" class="widget-check" />
            <span>{{ copy(point) }}</span>
          </li>
        </ul>
      </div>
    </section>

    <section class="section compliance-section">
      <div class="container compliance-layout">
        <nav class="compliance-nav" :aria-label="copy('Security sections')">
          <div class="compliance-nav-sticky">
            <ul class="compliance-nav-list">
              <li v-for="heading in securityHeadings" :key="heading.id">
                <a
                  :href="`#${heading.id}`"
                  :class="{ active: activeId === heading.id }"
                  >{{ copy(heading.label) }}</a
                >
              </li>
            </ul>
          </div>
        </nav>

        <div class="prose compliance-notice">
          <details class="last-updated">
            <summary>{{ copy('Last updated: 26 July 2026') }}</summary>
            <p>
              {{
                copy(
                  'This is the current version of the security page. Previous versions will be archived here when the page changes materially.',
                )
              }}
            </p>
          </details>

          <h2 id="infrastructure">{{ copy('Infrastructure and access') }}</h2>
          <p>
            {{
              copy(
                'Fidibeki runs on managed cloud infrastructure. We do not own or have physical access to the underlying data centers. Production access is limited to designated operators, uses multi factor authentication, and is logged. We deploy through an automated pipeline with required review, continuous integration, and dependency scanning.',
              )
            }}
          </p>

          <h2 id="encryption">{{ copy('Encryption and data protection') }}</h2>
          <ul>
            <li>
              {{
                copy(
                  'All traffic between browsers, APIs, and integrations uses TLS.',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Integration secrets and webhook headers are encrypted at rest with AES-256-GCM.',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Object storage is private by default; direct links are never exposed to users.',
                )
              }}
            </li>
            <li>
              {{
                copy(
                  'Session replay and attachment playback stream through the authenticated API.',
                )
              }}
            </li>
          </ul>

          <h2 id="application-security">{{ copy('Application security') }}</h2>
          <ul>
            <li>{{ copy('All code changes are reviewed before merging;') }}</li>
            <li>
              {{
                copy(
                  'Automated tests, linting, and vulnerability scanning run in CI;',
                )
              }}
            </li>
            <li>
              {{ copy('No production secrets are stored in source code;') }}
            </li>
            <li>
              {{
                copy(
                  'Error telemetry is scrubbed to avoid collecting PII or secrets.',
                )
              }}
            </li>
          </ul>

          <h2 id="incident-response">
            {{ copy('Incident response and disclosure') }}
          </h2>
          <p>
            {{
              copy(
                'We maintain an incident response runbook and an internal breach register that records incident facts, assessments, chronology, and containment behind a platform-administration boundary. For personal data breaches, we notify affected customer/controllers without undue delay, and what we owe you is set out in the',
              )
            }}
            <NuxtLinkLocale to="/dpa">{{
              copy('Data Processing Addendum')
            }}</NuxtLinkLocale
            >{{
              copy('. To report a security issue or vulnerability, email us at')
            }}
            <a href="mailto:security@fidibeki.com">{{
              copy('security@fidibeki.com')
            }}</a
            >{{
              copy(
                '. Please include a proof of concept and do not test against customer workspaces or production data.',
              )
            }}
          </p>

          <h2 id="certifications">
            {{ copy('Certifications and compliance claims') }}
          </h2>
          <p>
            {{
              copy(
                'Fidibeki does not currently hold SOC 2, ISO 27001, or similar certifications, and we will not imply otherwise. If your security review needs specific answers, questionnaire responses, or roadmap commitments,',
              )
            }}
            <a :href="links.demo" rel="noopener" target="_blank">{{
              copy('book a demo')
            }}</a
            >.
          </p>
        </div>
      </div>
    </section>

    <CtaSection
      :title="copy('Have a security question?')"
      :body="
        copy(
          'Book a demo and bring your security checklist. We would rather answer it before you buy than after.',
        )
      "
    />
  </div>
</template>

<style scoped>
.pillar-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 40px 80px;
}
.pillar-card {
  border-top: 1px solid var(--line);
  padding-top: 24px;
}
.pillar-icon {
  display: inline-flex;
  margin-bottom: 24px;
}
.pillar-card h2 {
  font-size: 23px;
  margin-bottom: 16px;
}
.pillar-card p {
  color: var(--muted);
  font-size: 15px;
  margin: 0;
}
.widget-section {
  padding-top: 24px;
}
.widget-list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-width: 704px;
  display: grid;
  gap: 20px;
}
.widget-list li {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  color: var(--ink-soft);
}
.widget-check {
  flex-shrink: 0;
  margin-top: 5px;
}
.compliance-section {
  padding-top: 24px;
}
@media (max-width: 700px) {
  .pillar-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 32px;
  }
}
</style>
