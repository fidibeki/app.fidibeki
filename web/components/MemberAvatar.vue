<script setup lang="ts">
import { identityInitials, memberAvatarURL } from '~/utils/identity'

const props = withDefaults(defineProps<{
  memberId: string
  name?: string | null
  email?: string | null
  avatarVersion?: string | null
  size?: 64 | 256
}>(), {
  name: null,
  email: null,
  avatarVersion: null,
  size: 64,
})

const { $apiFetch } = useNuxtApp()
const imageURL = ref('')
const failed = ref(false)
let requestGeneration = 0

const initials = computed(() => identityInitials(props.name, props.email))

function releaseImage() {
  if (imageURL.value) URL.revokeObjectURL(imageURL.value)
  imageURL.value = ''
}

watch(
  () => [props.memberId, props.avatarVersion, props.size] as const,
  async () => {
    if (!import.meta.client) return
    const generation = ++requestGeneration
    releaseImage()
    failed.value = false
    const source = memberAvatarURL(props.memberId, props.avatarVersion, props.size)
    if (!source) return
    try {
      const blob = await $apiFetch<Blob>(source, { responseType: 'blob' })
      if (generation !== requestGeneration) return
      imageURL.value = URL.createObjectURL(blob)
    } catch {
      if (generation === requestGeneration) failed.value = true
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  requestGeneration += 1
  releaseImage()
})
</script>

<template>
  <span class="member-avatar">
    <img
      v-if="imageURL && !failed"
      :src="imageURL"
      alt=""
      @error="failed = true"
    />
    <span v-else aria-hidden="true">{{ initials }}</span>
  </span>
</template>

<style scoped>
.member-avatar {
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
.member-avatar img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
</style>
