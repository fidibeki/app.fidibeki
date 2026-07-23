<script setup lang="ts">
import type { NuxtError } from '#app'

const props = defineProps<{ error: NuxtError }>()

const is404 = computed(() => props.error.statusCode === 404)

useHead({
  title: is404.value ? 'Page not found · Fidibeki' : 'Something went wrong · Fidibeki',
  meta: [{ name: 'robots', content: 'noindex' }],
})
</script>

<template>
  <div class="error-page">
    <a href="#main" class="skip-link">Skip to content</a>
    <SiteHeader />
    <main id="main" class="error-main">
      <div class="container error-inner">
        <p class="eyebrow">{{ error.statusCode }}</p>
        <h1>{{ is404 ? 'This page does not exist' : 'Something went wrong' }}</h1>
        <p class="lede">
          {{
            is404
              ? 'The page you are looking for may have moved. Try one of the links below.'
              : 'An unexpected error occurred. The home page is a safe place to start over.'
          }}
        </p>
        <div class="error-actions">
          <NuxtLink to="/" class="btn btn-primary">Back to home</NuxtLink>
          <NuxtLink to="/docs" class="btn btn-ghost">Read the docs</NuxtLink>
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
  padding-top: var(--header-height);
}

.error-inner {
  padding-block: 5rem;
}

.error-actions {
  display: flex;
  gap: 0.85rem;
  margin-top: 1.5rem;
}
</style>
