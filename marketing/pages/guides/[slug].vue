<script setup lang="ts">
const { copy } = useMarketingI18n()

import { sectionAnchor } from '~/utils/section-anchor'
import { guides } from '~/data/guides'

const route = useRoute()
const doc = guides.find(entry => entry.slug === route.params.slug)

if (!doc) {
  throw createError({
    statusCode: 404,
    statusMessage: 'Doc not found',
    fatal: true,
  })
}

const config = useRuntimeConfig()
const siteUrl = config.public.siteUrl.replace(/\/$/, '')

const otherDocs = guides.filter((entry) => entry.slug !== doc.slug)

useSiteMeta({
  title: doc.title,
  description: doc.description,
  path: `/guides/${doc.slug}`,
  breadcrumbs: [
    { name: 'Home', path: '/' },
    { name: 'Guides', path: '/guides' },
    { name: doc.title, path: `/guides/${doc.slug}` },
  ],
  structuredData: [
    {
      '@context': 'https://schema.org',
      '@type': 'Article',
      headline: doc.title,
      description: doc.description,
      url: `${siteUrl}/guides/${doc.slug}`,
      author: { '@type': 'Organization', name: 'Fidibeki', url: siteUrl },
      publisher: { '@type': 'Organization', name: 'Fidibeki', url: siteUrl },
      datePublished: doc.published,
    },
  ],
})
</script>

<template>
  <div v-if="doc">
    <PageHero
      :eyebrow="
        copy('Published by Fidibeki')
      "
      :title="copy(doc.title)"
      :lede="copy(doc.description)"
    >
      <template #breadcrumbs>
        <Breadcrumbs
          :items="[
            { name: 'Home', path: '/' },
            { name: 'Guides', path: '/guides' },
            { name: doc.title, path: `/guides/${doc.slug}` },
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
          <p class="guide-byline">{{ copy('Published by Fidibeki') }} · <time :datetime="doc.published">{{ copy('2 October 2026') }}</time></p>
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
            <h2 class="aside-heading">{{ copy('Related reading') }}</h2>
            <ul class="aside-related">
              <li v-for="link in doc.related" :key="link.to"><NuxtLinkLocale :to="link.to">{{ copy(link.label) }}</NuxtLinkLocale></li>
            </ul>
          </div>
          <div class="aside-section">
            <h2 class="aside-heading">{{ copy('More guides') }}</h2>
            <ul class="aside-related">
              <li v-for="entry in otherDocs" :key="entry.slug">
                <NuxtLinkLocale :to="`/guides/${entry.slug}`">{{
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
.guide-byline { color: var(--muted); font-size: 13px; margin-bottom: 36px; }
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
