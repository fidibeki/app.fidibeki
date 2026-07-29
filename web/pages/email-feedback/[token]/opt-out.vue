<script setup lang="ts">
import { AlertTriangle, CheckCircle2, ExternalLink, MailX, ShieldCheck } from '@lucide/vue'
import { extractAPIError } from '~/utils/safe-api-error'

definePageMeta({ layout: false })

interface OptOutView {
  controller_name: string
  locale: string
  privacy_policy_url: string
  opted_out: boolean
}

const copy = {
  en: {
    pageTitle: 'Email feedback preferences - Fidibeki',
    loading: 'Loading email preferences',
    eyebrow: 'Email feedback',
    title: 'Stop receiving feedback requests',
    description: (controller: string) =>
      `You will no longer receive feedback requests sent by ${controller} through Fidibeki.`,
    scope: 'This applies to future email feedback requests from this organization.',
    privacy: 'Privacy policy',
    action: 'Stop email requests',
    sending: 'Updating preferences',
    doneTitle: 'Email requests stopped',
    doneDescription: (controller: string) =>
      `You will no longer receive feedback requests from ${controller}.`,
    unavailableTitle: 'Link unavailable',
    unavailableDescription: 'This link is invalid or no longer available.',
    errorTitle: 'Could not load preferences',
    errorDescription: 'Please try again in a moment.',
    retry: 'Try again',
    submitError: 'Could not update your email preferences. Please try again.',
    footer: 'Feedback delivery powered by Fidibeki',
  },
  pt: {
    pageTitle: 'Preferências de feedback por email - Fidibeki',
    loading: 'A carregar preferências de email',
    eyebrow: 'Feedback por email',
    title: 'Deixar de receber pedidos de feedback',
    description: (controller: string) =>
      `Deixará de receber pedidos de feedback enviados por ${controller} através da Fidibeki.`,
    scope: 'Esta preferência aplica-se a futuros pedidos de feedback por email desta organização.',
    privacy: 'Política de privacidade',
    action: 'Deixar de receber pedidos',
    sending: 'A atualizar preferências',
    doneTitle: 'Pedidos por email desativados',
    doneDescription: (controller: string) =>
      `Deixará de receber pedidos de feedback de ${controller}.`,
    unavailableTitle: 'Ligação indisponível',
    unavailableDescription: 'Esta ligação é inválida ou já não está disponível.',
    errorTitle: 'Não foi possível carregar as preferências',
    errorDescription: 'Tente novamente dentro de alguns instantes.',
    retry: 'Tentar novamente',
    submitError: 'Não foi possível atualizar as preferências de email. Tente novamente.',
    footer: 'Envio de feedback através da Fidibeki',
  },
} as const

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const token = computed(() => String(route.params.token || ''))
const path = computed(
  () => `/v1/public/email-feedback/${encodeURIComponent(token.value)}/opt-out`,
)

const sending = ref(false)
const submitError = ref('')
const done = ref(false)
const confirmation = ref<HTMLElement | null>(null)

const { data, status, error, refresh } = await useAsyncData(
  () => `email-feedback-opt-out:${token.value}`,
  () => $apiFetch<{ opt_out: OptOutView }>(path.value, {
    auth: false,
    retryAuth: false,
  }),
  { watch: [token], server: false },
)

const optOut = computed(() => data.value?.opt_out)
const language = computed<'en' | 'pt'>(() =>
  optOut.value?.locale.toLowerCase().startsWith('pt') ? 'pt' : 'en',
)
const text = computed(() => copy[language.value])
const controllerName = computed(() => optOut.value?.controller_name.trim() || 'this organization')
const isUnavailable = computed(() => extractAPIError(error.value)?.code === 'not_found')
const isOptedOut = computed(() => Boolean(optOut.value?.opted_out || done.value))

function retryLoad() {
  void refresh()
}

async function optOutOfEmailFeedback() {
  if (sending.value || isOptedOut.value) return
  sending.value = true
  submitError.value = ''
  try {
    await $apiFetch(path.value, {
      method: 'POST',
      body: {},
      auth: false,
      retryAuth: false,
    })
    done.value = true
    await nextTick()
    confirmation.value?.focus()
  } catch {
    submitError.value = text.value.submitError
  } finally {
    sending.value = false
  }
}

useHead(() => ({
  title: text.value.pageTitle,
  htmlAttrs: { lang: language.value },
  meta: [{ name: 'robots', content: 'noindex,nofollow' }],
}))
</script>

