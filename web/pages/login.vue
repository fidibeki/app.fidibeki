<script setup lang="ts">
import { ArrowRight, Building2, CheckCircle2, RefreshCw } from '@lucide/vue'
import type { TenantConfiguration } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'
import { baseDomainURL, workspaceSlugFromStandardHost } from '~/utils/workspace-url'

definePageMeta({ layout: false })

const route = useRoute()
const { authenticated } = useAuth()
const { $apiFetch } = useNuxtApp()
const email = ref('')
const pending = ref(false)
const sent = ref(false)
const errorMessage = ref('')
const requestURL = useRequestURL()
const requestEvent = import.meta.server ? useRequestEvent() : undefined
const config = useRuntimeConfig()
const baseDomain = String(config.public.baseDomain || requestURL.hostname)
const signupURL = baseDomainURL(baseDomain, '/signup', requestURL)
const suggestedSlug = workspaceSlugFromStandardHost(requestURL.hostname, baseDomain)
const suggestedSignupURL = suggestedSlug
  ? baseDomainURL(baseDomain, `/signup?slug=${encodeURIComponent(suggestedSlug)}`, requestURL)
  : ''
const workspaceState = useState<'checking' | 'available' | 'unavailable' | 'error'>(
  `workspace:availability:${requestURL.host}`,
  () => 'checking',
)
const hostLabel = computed(() => {
  const first = requestURL.hostname.split('.')[0]
  return first && !['localhost', '127', 'www'].includes(first) ? first : 'your workspace'
})

function apiStatus(error: unknown) {
  if (!error || typeof error !== 'object') return 0
  const value = error as { status?: number, statusCode?: number, response?: { status?: number } }
  return value.response?.status || value.statusCode || value.status || 0
}

function setPageStatus(status: number, message: string) {
  if (requestEvent) setResponseStatus(requestEvent, status, message)
}

async function checkWorkspace() {
  workspaceState.value = 'checking'
  try {
    await $apiFetch<TenantConfiguration>('/v1/config', { auth: false, retryAuth: false })
    workspaceState.value = 'available'
  } catch (error) {
    const status = apiStatus(error)
    if (status === 403 || status === 404) {
      workspaceState.value = 'unavailable'
      setPageStatus(404, 'Not Found')
      return
    }
    workspaceState.value = 'error'
    setPageStatus(503, 'Service Unavailable')
  }
}

async function submit() {
  if (pending.value) return
  pending.value = true
  sent.value = false
  errorMessage.value = ''
  try {
    await $apiFetch('/auth/login', {
      method: 'POST',
      body: { email: email.value },
      auth: false,
      retryAuth: false,
    })
    sent.value = true
  } catch (error) {
    errorMessage.value = apiErrorMessage(error, 'The sign-in request could not be sent.')
  } finally {
    pending.value = false
  }
}

if (import.meta.server && !import.meta.dev) await checkWorkspace()

onMounted(async () => {
  if (workspaceState.value === 'checking') await checkWorkspace()
  if (workspaceState.value === 'available' && authenticated.value) {
    const destination = typeof route.query.return_to === 'string' ? route.query.return_to : '/'
    await navigateTo(destination)
  }
})

useHead({ title: 'Sign in · Fidibeki' })
</script>

<template>
  <main class="auth-page">
    <section class="auth-context">
      <NuxtLink to="/" class="app-brand auth-brand"><img src="/brand/fidibeki-white.png" alt="Fidibeki" /></NuxtLink>
      <div>
        <h1>Return to the feedback queue.</h1>
        <p>Review captured evidence, connect product signals to issues, and keep the team moving from one operational workspace.</p>
      </div>
    </section>
    <section class="auth-panel">
      <div v-if="workspaceState === 'checking'" class="auth-form auth-workspace-state" role="status">
        <span class="app-spinner" />
        <p>Checking workspace</p>
      </div>
      <div v-else-if="workspaceState === 'unavailable'" class="auth-form auth-workspace-state">
        <Building2 :size="24" aria-hidden="true" />
        <p class="app-eyebrow">Workspace access</p>
        <h2>Workspace unavailable</h2>
        <p>This workspace cannot be accessed from this address.</p>
        <a v-if="suggestedSlug" class="app-button app-button--primary" :href="suggestedSignupURL">Create {{ suggestedSlug }} workspace <ArrowRight :size="16" /></a>
        <a v-else class="app-button" :href="signupURL">Go to Fidibeki</a>
      </div>
      <div v-else-if="workspaceState === 'error'" class="auth-form auth-workspace-state" role="alert">
        <Building2 :size="24" aria-hidden="true" />
        <p class="app-eyebrow">Workspace access</p>
        <h2>Workspace check unavailable</h2>
        <p>Fidibeki could not verify this workspace right now.</p>
        <button class="app-button" type="button" @click="checkWorkspace"><RefreshCw :size="15" /> Retry</button>
      </div>
      <form v-else class="auth-form" @submit.prevent="submit">
        <p class="app-eyebrow">{{ hostLabel }}</p>
        <h2>Sign in to Fidibeki</h2>
        <p>We will send a one-time sign-in link to your work email.</p>
        <label>
          Work email
          <input v-model.trim="email" class="app-input" type="email" autocomplete="email" required placeholder="you@company.com" />
        </label>
        <button class="app-button app-button--primary" type="submit" :disabled="pending || !email">
          {{ pending ? 'Sending link…' : 'Send sign-in link' }} <ArrowRight v-if="!pending" :size="16" />
        </button>
        <p v-if="sent" class="auth-message auth-message--success" role="status"><CheckCircle2 :size="15" aria-hidden="true" /><span>Check your inbox. The link expires in 10 minutes.</span></p>
        <p v-if="errorMessage" class="auth-message auth-message--error" role="alert">{{ errorMessage }}</p>
        <p class="auth-signup-link">New to Fidibeki? <a :href="signupURL">Create a workspace</a></p>
      </form>
    </section>
  </main>
</template>

<style scoped>
.auth-brand img { display: block; width: 142px; height: auto; }
.auth-workspace-state { display: flex; align-items: flex-start; flex-direction: column; }
.auth-workspace-state > svg { margin-bottom: 18px; color: var(--teal); }
.auth-workspace-state > .app-spinner { margin-bottom: 10px; }
.auth-workspace-state > h2 { margin-bottom: 8px; }
.auth-workspace-state > p:not(.app-eyebrow) { margin-bottom: 22px; }
.auth-signup-link { margin-top: 20px !important; text-align: center; }
.auth-signup-link a { color: var(--teal); font-weight: 750; }
</style>
