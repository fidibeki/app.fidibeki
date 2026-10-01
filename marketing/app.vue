<script setup lang="ts">
import { siteDescription, siteName } from '~/data/site'

const config = useRuntimeConfig()
const siteUrl = config.public.siteUrl.replace(/\/$/, '')

useHead({
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
        description: siteDescription,
      }),
    },
    {
      type: 'application/ld+json',
      innerHTML: JSON.stringify({
        '@context': 'https://schema.org',
        '@type': 'WebSite',
        name: siteName,
        url: `${siteUrl}/`,
      }),
    },
  ],
})
</script>

<template>
  <div class="app-shell">
    <a href="#main" class="skip-link">Skip to content</a>
    <SiteHeader />
    <main id="main" class="app-main">
      <NuxtPage />
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