<template>
  <div class="opt-out-page">
    <header class="opt-out-topbar">
      <span class="app-brand">
        <img class="app-brand-light" src="/brand/fidibeki-color.png" alt="Fidibeki" />
        <img class="app-brand-dark" src="/brand/fidibeki-white.png" alt="Fidibeki" />
      </span>
    </header>

    <main>
      <section
        v-if="status === 'idle' || status === 'pending'"
        class="opt-out-state"
        aria-live="polite"
      >
        <span class="app-spinner" />
        <h1>{{ text.loading }}</h1>
      </section>

      <section v-else-if="isUnavailable" class="opt-out-state" role="alert">
        <AlertTriangle :size="26" />
        <h1>{{ text.unavailableTitle }}</h1>
        <p>{{ text.unavailableDescription }}</p>
      </section>

      <section v-else-if="error || !optOut" class="opt-out-state" role="alert">
        <AlertTriangle :size="26" />
        <h1>{{ text.errorTitle }}</h1>
        <p>{{ text.errorDescription }}</p>
        <button type="button" class="secondary-button" @click="retryLoad">
          {{ text.retry }}
        </button>
      </section>

      <section
        v-else-if="isOptedOut"
        ref="confirmation"
        class="opt-out-state opt-out-done"
        tabindex="-1"
        role="status"
      >
        <CheckCircle2 :size="28" />
        <h1>{{ text.doneTitle }}</h1>
        <p>{{ text.doneDescription(controllerName) }}</p>
      </section>

      <section v-else class="opt-out-panel">
        <p class="opt-out-eyebrow"><MailX :size="14" /> {{ text.eyebrow }}</p>
        <h1>{{ text.title }}</h1>
        <p class="opt-out-description">{{ text.description(controllerName) }}</p>

        <div class="opt-out-scope">
          <ShieldCheck :size="18" />
          <p>{{ text.scope }}</p>
        </div>

        <a
          class="opt-out-privacy"
          :href="optOut.privacy_policy_url"
          target="_blank"
          rel="noopener noreferrer"
        >
          {{ text.privacy }}
          <ExternalLink :size="14" />
        </a>

        <p v-if="submitError" class="opt-out-error" role="alert">{{ submitError }}</p>

        <button
          type="button"
          class="primary-button opt-out-action"
          :disabled="sending"
          @click="optOutOfEmailFeedback"
        >
          {{ sending ? text.sending : text.action }}
        </button>
      </section>
    </main>

    <footer>{{ text.footer }}</footer>
  </div>
</template>

<style scoped>
.opt-out-page {
  display: flex;
  min-height: 100vh;
  flex-direction: column;
}

.opt-out-topbar {
  display: flex;
  min-height: 56px;
  align-items: center;
  justify-content: center;
  padding: 0 16px;
  background: var(--surface);
  border-bottom: 1px solid var(--line);
}

.opt-out-topbar .app-brand {
  display: block;
  width: auto;
}

.opt-out-topbar .app-brand img {
  width: auto;
  height: 22px;
}

main {
  display: grid;
  flex: 1 0 auto;
  padding: 32px 16px;
  place-items: center;
}

.opt-out-panel,
.opt-out-state {
  width: min(100%, 440px);
}

.opt-out-panel {
  padding: 24px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: 0 12px 32px var(--shadow-soft);
}

.opt-out-eyebrow {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--teal);
  font-size: 11px;
  font-weight: 750;
  text-transform: uppercase;
}

.opt-out-panel h1,
.opt-out-state h1 {
  margin-top: 8px;
  font-size: 21px;
  line-height: 1.3;
}

.opt-out-description {
  margin-top: 12px;
  color: var(--ink-soft);
  font-size: 13px;
  line-height: 1.65;
}

.opt-out-scope {
  display: grid;
  grid-template-columns: 18px minmax(0, 1fr);
  gap: 10px;
  margin-top: 18px;
  padding: 12px;
  background: var(--surface-soft);
  color: var(--muted);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  font-size: 11px;
  line-height: 1.55;
}

.opt-out-scope svg {
  color: var(--green);
}

.opt-out-privacy {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-top: 16px;
  color: var(--teal);
  font-size: 11px;
  font-weight: 700;
}

.opt-out-privacy:hover {
  text-decoration: underline;
}

.opt-out-error {
  margin-top: 16px;
  color: var(--red);
  font-size: 11px;
  line-height: 1.55;
}

.opt-out-action {
  width: 100%;
  margin-top: 20px;
}

.opt-out-state {
  display: grid;
  min-height: 260px;
  align-content: center;
  justify-items: center;
  gap: 8px;
  padding: 28px 20px;
  text-align: center;
}

.opt-out-state > svg {
  color: var(--amber);
}

.opt-out-state p {
  max-width: 360px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.6;
}

.opt-out-state button {
  margin-top: 10px;
}

.opt-out-done > svg {
  color: var(--green);
}

footer {
  flex: 0 0 auto;
  padding: 16px;
  color: var(--muted);
  border-top: 1px solid var(--line);
  font-size: 9px;
  text-align: center;
}

@media (max-width: 520px) {
  main {
    align-items: start;
    padding: 24px 12px;
  }

  .opt-out-panel {
    padding: 20px;
  }
}
</style>
