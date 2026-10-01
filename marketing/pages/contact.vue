<script setup lang="ts">
import {
  ArrowUpRight,
  Briefcase,
  CalendarClock,
  MessageSquare,
  ScrollText,
  ShieldCheck,
} from '@lucide/vue'
import { contactChannels } from '~/data/site'
import { photography } from '~/data/photography'

const links = useAppLinks()
const linkedInUrl = 'https://www.linkedin.com/company/fidibeki'

useSiteMeta({
  title: 'Contact',
  description:
    'How to reach Fidibeki: support, security, privacy, and enterprise addresses, plus who operates the service and where it is established.',
  path: '/contact',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Contact', path: '/contact' },
  ],
})

// Support is the front door; the rest are specialist routes. The layout says so
// rather than presenting four equal boxes and making the reader work it out.
const [primaryChannel, ...specialistChannels] = contactChannels

const specialistIcons = {
  security: ShieldCheck,
  privacy: ScrollText,
  sales: Briefcase,
} as const
</script>

<template>
  <div>
    <PageHero
      eyebrow="Contact"
      title="Reach a person"
      lede="No ticket portal, no queue position, no bot asking you to rephrase the question. Four addresses, one calendar, and a plain statement of who is on the other side."
    >
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Contact', path: '/contact' },
          ]"
        />
      </template>
      <template #media
        ><EditorialPhoto
          :photo="photography.aveiro"
          ratio="5 / 4"
          caption="Aveiro, Portugal"
          eager
      /></template>
    </PageHero>

    <section class="section switchboard-section">
      <div class="container switchboard">
        <a :href="`mailto:${primaryChannel.email}`" class="primary-panel">
          <span class="panel-label">Start here</span>
          <h2>{{ primaryChannel.label }}</h2>
          <p>{{ primaryChannel.blurb }}</p>
          <span class="panel-address">
            {{ primaryChannel.email }}
            <ArrowUpRight :size="18" aria-hidden="true" />
          </span>
          <span class="panel-note"
            >Answered by the person who built the thing you are asking
            about.</span
          >
        </a>

        <div class="specialist-list">
          <span class="specialist-heading">Or go direct</span>
          <a
            v-for="channel in specialistChannels"
            :key="channel.key"
            :href="`mailto:${channel.email}`"
            class="specialist-row"
          >
            <span class="specialist-icon">
              <component
                :is="specialistIcons[channel.key]"
                :size="17"
                aria-hidden="true"
              />
            </span>
            <span class="specialist-body">
              <span class="specialist-label">{{ channel.label }}</span>
              <span class="specialist-blurb">{{ channel.blurb }}</span>
              <span class="specialist-address">{{ channel.email }}</span>
            </span>
            <ArrowUpRight
              :size="16"
              aria-hidden="true"
              class="specialist-arrow"
            />
          </a>
        </div>
      </div>
    </section>

    <section class="section paths-section">
      <div class="container paths">
        <div class="path">
          <span class="path-icon"
            ><CalendarClock :size="18" aria-hidden="true"
          /></span>
          <div class="path-copy">
            <h2>Book a demo</h2>
            <p>
              Thirty minutes, screen shared, your questions rather than our
              slides. Usually an enterprise scope or a security review.
            </p>
            <a
              :href="links.demo"
              class="path-link"
              rel="noopener"
              target="_blank"
            >
              Find a time <ArrowUpRight :size="14" aria-hidden="true" />
            </a>
          </div>
        </div>

        <div class="path">
          <span class="path-icon"
            ><MessageSquare :size="18" aria-hidden="true"
          /></span>
          <div class="path-copy">
            <h2>Use the widget</h2>
            <p>
              This site runs the same widget we sell, so the fastest way to
              report something about Fidibeki is to report it with Fidibeki. It
              loads once you accept it in the banner.
            </p>
            <a
              :href="linkedInUrl"
              class="path-link"
              rel="noopener"
              target="_blank"
            >
              Or find us on LinkedIn
              <ArrowUpRight :size="14" aria-hidden="true" />
            </a>
          </div>
        </div>
      </div>
    </section>

    <section class="section imprint-section">
      <div class="container">
        <div class="imprint">
          <span class="imprint-label">Who you are contacting</span>
          <p class="imprint-lede">
            Fidibeki is a service operated by
            <strong>ACIDINEY ALVARO CARVALHO SOARES DIAS</strong>, an
            independent service provider established in Portugal. Fidibeki is
            the name of the service, not a company or a separate legal entity.
          </p>
          <div class="imprint-grid">
            <div class="imprint-field">
              <span class="imprint-key">Established</span>
              <address class="imprint-value">
                Travessa Quinta do Casal 18<br />
                3810-928 Aveiro<br />
                Portugal
              </address>
            </div>
            <div class="imprint-field">
              <span class="imprint-key">Tax and registration</span>
              <p class="imprint-value">
                On every invoice we issue, and on request.
              </p>
            </div>
            <div class="imprint-field">
              <span class="imprint-key">The paperwork</span>
              <p class="imprint-value">
                <NuxtLink to="/tos">Terms of service</NuxtLink><br />
                <NuxtLink to="/privacy">Privacy notice</NuxtLink><br />
                <NuxtLink to="/support">Support guides</NuxtLink>
              </p>
            </div>
          </div>
        </div>
      </div>
    </section>

    <CtaSection
      title="Rather just try it?"
      body="Start a free trial and write in when you hit something. Questions from inside the product are easier to answer than questions about it."
    />
  </div>
