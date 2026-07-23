<script setup lang="ts">
import { AlertTriangle, CheckCircle2 } from '@lucide/vue'
import type { MemberDomain } from '~/types/api'

definePageMeta({ layout: false })

const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
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
    errorMessage.value = t('auth.domainVerify.unsafe')
    return
  }
  if (!token) {
    state.value = 'failed'
    errorMessage.value = t('auth.domainVerify.missing')
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
    errorMessage.value = localizedError.message(error, 'auth.domainVerify.invalid')
  }
}

onMounted(verifyDomain)

useHead({ title: computed(() => t('auth.domainVerify.pageTitle')) })
</script>

<template>
  <main class="auth-page">
    <section class="auth-context">
      <NuxtLink to="/" class="app-brand"><img src="/brand/fidibeki-white.png" alt="Fidibeki" /></NuxtLink>
      <div><h1>{{ t('auth.domainVerify.contextTitle') }}</h1><p>{{ t('auth.domainVerify.contextDescription') }}</p></div>
    </section>
    <section class="auth-panel">
      <div class="auth-form domain-verification-state">
        <template v-if="state === 'verifying'">
          <span class="app-spinner" />
          <h2>{{ t('auth.domainVerify.verifying') }}</h2>
          <p>{{ t('auth.domainVerify.wait') }}</p>
        </template>
        <template v-else-if="state === 'success'">
          <CheckCircle2 :size="24" color="var(--green)" />
          <h2>{{ t('auth.domainVerify.verified', { domain: verifiedDomain }) }}</h2>
          <p>{{ t('auth.domainVerify.verifiedDescription') }}</p>
          <NuxtLink class="app-button app-button--primary" to="/login">{{ t('auth.domainVerify.continue') }}</NuxtLink>
        </template>
        <template v-else>
          <AlertTriangle :size="24" color="var(--red)" />
          <h2>{{ t('auth.domainVerify.failed') }}</h2>
          <p>{{ errorMessage }}</p>
          <NuxtLink class="app-button" to="/login">{{ t('auth.domainVerify.return') }}</NuxtLink>
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
