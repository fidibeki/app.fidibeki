<script setup lang="ts">
import { ArrowRight, BookOpen, Briefcase, Check, Clock, LifeBuoy, ScrollText, ShieldCheck } from '@lucide/vue'
import { contactChannels } from '~/data/site'
import { trialDays } from '~/data/pricing'

// One icon per destination. Four identical envelopes would make the reader
// parse the labels to tell the routes apart.
const channelIcons = {
  support: LifeBuoy,
  security: ShieldCheck,
  privacy: ScrollText,
  sales: Briefcase,
} as const

const links = useAppLinks()

useSiteMeta({
  title: 'Support',
  description:
    'How to get help with Fidibeki: read the guides, write to the right address, and know what to include so the first reply is the useful one.',
  path: '/support',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Support', path: '/support' },
  ],
})

const startingPoints = [
  {
    to: '/docs/installation',
    title: 'Install the widget',
    body: 'Allowed domains, the script tag, and why the loader refuses to run on an origin you have not listed.',
  },
  {
    to: '/docs/widget-rules',
    title: 'Widget rules and targeting',
    body: 'Why a rule did or did not fire, how interaction signals are computed, and what consent gates each capability.',
  },
  {
    to: '/docs/webhooks',
    title: 'Webhooks',
    body: 'Signature verification, retries, and how to tell a delivery failure from a signing mistake.',
  },
]

const includeInReport = [
  'Your workspace subdomain, and the project the problem belongs to.',
  'What you expected to happen, and what happened instead.',
  'For widget problems: the browser, its version, and the operating system.',
  'The rule involved, if the problem is about when the widget appears.',
  'A feedback or issue link from your workspace, if you already have one.',
  'A screenshot. It is almost always faster than the paragraph describing it.',
]
</script>

<template>
  <div>
    <PageHero
      eyebrow="Support"
      title="Help, and how to get it quickly"
      lede="Most questions have a written answer already. For the rest, there is a real person at the other end of the address — and a short list of details that turns one slow exchange into one useful reply."
    >
      <template #breadcrumbs>
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Support', path: '/support' }]" />
      </template>
    </PageHero>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <h2>Start with the guides</h2>
          <p class="lede">
            These three cover most of what people write in about. They are short, and they are
            honest about the parts that trip people up.
          </p>
        </div>

        <div class="start-grid">
          <NuxtLink
            v-for="(item, index) in startingPoints"
            :key="item.to"
            v-reveal="index * 60"
            :to="item.to"
            class="card start-card"
          >
            <span class="start-icon"><BookOpen :size="20" aria-hidden="true" /></span>
            <h3>{{ item.title }}</h3>
            <p>{{ item.body }}</p>
            <span class="start-meta">Read the guide <ArrowRight :size="14" aria-hidden="true" /></span>
          </NuxtLink>
        </div>

        <p class="start-all">
          Everything else lives in <NuxtLink to="/docs">the documentation</NuxtLink>.
        </p>
      </div>
    </section>

    <section class="section channels-section">
      <div class="container">
        <div class="section-head">
          <h2>Write to the right address</h2>
          <p class="lede">
            Four mailboxes, so your message lands with whoever can actually act on it. If you are
            unsure, use support and we will route it.
          </p>
        </div>

        <div class="channel-grid">
          <div
            v-for="(channel, index) in contactChannels"
            :key="channel.key"
            v-reveal="index * 60"
            class="card channel-card"
          >
            <span class="channel-icon">
              <component :is="channelIcons[channel.key]" :size="18" aria-hidden="true" />
            </span>
            <h3>{{ channel.label }}</h3>
            <p>{{ channel.blurb }}</p>
            <a :href="`mailto:${channel.email}`" class="channel-email">{{ channel.email }}</a>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container support-columns">
        <div class="card include-card">
          <h2>What to include</h2>
          <p class="include-intro">
            None of this is mandatory. All of it removes a round trip.
          </p>
          <ul class="include-list">
            <li v-for="item in includeInReport" :key="item">
              <Check :size="16" aria-hidden="true" class="include-check" />
              <span>{{ item }}</span>
            </li>
          </ul>
        </div>

        <div class="card expect-card">
          <span class="expect-icon"><Clock :size="20" aria-hidden="true" /></span>
          <h2>What to expect</h2>
          <p>
            We aim to reply within one business day. That is a target we work to, not a contractual
            service level — self-serve plans do not carry one, and we would rather say so than
            publish a number we cannot stand behind. Enterprise agreements can include a commitment.
          </p>
          <p>
            When something is broken, we will tell you it is broken. When something is not going to
            be fixed soon, we will tell you that too, rather than leaving the thread open and quiet.
          </p>
          <p class="expect-note">
            Security reports follow their own path — see
            <NuxtLink to="/security">how Fidibeki is secured</NuxtLink> before you send one.
          </p>
          <div class="expect-status">
            <span class="expect-status-label">Before you write, check live status:</span>
            <StatusPill variant="block" />
          </div>
        </div>
      </div>
    </section>

    <section class="section trial-section">
      <div class="container">
        <div class="card trial-card">
          <h2>Not a customer yet?</h2>
          <p>
            Setup questions before you buy are still support questions. Start the
            {{ trialDays }}-day trial and write in, or bring your list to a demo and we will install
            the widget with you.
          </p>
          <div class="trial-actions">
            <NuxtLink to="/sign-up" class="btn btn-primary">Start free trial</NuxtLink>
            <a :href="links.demo" class="btn btn-ghost" rel="noopener" target="_blank">Book a demo</a>
          </div>
        </div>
      </div>
    </section>

    <CtaSection
      title="Still stuck?"
      body="Write to support@fidibeki.com. The person answering is the person who built the thing you are asking about."
    />
  </div>
