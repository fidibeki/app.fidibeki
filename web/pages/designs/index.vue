<script setup lang="ts">
import { ArrowRight, Boxes, CheckCircle2 } from '@lucide/vue'
import { concepts } from '~/data/design-fixtures'

const route = useRoute()

definePageMeta({ layout: false })
</script>

<template>
  <main class="design-index">
    <header class="index-header">
      <div class="fidibeki-mark"><span>F</span> Fidibeki</div>
      <div class="index-heading">
        <p class="section-label">Backoffice design review</p>
        <h1>Five ways to operate on product feedback.</h1>
        <p>
          Every study uses the same Acme Studio data and the same three workflows. Compare information
          architecture, density, and interaction model before choosing the Phase 11 foundation.
        </p>
      </div>
      <div class="review-facts" aria-label="Comparison criteria">
        <span><CheckCircle2 :size="16" /> Same fixture data</span>
        <span><CheckCircle2 :size="16" /> Desktop and mobile</span>
        <span><CheckCircle2 :size="16" /> Working interactions</span>
      </div>
    </header>

    <p v-if="route.query.invalid" class="invalid-notice" role="status">
      That design view does not exist. Choose one of the five studies below.
    </p>

    <section class="concept-list" aria-label="Design studies">
      <NuxtLink
        v-for="concept in concepts"
        :key="concept.slug"
        class="concept-row"
        :to="`/designs/${concept.slug}/overview`"
      >
        <span class="concept-number">{{ concept.number }}</span>
        <div>
          <h2>{{ concept.name }}</h2>
          <p>{{ concept.thesis }}</p>
        </div>
        <span class="best-for"><Boxes :size="15" /> {{ concept.bestFor }}</span>
        <span class="open-study" aria-label="Open study"><ArrowRight :size="18" /></span>
      </NuxtLink>
    </section>
  </main>
</template>

<style scoped>
.design-index {
  width: min(1120px, calc(100% - 40px));
  margin: 0 auto;
  padding: 52px 0 80px;
}

.index-header {
  display: grid;
  grid-template-columns: 180px minmax(0, 1fr) 220px;
  gap: 40px;
  padding-bottom: 40px;
  border-bottom: 1px solid var(--line-strong);
}

.fidibeki-mark {
  display: flex;
  height: 34px;
  align-items: center;
  gap: 10px;
  font-weight: 750;
}

.fidibeki-mark span {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  background: var(--ink);
  color: #fff;
  border-radius: 6px;
}

.index-heading h1 {
  max-width: 700px;
  margin-top: 10px;
  font-size: 38px;
  font-weight: 680;
  letter-spacing: 0;
  line-height: 1.12;
}

.index-heading > p:last-child {
  max-width: 680px;
  margin-top: 15px;
  color: var(--muted);
  font-size: 15px;
  line-height: 1.65;
}

.review-facts {
  display: flex;
  flex-direction: column;
  gap: 11px;
  padding-top: 26px;
}

.review-facts span {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--ink-soft);
  font-size: 12px;
  font-weight: 650;
}

.review-facts svg {
  color: var(--teal);
}

.invalid-notice {
  margin-top: 22px;
  padding: 12px 14px;
  background: var(--amber-soft);
  border: 1px solid #efd09a;
  border-radius: var(--radius);
}

.concept-list {
  margin-top: 18px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 8px;
}

.concept-row {
  display: grid;
  min-height: 104px;
  grid-template-columns: 56px minmax(0, 1fr) 240px 40px;
  align-items: center;
  gap: 20px;
  padding: 20px 22px;
  border-bottom: 1px solid var(--line);
}

.concept-row:last-child {
  border-bottom: 0;
}

.concept-row:hover {
  background: #f0f6f4;
}

.concept-number {
  color: var(--teal);
  font-size: 13px;
  font-weight: 750;
}

.concept-row h2 {
  font-size: 19px;
  font-weight: 700;
}

.concept-row p {
  margin-top: 4px;
  color: var(--muted);
}

.best-for {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--ink-soft);
  font-size: 12px;
  font-weight: 650;
}

.open-study {
  display: grid;
  width: 36px;
  height: 36px;
  place-items: center;
  border: 1px solid var(--line);
  border-radius: 6px;
}

@media (max-width: 800px) {
  .design-index {
    width: min(100% - 28px, 600px);
    padding-top: 24px;
  }

  .index-header {
    grid-template-columns: 1fr;
    gap: 24px;
  }

  .index-heading h1 {
    font-size: 30px;
  }

  .review-facts {
    padding-top: 0;
  }

  .concept-row {
    grid-template-columns: 38px minmax(0, 1fr) 36px;
    gap: 12px;
    padding: 16px;
  }

  .best-for {
    display: none;
  }
}
</style>
