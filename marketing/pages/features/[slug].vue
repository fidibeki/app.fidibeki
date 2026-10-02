<script setup lang="ts">
const { copy } = useMarketingI18n()

import { Check } from '@lucide/vue'
import { featureBySlug, features } from '~/data/features'
import { competitors, versusPath } from '~/data/comparisons'
import { guides } from '~/data/guides'
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

const comparisons = competitors.filter((entry) =>
  entry.featureSlugs.includes(feature.slug),
)
const reading = guides.filter(guide => guide.related.some(link => link.to === `/features/${feature.slug}`))
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
      :eyebrow="`${copy(feature.loopStage)} · ${copy(feature.availability)}`"
      :title="copy(feature.name)"
      :lede="copy(feature.tagline)"
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
        <a :href="links.signUp" class="btn btn-primary">{{
          copy('Start free trial')
        }}</a>
        <NuxtLinkLocale to="/docs" class="btn btn-ghost">{{
          copy('Read the docs')
        }}</NuxtLinkLocale>
      </template>
      <template v-if="featurePhotography[feature.slug]" #media
        ><EditorialPhoto :photo="featurePhotography[feature.slug]!" eager
      /></template>
    </PageHero>

    <section class="section feature-detail">
      <div class="container detail-grid">
        <div>
          <p class="lede">{{ copy(feature.summary) }}</p>
          <DetailSections :sections="feature.sections" />
        </div>
        <aside class="detail-aside">
          <div v-if="reading.length" class="aside-section">
            <h2 class="aside-heading">{{ copy('Related reading') }}</h2>
            <ul class="aside-related"><li v-for="guide in reading" :key="guide.slug"><NuxtLinkLocale :to="`/guides/${guide.slug}`">{{ copy(guide.title) }}</NuxtLinkLocale></li></ul>
          </div>
          <div class="aside-section">
            <h2 class="aside-heading">{{ copy('At a glance') }}</h2>
            <ul class="aside-list">
              <li v-for="highlight in feature.highlights" :key="highlight">
                <Check :size="16" aria-hidden="true" class="aside-check" />
                <span>{{ copy(highlight) }}</span>
              </li>
            </ul>
          </div>
          <div v-if="related.length" class="aside-section">
            <h2 class="aside-heading">{{ copy('Related') }}</h2>
            <ul class="aside-related">
              <li v-for="entry in related" :key="entry.slug">
                <NuxtLinkLocale :to="`/features/${entry.slug}`">{{
                  copy(entry.name)
                }}</NuxtLinkLocale>
              </li>
            </ul>
          </div>
          <div v-if="comparisons.length" class="aside-section">
            <h2 class="aside-heading">{{ copy('Compare tools') }}</h2>
            <ul class="aside-related">
              <li v-for="entry in comparisons" :key="entry.slug">
                <NuxtLinkLocale :to="versusPath(entry.slug)">{{
                  copy('Fidibeki vs {competitor}', { competitor: entry.name })
                }}</NuxtLinkLocale>
              </li>
            </ul>
          </div>
        </aside>
      </div>
    </section>

    <CtaSection />
  </div>
</template>
