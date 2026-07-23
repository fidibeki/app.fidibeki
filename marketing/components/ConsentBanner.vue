<script setup lang="ts">
const { choice, accept, decline, initialize } = useConsent()

// Initialized on mount so the prerendered HTML never contains the banner
// and hydration stays consistent.
const ready = ref(false)

onMounted(() => {
  initialize()
  ready.value = true
})

const visible = computed(() => ready.value && choice.value === 'pending')
</script>

<template>
  <aside v-if="visible" class="consent-banner" aria-label="Privacy choices">
    <p class="consent-text">
      We use privacy-friendly analytics (Umami) to see what's working — no cookies, no personal
      data, and only with your consent.
      <NuxtLink to="/privacy">Learn more</NuxtLink>
    </p>
    <div class="consent-actions">
      <button type="button" class="btn btn-ghost consent-btn" @click="decline">Decline</button>
      <button type="button" class="btn btn-primary consent-btn" @click="accept">Allow</button>
    </div>
  </aside>
</template>

<style scoped>
.consent-banner {
  position: fixed;
  bottom: 1rem;
  left: 1rem;
  right: 1rem;
  z-index: 60;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 0.75rem 1.5rem;
  max-width: 46rem;
  margin-inline: auto;
  padding: 0.85rem 1.25rem;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--canvas) 92%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  box-shadow: var(--shadow-overlay);
  animation: consent-in 420ms cubic-bezier(0.2, 0.65, 0.25, 1);
}

@keyframes consent-in {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
}

.consent-text {
  margin: 0;
  font-size: 0.88rem;
  color: var(--ink-soft);
  text-wrap: pretty;
}

.consent-actions {
  display: flex;
  gap: 0.5rem;
  flex-shrink: 0;
}

.consent-btn {
  padding: 0.45rem 0.95rem;
  font-size: 0.86rem;
}
</style>
