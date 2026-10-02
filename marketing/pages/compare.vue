<script setup lang="ts">
import { ArrowRight, ArrowUpRight } from '@lucide/vue'
import { competitors, alternativePath, versusPath } from '~/data/comparisons'
const { copy } = useMarketingI18n()
useSiteMeta({
  title: 'Compare website feedback tools',
  description:
    'Compare Fidibeki with Marker.io, Userback, BugHerd and Canny. Explore product fit, plan limits and practical switching guides with official sources.',
  path: '/compare',
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Compare', path: '/compare' },
  ],
})
</script>
<template>
  <div>
    <PageHero
      :eyebrow="copy('Compare')"
      :title="copy('Choose the tool for the work.')"
      :lede="
        copy(
          'Compare feedback capture, team workflows and pricing. Start with the task your team needs to complete.',
        )
      "
      ><template #breadcrumbs
        ><Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Compare', path: '/compare' },
          ]" /></template
    ></PageHero>
    <section class="section comparison-directory">
      <div class="container">
        <article
          v-for="competitor in competitors"
          :key="competitor.slug"
          class="comparison-directory-row"
        >
          <div>
            <h2>{{ competitor.name }}</h2>
            <p>{{ copy(competitor.focus) }}</p>
          </div>
          <div class="comparison-directory-links">
            <NuxtLinkLocale :to="versusPath(competitor.slug)"
              >{{
                copy('Fidibeki vs {competitor}', {
                  competitor: competitor.name,
                })
              }}
              <ArrowRight :size="18" aria-hidden="true" /></NuxtLinkLocale
            ><NuxtLinkLocale :to="alternativePath(competitor.slug)"
              >{{
                copy('An alternative to {competitor}', {
                  competitor: competitor.name,
                })
              }}
              <ArrowUpRight :size="18" aria-hidden="true"
            /></NuxtLinkLocale>
          </div>
        </article>
        <p class="comparison-directory-note">
          {{
            copy(
              'The comparison pages explain differences. The alternative guides help you plan a switch. Both include sources, plan limits and reasons to keep your current tool.',
            )
          }}
        </p>
      </div>
    </section>
    <CtaSection />
  </div>
</template>
<style scoped>
.comparison-directory {
  padding-top: 12px;
}
.comparison-directory-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 48px;
  padding-block: 40px;
  border-bottom: 1px solid var(--line);
}
.comparison-directory-row h2 {
  font-size: 30px;
}
.comparison-directory-row p {
  color: var(--muted);
  margin: 0;
}
.comparison-directory-links {
  display: grid;
  gap: 12px;
  align-content: center;
}
.comparison-directory-links a {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  min-height: 44px;
}
.comparison-directory-links svg {
  flex-shrink: 0;
}
.comparison-directory-note {
  color: var(--muted);
  max-width: 720px;
  margin-top: 40px;
}
@media (max-width: 650px) {
  .comparison-directory-row {
    grid-template-columns: minmax(0, 1fr);
    gap: 20px;
    padding-block: 28px;
  }
}
</style>