</template>

<style scoped>
.switchboard-section {
  padding-top: 24px;
}
.switchboard {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 80px;
  align-items: start;
}
.primary-panel {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  background: var(--surface-soft);
  border-radius: 10px;
  padding: 40px;
  color: var(--ink);
}
.primary-panel:hover {
  text-decoration: none;
}
.panel-label,
.specialist-heading,
.imprint-label,
.imprint-key {
  font-size: 13px;
  color: var(--muted);
}
.primary-panel h2 {
  font-size: 32px;
  margin: 36px 0 20px;
}
.primary-panel p {
  color: var(--muted);
  margin: 0;
}
.panel-address {
  display: inline-flex;
  gap: 10px;
  align-items: center;
  font-size: 18px;
  margin-top: 40px;
  overflow-wrap: anywhere;
}
.primary-panel:hover .panel-address {
  text-decoration: underline;
  text-underline-offset: 5px;
}
.panel-address svg {
  flex-shrink: 0;
}
.panel-note {
  font-size: 12px;
  color: var(--muted);
  margin-top: 20px;
}
.specialist-list {
  display: flex;
  flex-direction: column;
}
.specialist-heading {
  margin-bottom: 20px;
}
.specialist-row {
  display: grid;
  grid-template-columns: 20px minmax(0, 1fr) 18px;
  gap: 16px;
  border-top: 1px solid var(--line);
  padding-block: 24px;
}
.specialist-row:last-child {
  border-bottom: 1px solid var(--line);
}
.specialist-icon,
.specialist-arrow {
  margin-top: 4px;
}
.specialist-row:hover {
  text-decoration: none;
}
.specialist-row:hover .specialist-address {
  text-decoration: underline;
  text-underline-offset: 4px;
}
.specialist-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.specialist-label {
  font-size: 18px;
  font-weight: 500;
}
.specialist-blurb {
  color: var(--muted);
  font-size: 14px;
}
.specialist-address {
  margin-top: 8px;
  font-size: 14px;
  overflow-wrap: anywhere;
}
.paths-section {
  padding-top: 0;
}
.paths {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 80px;
  border-top: 1px solid var(--line);
  padding-top: 32px;
}
.path {
  display: flex;
  align-items: flex-start;
  gap: 20px;
}
.path-icon {
  flex-shrink: 0;
  margin-top: 4px;
}
.path-copy h2 {
  font-size: 24px;
  margin-bottom: 16px;
}
.path-copy p {
  font-size: 15px;
  color: var(--muted);
  margin: 0;
}
.path-link {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  font-size: 14px;
  margin-top: 24px;
}
.imprint-section {
  background: var(--surface-soft);
}
.imprint-lede {
  margin: 28px 0 0;
  max-width: 720px;
  font-size: 18px;
  color: var(--ink-soft);
}
.imprint-lede strong {
  font-weight: 500;
  color: var(--ink);
}
.imprint-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 48px;
  margin-top: 48px;
  padding-top: 28px;
  border-top: 1px solid var(--line);
}
.imprint-key {
  display: block;
  margin-bottom: 16px;
}
.imprint-value {
  margin: 0;
  font-style: normal;
  font-size: 14px;
  color: var(--ink-soft);
  line-height: 1.8;
}
@media (max-width: 899px) {
  .switchboard,
  .paths {
    gap: 36px;
  }
  .primary-panel {
    padding: 28px;
  }
}
@media (max-width: 700px) {
  .switchboard,
  .paths,
  .imprint-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 36px;
  }
  .panel-address {
    font-size: 17px;
  }
}
</style>
