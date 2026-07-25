<script setup lang="ts">
import { ArrowRight, Building2, CheckCircle2, RefreshCw, ShieldCheck } from '@lucide/vue'
import type { SSOPolicy, TenantConfiguration } from '~/types/api'
import { loginPathsFor, ssoErrorMessageKey } from '~/utils/sso-policy'
import {
  applicationURL,
  isApplicationHost,
  normalizeWorkspaceSlug,
  workspaceLoginURL,
  workspaceSlugError,
  workspaceSlugFromStandardHost,
} from '~/utils/workspace-url'

definePageMeta({ layout: false })

const route = useRoute()
const { t } = useI18n()
const { authenticated } = useAuth()
const { $refreshSession } = useNuxtApp()
const localizedError = useLocalizedAPIError()
const { $apiFetch } = useNuxtApp()
const email = ref('')
const pending = ref(false)
const sent = ref(false)
const errorMessage = ref('')
const requestURL = useRequestURL()
const requestEvent = import.meta.server ? useRequestEvent() : undefined
const config = useRuntimeConfig()
const baseDomain = String(config.public.baseDomain || requestURL.hostname)
const appURL = String(config.public.appUrl || '')
const signupURL = applicationURL(appURL, baseDomain, '/signup', requestURL)
const locatorMode = isApplicationHost(requestURL.hostname, baseDomain, appURL)
const locatorSlug = ref('')
const locatorError = ref('')
// Organization-level SSO policy. It is fetched before any credential is
// entered and carries no membership information, so the login page can present
// the right path without revealing who belongs to this workspace.
const ssoPolicy = ref<SSOPolicy | null>(null)
const showBreakGlass = ref(false)
const ssoErrorKey = computed(() => ssoErrorMessageKey(route.query.sso_error))
const loginPaths = computed(() => loginPathsFor(ssoPolicy.value, showBreakGlass.value))
const suggestedSlug = workspaceSlugFromStandardHost(requestURL.hostname, baseDomain)
const suggestedSignupURL = suggestedSlug
  ? applicationURL(appURL, baseDomain, `/signup?slug=${encodeURIComponent(suggestedSlug)}`, requestURL)
  : ''
