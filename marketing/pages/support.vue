<script setup lang="ts">
import { ArrowRight, Check } from '@lucide/vue'
import { contactChannels } from '~/data/site'
import { trialDays } from '~/data/pricing'

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
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Support', path: '/support' },
          ]"
        />
      </template>
    </PageHero>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <h2>Start with the guides</h2>
          <p class="lede">
            These three cover most of what people write in about. They are
            short, and they are honest about the parts that trip people up.
          </p>
        </div>

        <div class="start-grid">
          <NuxtLink
            v-for="item in startingPoints"
            :key="item.to"
            :to="item.to"
            class="start-card"
          >
            <h3>{{ item.title }}</h3>
            <p>{{ item.body }}</p>
            <span class="start-meta"
              >Read the guide <ArrowRight :size="14" aria-hidden="true"
            /></span>
          </NuxtLink>
        </div>

        <p class="start-all">
          Everything else lives in
          <NuxtLink to="/docs">the documentation</NuxtLink>.
        </p>
      </div>
    </section>

    <section class="section channels-section">
      <div class="container">
        <div class="section-head">
          <h2>Write to the right address</h2>
          <p class="lede">
            Four mailboxes, so your message lands with whoever can actually act
            on it. If you are unsure, use support and we will route it.
          </p>
        </div>

        <div class="channel-grid">
          <div
            v-for="channel in contactChannels"
            :key="channel.key"
            class="channel-card"
          >
            <h3>{{ channel.label }}</h3>
            <p>{{ channel.blurb }}</p>
            <a :href="`mailto:${channel.email}`" class="channel-email">{{
              channel.email
            }}</a>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <div class="container support-columns">
        <div class="include-card">
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

        <div class="expect-card">
          <h2>What to expect</h2>
          <p>
            We aim to reply within one business day. That is a target we work
            to, not a contractual service level — self-serve plans do not carry
            one, and we would rather say so than publish a number we cannot
            stand behind. Enterprise agreements can include a commitment.
          </p>
          <p>
            When something is broken, we will tell you it is broken. When
            something is not going to be fixed soon, we will tell you that too,
            rather than leaving the thread open and quiet.
          </p>
          <p class="expect-note">
            Security reports follow their own path — see
            <NuxtLink to="/security">how Fidibeki is secured</NuxtLink> before
            you send one.
          </p>
          <div class="expect-status">
            <span class="expect-status-label"
              >Before you write, check live status:</span
            >
            <StatusPill variant="block" />
          </div>
        </div>
      </div>
    </section>

    <section class="section trial-section">
      <div class="container">
        <div class="trial-card">
          <h2>Not a customer yet?</h2>
          <p>
            Setup questions before you buy are still support questions. Start
            the
            {{ trialDays }}-day trial and write in, or bring your list to a demo
            and we will install the widget with you.
          </p>
          <div class="trial-actions">
            <NuxtLink to="/sign-up" class="btn btn-primary"
              >Start free trial</NuxtLink
            >
            <a
              :href="links.demo"
              class="btn btn-ghost"
              rel="noopener"
              target="_blank"
              >Book a demo</a
            >
          </div>
        </div>
      </div>
    </section>

    <section class="section support-close">
      <div class="container">
        <h2>Still stuck?</h2>
        <p>
          The person answering is the person who built the thing you are asking
          about.
        </p>
        <a href="mailto:support@fidibeki.com">support@fidibeki.com</a>
      </div>
    </section>
  </div>
</template>

<style scoped>
.start-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 40px;
}
.start-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  border-top: 1px solid var(--line);
  padding-top: 24px;
}
.start-card h3,
.channel-card h3 {
  font-size: 23px;
  margin-bottom: 16px;
}
.start-card p,
.channel-card p {
  margin: 0 0 24px;
  font-size: 15px;
  color: var(--muted);
}
.start-meta {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  margin-top: auto;
}
.start-card:hover {
  text-decoration: none;
}
.start-card:hover .start-meta {
  text-decoration: underline;
  text-underline-offset: 4px;
}
.start-all {
  margin: 36px 0 0;
  color: var(--muted);
  font-size: 14px;
}
.start-all a {
  color: var(--ink);
  text-decoration: underline;
}
.channels-section {
  padding-top: 24px;
}
.channel-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 40px 80px;
}
.channel-card {
  padding-top: 24px;
  border-top: 1px solid var(--line);
}
.channel-email {
  display: inline-block;
  font-size: 15px;
  text-decoration: underline;
  text-underline-offset: 4px;
  overflow-wrap: anywhere;
}
.support-columns {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 80px;
  border-top: 1px solid var(--line);
  padding-top: 36px;
}
.include-card h2,
.expect-card h2 {
  font-size: 28px;
  margin-bottom: 20px;
}
.include-intro,
.expect-card p {
  font-size: 15px;
  color: var(--muted);
}
.include-list {
  list-style: none;
  padding: 0;
  margin: 28px 0 0;
  display: grid;
  gap: 20px;
}
.include-list li {
  display: flex;
  gap: 12px;
  font-size: 15px;
  color: var(--ink-soft);
}
.include-check {
  flex-shrink: 0;
  margin-top: 4px;
}
.expect-status {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  padding-top: 24px;
  margin-top: 24px;
  border-top: 1px solid var(--line);
}
.expect-status-label {
  color: var(--muted);
  font-size: 13px;
}
.trial-section {
  padding-top: 0;
}
.trial-card {
  padding: 40px;
  border-radius: 10px;
  background: var(--surface-soft);
}
.trial-card h2 {
  font-size: 30px;
}
.trial-card p {
  max-width: 640px;
  color: var(--muted);
}
.trial-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 28px;
}
.support-close {
  padding-top: 0;
}
.support-close h2 {
  font-size: 30px;
}
.support-close p {
  color: var(--muted);
}
.support-close a {
  display: inline-block;
  text-decoration: underline;
  text-underline-offset: 5px;
}
@media (max-width: 800px) {
  .start-grid,
  .support-columns {
    grid-template-columns: minmax(0, 1fr);
    gap: 36px;
  }
}
@media (max-width: 600px) {
  .channel-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 32px;
  }
  .trial-card {
    padding: 28px;
  }
}
</style>
