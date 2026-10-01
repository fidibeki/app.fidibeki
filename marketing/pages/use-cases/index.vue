<script setup lang="ts">
import { ArrowUpRight } from '@lucide/vue'
import { useCases } from '~/data/use-cases'
import { useCasePhotography } from '~/data/photography'

useSiteMeta({
  title: 'Use cases',
  description:
    'How engineering, product, support, and research teams run the feedback workflow with Fidibeki: bug reports, feature requests, support triage, and in-product research.',
  path: '/use-cases',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Use cases', path: '/use-cases' },
  ],
})
</script>

<template>
  <div>
    <PageHero
      eyebrow="Use cases"
      title="One workflow, different jobs"
      lede="A bug to reproduce. An idea to consider. A customer who needs an answer. Start with the work your team does."
    >
      <template #breadcrumbs
        ><Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Use cases', path: '/use-cases' },
          ]"
      /></template>
    </PageHero>
    <section class="section catalog-section" aria-label="Use cases">
      <div class="container usecase-grid">
        <NuxtLink
          v-for="useCase in useCases"
          :key="useCase.slug"
          :to="`/use-cases/${useCase.slug}`"
          class="usecase-entry"
        >
          <EditorialPhoto
            :photo="useCasePhotography[useCase.slug]!"
            decorative
            ratio="8 / 5"
          />
          <span class="usecase-audience">{{ useCase.audience }}</span>
          <h2>{{ useCase.name }}</h2>
          <p>{{ useCase.summary }}</p>
          <span class="usecase-link"
            >Explore the workflow <ArrowUpRight :size="18" aria-hidden="true"
          /></span>
        </NuxtLink>
      </div>
    </section>
    <CtaSection />
  </div>
</template>
<style scoped>
.usecase-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  column-gap: 48px;
  row-gap: 64px;
}
.usecase-entry {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}
.usecase-audience {
  color: var(--muted);
  font-size: 13px;
  margin-top: 24px;
  margin-bottom: 14px;
}
.usecase-entry h2 {
  font-size: 32px;
  margin-bottom: 20px;
}
.usecase-entry p {
  color: var(--muted);
  max-width: 480px;
  margin-bottom: 32px;
}
.usecase-link {
  display: inline-flex;
  align-items: center;
  gap: 12px;
  font-size: 14px;
  margin-top: auto;
}
.usecase-entry:hover {
  text-decoration: none;
}
.usecase-entry:hover .usecase-link {
  text-decoration: underline;
  text-underline-offset: 4px;
}
@media (max-width: 700px) {
  .usecase-grid {
    grid-template-columns: minmax(0, 1fr);
    row-gap: 36px;
  }
  .usecase-audience {
    margin-bottom: 24px;
  }
  .usecase-entry h2 {
    font-size: 28px;
  }
}
</style>