const workspaceState = useState<'checking' | 'available' | 'unavailable' | 'error'>(
  `workspace:availability:${requestURL.host}`,
  () => 'checking',
)
const hostLabel = computed(() => {
  const first = requestURL.hostname.split('.')[0]
  return first && !['localhost', '127', 'www'].includes(first)
    ? first
    : t('auth.login.workspaceFallback')
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

async function loadSSOPolicy() {
  try {
    ssoPolicy.value = await $apiFetch<SSOPolicy>('/auth/sso/saml/status', {
      auth: false,
      retryAuth: false,
    })
  } catch {
    // A workspace without SSO, or a transient failure, leaves the existing
    // email path exactly as it was.
    ssoPolicy.value = null
  }
}

async function continueWithSSO() {
  const startURL = ssoPolicy.value?.start_url
  if (!startURL) return
  await navigateTo(startURL, { external: true })
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
    errorMessage.value = localizedError.message(error, 'auth.login.requestFailed')
  } finally {
    pending.value = false
  }
}

async function openWorkspace() {
  const normalized = normalizeWorkspaceSlug(locatorSlug.value)
  locatorSlug.value = normalized
  locatorError.value = workspaceSlugError(normalized) ? t('auth.signup.slugInvalid') : ''
  if (locatorError.value) return
  await navigateTo(workspaceLoginURL(normalized, baseDomain, requestURL), { external: true })
}

if (import.meta.server && !import.meta.dev && !locatorMode) await checkWorkspace()

// A member returning from an identity provider arrives through a cross-site
// POST redirect, and browsers withhold SameSite=Lax cookies from that
// navigation. The server-rendered pass therefore cannot see a live session and
// routes here. The same cookie is available to a same-site request from this
// page, so one client-side refresh recovers the session instead of asking an
// already-signed-in member to authenticate again.
async function recoverSessionForReturn() {
  if (authenticated.value) return true
  const returnTo = typeof route.query.return_to === 'string' ? route.query.return_to : ''
  if (!returnTo.startsWith('/') || returnTo.startsWith('//')) return false
  try {
    await $refreshSession()
  } catch {
    return false
  }
  return authenticated.value
}

onMounted(async () => {
  if (locatorMode) return
  if (workspaceState.value === 'checking') await checkWorkspace()
  if (workspaceState.value !== 'available') return
  const recovered = await recoverSessionForReturn()
  if (recovered || authenticated.value) {
    const destination = typeof route.query.return_to === 'string' ? route.query.return_to : '/'
    await navigateTo(destination)
    return
  }
  await loadSSOPolicy()
})

useHead({ title: computed(() => t('auth.login.pageTitle')) })
</script>

<template>
  <main class="auth-page">
    <section class="auth-context">
      <NuxtLink to="/" class="app-brand auth-brand"><img src="/brand/fidibeki-white.png" alt="Fidibeki" /></NuxtLink>
      <div>
        <h1>{{ t('auth.login.contextTitle') }}</h1>
        <p>{{ t('auth.login.contextDescription') }}</p>
      </div>
    </section>
    <section class="auth-panel">
      <form v-if="locatorMode" class="auth-form" @submit.prevent="openWorkspace">
        <p class="app-eyebrow">{{ t('auth.login.workspaceAccess') }}</p>
        <h2>{{ t('auth.login.locatorTitle') }}</h2>
        <p>{{ t('auth.login.locatorDescription') }}</p>
        <label>
          {{ t('auth.login.locatorLabel') }}
          <span class="locator-control" :data-invalid="Boolean(locatorError)">
            <input v-model="locatorSlug" type="text" autocomplete="organization" required :placeholder="t('auth.signup.workspacePlaceholder')" />
            <b>.{{ baseDomain }}</b>
          </span>
        </label>
        <p v-if="locatorError" class="auth-message auth-message--error" role="alert">{{ locatorError }}</p>
        <button class="app-button app-button--primary" type="submit" :disabled="!locatorSlug">
          {{ t('auth.login.locatorSubmit') }} <ArrowRight :size="16" />
        </button>
        <p class="auth-signup-link">{{ t('auth.login.newAccount') }} <a :href="signupURL">{{ t('auth.login.signup') }}</a></p>
      </form>
      <div v-else-if="workspaceState === 'checking'" class="auth-form auth-workspace-state" role="status">
        <span class="app-spinner" />
        <p>{{ t('auth.login.checkingWorkspace') }}</p>
      </div>
      <div v-else-if="workspaceState === 'unavailable'" class="auth-form auth-workspace-state">
        <Building2 :size="24" aria-hidden="true" />
        <p class="app-eyebrow">{{ t('auth.login.workspaceAccess') }}</p>
        <h2>{{ t('auth.login.workspaceUnavailable') }}</h2>
        <p>{{ t('auth.login.workspaceUnavailableDescription') }}</p>
        <a v-if="suggestedSlug" class="app-button app-button--primary" :href="suggestedSignupURL">{{ t('auth.login.createWorkspace', { workspace: suggestedSlug }) }} <ArrowRight :size="16" /></a>
        <a v-else class="app-button" :href="signupURL">{{ t('auth.login.goToFidibeki') }}</a>
      </div>
      <div v-else-if="workspaceState === 'error'" class="auth-form auth-workspace-state" role="alert">
        <Building2 :size="24" aria-hidden="true" />
        <p class="app-eyebrow">{{ t('auth.login.workspaceAccess') }}</p>
        <h2>{{ t('auth.login.workspaceCheckUnavailable') }}</h2>
        <p>{{ t('auth.login.workspaceCheckDescription') }}</p>
        <button class="app-button" type="button" @click="checkWorkspace"><RefreshCw :size="15" /> {{ t('auth.login.retry') }}</button>
      </div>
      <form v-else class="auth-form" @submit.prevent="submit">
        <p class="app-eyebrow">{{ hostLabel }}</p>
        <h2>{{ t('auth.login.title') }}</h2>
        <p>{{ loginPaths.localLoginEnabled ? t('auth.login.description') : t('auth.login.ssoRequiredDescription') }}</p>
        <p v-if="ssoErrorKey" class="auth-message auth-message--error auth-sso-error" role="alert">{{ t(ssoErrorKey) }}</p>
        <button v-if="loginPaths.ssoAvailable" class="app-button app-button--primary auth-sso-button" type="button" @click="continueWithSSO">
          <ShieldCheck :size="16" aria-hidden="true" /> {{ t('auth.login.continueWithSSO') }}
        </button>
        <p v-if="loginPaths.ssoAvailable && loginPaths.localLoginVisible" class="auth-divider"><span>{{ t('auth.login.or') }}</span></p>
        <template v-if="loginPaths.localLoginVisible">
          <label>
            {{ t('auth.login.email') }}
            <input v-model.trim="email" class="app-input" type="email" autocomplete="email" required :placeholder="t('auth.login.emailPlaceholder')" />
          </label>
          <button class="app-button" :class="{ 'app-button--primary': !loginPaths.ssoAvailable }" type="submit" :disabled="pending || !email">
            {{ pending ? t('auth.login.sending') : t('auth.login.submit') }} <ArrowRight v-if="!pending" :size="16" />
          </button>
        </template>
        <button
          v-else-if="loginPaths.breakGlassAvailable"
          class="auth-break-glass" type="button" @click="showBreakGlass = true"
        >
          {{ t('auth.login.breakGlassPrompt') }}
        </button>
        <p v-if="showBreakGlass && !loginPaths.localLoginEnabled" class="auth-message auth-message--warning" role="status">
          {{ t('auth.login.breakGlassNotice') }}
        </p>
        <p v-if="sent" class="auth-message auth-message--success" role="status"><CheckCircle2 :size="15" aria-hidden="true" /><span>{{ t('auth.login.sent') }}</span></p>
        <p v-if="errorMessage" class="auth-message auth-message--error" role="alert">{{ errorMessage }}</p>
        <p class="auth-signup-link">{{ t('auth.login.newAccount') }} <a :href="signupURL">{{ t('auth.login.signup') }}</a></p>
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
/* The base .auth-message shorthand sets margin-bottom to 0 with !important. */
.auth-sso-error { margin-bottom: 14px !important; }
.auth-sso-button { justify-content: center; }
.auth-divider { display: flex; align-items: center; gap: 10px; margin: 16px 0 !important; color: var(--muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.auth-divider::before, .auth-divider::after { flex: 1; height: 1px; background: var(--line); content: ''; }
.auth-break-glass { padding: 0; background: none; border: 0; color: var(--muted); font-size: 11px; font-weight: 650; text-align: left; text-decoration: underline; cursor: pointer; }
.auth-signup-link { margin-top: 20px !important; text-align: center; }
.auth-signup-link a { color: var(--teal); font-weight: 750; }
.locator-control { display: flex; min-height: 42px; align-items: center; overflow: hidden; margin-top: 6px; background: var(--surface); border: 1px solid var(--line-strong); border-radius: 5px; }
.locator-control:focus-within { border-color: var(--teal); box-shadow: 0 0 0 3px var(--accent-glow); }
.locator-control[data-invalid='true'] { border-color: var(--red); }
.locator-control input { min-width: 80px; flex: 1; padding: 0 0 0 10px; border: 0; outline: 0; }
.locator-control b { max-width: 55%; padding: 0 10px; overflow: hidden; color: var(--muted); font-size: 10px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
</style>
