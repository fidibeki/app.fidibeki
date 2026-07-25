<script setup lang="ts">
import { ArrowRight } from '@lucide/vue'
import { features } from '~/data/features'
import { availableIntegrations } from '~/data/integrations'
import { useCases } from '~/data/use-cases'
import { plans, formatPrice, trialDays } from '~/data/pricing'
import { customers, siteDescription, siteName } from '~/data/site'

const links = useAppLinks()
const config = useRuntimeConfig()
const siteUrl = config.public.siteUrl.replace(/\/$/, '')

useSiteMeta({
  title: 'Product feedback that finishes the job',
  description: siteDescription,
  path: '/',
  structuredData: [
    {
      '@context': 'https://schema.org',
      '@type': 'SoftwareApplication',
      name: siteName,
      applicationCategory: 'BusinessApplication',
      operatingSystem: 'Web',
      url: `${siteUrl}/`,
      description: siteDescription,
      offers: plans
        .filter((plan) => plan.monthlyPriceCents !== null)
        .map((plan) => ({
          '@type': 'Offer',
          name: `${plan.name} plan`,
          price: (plan.monthlyPriceCents! / 100).toFixed(2),
          priceCurrency: 'USD',
        })),
    },
  ],
})

// Product Hunt launch badge. The image is served live from Product Hunt so the
// ranking and vote count stay current; it is served per theme the same way the
// customer logo wall is.
const productHuntPostId = 1205878
const productHuntUrl =
  'https://www.producthunt.com/products/fidibeki?embed=true&utm_source=badge-featured&utm_medium=badge&utm_campaign=badge-fidibeki'
const productHuntAlt = `${siteName} - Product feedback that finishes the job | Product Hunt`
const productHuntBadge = (theme: 'light' | 'dark') =>
  `https://api.producthunt.com/widgets/embed-image/v1/featured.svg?post_id=${productHuntPostId}&theme=${theme}`

const trustPoints = [
  {
    title: 'Privacy first capture',
    body: 'The widget never reads input values, page text, or network payloads, and never builds a visitor fingerprint.',
  },
  {
    title: 'Passwordless by default',
    body: 'Magic link sign in with brief tokens scoped to the audience and rotating refresh cookies with reuse detection.',
  },
  {
    title: 'Uploads in quarantine',
    body: 'Attachments are verified for size and type, and only files verified by signature ever become visible or eligible for AI.',
  },
]
</script>

