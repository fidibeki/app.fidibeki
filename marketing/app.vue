<script setup lang="ts">
const { copy } = useMarketingI18n()

import { siteDescription, siteName } from '~/data/site'
import { localizedPath, marketingLocales } from '~/utils/locale'

const { locale } = useI18n()
const config = useRuntimeConfig()
const siteUrl = config.public.siteUrl.replace(/\/$/, '')
const localeHead = useLocaleHead()
useHead(() => ({
  htmlAttrs: localeHead.value.htmlAttrs,
  link: localeHead.value.link?.map((link) => ({
    ...link,
    href: link.href ? new URL(link.href, siteUrl).href : link.href,
  })),
  meta: localeHead.value.meta,
}))

useHead(() => ({
  titleTemplate: (title) => title ?? siteName,
  script: [
    {
      type: 'application/ld+json',
      innerHTML: JSON.stringify({
        '@context': 'https://schema.org',
        '@type': 'Organization',
        name: siteName,
        url: `${siteUrl}/`,
        logo: `${siteUrl}/brand/wordmark-color@2x.png`,
        description: copy(siteDescription),
      }),
    },
    {
      type: 'application/ld+json',
      innerHTML: JSON.stringify({
        '@context': 'https://schema.org',
        '@type': 'WebSite',
        inLanguage: marketingLocales.find(
          (entry) => entry.code === locale.value,
        )!.language,
        name: siteName,
        url: `${siteUrl}${localizedPath('/', locale.value)}`,
      }),
    },
  ],
}))
</script>

<template>
  <div class="app-shell">
    <a href="#main" class="skip-link">{{ copy('Skip to content') }}</a>
    <SiteHeader />
    <main id="main" class="app-main">
      <NuxtPage :page-key="(route) => route.path" />
    </main>
    <SiteFooter />
    <ConsentBanner />
  </div>
</template>

<style scoped>
.app-shell {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
}

.app-main {
  flex: 1;
  min-width: 0;
}
</style>
