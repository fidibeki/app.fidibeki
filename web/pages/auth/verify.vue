<script setup lang="ts">
import { AlertTriangle, CheckCircle2 } from '@lucide/vue'
import type { AuthResponse, Project } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'

definePageMeta({ layout: false })

const { $apiFetch } = useNuxtApp()
const route = useRoute()
const router = useRouter()
const { apply } = useAuth()
const state = ref<'verifying' | 'success' | 'failed'>('verifying')
const errorMessage = ref('')
let verificationAttempt = 0
let requestController: AbortController | null = null

function isCurrentAttempt(attempt: number, controller: AbortController) {
  return attempt === verificationAttempt &&
    requestController === controller &&
    !controller.signal.aborted
}

async function verifyCurrentLink() {
  const attempt = ++verificationAttempt
  requestController?.abort()
  const controller = new AbortController()
  requestController = controller

  state.value = 'verifying'
  errorMessage.value = ''
  const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  const token = fragment.get('token') || ''
  try {
    await router.replace({
      path: window.location.pathname,
      query: route.query,
      hash: '',
      force: true,
    })
  } catch {
    if (!isCurrentAttempt(attempt, controller)) return
    state.value = 'failed'
    errorMessage.value = 'This sign-in link could not be opened safely. Please request another link.'
    return
  }
  if (!isCurrentAttempt(attempt, controller)) return
  if (!token) {
    state.value = 'failed'
    errorMessage.value = 'This sign-in link is missing its verification token.'
    return
  }

  let response: AuthResponse
  try {
    response = await $apiFetch<AuthResponse>('/auth/verify', {
      method: 'POST',
      body: { token },
      auth: false,
      retryAuth: false,
      signal: controller.signal,
    })
  } catch (error) {
    if (!isCurrentAttempt(attempt, controller)) return
    state.value = 'failed'
    errorMessage.value = apiErrorMessage(error, 'This sign-in link is invalid or has expired.')
    return
  }

  if (!isCurrentAttempt(attempt, controller)) return
  apply(response)
  state.value = 'success'

  const returnTo = typeof route.query.return_to === 'string' && route.query.return_to.startsWith('/') &&
    !route.query.return_to.startsWith('//') ? route.query.return_to : ''
  if (returnTo) {
    if (!isCurrentAttempt(attempt, controller)) return
    await navigateTo(returnTo)
    return
  }

  const permissions = response.member.permissions
  if (canActivateWorkspace(permissions)) {
    let projects: Project[] | null = null
    try {
      const result = await $apiFetch<{ projects: Project[] }>('/v1/projects', {
        retryAuth: false,
        signal: controller.signal,
      })
      if (!isCurrentAttempt(attempt, controller)) return
      projects = result.projects
    } catch {
      if (!isCurrentAttempt(attempt, controller)) return
      // Authentication succeeded; the normal workspace route can recover from a transient list failure.
    }
    if (projects?.length === 0) {
      if (!isCurrentAttempt(attempt, controller)) return
      await navigateTo('/getting-started')
      return
    }
  }

  if (!isCurrentAttempt(attempt, controller)) return
  await navigateTo(firstAccessibleWorkspacePath(permissions))
}

function handleHashChange() {
  void verifyCurrentLink()
}

onMounted(() => {
  window.addEventListener('hashchange', handleHashChange)
  void verifyCurrentLink()
})

onBeforeUnmount(() => {
  verificationAttempt += 1
  requestController?.abort()
  requestController = null
  window.removeEventListener('hashchange', handleHashChange)
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
