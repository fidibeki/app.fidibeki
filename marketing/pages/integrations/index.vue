<script setup lang="ts">
import { ArrowRight } from '@lucide/vue'
import { availableIntegrations, plannedIntegrations } from '~/data/integrations'

useSiteMeta({
  title: 'Integrations',
  description:
    'Route Fidibeki feedback and issue events into Slack, Microsoft Teams, and your own systems with signed webhooks. Available today on Scale and Enterprise plans.',
  path: '/integrations',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Integrations', path: '/integrations' },
  ],
})
</script>

<template>
  <div>
    <PageHero
      eyebrow="Integrations"
      title="Your feedback, in your tools"
      lede="Destinations subscribe to exactly the events they need: feedback created, issues created, updated, or resolved. They retry and log every message."
    >
      <template #breadcrumbs>
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Integrations', path: '/integrations' }]" />
      </template>
    </PageHero>

    <section class="section">
      <div class="container">
        <div class="section-head">
          <h2>Available today</h2>
          <p class="lede">On Scale and Enterprise plans.</p>
        </div>
        <div class="integration-grid">
          <NuxtLink
            v-for="(integration, index) in availableIntegrations"
            :key="integration.slug"
            v-reveal="index * 60"
            :to="`/integrations/${integration.slug}`"
            class="card integration-card"
          >
            <span class="integration-icon"><FeatureIcon :name="integration.icon" :size="24" /></span>
            <h3>{{ integration.name }}</h3>
            <p>{{ integration.summary }}</p>
            <span class="integration-more">Details <ArrowRight :size="14" aria-hidden="true" /></span>
          </NuxtLink>
        </div>
      </div>
    </section>

    <section class="section planned-section">
      <div class="container">
        <div class="section-head">
          <h2>Planned</h2>
          <p class="lede">On the roadmap, not shipped yet. We say so honestly.</p>
        </div>
        <div class="integration-grid">
          <NuxtLink
            v-for="integration in plannedIntegrations"
            :key="integration.slug"
            :to="`/integrations/${integration.slug}`"
            class="card integration-card planned"
          >
            <span class="integration-icon"><FeatureIcon :name="integration.icon" :size="24" /></span>
            <h3>{{ integration.name }} <span class="badge badge-neutral">Planned</span></h3>
            <p>{{ integration.summary }}</p>
          </NuxtLink>
        </div>
      </div>
    </section>

    <CtaSection
      title="Need an integration we haven’t built?"
      body="Webhooks give you a signed event stream today. Telling us what you need shapes what we build next."
    />
  </div>
</template>

<style scoped>
.integration-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1rem;
}

.integration-card {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
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
  font-size: 0.94rem;
}

.integration-card h3 {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin: 0;
}

.integration-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 46px;
  height: 46px;
  margin-bottom: 0.4rem;
  border-radius: 10px;
  background: var(--accent-soft);
  color: var(--accent-text);
}

.integration-more {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  margin-top: 0.5rem;
  color: var(--accent-text);
  font-size: 0.88rem;
  font-weight: 600;
}

.planned-section {
  padding-top: 0;
}

.integration-card.planned {
  border-style: dashed;
}

@media (min-width: 800px) {
  .integration-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}
</style>
