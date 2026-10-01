<script setup lang="ts">
import { Check } from '@lucide/vue'
import { featureBySlug, features } from '~/data/features'
import { featurePhotography } from '~/data/photography'

const route = useRoute()
const feature = featureBySlug(route.params.slug as string)

if (!feature) {
  throw createError({
    statusCode: 404,
    statusMessage: 'Feature not found',
    fatal: true,
  })
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
    <PageHero
      show-eyebrow
      :eyebrow="`${feature.loopStage} · ${feature.availability}`"
      :title="feature.name"
      :lede="feature.tagline"
    >
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
      <template v-if="featurePhotography[feature.slug]" #media
        ><EditorialPhoto :photo="featurePhotography[feature.slug]!" eager
      /></template>
    </PageHero>

    <section class="section feature-detail">
      <div class="container detail-grid">
        <div>
          <p class="lede">{{ feature.summary }}</p>
          <DetailSections :sections="feature.sections" />
        </div>
        <aside class="detail-aside">
          <div class="aside-section">
            <h2 class="aside-heading">At a glance</h2>
            <ul class="aside-list">
              <li v-for="highlight in feature.highlights" :key="highlight">
                <Check :size="16" aria-hidden="true" class="aside-check" />
                <span>{{ highlight }}</span>
              </li>
            </ul>
          </div>
          <div v-if="related.length" class="aside-section">
            <h2 class="aside-heading">Related</h2>
            <ul class="aside-related">
              <li v-for="entry in related" :key="entry.slug">
                <NuxtLink :to="`/features/${entry.slug}`">{{
                  entry.name
                }}</NuxtLink>
              </li>
            </ul>
          </div>
        </aside>
      </div>
    </section>

    <CtaSection />
  </div>
</template>