<template>
  <div>
    <section class="hero">
      <div class="container hero-grid">
        <div class="hero-copy">
          <p class="eyebrow">From report to resolution</p>
          <h1>Product feedback that <span class="hero-accent">finishes the job</span></h1>
          <p class="lede">
            Bug reports arrive with context already attached. Reporters add screenshots, audio, or
            replay when they want. Your team triages, tracks, and updates them from one inbox.
          </p>
          <div class="hero-actions">
            <a :href="links.signUp" class="btn btn-primary btn-large">Start free trial</a>
            <a :href="links.demo" class="btn btn-ghost btn-large" rel="noopener" target="_blank">Book a demo</a>
          </div>
          <p class="hero-note">{{ trialDays }}-day free trial · No credit card required</p>
          <a
            class="hero-badge"
            :href="productHuntUrl"
            rel="noopener"
            target="_blank"
          >
            <img
              class="hero-badge-light"
              :src="productHuntBadge('light')"
              :alt="productHuntAlt"
              width="250"
              height="54"
              loading="lazy"
            />
            <img
              class="hero-badge-dark"
              :src="productHuntBadge('dark')"
              :alt="productHuntAlt"
              width="250"
              height="54"
              loading="lazy"
            />
          </a>
        </div>
        <div class="hero-visual">
          <ProductVignette />
        </div>
      </div>
    </section>

    <section class="customers-band" aria-label="Customers using Fidibeki">
      <div class="container customers-inner">
        <span class="customers-label">Trusted by</span>
        <ul class="customers-list">
          <li v-for="customer in customers" :key="customer.name">
            <a :href="customer.url" class="customer-chip" rel="noopener" target="_blank">
              <img
                class="customer-logo-light"
                :src="customer.logoLight"
                :alt="`${customer.name} logo`"
              />
              <img
                class="customer-logo-dark"
                :src="customer.logoDark"
                :alt="`${customer.name} logo`"
              />
            </a>
          </li>
        </ul>
      </div>
    </section>

    <section class="section" aria-labelledby="loop-heading">
      <div class="container">
        <div class="section-head">
          <p class="eyebrow">The loop</p>
          <h2 id="loop-heading">Most tools stop at the inbox. Fidibeki helps you finish the job.</h2>
          <p class="lede">
            Reports come in, decisions get made, and the person who spoke up hears the outcome.
            Every stage leaves an audit trail.
          </p>
        </div>
        <LoopDemo />
        <LoopDiagram />
      </div>
    </section>

    <section class="section" aria-labelledby="features-heading">
      <div class="container">
        <div class="section-head">
          <p class="eyebrow">Features</p>
          <h2 id="features-heading">Everything between a raw report and your team’s resolution</h2>
        </div>
        <div class="feature-grid">
          <NuxtLink
            v-for="feature in features"
            :key="feature.slug"
            :to="`/features/${feature.slug}`"
            class="card feature-card"
          >
            <span class="feature-icon"><FeatureIcon :name="feature.icon" /></span>
            <h3>{{ feature.name }}</h3>
            <p>{{ feature.tagline }}</p>
            <span class="feature-more">Learn more <ArrowRight :size="14" aria-hidden="true" /></span>
          </NuxtLink>
        </div>
      </div>
    </section>

    <section class="section" aria-labelledby="integrations-heading">
      <div class="container">
        <div class="section-head">
          <p class="eyebrow">Integrations</p>
          <h2 id="integrations-heading">Signed events into the tools you already run</h2>
          <p class="lede">
            Send feedback and issue events to Slack, Microsoft Teams, or your own systems. Every
            delivery is signed, retried, and logged.
          </p>
        </div>
        <div class="integration-row">
          <NuxtLink
            v-for="integration in availableIntegrations"
            :key="integration.slug"
            :to="`/integrations/${integration.slug}`"
            class="card integration-card"
          >
            <span class="feature-icon"><FeatureIcon :name="integration.icon" /></span>
            <div>
              <h3>{{ integration.name }}</h3>
              <p>{{ integration.tagline }}</p>
            </div>
          </NuxtLink>
        </div>
      </div>
    </section>

    <section v-reveal.line class="section trust-band" aria-labelledby="trust-heading">
      <div class="container">
        <div class="section-head">
          <p class="eyebrow">Trust</p>
          <h2 id="trust-heading">Built like infrastructure, because it holds your customers’ words</h2>
        </div>
        <div class="trust-grid">
          <div
            v-for="(point, index) in trustPoints"
            :key="point.title"
            v-reveal="index * 70"
            class="trust-item"
          >
            <h3>{{ point.title }}</h3>
            <p>{{ point.body }}</p>
          </div>
        </div>
        <p class="trust-links">
          <NuxtLink to="/security">How Fidibeki is secured</NuxtLink>
          <span aria-hidden="true"> · </span>
          <NuxtLink to="/privacy">How we treat data</NuxtLink>
        </p>
      </div>
    </section>

    <section class="section" aria-labelledby="usecases-heading">
      <div class="container">
        <div class="section-head">
          <p class="eyebrow">Use cases</p>
          <h2 id="usecases-heading">One workflow, many teams</h2>
        </div>
        <div class="usecase-grid">
          <NuxtLink
            v-for="useCase in useCases"
            :key="useCase.slug"
            :to="`/use-cases/${useCase.slug}`"
            class="card usecase-card"
          >
            <span class="usecase-audience">{{ useCase.audience }}</span>
            <h3>{{ useCase.name }}</h3>
            <p>{{ useCase.tagline }}</p>
          </NuxtLink>
        </div>
      </div>
    </section>

    <section class="section" aria-labelledby="pricing-heading">
      <div class="container">
        <div class="section-head">
          <p class="eyebrow">Pricing</p>
          <h2 id="pricing-heading">Honest plans, from first project to enterprise</h2>
        </div>
        <div class="pricing-teaser">
          <div v-for="(plan, index) in plans" :key="plan.id" v-reveal="index * 70" class="pricing-teaser-item">
            <span class="pricing-teaser-name">{{ plan.name }}</span>
            <span class="pricing-teaser-price">
              {{ formatPrice(plan.monthlyPriceCents) }}<span v-if="plan.monthlyPriceCents !== null" class="pricing-teaser-period">/mo</span>
            </span>
          </div>
        </div>
        <p class="pricing-teaser-link">
          <NuxtLink to="/pricing" class="btn btn-ghost">Compare all plans <ArrowRight :size="15" aria-hidden="true" /></NuxtLink>
        </p>
      </div>
    </section>

    <CtaSection />
  </div>
</template>

<style scoped>
.hero {
  position: relative;
  padding-block: clamp(3.5rem, 3rem + 4vw, 7rem) clamp(2.5rem, 2rem + 3vw, 5rem);
  background: var(--hero-glow);
  overflow: hidden;
}

.hero::before {
  content: '';
  position: absolute;
  inset: 0;
  background-image: linear-gradient(var(--grid-line) 1px, transparent 1px),
    linear-gradient(90deg, var(--grid-line) 1px, transparent 1px);
  background-size: 44px 44px;
  mask-image: radial-gradient(70% 60% at 50% 20%, black, transparent);
  -webkit-mask-image: radial-gradient(70% 60% at 50% 20%, black, transparent);
  pointer-events: none;
}

.hero-grid {
  position: relative;
  display: grid;
  grid-template-columns: 1fr;
  gap: 3rem;
  align-items: center;
}

.hero-accent {
  color: var(--accent-text);
}

.hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.85rem;
  margin-top: 1.9rem;
}

.hero-note {
  margin-top: 1rem;
  color: var(--muted);
  font-size: 0.88rem;
}

.hero-badge {
  display: inline-block;
  margin-top: 1.25rem;
  border-radius: 10px;
  transition: transform 160ms ease;
}

