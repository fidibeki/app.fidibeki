<script setup lang="ts">
import { useCaseBySlug } from '~/data/use-cases'
import { features } from '~/data/features'

const route = useRoute()
const useCase = useCaseBySlug(route.params.slug as string)

if (!useCase) {
  throw createError({ statusCode: 404, statusMessage: 'Use case not found', fatal: true })
}

const links = useAppLinks()

const relatedFeatures = useCase.featureSlugs
  .map((slug) => features.find((entry) => entry.slug === slug))
  .filter((entry) => entry !== undefined)

useSiteMeta({
  title: useCase.name,
  description: useCase.tagline,
  path: `/use-cases/${useCase.slug}`,
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Use cases', path: '/use-cases' },
    { name: useCase.name, path: `/use-cases/${useCase.slug}` },
  ],
})
</script>

<template>
  <div v-if="useCase">
    <PageHero :eyebrow="useCase.audience" :title="useCase.name" :lede="useCase.tagline">
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Use cases', path: '/use-cases' },
            { name: useCase.name, path: `/use-cases/${useCase.slug}` },
          ]"
        />
      </template>
      <template #actions>
        <a :href="links.signUp" class="btn btn-primary">Start free trial</a>
        <a :href="links.demo" class="btn btn-ghost" rel="noopener" target="_blank">Book a demo</a>
      </template>
    </PageHero>

    <section class="section">
      <div class="container detail-grid">
        <div>
          <p class="lede">{{ useCase.summary }}</p>
          <DetailSections :sections="useCase.sections" />
        </div>
        <aside class="detail-aside">
          <div class="card">
            <h2 class="aside-heading">Features in this workflow</h2>
            <ul class="aside-related">
              <li v-for="entry in relatedFeatures" :key="entry.slug">
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

.aside-heading {
  font-size: 0.85rem;
  font-family: var(--font-body);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin-bottom: 0.9rem;
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
