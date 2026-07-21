<script setup lang="ts">
const route = useRoute()
await navigateTo({ path: '/billing', query: route.query }, { redirectCode: 302 })
</script>

<template><div /></template>
