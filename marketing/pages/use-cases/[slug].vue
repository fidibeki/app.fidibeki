<script setup lang="ts">
const { copy } = useMarketingI18n()

import { useCaseBySlug } from '~/data/use-cases'
import { useCasePhotography } from '~/data/photography'
import { features } from '~/data/features'

const route = useRoute()
const useCase = useCaseBySlug(route.params.slug as string)

if (!useCase) {
  throw createError({
    statusCode: 404,
    statusMessage: 'Use case not found',
    fatal: true,
  })
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
    <PageHero
      show-eyebrow
      :eyebrow="copy(useCase.audience)"
      :title="copy(useCase.name)"
      :lede="copy(useCase.tagline)"
    >
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
        <a :href="links.signUp" class="btn btn-primary">{{
          copy('Start free trial')
        }}</a>
        <a
          :href="links.demo"
          class="btn btn-ghost"
          rel="noopener"
          target="_blank"
          >{{ copy('Book a demo') }}</a
        >
      </template>
      <template #media
        ><EditorialPhoto :photo="useCasePhotography[useCase.slug]!" eager
      /></template>
    </PageHero>

    <section class="section">
      <div class="container detail-grid">
        <div>
          <p class="lede">{{ copy(useCase.summary) }}</p>
          <DetailSections :sections="useCase.sections" />
        </div>
        <aside class="detail-aside">
          <div class="aside-section">
            <h2 class="aside-heading">
              {{ copy('Features in this workflow') }}
            </h2>
            <ul class="aside-related">
              <li v-for="entry in relatedFeatures" :key="entry.slug">
                <NuxtLinkLocale :to="`/features/${entry.slug}`">{{
                  copy(entry.name)
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
