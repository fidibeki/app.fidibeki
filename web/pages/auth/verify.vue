<script setup lang="ts">
import { AlertTriangle, CheckCircle2 } from '@lucide/vue'
import type { AuthResponse, Project } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'

definePageMeta({ layout: false })

const { $apiFetch } = useNuxtApp()
const route = useRoute()
const { apply } = useAuth()
const state = ref<'verifying' | 'success' | 'failed'>('verifying')
const errorMessage = ref('')

onMounted(async () => {
  const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  const token = fragment.get('token') || ''
  window.history.replaceState({}, document.title, `${window.location.pathname}${window.location.search}`)
  if (!token) {
    state.value = 'failed'
    errorMessage.value = 'This sign-in link is missing its verification token.'
    return
  }
  try {
    const response = await $apiFetch<AuthResponse>('/auth/verify', {
      method: 'POST',
      body: { token },
      auth: false,
      retryAuth: false,
    })
    apply(response)
    state.value = 'success'
    const returnTo = typeof route.query.return_to === 'string' && route.query.return_to.startsWith('/') &&
      !route.query.return_to.startsWith('//') ? route.query.return_to : ''
    if (returnTo) {
      await navigateTo(returnTo)
      return
    }
    const permissions = response.member.permissions
    if (canActivateWorkspace(permissions)) {
      try {
        const projects = await $apiFetch<{ projects: Project[] }>('/v1/projects')
        if (projects.projects.length === 0) {
          await navigateTo('/getting-started')
          return
        }
      } catch {
        // Authentication succeeded; the normal workspace route can recover from a transient list failure.
      }
    }
    await navigateTo(firstAccessibleWorkspacePath(permissions))
  } catch (error) {
    state.value = 'failed'
    errorMessage.value = apiErrorMessage(error, 'This sign-in link is invalid or has expired.')
  }
})

useHead({ title: 'Verify sign in · Fidibeki' })
</script>

<template>
  <main class="auth-page">
    <section class="auth-context">
      <NuxtLink to="/" class="app-brand"><img src="/brand/fidibeki-white.png" alt="Fidibeki" /></NuxtLink>
      <div><h1>Secure workspace access.</h1><p>The verification token is removed from the address before it is exchanged for your session.</p></div>
    </section>
    <section class="auth-panel">
      <div class="auth-form">
        <template v-if="state === 'verifying'">
          <span class="app-spinner" />
          <h2>Verifying your sign-in link</h2>
          <p>This should only take a moment.</p>
        </template>
        <template v-else-if="state === 'success'">
          <CheckCircle2 :size="24" color="var(--green)" />
          <h2>Signed in</h2>
          <p>Opening your workspace…</p>
        </template>
        <template v-else>
          <AlertTriangle :size="24" color="var(--red)" />
          <h2>Sign-in link could not be verified</h2>
          <p>{{ errorMessage }}</p>
          <NuxtLink class="app-button" to="/login">Request another link</NuxtLink>
        </template>
      </div>
    </section>
  </main>
</template>
