<script setup lang="ts">
import { AlertTriangle, CheckCircle2 } from '@lucide/vue'
import type { MemberDomain } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'

definePageMeta({ layout: false })

const { $apiFetch } = useNuxtApp()
const route = useRoute()
const router = useRouter()
const state = ref<'verifying' | 'success' | 'failed'>('verifying')
const errorMessage = ref('')
const verifiedDomain = ref('')

async function verifyDomain() {
  const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  const token = fragment.get('token') || ''
  try {
    await router.replace({ path: window.location.pathname, query: route.query, hash: '', force: true })
  } catch {
    state.value = 'failed'
    errorMessage.value = 'This verification link could not be opened safely. Request another link.'
    return
  }
  if (!token) {
    state.value = 'failed'
    errorMessage.value = 'This verification link is missing its token.'
    return
  }

  try {
    const response = await $apiFetch<{ member_domain: MemberDomain }>('/auth/member-domains/verify', {
      method: 'POST',
      body: { token },
      auth: false,
      retryAuth: false,
    })
    verifiedDomain.value = response.member_domain.domain
    state.value = 'success'
  } catch (error) {
    state.value = 'failed'
    errorMessage.value = apiErrorMessage(error, 'This verification link is invalid or has expired.')
  }
}

onMounted(verifyDomain)

useHead({ title: 'Verify member domain · Fidibeki' })
</script>

<template>
  <main class="auth-page">
    <section class="auth-context">
      <NuxtLink to="/" class="app-brand"><img src="/brand/fidibeki-white.png" alt="Fidibeki" /></NuxtLink>
      <div><h1>Protect workspace membership.</h1><p>Verified domains keep invitations and new member activation inside the organizations you approve.</p></div>
    </section>
    <section class="auth-panel">
      <div class="auth-form domain-verification-state">
        <template v-if="state === 'verifying'">
          <span class="app-spinner" />
          <h2>Verifying member domain</h2>
          <p>This should only take a moment.</p>
        </template>
        <template v-else-if="state === 'success'">
          <CheckCircle2 :size="24" color="var(--green)" />
          <h2>{{ verifiedDomain }} is verified</h2>
          <p>Workspace members with this email domain can now be invited and activated.</p>
          <NuxtLink class="app-button app-button--primary" to="/login">Continue to sign in</NuxtLink>
        </template>
        <template v-else>
          <AlertTriangle :size="24" color="var(--red)" />
          <h2>Domain could not be verified</h2>
          <p>{{ errorMessage }}</p>
          <NuxtLink class="app-button" to="/login">Return to sign in</NuxtLink>
        </template>
      </div>
    </section>
  </main>
</template>

<style scoped>
.domain-verification-state { display: grid; justify-items: start; gap: 10px; }
.domain-verification-state h2, .domain-verification-state p { margin: 0; }
.domain-verification-state .app-button { width: auto; margin-top: 8px; }
</style>
