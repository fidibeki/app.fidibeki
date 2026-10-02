<script setup lang="ts">
const { copy } = useMarketingI18n()

defineProps<{
  items: { name: string; path: string }[]
}>()
</script>

<template>
  <nav class="breadcrumbs" :aria-label="copy('Breadcrumb')">
    <ol>
      <li v-for="(item, index) in items" :key="item.path">
        <NuxtLinkLocale v-if="index < items.length - 1" :to="item.path">{{
          copy(item.name)
        }}</NuxtLinkLocale>
        <span v-else aria-current="page">{{ copy(item.name) }}</span>
      </li>
    </ol>
  </nav>
</template>

<style scoped>
.breadcrumbs {
  margin-bottom: 1.25rem;
  font-size: 0.85rem;
}

.breadcrumbs ol {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
  list-style: none;
  margin: 0;
  padding: 0;
}

.breadcrumbs li {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
}

.breadcrumbs li + li::before {
  content: '/';
  color: var(--muted);
}

.breadcrumbs a {
  color: var(--muted);
}

.breadcrumbs a:hover {
  color: var(--accent-text);
}

.breadcrumbs span[aria-current] {
  color: var(--ink-soft);
  font-weight: 400;
}
</style>
