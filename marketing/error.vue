<script setup lang="ts">
const { copy } = useMarketingI18n()

import type { NuxtError } from '#app'

const props = defineProps<{ error: NuxtError }>()

const is404 = computed(() => props.error.statusCode === 404)

useHead({
  title: is404.value
    ? 'Page not found · Fidibeki'
    : 'Something went wrong · Fidibeki',
  meta: [{ name: 'robots', content: 'noindex' }],
})
</script>

<template>
  <div class="error-page">
    <a href="#main" class="skip-link">{{ copy('Skip to content') }}</a>
    <SiteHeader />
    <main id="main" class="error-main">
      <div class="container error-inner">
        <p class="eyebrow">{{ copy(error.statusCode) }}</p>
        <h1>
          {{
            copy(is404 ? 'This page does not exist' : 'Something went wrong')
          }}
        </h1>
        <p class="lede">
          {{
            copy(
              is404
                ? 'The page you are looking for may have moved. Try one of the links below.'
                : 'An unexpected error occurred. The home page is a safe place to start over.',
            )
          }}
        </p>
        <div class="error-actions">
          <NuxtLinkLocale to="/" class="btn btn-primary">{{
            copy('Back to home')
          }}</NuxtLinkLocale>
          <NuxtLinkLocale to="/docs" class="btn btn-ghost">{{
            copy('Read the docs')
          }}</NuxtLinkLocale>
        </div>
      </div>
    </main>
    <SiteFooter />
  </div>
</template>

<style scoped>
.error-page {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
}

.error-main {
  flex: 1;
  display: flex;
  align-items: center;
}

.error-inner {
  padding-block: 5rem;
}

.error-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.85rem;
  margin-top: 1.5rem;
}
</style>