</template>

<style scoped>
.start-grid,
.channel-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1rem;
}

.start-card {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  color: var(--ink);
  transition: border-color 160ms ease, transform 160ms ease;
}

.start-card:hover {
  text-decoration: none;
  border-color: var(--accent);
  transform: translateY(-3px);
}

.start-card h3,
.channel-card h3 {
  margin: 0;
  font-size: 1.1rem;
}

.start-card p,
.channel-card p {
  margin: 0;
  color: var(--muted);
  font-size: 0.94rem;
}

.start-icon,
.channel-icon,
.expect-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 42px;
  height: 42px;
  margin-bottom: 0.4rem;
  border-radius: 10px;
  background: var(--accent-soft);
  color: var(--accent-text);
}

.start-meta {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  margin-top: 0.5rem;
  font-size: 0.85rem;
  font-weight: 600;
  color: var(--accent-text);
}

.start-all {
  margin-top: 1.25rem;
  color: var(--muted);
}

.channels-section {
  background: var(--surface-soft);
}

.channel-card {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

.channel-email {
  margin-top: 0.6rem;
  font-size: 0.9rem;
  font-weight: 600;
  word-break: break-word;
}

.support-columns {
  display: grid;
  gap: 1.25rem;
}

.include-card,
.expect-card {
  display: flex;
  flex-direction: column;
}

.include-card h2,
.expect-card h2 {
  margin: 0 0 0.4rem;
  font-size: 1.25rem;
}

.include-intro {
  margin: 0 0 1rem;
  color: var(--muted);
  font-size: 0.94rem;
}

.include-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}

.include-list li {
  display: flex;
  gap: 0.55rem;
  align-items: flex-start;
  color: var(--ink-soft);
  font-size: 0.94rem;
}

.include-check {
  flex-shrink: 0;
  margin-top: 0.2rem;
  color: var(--accent-text);
}

.expect-card p {
  margin: 0 0 0.8rem;
  color: var(--ink-soft);
  font-size: 0.94rem;
}

.expect-note {
  margin-bottom: 0;
  font-size: 0.88rem;
  color: var(--muted);
}

.expect-status {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.6rem;
  margin-top: 1.1rem;
  padding-top: 1.1rem;
  border-top: 1px solid var(--line);
}

.expect-status-label {
  font-size: 0.88rem;
  color: var(--muted);
}

.trial-section {
  padding-top: 0;
}

.trial-card {
  text-align: center;
}

.trial-card h2 {
  margin: 0 0 0.5rem;
  font-size: 1.3rem;
}

.trial-card p {
  margin: 0 auto;
  max-width: 40rem;
  color: var(--ink-soft);
}

.trial-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.85rem;
  justify-content: center;
  margin-top: 1.4rem;
}

@media (min-width: 700px) {
  .start-grid {
    grid-template-columns: repeat(3, 1fr);
  }

  .channel-grid {
    grid-template-columns: repeat(2, 1fr);
  }

  .support-columns {
    grid-template-columns: 1fr 1fr;
    gap: 1.5rem;
  }
}
</style>
