<script setup lang="ts">
const { copy } = useMarketingI18n()

import { ArrowUpRight } from '@lucide/vue'
import { features } from '~/data/features'
import { photography } from '~/data/photography'

useSiteMeta({
  title: 'Features',
  description:
    'Feedback widget, optional session replay, AI assisted analysis on eligible plans, issue triage, reporter updates, and workspace permissions. Every Fidibeki capability, explained.',
  path: '/features',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Features', path: '/features' },
  ],
})
</script>

<template>
  <div>
    <PageHero
      :eyebrow="copy('Features')"
      :title="copy('Every stage of the workflow, covered')"
      :lede="
        copy(
          'Collect feedback with context, decide what to work on, and let people know what changed.',
        )
      "
    >
      <template #breadcrumbs
        ><Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Features', path: '/features' },
          ]"
      /></template>
      <template #media
        ><EditorialPhoto :photo="photography.collaboration" eager
      /></template>
    </PageHero>
    <section class="section catalog-section" :aria-label="copy('Features')">
      <div class="container">
        <NuxtLinkLocale
          v-for="feature in features"
          :key="feature.slug"
          :to="`/features/${feature.slug}`"
          class="catalog-row"
        >
          <div>
            <h2>{{ copy(feature.name) }}</h2>
            <span class="catalog-meta"
              >{{ copy(feature.loopStage) }} ·
              {{ copy(feature.availability) }}</span
            >
          </div>
          <p>{{ copy(feature.summary) }}</p>
          <ArrowUpRight :size="22" aria-hidden="true" />
        </NuxtLinkLocale>
      </div>
    </section>
    <CtaSection />
  </div>
</template>
