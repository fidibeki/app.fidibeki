<script setup lang="ts">
import { ArrowRight, CheckCircle2 } from '@lucide/vue'
import { apiErrorMessage } from '~/utils/api-fetch'

definePageMeta({ layout: false })

const route = useRoute()
const { authenticated } = useAuth()
const { $apiFetch } = useNuxtApp()
const email = ref('')
const pending = ref(false)
const sent = ref(false)
const errorMessage = ref('')
const requestURL = useRequestURL()
const hostLabel = computed(() => {
  const first = requestURL.hostname.split('.')[0]
  return first && !['localhost', '127', 'www'].includes(first) ? first : 'your workspace'
})

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

onMounted(async () => {
  if (authenticated.value) {
    const destination = typeof route.query.return_to === 'string' ? route.query.return_to : '/'
    await navigateTo(destination)
  }
})

useHead({ title: 'Sign in · Fidibeki' })
</script>

<template>
  <main class="auth-page">
    <section class="auth-context">
      <NuxtLink to="/" class="app-brand"><span>F</span><b>fidibeki</b></NuxtLink>
      <div>
        <h1>Return to the feedback queue.</h1>
        <p>Review captured evidence, connect product signals to issues, and keep the team moving from one operational workspace.</p>
      </div>
    </section>
    <section class="auth-panel">
      <form class="auth-form" @submit.prevent="submit">
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
        <p v-if="sent" class="auth-message auth-message--success" role="status"><CheckCircle2 :size="15" /> Check your inbox. The link expires in 10 minutes.</p>
        <p v-if="errorMessage" class="auth-message auth-message--error" role="alert">{{ errorMessage }}</p>
      </form>
    </section>
  </main>
</template>
