<script setup lang="ts">
import { ArrowUpRight } from '@lucide/vue'
import { guides } from '~/data/guides'
import { photography } from '~/data/photography'
const { copy } = useMarketingI18n()
useSiteMeta({
  title: 'Practical guides to website feedback',
  description: 'Choose a feedback tool, collect a useful bug report, and decide what to do next. Practical guides for small SaaS teams.',
  path: '/guides',
  breadcrumbs: [{ name: 'Home', path: '/' }, { name: 'Guides', path: '/guides' }],
})
</script>

<template>
  <div>
    <PageHero :eyebrow="copy('Guides')" :title="copy('Practical guides to website feedback')" :lede="copy('Choose a feedback tool, collect a useful bug report, and decide what to do next. Practical guides for small SaaS teams.')">
      <template #breadcrumbs><Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Guides', path: '/guides' }]" /></template>
    </PageHero>
    <section class="section">
      <div class="container guide-grid">
        <NuxtLinkLocale v-for="guide in guides" :key="guide.slug" :to="`/guides/${guide.slug}`" class="guide-entry">
          <EditorialPhoto :photo="photography[guide.photo]" decorative ratio="8 / 5" />
          <h2>{{ copy(guide.title) }}</h2>
          <p>{{ copy(guide.description) }}</p>
          <span>{{ copy('Read guide') }} <ArrowUpRight :size="18" aria-hidden="true" /></span>
        </NuxtLinkLocale>
      </div>
    </section>
    <CtaSection />
  </div>
</template>

<style scoped>
.guide-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 48px; }
.guide-entry { display: flex; flex-direction: column; align-items: flex-start; }
.guide-entry h2 { font-size: clamp(24px, 2.4vw, 32px); margin: 24px 0 16px; }
.guide-entry p { color: var(--muted); margin-bottom: 24px; }
.guide-entry span { display: inline-flex; align-items: center; gap: 12px; margin-top: auto; font-size: 14px; }
.guide-entry:hover { text-decoration: none; }
.guide-entry:hover span { text-decoration: underline; text-underline-offset: 4px; }
@media (max-width: 700px) { .guide-grid { grid-template-columns: minmax(0, 1fr); gap: 40px; } }
</style>
