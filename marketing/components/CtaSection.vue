<script setup lang="ts">
const { copy } = useMarketingI18n()

withDefaults(
  defineProps<{
    title?: string
    body?: string
  }>(),
  {
    title: 'See what your users have to say.',
    body: 'Start a 14-day free trial, no credit card required, or book a demo and we will walk you through the workflow.',
  },
)

const links = useAppLinks()
</script>

<template>
  <section class="cta-final">
    <div class="container cta-inner">
      <div class="cta-copy">
        <h2>{{ copy(title) }}</h2>
        <p class="cta-body">{{ copy(body) }}</p>
      </div>
      <div class="cta-actions">
        <a :href="links.signUp" class="btn btn-primary btn-large">{{
          copy('Start free trial')
        }}</a>
        <a
          :href="links.demo"
          class="btn btn-ghost btn-large"
          rel="noopener"
          target="_blank"
          >{{ copy('Book a demo') }}</a
        >
      </div>
    </div>
  </section>
</template>

<style scoped>
.cta-final {
  padding-bottom: 72px;
}
.cta-inner {
  padding-top: 48px;
  display: flex;
  flex-direction: column;
  gap: 28px;
}
.cta-copy h2 {
  margin-bottom: 20px;
  max-width: 24ch;
}
.cta-body {
  margin: 0;
  max-width: 52ch;
  color: var(--muted);
  font-size: 16px;
  text-wrap: pretty;
}
.cta-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
@media (min-width: 900px) {
  .cta-inner {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    gap: 64px;
  }
  .cta-actions {
    flex-shrink: 0;
  }
}
@media (max-width: 600px) {
  .cta-final {
    padding-bottom: 48px;
  }
  .cta-inner {
    padding-top: 32px;
  }
}
</style>
