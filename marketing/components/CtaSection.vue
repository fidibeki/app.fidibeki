<script setup lang="ts">
withDefaults(
  defineProps<{
    title?: string
    body?: string
  }>(),
  {
    title: 'Close the loop on your product feedback',
    body: 'Start a 14-day free trial — no credit card required — or book a demo and we will walk you through the whole loop.',
  },
)

const links = useAppLinks()
</script>

<template>
  <section v-reveal.line class="cta-final">
    <div class="container cta-inner">
      <div class="cta-copy">
        <h2>{{ title }}</h2>
        <p class="cta-body">{{ body }}</p>
      </div>
      <div class="cta-actions">
        <a :href="links.signUp" class="btn btn-primary btn-large">Start free trial</a>
        <a :href="links.demo" class="btn btn-ghost btn-large" rel="noopener" target="_blank">Book a demo</a>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* Minimal close, consistent with the rest of the page: a hairline and type. */
.cta-final {
  margin-top: clamp(2.5rem, 2rem + 2vw, 4rem);
  padding-block: clamp(3rem, 2.5rem + 2vw, 4.5rem);
  border-top: 1px solid var(--line);
}

.cta-inner {
  display: flex;
  flex-direction: column;
  gap: 1.75rem;
}

.cta-copy h2 {
  margin-bottom: 0.5rem;
  max-width: 24ch;
}

.cta-body {
  margin: 0;
  max-width: 52ch;
  color: var(--muted);
  font-size: 1.05rem;
  text-wrap: pretty;
}

.cta-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.85rem;
}

@media (min-width: 900px) {
  .cta-inner {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    gap: 2.5rem;
  }

  .cta-actions {
    flex-shrink: 0;
  }
}
</style>
