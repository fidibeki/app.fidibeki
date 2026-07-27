<script setup lang="ts">
import { ArrowUpRight, Briefcase, CalendarClock, MessageSquare, ScrollText, ShieldCheck } from '@lucide/vue'
import { contactChannels } from '~/data/site'

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
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Contact', path: '/contact' }]" />
      </template>
    </PageHero>

    <section class="section switchboard-section">
      <div class="container switchboard">
        <a v-reveal :href="`mailto:${primaryChannel.email}`" class="primary-panel">
          <span class="panel-label">Start here</span>
          <h2>{{ primaryChannel.label }}</h2>
          <p>{{ primaryChannel.blurb }}</p>
          <span class="panel-address">
            {{ primaryChannel.email }}
            <ArrowUpRight :size="18" aria-hidden="true" />
          </span>
          <span class="panel-note">Answered by the person who built the thing you are asking about.</span>
        </a>

        <div v-reveal="80" class="specialist-list">
          <span class="specialist-heading">Or go direct</span>
          <a
            v-for="channel in specialistChannels"
            :key="channel.key"
            :href="`mailto:${channel.email}`"
            class="specialist-row"
          >
            <span class="specialist-icon">
              <component :is="specialistIcons[channel.key]" :size="17" aria-hidden="true" />
            </span>
            <span class="specialist-body">
              <span class="specialist-label">{{ channel.label }}</span>
              <span class="specialist-blurb">{{ channel.blurb }}</span>
              <span class="specialist-address">{{ channel.email }}</span>
            </span>
            <ArrowUpRight :size="16" aria-hidden="true" class="specialist-arrow" />
          </a>
        </div>
      </div>
    </section>

    <section class="section paths-section">
      <div class="container paths">
        <div class="path">
          <span class="path-icon"><CalendarClock :size="18" aria-hidden="true" /></span>
          <div class="path-copy">
            <h2>Book a demo</h2>
            <p>
              Thirty minutes, screen shared, your questions rather than our slides. Usually an
              enterprise scope or a security review.
            </p>
            <a :href="links.demo" class="path-link" rel="noopener" target="_blank">
              Find a time <ArrowUpRight :size="14" aria-hidden="true" />
            </a>
          </div>
        </div>

        <div class="path">
          <span class="path-icon"><MessageSquare :size="18" aria-hidden="true" /></span>
          <div class="path-copy">
            <h2>Use the widget</h2>
            <p>
              This site runs the same widget we sell, so the fastest way to report something about
              Fidibeki is to report it with Fidibeki. It loads once you accept it in the banner.
            </p>
            <a :href="linkedInUrl" class="path-link" rel="noopener" target="_blank">
              Or find us on LinkedIn <ArrowUpRight :size="14" aria-hidden="true" />
            </a>
          </div>
        </div>
      </div>
    </section>

    <section class="section imprint-section">
      <div class="container">
        <div v-reveal.line class="imprint">
          <span class="imprint-label">Who you are contacting</span>
          <p class="imprint-lede">
            Fidibeki is a service operated by <strong>ACIDINEY ALVARO CARVALHO SOARES DIAS</strong>,
            an independent service provider established in Portugal. Fidibeki is the name of the
            service, not a company or a separate legal entity.
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
/* ---- switchboard: one front door, three side doors ---- */

.switchboard {
  display: grid;
  gap: 1.25rem;
}

.primary-panel {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  padding: clamp(1.5rem, 1rem + 2vw, 2.5rem);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background:
    radial-gradient(80% 60% at 15% 0%, var(--accent-soft), transparent 70%),
    var(--surface);
  box-shadow: var(--shadow-card);
  color: var(--ink);
  overflow: hidden;
  transition: border-color 200ms ease, transform 200ms ease;
}

.primary-panel::after {
  /* Hairline grid, echoing the product surfaces without competing with them. */
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
  background-image: linear-gradient(var(--grid-line) 1px, transparent 1px);
  background-size: 100% 2.25rem;
  opacity: 0.9;
}

.primary-panel:hover {
  text-decoration: none;
  border-color: var(--accent);
  transform: translateY(-3px);
}

.panel-label,
.specialist-heading,
.imprint-label,
.imprint-key {
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.09em;
  text-transform: uppercase;
  color: var(--accent-text);
}

.primary-panel h2 {
  position: relative;
  margin: 0.7rem 0 0.5rem;
  font-size: clamp(1.5rem, 1.2rem + 1.4vw, 2rem);
  line-height: 1.1;
}

.primary-panel p {
  position: relative;
  margin: 0;
  max-width: 34rem;
  color: var(--ink-soft);
}

