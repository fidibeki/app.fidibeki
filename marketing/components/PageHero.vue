<script setup lang="ts">
defineProps<{
  eyebrow: string
  title: string
  lede: string
  showEyebrow?: boolean
}>()
</script>

<template>
  <section class="page-hero">
    <div class="container">
      <slot name="breadcrumbs" />
      <div class="hero-layout" :class="{ 'hero-with-media': $slots.media }">
        <div class="hero-copy">
          <p v-if="showEyebrow || !$slots.breadcrumbs" class="eyebrow">
            {{ eyebrow }}
          </p>
          <h1>{{ title }}</h1>
          <p class="lede">{{ lede }}</p>
          <div v-if="$slots.actions" class="hero-actions">
            <slot name="actions" />
          </div>
        </div>
        <div v-if="$slots.media" class="hero-media"><slot name="media" /></div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.hero-with-media {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 64px;
  align-items: center;
}
.hero-copy,
.hero-media {
  min-width: 0;
}
.hero-with-media h1 {
  font-size: clamp(34px, 3.8vw, 48px);
}
.hero-with-media .lede {
  font-size: 17px;
}
@media (max-width: 899px) {
  .hero-with-media {
    grid-template-columns: minmax(0, 1fr);
    gap: 32px;
  }
}

.page-hero {
  padding-block: 64px 40px;
}
.page-hero :deep(.breadcrumbs) {
  margin-bottom: 40px;
}
.page-hero h1 {
  max-width: 900px;
  margin-bottom: 24px;
}
.page-hero .lede {
  max-width: 660px;
  margin-bottom: 0;
}
.hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 28px;
}
@media (max-width: 600px) {
  .page-hero {
    padding-block: 32px 28px;
  }
  .page-hero :deep(.breadcrumbs) {
    margin-bottom: 28px;
  }
  .page-hero h1 {
    margin-bottom: 20px;
  }
}
</style>
