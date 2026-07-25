<script setup lang="ts">
import { AlertTriangle, ArrowRight, CheckCircle2, ExternalLink, RefreshCw } from '@lucide/vue'
import type { PlanCatalogResponse, SignupResponse, TenantPlan } from '~/types/api'
import { extractAPIError } from '~/utils/safe-api-error'
import {
  applicationURL,
  normalizeWorkspaceSlug,
  workspaceLoginURL,
  workspaceSlugError,
} from '~/utils/workspace-url'

definePageMeta({ layout: false })

const config = useRuntimeConfig()
const route = useRoute()
const requestURL = useRequestURL()
const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const regionalFormat = useRegionalFormat()
const localizedError = useLocalizedAPIError()
const baseDomain = String(config.public.baseDomain || requestURL.hostname)
const appURL = String(config.public.appUrl || '')
const siteKey = String(config.public.turnstileSiteKey || '')
const name = ref('')
const requestedSlug = typeof route.query.slug === 'string' ? normalizeWorkspaceSlug(route.query.slug) : ''
const initialSlug = requestedSlug && !workspaceSlugError(requestedSlug) ? requestedSlug : ''
const slug = ref(initialSlug)
const email = ref('')
const selectedPlan = ref<TenantPlan>('growth')
const turnstileToken = ref('')
const challengeFailed = ref(false)
const slugTouched = ref(Boolean(initialSlug))
const pending = ref(false)
const errorMessage = ref('')
const fieldErrors = ref<Record<string, string>>({})
const created = ref<SignupResponse['tenant'] | null>(null)
const locatorSlug = ref('')
const locatorError = ref('')
const slugInput = ref<HTMLInputElement | null>(null)
const emailInput = ref<HTMLInputElement | null>(null)
const turnstile = ref<{ reset: () => void; retry: () => Promise<void> } | null>(null)

const { data: catalog, status: plansStatus, error: plansError, refresh: refreshPlans } = await useAsyncData(
  'public-plan-catalog',
  () => $apiFetch<PlanCatalogResponse>('/v1/plans', { auth: false, retryAuth: false }),
  { server: false },
)

const signupSlugError = computed(() =>
  slug.value && workspaceSlugError(slug.value) ? t('auth.signup.slugInvalid') : '',
)
const createdLoginURL = computed(() => created.value
  ? workspaceLoginURL(created.value.slug, baseDomain, requestURL)
  : '')

watch(name, (value) => {
  if (!slugTouched.value) slug.value = normalizeWorkspaceSlug(value)
})

function planName(plan: TenantPlan) {
  return t(`auth.signup.plans.${plan}`)
}

