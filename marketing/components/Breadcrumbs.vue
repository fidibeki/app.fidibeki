<script setup lang="ts">
defineProps<{
  items: { name: string; path: string }[]
}>()
</script>

<template>
  <nav class="breadcrumbs" aria-label="Breadcrumb">
    <ol>
      <li v-for="(item, index) in items" :key="item.path">
        <NuxtLink v-if="index < items.length - 1" :to="item.path">{{
          item.name
        }}</NuxtLink>
        <span v-else aria-current="page">{{ item.name }}</span>
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
