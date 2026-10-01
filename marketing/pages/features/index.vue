<script setup lang="ts">
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
      eyebrow="Features"
      title="Every stage of the workflow, covered"
      lede="Collect feedback with context, decide what to work on, and let people know what changed."
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
    <section class="section catalog-section" aria-label="Features">
      <div class="container">
        <NuxtLink
          v-for="feature in features"
          :key="feature.slug"
          :to="`/features/${feature.slug}`"
          class="catalog-row"
        >
          <div>
            <h2>{{ feature.name }}</h2>
            <span class="catalog-meta"
              >{{ feature.loopStage }} · {{ feature.availability }}</span
            >
          </div>
          <p>{{ feature.summary }}</p>
          <ArrowUpRight :size="22" aria-hidden="true" />
        </NuxtLink>
      </div>
    </section>
    <CtaSection />
  </div>
</template>
