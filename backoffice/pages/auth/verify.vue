<script setup lang="ts">
definePageMeta({ layout:false })
const route=useRoute();const {apply}=usePlatformAuth();const state=ref('Verifying your secure link…')
onMounted(async()=>{const token=typeof route.query.token==='string'?route.query.token:'';if(!token){state.value='This sign-in link is incomplete.';return}try{const response=await $fetch('/admin/auth/verify',{method:'POST',body:{token},credentials:'include'});apply(response as { access_token: string; admin: { id: string; email: string; permissions: string[] } });await navigateTo('/')}catch{state.value='This sign-in link is invalid, expired, or already used.'}})
</script>
<template><main class="login"><section class="login-card"><p class="eyebrow">Platform admin</p><h1>{{ state }}</h1><NuxtLink v-if="state.includes('invalid')||state.includes('incomplete')" class="button" to="/login">Request another link</NuxtLink></section></main></template>