function planPrice(cents?: number | null) {
  if (cents == null) return ''
  return regionalFormat.currency(cents / 100, 'EUR', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

function planProjectLimit(count: number) {
  return regionalFormat.plural('auth.signup.projectLimit', count)
}

function planFeedbackLimit(count: number) {
  return t('auth.signup.feedbackLimit', { count: regionalFormat.integer(count) })
}

function editSlug(event: Event) {
  slugTouched.value = true
  slug.value = normalizeWorkspaceSlug((event.target as HTMLInputElement).value)
}

function resetChallenge(message = '') {
  challengeFailed.value = false
  turnstileToken.value = ''
  turnstile.value?.reset()
  if (message) errorMessage.value = message
}

function failChallenge() {
  turnstileToken.value = ''
  challengeFailed.value = true
  errorMessage.value = t('auth.signup.challengeUnavailable')
}

function expireChallenge() {
  resetChallenge(t('auth.signup.challengeExpired'))
}

async function retryChallenge() {
  challengeFailed.value = false
  errorMessage.value = ''
  await turnstile.value?.retry()
}

async function submit() {
  if (pending.value || !catalog.value) return
  const validationError = workspaceSlugError(slug.value)
  if (validationError) {
    errorMessage.value = t('auth.signup.slugInvalid')
    slugInput.value?.focus()
    return
  }
  if (siteKey && !turnstileToken.value) {
    errorMessage.value = t('auth.signup.challengeRequired')
    return
  }

  pending.value = true
  errorMessage.value = ''
  fieldErrors.value = {}
  try {
    const response = await $apiFetch<SignupResponse>('/v1/signup', {
      method: 'POST',
      auth: false,
      retryAuth: false,
      body: {
        name: name.value,
        slug: slug.value,
        email: email.value,
        plan: selectedPlan.value,
        turnstile_token: turnstileToken.value,
      },
    })
    created.value = response.tenant
  } catch (error) {
    fieldErrors.value = localizedError.fields(error)
    const message = localizedError.message(error, 'auth.signup.createFailed')
    resetChallenge(Object.keys(fieldErrors.value).length ? '' : message)
    await nextTick()
    if (fieldErrors.value.slug || extractAPIError(error)?.code === 'conflict') slugInput.value?.focus()
    else if (fieldErrors.value.email) emailInput.value?.focus()
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

useHead({ title: computed(() => t('auth.signup.pageTitle')) })
</script>

<template>
  <main class="signup-page">
    <section class="signup-context">
      <NuxtLink :to="applicationURL(appURL, baseDomain, '/signup', requestURL)" class="signup-brand" aria-label="Fidibeki">
        <img src="/brand/fidibeki-white.png" alt="Fidibeki" />
      </NuxtLink>
      <div class="signup-context-copy">
        <p class="signup-kicker">{{ t('auth.signup.setup') }}</p>
        <h1>{{ t('auth.signup.contextTitle') }}</h1>
        <p>{{ t('auth.signup.contextDescription') }}</p>
      </div>
      <form class="workspace-locator" @submit.prevent="openWorkspace">
        <label for="workspace-locator">{{ t('auth.signup.existingWorkspace') }}</label>
        <div>
          <input id="workspace-locator" v-model="locatorSlug" type="text" autocomplete="organization" :placeholder="t('auth.signup.workspacePlaceholder')" />
          <span>.{{ baseDomain }}</span>
          <button type="submit" :title="t('auth.signup.openWorkspace')"><ArrowRight :size="16" /></button>
        </div>
        <p v-if="locatorError" role="alert">{{ locatorError }}</p>
      </form>
    </section>

    <section class="signup-panel">
      <div v-if="created" class="signup-confirmation">
        <CheckCircle2 :size="25" />
        <p class="app-eyebrow">{{ t('auth.signup.created') }}</p>
        <h2>{{ t('auth.signup.checkEmail', { email }) }}</h2>
        <p>{{ t('auth.signup.createdDescription') }}</p>
        <a class="app-button app-button--primary" :href="createdLoginURL">
          {{ t('auth.signup.openCreated', { workspace: created.slug }) }} <ExternalLink :size="15" />
        </a>
        <code>{{ createdLoginURL }}</code>
      </div>

      <form v-else class="signup-form" @submit.prevent="submit">
        <header>
          <p class="app-eyebrow">{{ t('auth.signup.trial') }}</p>
          <h2>{{ t('auth.signup.title') }}</h2>
          <p>{{ t('auth.signup.description') }}</p>
        </header>

        <div class="signup-field-grid">
          <label>
            {{ t('auth.signup.workspaceName') }}
            <input v-model.trim="name" class="app-input" type="text" autocomplete="organization" required maxlength="120" :placeholder="t('auth.signup.workspaceNamePlaceholder')" />
            <small v-if="fieldErrors.name">{{ fieldErrors.name }}</small>
          </label>
          <label>
            {{ t('auth.signup.ownerEmail') }}
            <input ref="emailInput" v-model.trim="email" class="app-input" type="email" autocomplete="email" required :placeholder="t('auth.signup.ownerEmailPlaceholder')" />
            <small v-if="fieldErrors.email">{{ fieldErrors.email }}</small>
          </label>
        </div>

        <label class="signup-slug-field">
          {{ t('auth.signup.workspaceURL') }}
          <span class="signup-slug-control" :data-invalid="Boolean(signupSlugError)">
            <input ref="slugInput" :value="slug" type="text" autocomplete="off" required minlength="3" maxlength="24" @input="editSlug" />
            <b>.{{ baseDomain }}</b>
          </span>
          <small v-if="signupSlugError || fieldErrors.slug">{{ signupSlugError || fieldErrors.slug }}</small>
        </label>

        <fieldset class="signup-plans">
          <legend>{{ t('auth.signup.startingPlan') }}</legend>
          <div v-if="plansStatus === 'pending'" class="signup-plan-state"><span class="app-spinner" /> {{ t('auth.signup.loadingPlans') }}</div>
          <div v-else-if="plansError" class="signup-plan-state signup-plan-state--error">
            <AlertTriangle :size="16" /> {{ t('auth.signup.plansUnavailable') }}
            <button type="button" :title="t('auth.signup.retryPlans')" @click="refreshPlans()"><RefreshCw :size="15" /></button>
          </div>
          <div v-else class="signup-plan-grid">
            <label v-for="plan in catalog?.plans" :key="plan.plan" :data-selected="selectedPlan === plan.plan">
              <input v-model="selectedPlan" type="radio" name="plan" :value="plan.plan" />
              <span><b>{{ planName(plan.plan) }}</b><strong>{{ planPrice(plan.monthly_price_cents) }}<small>{{ t('auth.signup.monthly') }}</small></strong></span>
              <small>{{ planProjectLimit(plan.limits.projects) }} · {{ planFeedbackLimit(plan.limits.feedback_per_month) }}</small>
            </label>
          </div>
        </fieldset>

        <TurnstileWidget
          ref="turnstile"
          :site-key="siteKey"
          @verified="turnstileToken = $event"
          @expired="expireChallenge"
          @failed="failChallenge"
        />

        <p v-if="errorMessage" class="auth-message auth-message--error" role="alert">{{ errorMessage }}</p>
        <button v-if="challengeFailed" class="app-button signup-challenge-retry" type="button" @click="retryChallenge">
          <RefreshCw :size="15" /> {{ t('auth.signup.retryVerification') }}
        </button>
        <button class="app-button app-button--primary signup-submit" type="submit" :disabled="pending || plansStatus !== 'success' || Boolean(signupSlugError)">
          {{ pending ? t('auth.signup.creating') : t('auth.signup.submit') }} <ArrowRight v-if="!pending" :size="16" />
        </button>
      </form>
    </section>
  </main>
</template>

<style scoped>
.signup-page { display: grid; min-height: 100vh; grid-template-columns: minmax(320px, .82fr) minmax(540px, 1.18fr); background: var(--surface); }
/* Fixed dark brand panel in both themes, mirroring .auth-context. */
.signup-context { display: flex; min-height: 100vh; flex-direction: column; justify-content: space-between; gap: 48px; padding: clamp(28px, 4vw, 58px); background: #17231f; color: #f5f8f7; }
.signup-brand { display: inline-flex; width: fit-content; }
.signup-brand img { display: block; width: 144px; height: auto; }
.signup-context-copy { max-width: 530px; }
.signup-kicker { margin-bottom: 12px; color: #79c8bb; font-size: 10px; font-weight: 800; text-transform: uppercase; }
.signup-context-copy h1 { max-width: 510px; font-size: clamp(34px, 4.2vw, 58px); line-height: 1.02; }
.signup-context-copy > p:last-child { max-width: 430px; margin-top: 16px; color: #b9c7c2; font-size: 14px; line-height: 1.65; }
.workspace-locator { max-width: 430px; }
.workspace-locator > label { display: block; margin-bottom: 8px; color: var(--line-strong); font-size: 11px; font-weight: 700; }
.workspace-locator > div { display: flex; min-height: 42px; align-items: center; overflow: hidden; background: #26332f; border: 1px solid #4a5a54; border-radius: 5px; }
.workspace-locator input { min-width: 80px; flex: 1; padding: 0 0 0 11px; background: transparent; color: #fff; border: 0; outline: 0; }
.workspace-locator span { max-width: 190px; overflow: hidden; color: var(--muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.workspace-locator button { display: grid; width: 42px; height: 40px; margin-left: 7px; place-items: center; background: var(--surface); color: #17231f; border: 0; }
.workspace-locator > p { margin-top: 7px; color: var(--red-line); font-size: 10px; }
.signup-panel { display: flex; min-height: 100vh; align-items: center; justify-content: center; padding: 42px clamp(28px, 6vw, 88px); }
.signup-form, .signup-confirmation { width: min(650px, 100%); }
.signup-form > header { margin-bottom: 25px; }
.signup-form h2, .signup-confirmation h2 { margin: 5px 0 7px; font-size: 23px; }
.signup-form header > p:last-child, .signup-confirmation > p { color: var(--muted); font-size: 12px; line-height: 1.55; }
.signup-field-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.signup-form label, .signup-plans legend { color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.signup-field-grid label, .signup-slug-field { display: flex; flex-direction: column; gap: 6px; }
.signup-field-grid .app-input { width: 100%; min-width: 0; min-height: 42px; }
.signup-field-grid label > small { color: var(--red); font-size: 9px; font-weight: 600; }
.signup-slug-field { margin-top: 13px; }
.signup-slug-control { display: flex; min-height: 42px; align-items: center; overflow: hidden; background: var(--surface); border: 1px solid var(--line-strong); border-radius: 5px; }
.signup-slug-control:focus-within { border-color: var(--teal); box-shadow: 0 0 0 3px var(--accent-glow); }
.signup-slug-control[data-invalid='true'] { border-color: var(--red); }
.signup-slug-control input { min-width: 80px; flex: 1; padding: 0 0 0 10px; border: 0; outline: 0; }
.signup-slug-control b { max-width: 55%; padding: 0 10px; overflow: hidden; color: var(--muted); font-size: 10px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.signup-slug-field > small { color: var(--red); font-size: 9px; font-weight: 600; }
.signup-plans { min-width: 0; margin: 19px 0 15px; padding: 0; border: 0; }
.signup-plans legend { margin-bottom: 7px; }
.signup-plan-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
.signup-plan-grid > label { display: flex; min-height: 83px; flex-direction: column; justify-content: space-between; gap: 7px; padding: 10px; border: 1px solid var(--line); border-radius: 5px; cursor: pointer; }
.signup-plan-grid > label:hover { background: var(--surface-subtle); border-color: var(--line-strong); }
.signup-plan-grid > label[data-selected='true'] { background: var(--teal-soft); border-color: var(--teal); box-shadow: inset 0 0 0 1px var(--teal); }
.signup-plan-grid input { position: absolute; width: 1px; height: 1px; opacity: 0; }
.signup-plan-grid label > span { display: flex; align-items: baseline; justify-content: space-between; gap: 6px; }
.signup-plan-grid label > span b { font-size: 11px; }
.signup-plan-grid strong { font-size: 13px; }
.signup-plan-grid strong small, .signup-plan-grid label > small { color: var(--muted); font-size: 8px; font-weight: 600; }
.signup-plan-state { display: flex; min-height: 83px; align-items: center; justify-content: center; gap: 8px; color: var(--muted); border: 1px solid var(--line); border-radius: 5px; font-size: 11px; }
.signup-plan-state--error { color: var(--red); }
.signup-plan-state button { display: grid; width: 30px; height: 30px; place-items: center; background: var(--surface); border: 1px solid var(--line); border-radius: 4px; }
.signup-submit { width: 100%; min-height: 44px; margin-top: 14px; }
.signup-challenge-retry { width: 100%; margin-top: 10px; }
.signup-confirmation > svg { color: var(--green); margin-bottom: 24px; }
.signup-confirmation > .app-button { width: 100%; min-height: 44px; margin-top: 26px; }
.signup-confirmation code { display: block; margin-top: 12px; padding: 10px; overflow: hidden; background: var(--surface-soft); color: var(--muted); border: 1px solid var(--line); border-radius: 5px; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 900px) { .signup-page { grid-template-columns: 1fr; } .signup-context { min-height: 220px; gap: 28px; padding: 24px; } .signup-context-copy h1 { font-size: 31px; } .signup-context-copy > p:last-child { display: none; } .workspace-locator { display: none; } .signup-panel { min-height: calc(100vh - 220px); align-items: flex-start; padding: 30px 24px 50px; } }
@media (max-width: 560px) { .signup-field-grid, .signup-plan-grid { grid-template-columns: 1fr; } .signup-plan-grid > label { min-height: 68px; } .signup-slug-control b { max-width: 48%; } }
</style>
