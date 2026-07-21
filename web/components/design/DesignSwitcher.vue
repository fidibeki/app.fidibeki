<script setup lang="ts">
import { ChevronDown, FlaskConical } from '@lucide/vue'
import { concepts } from '~/data/design-fixtures'
import type { ConceptSlug, ViewSlug } from '~/types/design'
import { conceptPath } from '~/utils/design'

const props = defineProps<{
  concept: ConceptSlug
  view: ViewSlug
}>()

const router = useRouter()

function changeConcept(event: Event) {
  const target = event.target as HTMLSelectElement
  router.push(conceptPath(target.value as ConceptSlug, props.view))
}

function changeView(view: ViewSlug) {
  router.push(conceptPath(props.concept, view))
}
</script>

<template>
  <header class="design-switcher">
    <NuxtLink class="lab-brand" to="/designs" aria-label="Back to design studies">
      <FlaskConical :size="17" aria-hidden="true" />
      <span>Fidibeki design lab</span>
    </NuxtLink>

    <div class="concept-select-wrap">
      <label class="sr-only" for="concept-select">Design concept</label>
      <select id="concept-select" :value="concept" @change="changeConcept">
        <option v-for="item in concepts" :key="item.slug" :value="item.slug">
          {{ item.number }} · {{ item.name }}
        </option>
      </select>
      <ChevronDown :size="15" aria-hidden="true" />
    </div>

    <nav class="view-switcher" aria-label="Representative view">
      <button :data-active="view === 'overview'" @click="changeView('overview')">Overview</button>
      <button :data-active="view === 'feedback'" @click="changeView('feedback')">Feedback</button>
      <button :data-active="view === 'feedback-detail'" @click="changeView('feedback-detail')">Detail</button>
    </nav>
  </header>
</template>

<style scoped>
.design-switcher {
  position: relative;
  z-index: 80;
  display: grid;
  min-height: 48px;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 16px;
  padding: 4px 12px;
  background: #17211f;
  color: #fff;
  border-bottom: 1px solid #0c1210;
}

.lab-brand {
  display: inline-flex;
  width: max-content;
  min-height: 38px;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 700;
}

.concept-select-wrap {
  position: relative;
  display: flex;
  align-items: center;
}

.concept-select-wrap select {
  min-width: 210px;
  height: 36px;
  padding: 0 34px 0 12px;
  appearance: none;
  background: #26312e;
  color: #fff;
  border: 1px solid #46524f;
  border-radius: 5px;
  font-size: 13px;
  font-weight: 650;
}

.concept-select-wrap svg {
  position: absolute;
  right: 10px;
  pointer-events: none;
}

.view-switcher {
  display: flex;
  justify-self: end;
  padding: 3px;
  background: #26312e;
  border-radius: 5px;
}

.view-switcher button {
  min-height: 32px;
  padding: 0 11px;
  background: transparent;
  color: #cbd4d1;
  border: 0;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 650;
}

.view-switcher button[data-active='true'] {
  background: #fff;
  color: #17211f;
}

@media (max-width: 720px) {
  .design-switcher {
    grid-template-columns: 40px 1fr;
    gap: 8px;
    padding: 5px 8px;
  }

  .lab-brand span {
    display: none;
  }

  .concept-select-wrap select {
    width: 100%;
    min-width: 0;
  }

  .view-switcher {
    grid-column: 1 / -1;
    width: 100%;
  }

  .view-switcher button {
    flex: 1;
  }
}
</style>