.hero-badge:hover,
.hero-badge:focus-visible {
  transform: translateY(-2px);
}

.hero-badge img {
  display: block;
  width: 250px;
  height: 54px;
}

.hero-visual {
  justify-self: center;
  width: 100%;
  max-width: 30rem;
}

.customers-band {
  padding-block: 1.9rem 0.5rem;
}

.customers-inner {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1.1rem;
}

.customers-label {
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.09em;
  text-transform: uppercase;
  color: var(--muted);
}

.customers-list {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 1.5rem 2.5rem;
  list-style: none;
  margin: 0;
  padding: 0;
}

.customer-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.55rem;
  color: var(--ink-soft);
  font-weight: 640;
  font-family: var(--font-display);
  font-size: 1.02rem;
  /* Logo-wall treatment: quiet until hovered or focused. */
  filter: grayscale(1);
  opacity: 0.6;
  transition: filter 180ms ease, opacity 180ms ease;
}

.customer-chip:hover,
.customer-chip:focus-visible {
  text-decoration: none;
  filter: none;
  opacity: 1;
}

.customer-chip img {
  height: 30px;
  width: auto;
}

:root[data-theme='light'] .customer-logo-dark,
:root[data-theme='light'] .hero-badge-dark {
  display: none;
}

:root[data-theme='dark'] .customer-logo-light,
:root[data-theme='dark'] .hero-badge-light {
  display: none;
}

.feature-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1rem;
}

.feature-card {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  color: var(--ink);
  transition: border-color 160ms ease, transform 160ms ease, box-shadow 160ms ease;
}

.feature-card:hover {
  text-decoration: none;
  border-color: var(--accent);
  transform: translateY(-3px);
  box-shadow: var(--shadow-card);
}

.feature-card p {
  margin: 0;
  color: var(--muted);
  font-size: 0.94rem;
}

.feature-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 42px;
  height: 42px;
  margin-bottom: 0.5rem;
  border-radius: 10px;
  background: var(--accent-soft);
  color: var(--accent-text);
  flex-shrink: 0;
}

.feature-more {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  margin-top: 0.6rem;
  color: var(--accent-text);
  font-size: 0.88rem;
  font-weight: 600;
}

.integration-row {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1rem;
}

.integration-card {
  display: flex;
  gap: 1rem;
  align-items: flex-start;
  color: var(--ink);
  transition: border-color 160ms ease, transform 160ms ease;
}

.integration-card:hover {
  text-decoration: none;
  border-color: var(--accent);
  transform: translateY(-3px);
}

.integration-card p {
  margin: 0;
  color: var(--muted);
  font-size: 0.92rem;
}

.trust-band {
  background: var(--surface-soft);
  border-block: 1px solid var(--line);
}

.trust-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1.5rem;
}

.trust-item h3 {
  margin-bottom: 0.3rem;
  font-size: 1.05rem;
}

.trust-item p {
  margin: 0;
  color: var(--muted);
  font-size: 0.94rem;
}

.trust-links {
  margin-top: 2rem;
  font-weight: 600;
}

.usecase-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1rem;
}

.usecase-card {
  color: var(--ink);
  transition: border-color 160ms ease, transform 160ms ease;
}

.usecase-card:hover {
  text-decoration: none;
  border-color: var(--accent);
  transform: translateY(-3px);
}

.usecase-card p {
  margin: 0;
  color: var(--muted);
  font-size: 0.94rem;
}

.usecase-audience {
  display: inline-block;
  margin-bottom: 0.6rem;
  font-size: 0.76rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--accent-text);
}

.pricing-teaser {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 1px;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  overflow: hidden;
  background: var(--line);
}

.pricing-teaser-item {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 1.4rem;
  background: var(--surface);
}

.pricing-teaser-name {
  font-weight: 640;
  font-family: var(--font-display);
}

.pricing-teaser-price {
  font-size: 1.5rem;
  font-weight: 640;
  font-family: var(--font-display);
  color: var(--accent-text);
}

.pricing-teaser-period {
  font-size: 0.9rem;
  color: var(--muted);
  font-weight: 500;
}

.pricing-teaser-link {
  margin-top: 1.5rem;
}

@media (min-width: 700px) {
  .feature-grid {
    grid-template-columns: repeat(2, 1fr);
  }

  .trust-grid {
    grid-template-columns: repeat(2, 1fr);
  }

  .usecase-grid {
    grid-template-columns: repeat(2, 1fr);
  }

  .integration-row {
    grid-template-columns: repeat(3, 1fr);
  }

  .integration-card {
    flex-direction: column;
  }
}

@media (min-width: 1000px) {
  .hero-grid {
    grid-template-columns: 1.15fr 1fr;
  }

  .hero-visual {
    justify-self: end;
  }

  .feature-grid {
    grid-template-columns: repeat(3, 1fr);
  }

  .trust-grid {
    grid-template-columns: repeat(3, 1fr);
  }

  .usecase-grid {
    grid-template-columns: repeat(4, 1fr);
  }

  .pricing-teaser {
    grid-template-columns: repeat(4, 1fr);
  }
}
</style>
