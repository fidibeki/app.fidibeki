<script setup lang="ts">
const { direction, effectiveLocale } = useRegionalContext()

useHead(() => ({
  htmlAttrs: {
    dir: direction.value,
    lang: effectiveLocale.value,
  },
}))
</script>

<template>
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
</template>
