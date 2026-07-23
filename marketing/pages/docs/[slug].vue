<script setup lang="ts">
import { docBySlug, docs } from '~/data/docs'

const route = useRoute()
const doc = docBySlug(route.params.slug as string)

if (!doc) {
  throw createError({ statusCode: 404, statusMessage: 'Doc not found', fatal: true })
}

const config = useRuntimeConfig()
const siteUrl = config.public.siteUrl.replace(/\/$/, '')

const otherDocs = docs.filter((entry) => entry.slug !== doc.slug)

useSiteMeta({
  title: doc.title,
  description: doc.description,
  path: `/docs/${doc.slug}`,
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Docs', path: '/docs' },
    { name: doc.title, path: `/docs/${doc.slug}` },
  ],
  structuredData: [
    {
      '@context': 'https://schema.org',
      '@type': 'TechArticle',
      headline: doc.title,
      description: doc.description,
      url: `${siteUrl}/docs/${doc.slug}`,
      author: { '@type': 'Organization', name: 'Fidibeki' },
    },
  ],
})
</script>

<template>
  <div v-if="doc">
    <PageHero :eyebrow="`Docs · ${doc.minutes} min read`" :title="doc.title" :lede="doc.description">
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Docs', path: '/docs' },
            { name: doc.title, path: `/docs/${doc.slug}` },
          ]"
        />
      </template>
    </PageHero>

    <section class="section">
      <div class="container detail-grid">
        <DetailSections :sections="doc.sections" />
        <aside class="detail-aside">
          <div class="card">
            <h2 class="aside-heading">More guides</h2>
            <ul class="aside-related">
              <li v-for="entry in otherDocs" :key="entry.slug">
                <NuxtLink :to="`/docs/${entry.slug}`">{{ entry.title }}</NuxtLink>
              </li>
            </ul>
          </div>
        </aside>
      </div>
    </section>

    <CtaSection
      title="Ready to try it on your own site?"
      body="Start a free trial, create a project, and the in-app checklist walks you through your first live connection."
    />
  </div>
</template>

<style scoped>
.detail-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 2.5rem;
  align-items: start;
}

.detail-grid > * {
  /* Grid items default to min-width auto; code blocks must not widen the page. */
  min-width: 0;
}

.aside-heading {
  font-size: 0.85rem;
  font-family: var(--font-body);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin-bottom: 0.9rem;
}

.aside-related {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  font-weight: 600;
}

@media (min-width: 900px) {
  .detail-grid {
    grid-template-columns: 1.6fr 1fr;
  }

  .detail-aside {
    position: sticky;
    top: calc(var(--header-height) + 1.5rem);
  }
}
</style>
