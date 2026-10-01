<script setup lang="ts">
defineProps<{ compact?: boolean }>()
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  Hash,
  Link,
  Webhook,
} from '@lucide/vue'
import { integrations } from '~/data/integrations'

const destinations = [
  {
    slug: 'slack',
    label: 'Slack',
    heading: 'product-feedback',
    kind: 'notification',
    note: 'New feedback and issue updates, delivered to the channels you choose.',
    link: 'Connect Slack',
  },
  {
    slug: 'microsoft-teams',
    label: 'Teams',
    heading: 'Product team / Feedback',
    kind: 'notification',
    note: 'Keep your team up to date with feedback and issue notifications in Teams.',
    link: 'Connect Microsoft Teams',
  },
  {
    slug: 'jira',
    label: 'Jira',
    heading: 'Product / Backlog',
    kind: 'issue',
    note: 'Create a linked Jira issue from Fidibeki, by hand or automatically.',
    link: 'Connect Jira',
  },
  {
    slug: 'linear',
    label: 'Linear',
    heading: 'Product / Issues',
    kind: 'issue',
    note: 'Send an issue to Linear with its priority and a link back to the feedback.',
    link: 'Connect Linear',
  },
  {
    slug: 'webhooks',
    label: 'Webhooks',
    heading: 'Your endpoint',
    kind: 'event',
    note: 'Send signed events to your own systems, with retries and delivery logs.',
    link: 'Explore webhooks',
  },
] as const
const selected = ref<string>('slack')
const destination = computed(
  () => destinations.find((item) => item.slug === selected.value)!,
)
const availability = computed(
  () => integrations.find((item) => item.slug === selected.value)?.availability,
)
const exampleTitle = 'Checkout button does not respond on mobile'
</script>

<template>
  <section
    class="home-connections"
    :class="{ 'connections-compact': compact }"
    :aria-labelledby="compact ? undefined : 'connections-heading'"
    :aria-label="compact ? 'Integration example' : undefined"
  >
    <div v-if="!compact" class="connections-intro">
      <h2 id="connections-heading">Feedback, wherever<br />your team works.</h2>
      <p>
        Send updates to a channel. Turn reports into issues. Bring the context
        with you.
      </p>
      <NuxtLink to="/integrations" class="home-text-link"
        >Browse integrations <ArrowRight :size="16" aria-hidden="true"
      /></NuxtLink>
    </div>
    <div class="connections-example">
      <div
        class="connections-switcher"
        role="group"
        aria-label="Preview an integration"
      >
        <button
          v-for="item in destinations"
          :key="item.slug"
          type="button"
          :aria-pressed="selected === item.slug"
          aria-controls="integration-preview"
          @click="selected = item.slug"
        >
          {{ item.label }}
        </button>
      </div>
      <div
        id="integration-preview"
        class="connection-preview"
        role="region"
        :aria-label="`${destination.label} example`"
      >
        <div class="connection-window-heading">
          <span
            ><Webhook
              v-if="destination.kind === 'event'"
              :size="15"
              aria-hidden="true"
            /><Hash
              v-else-if="destination.kind === 'notification'"
              :size="15"
              aria-hidden="true"
            /><Link v-else :size="15" aria-hidden="true" />{{
              destination.heading
            }}</span
          ><span>Example</span>
        </div>
        <div :key="selected" class="connection-message">
          <template v-if="destination.kind === 'notification'">
            <div class="connection-sender">
              <BrandMark /><strong>Fidibeki</strong><span>App</span>
            </div>
            <div class="connection-message-body">
              <span class="connection-event-label">New feedback</span>
              <p>{{ exampleTitle }}</p>
              <span class="connection-context"
                >“The checkout button isn't working on my phone.”</span
              ><span class="connection-source"
                >View feedback in Fidibeki
                <ArrowUpRight :size="13" aria-hidden="true"
              /></span>
            </div>
          </template>
          <template v-else-if="destination.kind === 'issue'">
            <span class="connection-issue-key"
              >DEMO-24 <span>Issue linked to Fidibeki</span></span
            >
            <p class="connection-issue-title">{{ exampleTitle }}</p>
            <div class="connection-issue-meta">
              <span><span class="connection-status-dot" /> Todo</span
              ><span>High priority</span><span>Bug</span>
            </div>
            <span class="connection-source"
              ><BrandMark /> Original feedback attached</span
            >
          </template>
          <template v-else>
            <div class="connection-event">
              <Webhook :size="22" aria-hidden="true" /><code
                >feedback.created</code
              >
            </div>
            <p class="connection-event-copy">
              A new report, ready for your workflow.
            </p>
            <div class="connection-event-details">
              <span>Payload <strong>Signed JSON</strong></span
              ><span
                >Delivery
                <strong
                  ><Check :size="13" aria-hidden="true" /> Logged</strong
                ></span
              >
            </div>
          </template>
        </div>
      </div>
      <div class="connection-caption" aria-live="polite">
        <p>{{ destination.note }}</p>
        <div>
          <NuxtLink :to="`/integrations/${destination.slug}`"
            >{{ destination.link }}
            <ArrowUpRight :size="14" aria-hidden="true" /></NuxtLink
          ><span>{{ availability }}</span>
        </div>
      </div>
    </div>
    <div v-if="!compact" class="connections-details">
      <NuxtLink to="/features/ai-analysis"
        ><h3>Help with the first read.</h3>
        <p>AI-assisted text cleanup, transcription, and sentiment.</p>
        <span
          >Explore AI analysis
          <ArrowUpRight :size="15" aria-hidden="true" /></span
      ></NuxtLink>
      <NuxtLink to="/security"
        ><h3>Shared on their terms.</h3>
        <p>Your users choose the context they share with your team.</p>
        <span
          >Privacy and security
          <ArrowUpRight :size="15" aria-hidden="true" /></span
      ></NuxtLink>
    </div>
  </section>
