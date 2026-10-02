<script setup lang="ts">
const { copy } = useMarketingI18n()

import { sectionAnchor } from '~/utils/section-anchor'
import { docBySlug, docs } from '~/data/docs'

const route = useRoute()
const doc = docBySlug(route.params.slug as string)

if (!doc) {
  throw createError({
    statusCode: 404,
    statusMessage: 'Doc not found',
    fatal: true,
  })
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
    <PageHero
      show-eyebrow
      :eyebrow="
        copy('Docs · {minutes} min read', { minutes: copy(doc.minutes) })
      "
      :title="copy(doc.title)"
      :lede="copy(doc.description)"
    >
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
        <div>
          <details class="mobile-contents">
            <summary>{{ copy('On this page') }}</summary>
            <nav :aria-label="copy('Article sections')">
              <ul class="aside-related">
                <li v-for="section in doc.sections" :key="section.heading">
                  <a :href="`#${sectionAnchor(section.heading)}`">{{
                    copy(section.heading)
                  }}</a>
                </li>
              </ul>
            </nav>
          </details>
          <DetailSections :sections="doc.sections" />
        </div>
        <aside class="detail-aside">
          <nav
            :aria-label="copy('On this page')"
            class="aside-section desktop-contents"
          >
            <h2 class="aside-heading">{{ copy('On this page') }}</h2>
            <ul class="aside-related">
              <li v-for="section in doc.sections" :key="section.heading">
                <a :href="`#${sectionAnchor(section.heading)}`">{{
                  copy(section.heading)
                }}</a>
              </li>
            </ul>
          </nav>
          <div class="aside-section">
            <h2 class="aside-heading">{{ copy('More guides') }}</h2>
            <ul class="aside-related">
              <li v-for="entry in otherDocs" :key="entry.slug">
                <NuxtLinkLocale :to="`/docs/${entry.slug}`">{{
                  copy(entry.title)
                }}</NuxtLinkLocale>
              </li>
            </ul>
          </div>
        </aside>
      </div>
    </section>

    <CtaSection
      :title="copy('Ready to try it on your own site?')"
      :body="
        copy(
          'Start a free trial, create a project, and the in-app checklist walks you through your first live connection.',
        )
      "
    />
  </div>
</template>

<style scoped>
.mobile-contents {
  display: none;
}
@media (max-width: 899px) {
  .desktop-contents {
    display: none;
  }
  .mobile-contents {
    display: block;
    padding-bottom: 24px;
    margin-bottom: 36px;
    border-bottom: 1px solid var(--line);
  }
  .mobile-contents summary {
    cursor: pointer;
    font-size: 14px;
    width: fit-content;
  }
  .mobile-contents nav {
    margin-top: 24px;
  }
}
</style>
