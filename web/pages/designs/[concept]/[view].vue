<script setup lang="ts">
import ClearspaceStudy from '~/components/studies/ClearspaceStudy.vue'
import DispatchStudy from '~/components/studies/DispatchStudy.vue'
import FlowboardStudy from '~/components/studies/FlowboardStudy.vue'
import ProjectDeckStudy from '~/components/studies/ProjectDeckStudy.vue'
import SignalObservatoryStudy from '~/components/studies/SignalObservatoryStudy.vue'
import type { ConceptSlug, ViewSlug } from '~/types/design'
import { isConceptSlug, isViewSlug } from '~/utils/design'

definePageMeta({ layout: false })

const route = useRoute()
const conceptParam = Array.isArray(route.params.concept) ? route.params.concept[0] : route.params.concept
const viewParam = Array.isArray(route.params.view) ? route.params.view[0] : route.params.view

if (!isConceptSlug(conceptParam) || !isViewSlug(viewParam)) {
  await navigateTo('/designs?invalid=1', { replace: true })
}

const concept = conceptParam as ConceptSlug
const view = viewParam as ViewSlug

const studyComponents = {
  clearspace: ClearspaceStudy,
  dispatch: DispatchStudy,
  'signal-observatory': SignalObservatoryStudy,
  'project-deck': ProjectDeckStudy,
  flowboard: FlowboardStudy,
}

const study = computed(() => studyComponents[concept])

useHead({
  title: `${String(concept)} · Fidibeki design lab`,
})
</script>

<template>
  <div>
    <DesignSwitcher :concept="concept" :view="view" />
    <component :is="study" :view="view" />
  </div>
</template>
