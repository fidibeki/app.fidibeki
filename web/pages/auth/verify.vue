<script setup lang="ts">
import { AlertTriangle, CheckCircle2 } from '@lucide/vue'
import type { AuthOrTwoFactorResponse, AuthResponse, Project } from '~/types/api'
import { isAuthResponse, twoFactorRequirement } from '~/types/api'

definePageMeta({ layout: false })

const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const route = useRoute()
const router = useRouter()
const { apply, pendingTwoFactor } = useAuth()
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
    errorMessage.value = t('auth.verify.unsafe')
    return
  }
  if (!isCurrentAttempt(attempt, controller)) return
  if (!token) {
    state.value = 'failed'
    errorMessage.value = t('auth.verify.missing')
    return
  }

  let verified: AuthOrTwoFactorResponse
  try {
    verified = await $apiFetch<AuthOrTwoFactorResponse>('/auth/verify', {
      method: 'POST',
      body: { token },
      auth: false,
      retryAuth: false,
      signal: controller.signal,
    })
  } catch (error) {
    if (!isCurrentAttempt(attempt, controller)) return
    state.value = 'failed'
    errorMessage.value = localizedError.message(error, 'auth.verify.invalid')
    return
  }

  if (!isCurrentAttempt(attempt, controller)) return
  const returnTo = typeof route.query.return_to === 'string' && route.query.return_to.startsWith('/') &&
    !route.query.return_to.startsWith('//') ? route.query.return_to : ''

  if (!isAuthResponse(verified)) {
    // The workspace still owes a two-factor challenge, so this sign-in carries
    // no token: send the member on to answer it.
    pendingTwoFactor.value = twoFactorRequirement(verified)
    state.value = 'success'
    await navigateTo({ path: '/auth/two-factor', query: returnTo ? { return_to: returnTo } : {} })
    return
  }

  const response: AuthResponse = verified
  apply(response)
  state.value = 'success'

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
  const fragment = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  if (!fragment.get('token')) return
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

useHead({ title: computed(() => t('auth.verify.pageTitle')) })
</script>

<template>
  <main class="auth-page">
    <section class="auth-context">
      <NuxtLink to="/" class="app-brand"><img src="/brand/fidibeki-white.png" alt="Fidibeki" /></NuxtLink>
      <div><h1>{{ t('auth.verify.contextTitle') }}</h1><p>{{ t('auth.verify.contextDescription') }}</p></div>
    </section>
    <section class="auth-panel">
      <div class="auth-form">
        <template v-if="state === 'verifying'">
          <span class="app-spinner" />
          <h2>{{ t('auth.verify.verifying') }}</h2>
          <p>{{ t('auth.verify.wait') }}</p>
        </template>
        <template v-else-if="state === 'success'">
          <CheckCircle2 :size="24" color="var(--green)" />
          <h2>{{ t('auth.verify.signedIn') }}</h2>
          <p>{{ t('auth.verify.opening') }}</p>
        </template>
        <template v-else>
          <AlertTriangle :size="24" color="var(--red)" />
          <h2>{{ t('auth.verify.failed') }}</h2>
          <p>{{ errorMessage }}</p>
          <NuxtLink class="app-button" to="/login">{{ t('auth.verify.requestAnother') }}</NuxtLink>
        </template>
      </div>
    </section>
  </main>
</template>
