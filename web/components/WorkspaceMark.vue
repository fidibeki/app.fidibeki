<script setup lang="ts">
import { identityInitials, workspaceLogoURL } from '~/utils/identity'

const props = withDefaults(defineProps<{
  tenantId: string
  name?: string | null
  logoVersion?: string | null
  size?: 64 | 256
  alt?: string
}>(), {
  name: 'Workspace',
  logoVersion: null,
  size: 64,
  alt: '',
})

const failed = ref(false)
const source = computed(() => workspaceLogoURL(props.tenantId, props.logoVersion, props.size))
const initials = computed(() => identityInitials(props.name))

watch(source, () => {
  failed.value = false
})
</script>

<template>
  <span class="workspace-mark" :data-size="size">
    <img v-if="source && !failed" :src="source" :alt="alt" @error="failed = true" />
    <span v-else aria-hidden="true">{{ initials }}</span>
  </span>
</template>

<style scoped>
.workspace-mark {
  display: grid;
  width: 100%;
  height: 100%;
  min-width: 0;
  place-items: center;
  overflow: hidden;
  background: var(--teal-soft);
  color: var(--teal);
  border-radius: inherit;
  font-size: inherit;
  font-weight: 800;
  line-height: 1;
}
.workspace-mark img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}
</style>
