<script setup lang="ts">
import { integrationBySlug } from '~/data/integrations'

const route = useRoute()
const integration = integrationBySlug(route.params.slug as string)

if (!integration) {
  throw createError({ statusCode: 404, statusMessage: 'Integration not found', fatal: true })
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
    <PageHero :eyebrow="integration.availability" :title="`${integration.name} integration`" :lede="integration.tagline">
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Integrations', path: '/integrations' },
            { name: integration.name, path: `/integrations/${integration.slug}` },
          ]"
        />
      </template>
      <template #actions>
        <a v-if="integration.status === 'available'" :href="links.signUp" class="btn btn-primary">Start free trial</a>
        <a v-else :href="links.demo" class="btn btn-primary" rel="noopener" target="_blank">Tell us you need this</a>
        <NuxtLink to="/docs/webhooks" class="btn btn-ghost">Webhook docs</NuxtLink>
      </template>
    </PageHero>

    <section class="section">
      <div class="container detail-grid">
        <div>
          <p class="lede">{{ integration.summary }}</p>
          <DetailSections :sections="integration.sections" />
        </div>
        <aside v-if="integration.events" class="detail-aside">
          <div class="card">
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

<style scoped>
.detail-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 2.5rem;
  align-items: start;
}

.detail-grid > * {
  /* Grid items default to min-width auto; code blocks must not widen the page. */
  min-width: 0;
}

.aside-heading {
  font-size: 0.85rem;
  font-family: var(--font-body);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin-bottom: 0.9rem;
}

.event-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.event-list code {
  display: inline-block;
  padding: 0.25rem 0.6rem;
  border: 1px solid var(--line);
  border-radius: var(--radius);
  background: var(--surface-soft);
  font-size: 0.82rem;
}

@media (min-width: 900px) {
  .detail-grid {
    grid-template-columns: 1.6fr 1fr;
  }

  .detail-aside {
    position: sticky;
    top: calc(var(--header-height) + 1.5rem);
  }
}
</style>
