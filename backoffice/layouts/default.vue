<script setup lang="ts">
const { clear } = usePlatformAuth()
async function logout(){ try { await $fetch('/admin/auth/logout',{method:'POST',credentials:'include'}) } finally { clear(); await navigateTo('/login') } }
</script>
<template><div class="shell"><aside class="rail"><NuxtLink class="brand" to="/"><i>F</i>Control room</NuxtLink><nav><NuxtLink to="/">Overview</NuxtLink><NuxtLink to="/workspaces">Workspaces</NuxtLink><NuxtLink to="/workspaces/new">New workspace</NuxtLink><a href="/admin/queues/">Queues</a><a href="/admin/operations/privacy-incidents">Privacy</a><button class="button" type="button" @click="logout">Sign out</button></nav></aside><main class="main"><slot /></main></div></template>
