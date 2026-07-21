<script setup lang="ts">
import { isConceptSlug } from '~/utils/design'

definePageMeta({ layout: false })

const route = useRoute()
const concept = route.params.concept

if (!isConceptSlug(concept)) {
  await navigateTo('/designs?invalid=1', { replace: true })
} else {
  await navigateTo(`/designs/${concept}/overview`, { replace: true })
}
</script>
