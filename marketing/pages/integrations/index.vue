<script setup lang="ts">
import { ArrowUpRight } from '@lucide/vue'
import { availableIntegrations, plannedIntegrations } from '~/data/integrations'

useSiteMeta({
  title: 'Integrations',
  description:
    'Connect Fidibeki with Slack, Microsoft Teams, Jira, Linear, and your own systems. Compare integration capabilities and plan availability.',
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
      lede="Send updates to your team, link feedback to the work, or connect your own systems."
    >
      <template #breadcrumbs
        ><Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Integrations', path: '/integrations' },
          ]"
      /></template>
    </PageHero>
    <div class="container integration-example">
      <HomeIntegrations compact />
    </div>
    <section
      class="section catalog-section"
      aria-labelledby="available-heading"
    >
      <div class="container">
        <h2 id="available-heading" class="catalog-heading">
          Available integrations
        </h2>
        <NuxtLink
          v-for="integration in availableIntegrations"
          :key="integration.slug"
          :to="`/integrations/${integration.slug}`"
          class="catalog-row"
        >
          <div>
            <h3>{{ integration.name }}</h3>
            <span class="catalog-meta">{{ integration.availability }}</span>
          </div>
          <p>{{ integration.summary }}</p>
          <ArrowUpRight :size="22" aria-hidden="true" />
        </NuxtLink>
      </div>
    </section>
    <section
      v-if="plannedIntegrations.length"
      class="section planned-section"
      aria-labelledby="planned-heading"
    >
      <div class="container">
        <h2 id="planned-heading" class="catalog-heading">On the roadmap</h2>
        <NuxtLink
          v-for="integration in plannedIntegrations"
          :key="integration.slug"
          :to="`/integrations/${integration.slug}`"
          class="catalog-row"
        >
          <div>
            <h3>{{ integration.name }}</h3>
            <span class="catalog-meta">Planned · Not available yet</span>
          </div>
          <p>{{ integration.summary }}</p>
          <ArrowUpRight :size="22" aria-hidden="true" />
        </NuxtLink>
      </div>
    </section>
    <CtaSection
      title="Need another connection?"
      body="Use signed webhooks to connect your own systems, or tell us which integration your team needs."
    />
  </div>
</template>
<style scoped>
.integration-example {
  padding-block: 24px 64px;
}
.catalog-heading {
  font-size: 15px;
  font-weight: 400;
  color: var(--muted);
  margin-bottom: 28px;
}
.planned-section {
  padding-top: 0;
}
@media (max-width: 600px) {
  .integration-example {
    padding-block: 0 40px;
  }
}
</style>