</template>

<style scoped>
.connections-compact.home-connections {
  display: block;
  border: 0;
  padding: 0;
}
.connections-compact .connections-example {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(0, 1fr);
  column-gap: 64px;
}
.connections-compact .connections-switcher {
  grid-column: 1;
}
.connections-compact .connection-preview {
  grid-column: 1;
  min-height: 250px;
}
.connections-compact .connection-caption {
  grid-column: 2;
  grid-row: 2;
  align-self: center;
  margin: 0;
}
.connections-compact .connection-caption > p {
  font-size: 22px;
  line-height: 1.5;
  max-width: 30ch;
  color: var(--ink);
}
.connections-compact .connection-caption > div {
  flex-direction: column;
  align-items: flex-start;
  gap: 12px;
  margin-top: 28px;
}
@media (max-width: 899px) {
  .connections-compact .connections-example {
    display: block;
  }
  .connections-compact .connection-caption {
    margin-top: 20px;
  }
  .connections-compact .connection-caption > p {
    font-size: 16px;
    max-width: none;
  }
  .connections-compact .connection-caption > div {
    flex-direction: row;
    align-items: center;
    margin-top: 16px;
  }
}

.home-connections {
  display: grid;
  grid-template-columns: 0.9fr 1.1fr;
  column-gap: 96px;
  row-gap: 60px;
  padding: 76px 0 90px;
  border-top: 1px solid var(--home-line);
}
.connections-intro {
  padding-top: 18px;
}
.connections-intro h2 {
  font-size: 34px;
  line-height: 1.17;
  letter-spacing: -0.03em;
  font-weight: 500;
}
.connections-intro > p {
  max-width: 32ch;
  margin-top: 23px;
  color: var(--home-muted);
  font-size: 16px;
  line-height: 1.6;
}
.connections-intro .home-text-link {
  margin-top: 30px;
}
.connections-example {
  min-width: 0;
}
.connections-switcher {
  display: flex;
  align-items: center;
  gap: 24px;
  margin-bottom: 19px;
}
.connections-switcher button {
  position: relative;
  padding: 12px 0;
  min-height: 44px;
  border: 0;
  color: var(--home-muted);
  background: transparent;
  cursor: pointer;
  font-size: 14px;
  white-space: nowrap;
}
.connections-switcher button::after {
  content: '';
  position: absolute;
  bottom: 0;
  inset-inline: 0;
  height: 2px;
  background: var(--home-ink);
  transform: scaleX(0);
  transform-origin: left;
  transition: transform 200ms cubic-bezier(0.16, 1, 0.3, 1);
}
.connections-switcher button[aria-pressed='true'] {
  color: var(--home-ink);
}
.connections-switcher button[aria-pressed='true']::after {
  transform: scaleX(1);
}
.connections-switcher button:hover {
  color: var(--home-ink);
}
.connection-preview {
  min-height: 271px;
  border-radius: 12px;
  background: var(--home-soft);
  overflow: hidden;
}
.connection-window-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 17px 22px;
  border-bottom: 1px solid var(--home-line);
  font-size: 12px;
}
.connection-window-heading > span:first-child {
  display: flex;
  align-items: center;
  gap: 8px;
}
.connection-window-heading > span:last-child {
  font-size: 11px;
  color: var(--home-muted);
}
.connection-message {
  padding: 25px 25px 22px;
  animation: connection-change 240ms cubic-bezier(0.16, 1, 0.3, 1);
}
.connection-sender {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 13px;
}
.connection-sender > svg {
  width: 24px;
  height: 24px;
}
.connection-sender strong {
  font-weight: 600;
}
.connection-sender > span {
  color: var(--home-muted);
  font-size: 10px;
}
.connection-message-body {
  padding-left: 33px;
  margin-top: 10px;
}
.connection-event-label {
  color: var(--home-muted);
  font-size: 11px;
}
.connection-message-body > p {
  max-width: 34ch;
  margin-top: 6px;
  font-size: 16px;
  line-height: 1.4;
  font-weight: 500;
  letter-spacing: -0.015em;
}
.connection-context {
  display: block;
  margin-top: 9px;
  font-size: 12px;
  color: var(--home-muted);
  line-height: 1.5;
}
.connection-source {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 20px;
  font-size: 11px;
  color: var(--home-muted);
}
.connection-source > svg {
  width: 15px;
  height: 15px;
}
.connection-issue-key {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 11px;
  color: var(--home-muted);
}
.connection-issue-title {
  margin-top: 17px;
  max-width: 30ch;
  font-size: 21px;
  font-weight: 500;
  line-height: 1.3;
  letter-spacing: -0.025em;
}
.connection-issue-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 15px;
  margin-top: 20px;
  font-size: 11px;
  color: var(--home-muted);
}
.connection-issue-meta > span {
  display: flex;
  align-items: center;
  gap: 6px;
}
.connection-status-dot {
  width: 9px;
  height: 9px;
  border: 1px solid currentColor;
  border-radius: 50%;
}
.connection-event {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 4px;
}
.connection-event code {
  font-family: ui-monospace, monospace;
  font-size: 19px;
}
.connection-event-copy {
  margin-top: 18px;
  color: var(--home-muted);
  font-size: 14px;
}
.connection-event-details {
  display: flex;
  gap: 50px;
  margin-top: 28px;
  font-size: 11px;
}
.connection-event-details > span {
  display: flex;
  flex-direction: column;
  gap: 7px;
  color: var(--home-muted);
}
.connection-event-details strong {
  display: flex;
  gap: 5px;
  align-items: center;
  font-weight: 400;
  color: var(--home-ink);
  font-size: 12px;
}
.connection-caption {
  min-height: 105px;
  padding-top: 18px;
}
.connection-caption > p {
  color: var(--home-muted);
  font-size: 14px;
  line-height: 1.55;
  max-width: 55ch;
}
.connection-caption > div {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px 20px;
  justify-content: space-between;
  margin-top: 14px;
}
.connection-caption a {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.connection-caption > div > span {
  color: var(--home-muted);
  font-size: 11px;
}
.connections-details {
  grid-column: 1 / -1;
}
.connections-details > a {
  display: grid;
  grid-template-columns: 0.75fr 1.5fr auto;
  align-items: center;
  gap: 28px;
  padding: 25px 0;
  border-top: 1px solid var(--home-line);
}
.connections-details > a:last-child {
  border-bottom: 1px solid var(--home-line);
}
.connections-details h3 {
  font-size: 16px;
  line-height: 1.4;
  letter-spacing: -0.015em;
  font-weight: 500;
}
.connections-details p {
  color: var(--home-muted);
  font-size: 14px;
  line-height: 1.5;
}
.connections-details a > span {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 12px;
  white-space: nowrap;
}
.connections-details a:hover > span,
.connection-caption a:hover,
.connections-intro a:hover {
  text-decoration: underline;
  text-underline-offset: 4px;
}
@keyframes connection-change {
  from {
    opacity: 0.65;
    transform: translateY(3px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
@media (max-width: 1000px) {
  .home-connections {
    column-gap: 45px;
    grid-template-columns: 0.8fr 1.2fr;
  }
  .connections-switcher {
    gap: 18px;
  }
  .connections-intro h2 {
    font-size: 30px;
  }
  .connections-details > a {
    grid-template-columns: 1fr 1.3fr auto;
    gap: 18px;
  }
}
@media (max-width: 700px) {
  .home-connections {
    grid-template-columns: 1fr;
    gap: 34px;
    padding: 48px 0 56px;
  }
  .connections-intro {
    padding-top: 0;
  }
  .connections-intro h2 {
    font-size: 31px;
  }
  .connections-intro > p {
    max-width: 40ch;
    margin-top: 18px;
    font-size: 15px;
  }
  .connections-intro .home-text-link {
    margin-top: 20px;
  }
  .connections-switcher {
    justify-content: space-between;
    gap: 12px;
  }
  .connections-switcher button {
    font-size: 13px;
  }
  .connection-window-heading {
    padding: 16px 18px;
  }
  .connection-message {
    padding: 23px 18px;
  }
  .connection-message-body {
    padding-left: 0;
  }
  .connection-message-body > p {
    font-size: 16px;
  }
  .connection-issue-title {
    font-size: 20px;
  }
  .connection-caption {
    min-height: 120px;
  }
  .connections-details > a {
    grid-template-columns: 1fr;
    gap: 8px;
    padding: 24px 0;
  }
  .connections-details a > span {
    margin-top: 5px;
  }
  .connections-details p {
    max-width: 35ch;
  }
}
@media (max-width: 360px) {
  .connections-switcher {
    gap: 8px;
  }
  .connections-switcher button {
    font-size: 12px;
  }
  .connection-issue-key {
    flex-wrap: wrap;
    gap: 5px;
  }
  .connection-event code {
    font-size: 17px;
  }
}
</style>
