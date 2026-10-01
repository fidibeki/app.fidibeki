<script setup lang="ts">
import { integrationBySlug } from '~/data/integrations'

const route = useRoute()
const integration = integrationBySlug(route.params.slug as string)

if (!integration) {
  throw createError({
    statusCode: 404,
    statusMessage: 'Integration not found',
    fatal: true,
  })
}

const links = useAppLinks()

useSiteMeta({
  title: `${integration.name} integration`,
  description: integration.tagline,
  path: `/integrations/${integration.slug}`,
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Integrations', path: '/integrations' },
    { name: integration.name, path: `/integrations/${integration.slug}` },
  ],
})
</script>

<template>
  <div v-if="integration">
    <PageHero
      show-eyebrow
      :eyebrow="integration.availability"
      :title="`${integration.name} integration`"
      :lede="integration.tagline"
    >
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Integrations', path: '/integrations' },
            {
              name: integration.name,
              path: `/integrations/${integration.slug}`,
            },
          ]"
        />
      </template>
      <template #actions>
        <a
          v-if="integration.status === 'available'"
          :href="links.signUp"
          class="btn btn-primary"
          >Start free trial</a
        >
        <a
          v-else
          :href="links.demo"
          class="btn btn-primary"
          rel="noopener"
          target="_blank"
          >Tell us you need this</a
        >
        <NuxtLink to="/docs/webhooks" class="btn btn-ghost"
          >Webhook docs</NuxtLink
        >
      </template>
    </PageHero>

    <section class="section">
      <div class="container detail-grid">
        <div>
          <p class="lede">{{ integration.summary }}</p>
          <DetailSections :sections="integration.sections" />
        </div>
        <aside v-if="integration.events" class="detail-aside">
          <div class="aside-section">
            <h2 class="aside-heading">Subscribable events</h2>
            <ul class="event-list">
              <li v-for="event in integration.events" :key="event">
                <code>{{ event }}</code>
              </li>
            </ul>
          </div>
        </aside>
      </div>
    </section>

    <CtaSection />
  </div>
</template>
