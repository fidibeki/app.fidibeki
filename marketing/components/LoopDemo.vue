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
    <!-- The recording carries its own browser chrome in-frame, so the page
         only mounts it: a bezel on a soft glow, like a screen on a wall. -->
    <div class="loop-demo-frame">
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
    </div>
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
  position: relative;
  max-width: 58rem;
  margin: 0 auto 3.25rem;
}

/* Soft accent wash behind the frame so the dark recording sits in the page
   instead of punching a hole in it — same treatment family as the hero glow. */
.loop-demo::before {
  content: '';
  position: absolute;
  /* No horizontal bleed by default: on narrow screens the figure spans the
     viewport and anything wider forces horizontal scroll. */
  inset: -12% 0;
  background: radial-gradient(55% 55% at 50% 45%, var(--accent-soft), transparent 72%);
  pointer-events: none;
}

@media (min-width: 1100px) {
  /* Wide screens have page margin to absorb the halo. */
  .loop-demo::before {
    inset: -12% -8%;
  }
}

.loop-demo-frame {
  position: relative;
  padding: clamp(0.2rem, 0.5vw, 0.3rem);
  border: 1px solid var(--line);
  border-radius: calc(var(--radius-lg) + 3px);
  background: var(--surface);
  box-shadow: var(--shadow-card);
}

.loop-demo-video {
  display: block;
  width: 100%;
  height: auto;
  aspect-ratio: 16 / 9;
  border-radius: var(--radius-lg);
  background: var(--surface-soft);
}

.loop-demo-caption {
  position: relative;
  margin: 1rem auto 0;
  max-width: 46rem;
  text-align: center;
  color: var(--muted);
  font-size: 0.9rem;
}
</style>
