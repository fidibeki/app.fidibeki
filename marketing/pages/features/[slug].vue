<script setup lang="ts">
import { Check } from '@lucide/vue'
import { featureBySlug, features } from '~/data/features'

const route = useRoute()
const feature = featureBySlug(route.params.slug as string)

if (!feature) {
  throw createError({ statusCode: 404, statusMessage: 'Feature not found', fatal: true })
}

const links = useAppLinks()

const related = feature.related
  .map((slug) => features.find((entry) => entry.slug === slug))
  .filter((entry) => entry !== undefined)

useSiteMeta({
  title: feature.name,
  description: feature.tagline,
  path: `/features/${feature.slug}`,
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Features', path: '/features' },
    { name: feature.name, path: `/features/${feature.slug}` },
  ],
})
</script>

<template>
  <div v-if="feature">
    <PageHero :eyebrow="`${feature.loopStage} · ${feature.availability}`" :title="feature.name" :lede="feature.tagline">
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Features', path: '/features' },
            { name: feature.name, path: `/features/${feature.slug}` },
          ]"
        />
      </template>
      <template #actions>
        <a :href="links.signUp" class="btn btn-primary">Start free trial</a>
        <NuxtLink to="/docs" class="btn btn-ghost">Read the docs</NuxtLink>
      </template>
    </PageHero>

    <section class="section feature-detail">
      <div class="container detail-grid">
        <div>
          <p class="lede">{{ feature.summary }}</p>
          <DetailSections :sections="feature.sections" />
        </div>
        <aside class="detail-aside">
          <div class="card">
            <h2 class="aside-heading">At a glance</h2>
            <ul class="aside-list">
              <li v-for="highlight in feature.highlights" :key="highlight">
                <Check :size="16" aria-hidden="true" class="aside-check" />
                <span>{{ highlight }}</span>
              </li>
            </ul>
          </div>
          <div v-if="related.length" class="card">
            <h2 class="aside-heading">Related</h2>
            <ul class="aside-related">
              <li v-for="entry in related" :key="entry.slug">
                <NuxtLink :to="`/features/${entry.slug}`">{{ entry.name }}</NuxtLink>
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

.detail-aside {
  display: flex;
  flex-direction: column;
  gap: 1rem;
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

.aside-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.65rem;
  font-size: 0.92rem;
}

.aside-list li {
  display: flex;
  gap: 0.5rem;
  align-items: flex-start;
}

.aside-check {
  color: var(--accent);
  flex-shrink: 0;
  margin-top: 0.18rem;
}

.aside-related {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  font-weight: 600;
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
