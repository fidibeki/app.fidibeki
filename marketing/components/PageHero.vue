<script setup lang="ts">
defineProps<{
  eyebrow: string
  title: string
  lede: string
}>()
</script>

<template>
  <section class="page-hero">
    <div class="container">
      <slot name="breadcrumbs" />
      <p class="eyebrow">{{ eyebrow }}</p>
      <h1>{{ title }}</h1>
      <p class="lede">{{ lede }}</p>
      <div v-if="$slots.actions" class="hero-actions">
        <slot name="actions" />
      </div>
    </div>
  </section>
</template>

<style scoped>
.page-hero {
  position: relative;
  padding-block: clamp(3rem, 2.5rem + 3vw, 5.5rem) clamp(1.5rem, 1rem + 2vw, 3rem);
  background: var(--hero-glow);
}

.page-hero h1 {
  max-width: 46rem;
}

.hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.85rem;
  margin-top: 1.75rem;
}
</style>