.panel-address {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
  margin-top: 1.5rem;
  padding: 0.55rem 0.95rem;
  border: 1px solid var(--line-strong);
  border-radius: 999px;
  background: var(--canvas);
  font-family: var(--font-mono);
  font-size: 0.95rem;
  font-weight: 600;
  color: var(--accent-text);
  word-break: break-word;
  transition: border-color 200ms ease, background 200ms ease;
}

.primary-panel:hover .panel-address {
  border-color: var(--accent);
  background: var(--accent-soft);
}

.panel-note {
  position: relative;
  margin-top: 0.9rem;
  font-size: 0.85rem;
  color: var(--muted);
}

/* ---- specialist routes: rows on hairlines, not boxes ---- */

.specialist-list {
  display: flex;
  flex-direction: column;
  padding: 0.35rem 0 0;
}

.specialist-heading {
  padding: 0 0 0.35rem;
}

.specialist-row {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: start;
  gap: 0.85rem;
  padding: 1.05rem 0.25rem;
  border-bottom: 1px solid var(--line);
  color: var(--ink);
  transition: padding-inline 200ms ease;
}

.specialist-row:first-of-type {
  border-top: 1px solid var(--line);
}

.specialist-row:hover {
  text-decoration: none;
  padding-inline: 0.6rem 0;
}

.specialist-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border-radius: 9px;
  background: var(--accent-soft);
  color: var(--accent-text);
  transition: background 200ms ease;
}

.specialist-row:hover .specialist-icon {
  background: var(--accent);
  color: var(--on-accent);
}

.specialist-body {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  min-width: 0;
}

.specialist-label {
  font-weight: 600;
  font-size: 0.98rem;
}

.specialist-blurb {
  color: var(--muted);
  font-size: 0.88rem;
  line-height: 1.5;
}

.specialist-address {
  margin-top: 0.3rem;
  font-family: var(--font-mono);
  font-size: 0.82rem;
  color: var(--accent-text);
  word-break: break-word;
}

.specialist-arrow {
  margin-top: 0.3rem;
  color: var(--muted);
  transition: transform 200ms ease, color 200ms ease;
}

.specialist-row:hover .specialist-arrow {
  transform: translate(2px, -2px);
  color: var(--accent-text);
}

/* ---- two paths: a band split by a rule, no cards ---- */

.paths-section {
  padding-top: 0;
}

.paths {
  display: grid;
  gap: 1.75rem;
  padding-top: clamp(1.75rem, 1rem + 2vw, 2.75rem);
  border-top: 1px solid var(--line);
}

.path {
  display: flex;
  gap: 0.9rem;
  align-items: flex-start;
}

.path-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 38px;
  height: 38px;
  border-radius: 10px;
  border: 1px solid var(--line);
  color: var(--accent-text);
}

.path-copy h2 {
  margin: 0 0 0.35rem;
  font-size: 1.15rem;
}

.path-copy p {
  margin: 0;
  color: var(--muted);
  font-size: 0.94rem;
}

.path-link {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  margin-top: 0.7rem;
  font-size: 0.9rem;
  font-weight: 600;
}

/* ---- imprint: a quiet record, not another card ---- */

.imprint-section {
  background: var(--surface-soft);
}

.imprint-lede {
  margin: 0.6rem 0 0;
  max-width: 46rem;
  color: var(--ink-soft);
}

.imprint-lede strong {
  font-weight: 600;
  color: var(--ink);
}

.imprint-grid {
  display: grid;
  gap: 1.5rem;
  margin-top: 2rem;
  padding-top: 1.5rem;
  border-top: 1px solid var(--line-strong);
}

.imprint-key {
  display: block;
  margin-bottom: 0.5rem;
  color: var(--muted);
}

.imprint-value {
  margin: 0;
  font-style: normal;
  font-size: 0.92rem;
  line-height: 1.7;
  color: var(--ink-soft);
}

@media (min-width: 900px) {
  .switchboard {
    grid-template-columns: 1.05fr 1fr;
    gap: 2.5rem;
    align-items: start;
  }

  .paths {
    grid-template-columns: 1fr 1fr;
    gap: 3rem;
  }

  .paths .path + .path {
    padding-left: 3rem;
    border-left: 1px solid var(--line);
  }

  .imprint-grid {
    grid-template-columns: repeat(3, 1fr);
    gap: 2.5rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .primary-panel,
  .specialist-row,
  .specialist-arrow,
  .specialist-icon,
  .panel-address {
    transition: none;
  }

  .primary-panel:hover {
    transform: none;
  }

  .specialist-row:hover {
    padding-inline: 0.25rem;
  }

  .specialist-row:hover .specialist-arrow {
    transform: none;
  }
}
</style>
