<script setup lang="ts">
definePageMeta({ layout:false })
const email=ref('');const sent=ref(false);const error=ref('');const pending=ref(false)
async function submit(){pending.value=true;error.value='';try{await $fetch('/admin/backoffice/auth/login',{method:'POST',body:{email:email.value}});sent.value=true}catch{error.value='We could not request a sign-in link.'}finally{pending.value=false}}
</script>
<template><main class="login"><form class="login-card" @submit.prevent="submit"><p class="eyebrow">Fidibeki · platform administration</p><h1>Keep the signal clear.</h1><p v-if="!sent" class="muted">Request a one-time link to enter the control room.</p><p v-else>Check your inbox. The secure link expires in ten minutes.</p><label v-if="!sent">Work email<input v-model="email" type="email" required autocomplete="email" placeholder="you@fidibeki.com"></label><p v-if="error" class="error" role="alert">{{ error }}</p><button v-if="!sent" class="button" :disabled="pending">{{ pending?'Requesting…':'Email secure link' }}</button></form></main></template>
