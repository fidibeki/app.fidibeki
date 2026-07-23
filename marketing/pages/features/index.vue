<script setup lang="ts">
import { ArrowRight } from '@lucide/vue'
import { features } from '~/data/features'

useSiteMeta({
  title: 'Features',
  description:
    'Feedback widget, session replay, AI analysis, issue triage, reporter updates, and workspace permissions — every Fidibeki capability, explained.',
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
      title="Every stage of the loop, covered"
      lede="From the first script tag to the last reporter email — these are the capabilities that move feedback from noise to outcome."
    >
      <template #breadcrumbs>
        <Breadcrumbs :items="[{ name: 'Home', path: '/' }, { name: 'Features', path: '/features' }]" />
      </template>
    </PageHero>

    <section class="section">
      <div class="container">
        <div class="feature-list">
          <NuxtLink
            v-for="(feature, index) in features"
            :key="feature.slug"
            v-reveal="index * 60"
            :to="`/features/${feature.slug}`"
            class="card feature-row"
          >
            <span class="feature-icon"><FeatureIcon :name="feature.icon" :size="24" /></span>
            <div class="feature-row-body">
              <div class="feature-row-head">
                <h2>{{ feature.name }}</h2>
                <span class="badge badge-neutral">{{ feature.loopStage }}</span>
                <span class="badge">{{ feature.availability }}</span>
              </div>
              <p>{{ feature.summary }}</p>
              <span class="feature-more">Explore {{ feature.name.toLowerCase() }} <ArrowRight :size="14" aria-hidden="true" /></span>
            </div>
          </NuxtLink>
        </div>
      </div>
    </section>

    <CtaSection />
  </div>
</template>

<style scoped>
.feature-list {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.feature-row {
  display: flex;
  gap: 1.25rem;
  align-items: flex-start;
  color: var(--ink);
  transition: border-color 160ms ease, transform 160ms ease;
}

.feature-row:hover {
  text-decoration: none;
  border-color: var(--accent);
  transform: translateY(-2px);
}

.feature-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 46px;
  height: 46px;
  border-radius: 10px;
  background: var(--accent-soft);
  color: var(--accent-text);
  flex-shrink: 0;
}

.feature-row-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.6rem;
  margin-bottom: 0.4rem;
}

.feature-row-head h2 {
  margin: 0;
  font-size: 1.3rem;
}

.feature-row-body p {
  margin: 0;
  color: var(--muted);
}

.feature-more {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  margin-top: 0.7rem;
  color: var(--accent-text);
  font-size: 0.9rem;
  font-weight: 600;
}

@media (max-width: 600px) {
  .feature-row {
    flex-direction: column;
  }
}
</style>
