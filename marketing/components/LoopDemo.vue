<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const videoRef = ref<HTMLVideoElement | null>(null)
// Reduced-motion visitors get a paused poster with controls instead of an
// autoplaying loop. Rendered false on the server so the prerendered markup
// stays identical for everyone; the real value lands on mount.
const reducedMotion = ref(false)
let observer: IntersectionObserver | null = null

onMounted(() => {
  const video = videoRef.value
  if (!video) return
  // Vue treats `muted` as an attribute, which only seeds the default. Autoplay
  // is blocked unless the property itself is set, so set it explicitly.
  video.muted = true
  reducedMotion.value = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  if (reducedMotion.value) return

  // Only spend bandwidth and decode time while the demo is actually on screen.
  observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          // Autoplay can still be refused (low power mode, data saver); the
          // poster stays up and nothing breaks.
          void video.play().catch(() => {})
        } else {
          video.pause()
        }
      }
    },
    { threshold: 0.35 },
  )
  observer.observe(video)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
})
</script>

<template>
  <figure class="loop-demo">
    <video
      ref="videoRef"
      class="loop-demo-video"
      :controls="reducedMotion"
      muted
      loop
      playsinline
      preload="none"
      poster="/demo/feedback-loop-poster.jpg"
      width="1280"
      height="720"
    >
      <source src="/demo/feedback-loop.mp4" type="video/mp4" />
    </video>
    <!-- The demo burns its captions into the video, so the same story is
         written out here for screen readers and anyone who blocks media. -->
    <figcaption class="loop-demo-caption">
      A guest taps a checkout button that does nothing, rates the page two stars, and writes one
      sentence. Fidibeki attaches the screenshot and the exact element, posts the report into
      Slack, and tells the guest once it is resolved.
    </figcaption>
  </figure>
</template>

<style scoped>
.loop-demo {
  /* The source is a 1080p screen recording whose UI is already composed large.
     Held under the full container width it reads at close to natural scale and
     leaves the loop diagram below room to breathe. */
  max-width: 60rem;
  margin: 0 0 3rem;
}

.loop-demo-video {
  display: block;
  width: 100%;
  height: auto;
  aspect-ratio: 16 / 9;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background: var(--surface-soft);
  box-shadow: var(--shadow-card);
}

.loop-demo-caption {
  margin-top: 0.85rem;
  color: var(--muted);
  font-size: 0.9rem;
  max-width: 44rem;
}
</style>
