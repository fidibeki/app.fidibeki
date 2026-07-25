<script setup lang="ts">
import type { AuthResponse } from '~/types/api'

definePageMeta({ layout: false })

const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const route = useRoute()
const { apply, pendingTwoFactor } = useAuth()

const code = ref('')
const pending = ref(false)
const errorMessage = ref('')
const useRecoveryCode = ref(false)

const inputLabel = computed(() =>
  useRecoveryCode.value ? t('auth.twoFactor.recoveryLabel') : t('auth.twoFactor.codeLabel'),
)
const inputPlaceholder = computed(() =>
  useRecoveryCode.value ? t('auth.twoFactor.recoveryPlaceholder') : t('auth.twoFactor.codePlaceholder'),
)

async function submit() {
  if (pending.value) return
  pending.value = true
  errorMessage.value = ''
  try {
    const response = await $apiFetch<AuthResponse>('/auth/2fa/challenge', {
      method: 'POST',
      body: { code: code.value },
      auth: false,
      retryAuth: false,
    })
    apply(response)
    pendingTwoFactor.value = null
    const returnTo = typeof route.query.return_to === 'string' && route.query.return_to.startsWith('/') &&
      !route.query.return_to.startsWith('//') ? route.query.return_to : ''
    await navigateTo(returnTo || firstAccessibleWorkspacePath(response.member.permissions))
  } catch (error) {
    errorMessage.value = localizedError.message(error, 'auth.twoFactor.failed')
    code.value = ''
  } finally {
    pending.value = false
  }
}

function toggleRecovery() {
  useRecoveryCode.value = !useRecoveryCode.value
  code.value = ''
  errorMessage.value = ''
}

useHead({ title: computed(() => t('auth.twoFactor.pageTitle')) })
</script>

<template>
  <main class="auth-page">
    <section class="auth-context">
      <NuxtLink to="/" class="app-brand auth-brand"><img src="/brand/fidibeki-white.png" alt="Fidibeki" /></NuxtLink>
      <div>
        <h1>{{ t('auth.twoFactor.contextTitle') }}</h1>
        <p>{{ t('auth.twoFactor.contextDescription') }}</p>
      </div>
    </section>
    <section class="auth-panel">
      <form class="auth-form" @submit.prevent="submit">
        <p class="app-eyebrow">{{ t('auth.securityVerification') }}</p>
        <h2>{{ t('auth.twoFactor.title') }}</h2>
        <p>{{ useRecoveryCode ? t('auth.twoFactor.recoveryDescription') : t('auth.twoFactor.description') }}</p>
        <label>
          {{ inputLabel }}
          <input
            v-model="code"
            class="app-input two-factor-code"
            :inputmode="useRecoveryCode ? 'text' : 'numeric'"
            :autocomplete="useRecoveryCode ? 'off' : 'one-time-code'"
            :maxlength="useRecoveryCode ? 13 : 7"
            :placeholder="inputPlaceholder"
            autofocus
            required
          />
        </label>
        <button class="app-button app-button--primary" type="submit" :disabled="pending">
          {{ pending ? t('auth.twoFactor.verifying') : t('auth.twoFactor.submit') }}
        </button>
        <p v-if="errorMessage" class="auth-message auth-message--error" role="alert">{{ errorMessage }}</p>
        <p class="two-factor-alternate">
          <button type="button" @click="toggleRecovery">
            {{ useRecoveryCode ? t('auth.twoFactor.useAuthenticator') : t('auth.twoFactor.useRecovery') }}
          </button>
        </p>
        <p class="auth-signup-link"><NuxtLink to="/login">{{ t('auth.twoFactor.startOver') }}</NuxtLink></p>
      </form>
    </section>
  </main>
</template>

<style scoped>
.auth-brand img { display: block; width: 142px; height: auto; }
.auth-signup-link { margin-top: 20px !important; text-align: center; }
.auth-signup-link a { color: var(--teal); font-weight: 750; }
.two-factor-code { letter-spacing: .28em; text-align: center; font-size: 17px; }
.two-factor-alternate { margin: 14px 0 0; text-align: center; }
.two-factor-alternate button { padding: 0; background: none; border: 0; color: var(--teal); font-size: 12px; font-weight: 700; cursor: pointer; }
.two-factor-alternate button:hover { text-decoration: underline; }
</style>
