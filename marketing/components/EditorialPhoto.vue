<script setup lang="ts">
import type { EditorialImage } from '~/data/photography'

withDefaults(
  defineProps<{
    photo: EditorialImage
    eager?: boolean
    decorative?: boolean
    caption?: string
    ratio?: string
  }>(),
  { eager: false, decorative: false, ratio: '3 / 2' },
)
</script>

<template>
  <figure
    class="editorial-photo"
    :style="{
      '--photo-ratio': ratio,
      '--photo-position': photo.position || '50% 50%',
    }"
  >
    <div class="photo-frame">
      <img
        :src="`/images/editorial/${photo.name}-1200.webp`"
        :srcset="`/images/editorial/${photo.name}-640.webp 640w, /images/editorial/${photo.name}-1200.webp 1200w`"
        sizes="(min-width: 1264px) 568px, (min-width: 900px) 46vw, (min-width: 600px) calc(100vw - 48px), calc(100vw - 40px)"
        :width="photo.width"
        :height="photo.height"
        :alt="decorative ? '' : photo.alt"
        :loading="eager ? 'eager' : 'lazy'"
        :fetchpriority="eager ? 'high' : 'auto'"
        decoding="async"
      />
    </div>
    <figcaption v-if="caption">
      <span>{{ caption }}</span>
      <a
        :href="photo.source"
        target="_blank"
        rel="noopener"
        :aria-label="`Photo by ${photo.photographer} on Unsplash`"
        >{{ photo.photographer }} ↗</a
      >
    </figcaption>
  </figure>
</template>

<style scoped>
.editorial-photo {
  margin: 0;
  min-width: 0;
  width: 100%;
}
.photo-frame {
  aspect-ratio: var(--photo-ratio);
  overflow: hidden;
  border-radius: 10px;
  background: var(--surface-soft);
}
.photo-frame img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: var(--photo-position);
}
figcaption {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 8px 20px;
  margin-top: 14px;
  color: var(--muted);
  font-size: 11px;
  line-height: 1.5;
}
figcaption a:hover {
  color: var(--ink);
}
</style>
