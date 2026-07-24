<script setup lang="ts">
import { ArrowRight } from '@lucide/vue'
import { useCases } from '~/data/use-cases'

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
      lede="The same capture-to-resolution workflow serves very different teams. Pick the shape that matches yours."
    >
      <template #breadcrumbs>
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Use cases', path: '/use-cases' }]" />
      </template>
    </PageHero>

    <section class="section">
      <div class="container">
        <div class="usecase-grid">
          <NuxtLink
            v-for="(useCase, index) in useCases"
            :key="useCase.slug"
            v-reveal="index * 60"
            :to="`/use-cases/${useCase.slug}`"
            class="card usecase-card"
          >
            <span class="usecase-icon"><FeatureIcon :name="useCase.icon" :size="24" /></span>
            <span class="usecase-audience">{{ useCase.audience }}</span>
            <h2>{{ useCase.name }}</h2>
            <p>{{ useCase.summary }}</p>
            <span class="usecase-more">Read more <ArrowRight :size="14" aria-hidden="true" /></span>
          </NuxtLink>
        </div>
      </div>
    </section>

    <CtaSection />
  </div>
</template>

<style scoped>
.usecase-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1rem;
}

.usecase-card {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  color: var(--ink);
  transition: border-color 160ms ease, transform 160ms ease;
}

.usecase-card:hover {
  text-decoration: none;
  border-color: var(--accent);
  transform: translateY(-3px);
}

.usecase-card h2 {
  margin: 0;
  font-size: 1.25rem;
}

.usecase-card p {
  margin: 0;
  color: var(--muted);
  font-size: 0.94rem;
}

.usecase-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 46px;
  height: 46px;
  margin-bottom: 0.4rem;
  border-radius: 10px;
  background: var(--accent-soft);
  color: var(--accent-text);
}

.usecase-audience {
  font-size: 0.76rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--accent-text);
}

.usecase-more {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  margin-top: 0.5rem;
  color: var(--accent-text);
  font-size: 0.88rem;
  font-weight: 600;
}

@media (min-width: 760px) {
  .usecase-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
