<script setup lang="ts">
import {
  AlertTriangle,
  CheckCircle2,
  ExternalLink,
  Mail,
  ShieldCheck,
  Star,
} from '@lucide/vue'
import { extractAPIError } from '~/utils/safe-api-error'

definePageMeta({ layout: false })

type ResponseBlockType = 'rating' | 'free_text' | 'reaction' | 'single_choice'

interface ResponseBlock {
  key: string
  type: ResponseBlockType
  required: boolean
  options?: Array<{ value: string }>
}

interface CollectionLocalization {
  prompt: string
  comment_placeholder: string
  contact_label: string
  consent_label: string
  submit_label: string
  success_message: string
  privacy_purpose_summary?: string
  privacy_lawful_basis_summary?: string
  privacy_retention_summary?: string
  block_labels?: Record<string, string>
  option_labels?: Record<string, Record<string, string>>
}

interface CollectionView {
  locale: string
  campaign_name: string
  introduction: string
  response_blocks: ResponseBlock[]
  allow_comment: boolean
  collect_contact: boolean
  ai_processing: {
    enabled: boolean
    provider?: string
    privacy_policy_url?: string
  }
  localization: CollectionLocalization
  controller_name: string
  privacy_policy_url: string
  definition_version: number
  controller_profile_version: number
}

const copy = {
  en: {
    loading: 'Loading feedback form',
    eyebrow: 'Email feedback',
    fallbackPrompt: 'Share your feedback',
    required: 'Required',
    ratingValue: (value: number) => `${value} of 5`,
    comment: 'Additional context',
    contactFallback: 'Email address',
    contactIntro: 'Keep me updated about this feedback',
    privacyPolicy: 'Privacy policy',
    aiNotice: 'Your written feedback may be processed by the configured AI provider.',
    submitFallback: 'Send feedback',
    submitting: 'Sending feedback',
    unavailableTitle: 'Link unavailable',
    unavailableDescription: 'This link is invalid, expired, or no longer available.',
    errorTitle: 'Could not load the form',
    errorDescription: 'Please try again in a moment.',
    retry: 'Try again',
    requiredError: 'Please complete this field.',
    emailError: 'Enter a valid email address.',
    submitError: 'Could not send your feedback. Please try again.',
    successFallback: 'Thank you for your feedback',
    successDescription: 'Your response was received.',
    footer: 'Feedback collection powered by Fidibeki',
  },
  pt: {
    loading: 'A carregar formulário de feedback',
    eyebrow: 'Feedback por email',
    fallbackPrompt: 'Partilhe o seu feedback',
    required: 'Obrigatório',
    ratingValue: (value: number) => `${value} de 5`,
    comment: 'Contexto adicional',
    contactFallback: 'Email',
    contactIntro: 'Quero receber atualizações sobre este feedback',
    privacyPolicy: 'Política de privacidade',
    aiNotice: 'O seu feedback escrito pode ser processado pelo fornecedor de AI configurado.',
    submitFallback: 'Enviar feedback',
    submitting: 'A enviar feedback',
    unavailableTitle: 'Ligação indisponível',
    unavailableDescription: 'Esta ligação é inválida, expirou ou já não está disponível.',
    errorTitle: 'Não foi possível carregar o formulário',
    errorDescription: 'Tente novamente dentro de alguns instantes.',
    retry: 'Tentar novamente',
    requiredError: 'Preencha este campo.',
    emailError: 'Introduza um email válido.',
    submitError: 'Não foi possível enviar o feedback. Tente novamente.',
    successFallback: 'Obrigado pelo seu feedback',
    successDescription: 'A sua resposta foi recebida.',
    footer: 'Recolha de feedback através da Fidibeki',
  },
} as const

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const token = computed(() => String(route.params.token || ''))
const path = computed(() => `/v1/public/email-feedback/${encodeURIComponent(token.value)}`)
const responses = reactive<Record<string, string | number>>({})
const comment = ref('')
const contactConsent = ref(false)
const reporterEmail = ref('')
const fieldErrors = reactive<Record<string, string>>({})
const submitting = ref(false)
const submitError = ref('')
const submitted = ref(false)
const resultState = ref<HTMLElement | null>(null)
const appliedPreselection = ref('')

const { data, status, error, refresh } = await useAsyncData(
  () => `email-feedback-collection:${token.value}`,
  () => $apiFetch<{ collection: CollectionView }>(path.value, {
    auth: false,
    retryAuth: false,
  }),
  { watch: [token], server: false },
)

const collection = computed(() => data.value?.collection)
const language = computed<'en' | 'pt'>(() =>
  collection.value?.locale.toLowerCase().startsWith('pt') ? 'pt' : 'en',
)
const text = computed(() => copy[language.value])
const localization = computed(() => collection.value?.localization)
const prompt = computed(() =>
  localization.value?.prompt.trim() || text.value.fallbackPrompt,
)
const isUnavailable = computed(() => extractAPIError(error.value)?.code === 'not_found')
const submitLabel = computed(() =>
  localization.value?.submit_label.trim() || text.value.submitFallback,
)
const successMessage = computed(() =>
  localization.value?.success_message.trim() || text.value.successFallback,
)

watch(
  [collection, () => route.query.rating],
  ([current, rawRating]) => {
    if (!current) return
    const preselectionKey = `${token.value}:${String(rawRating || '')}`
    if (appliedPreselection.value === preselectionKey) return
    appliedPreselection.value = preselectionKey
    const rating = Number(rawRating)
    const block = current.response_blocks.find(item => item.type === 'rating')
    if (block && Number.isInteger(rating) && rating >= 1 && rating <= 5 &&
      responses[block.key] === undefined) {
      responses[block.key] = rating
    }
  },
  { immediate: true },
)

watch(contactConsent, (enabled) => {
  if (!enabled) {
    reporterEmail.value = ''
    delete fieldErrors.reporter_email
  }
})

function blockLabel(block: ResponseBlock): string {
  return localization.value?.block_labels?.[block.key]?.trim() ||
    block.key.replaceAll('_', ' ').replace(/^./, character => character.toUpperCase())
}

function optionLabel(block: ResponseBlock, value: string): string {
  return localization.value?.option_labels?.[block.key]?.[value]?.trim() || value
}

function setResponse(block: ResponseBlock, value: string | number) {
  responses[block.key] = value
  delete fieldErrors[`responses.${block.key}`]
}

function responseValue(block: ResponseBlock): string | number | undefined {
  return responses[block.key]
}

function validate(): boolean {
  for (const key of Object.keys(fieldErrors)) delete fieldErrors[key]
  for (const block of collection.value?.response_blocks || []) {
    const value = responses[block.key]
    if (block.required && (
      value === undefined || value === null ||
      (typeof value === 'string' && value.trim() === '')
    )) {
      fieldErrors[`responses.${block.key}`] = text.value.requiredError
    }
  }
  if (contactConsent.value) {
    const value = reporterEmail.value.trim()
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value) || value.length > 320) {
      fieldErrors.reporter_email = text.value.emailError
    }
  }
  return Object.keys(fieldErrors).length === 0
}

async function submitFeedback() {
  const current = collection.value
  if (!current || submitting.value || submitted.value || !validate()) return
  submitting.value = true
  submitError.value = ''
  const submittedResponses: Record<string, string | number> = {}
  for (const block of current.response_blocks) {
    const value = responses[block.key]
    if (typeof value === 'number') submittedResponses[block.key] = value
    if (typeof value === 'string' && value.trim()) submittedResponses[block.key] = value.trim()
  }
  const body: Record<string, unknown> = { responses: submittedResponses }
  if (current.allow_comment && comment.value.trim()) body.comment = comment.value.trim()
  if (current.collect_contact && contactConsent.value) {
    body.reporter_email = reporterEmail.value.trim()
    body.notify_reporter = true
    body.consent = {
      purpose: 'feedback_updates',
      notice_version: '2026-07-controller-v1',
      controller_profile_version: current.controller_profile_version,
      widget_definition_version: current.definition_version,
      locale: current.locale.toLowerCase(),
      granted: true,
    }
  }
  try {
    await $apiFetch(path.value, {
      method: 'POST',
      body,
      auth: false,
      retryAuth: false,
    })
    submitted.value = true
    await nextTick()
    resultState.value?.focus()
  } catch {
    submitError.value = text.value.submitError
  } finally {
    submitting.value = false
  }
}

function retryLoad() {
  void refresh()
}

useHead(() => ({
  title: `${prompt.value} - Fidibeki`,
  htmlAttrs: { lang: language.value },
  meta: [{ name: 'robots', content: 'noindex,nofollow' }],
}))
</script>

<template>
  <div class="email-feedback-page">
    <header class="email-feedback-topbar">
      <span class="app-brand">
        <img class="app-brand-light" src="/brand/fidibeki-color.png" alt="Fidibeki" />
        <img class="app-brand-dark" src="/brand/fidibeki-white.png" alt="Fidibeki" />
      </span>
    </header>

    <main>
      <section
        v-if="status === 'idle' || status === 'pending'"
        class="email-feedback-state"
        aria-live="polite"
      >
        <span class="app-spinner" />
        <h1>{{ text.loading }}</h1>
      </section>

      <section v-else-if="isUnavailable" class="email-feedback-state" role="alert">
        <AlertTriangle :size="26" />
        <h1>{{ text.unavailableTitle }}</h1>
        <p>{{ text.unavailableDescription }}</p>
      </section>

      <section v-else-if="error || !collection" class="email-feedback-state" role="alert">
        <AlertTriangle :size="26" />
        <h1>{{ text.errorTitle }}</h1>
        <p>{{ text.errorDescription }}</p>
        <button type="button" class="secondary-button" @click="retryLoad">
          {{ text.retry }}
        </button>
      </section>

      <section
        v-else-if="submitted"
        ref="resultState"
        class="email-feedback-state email-feedback-done"
        tabindex="-1"
        role="status"
      >
        <CheckCircle2 :size="30" />
        <h1>{{ successMessage }}</h1>
        <p>{{ text.successDescription }}</p>
      </section>

      <form v-else class="email-feedback-form" novalidate @submit.prevent="submitFeedback">
        <header class="email-feedback-heading">
          <p class="email-feedback-eyebrow"><Mail :size="14" /> {{ text.eyebrow }}</p>
          <h1>{{ prompt }}</h1>
          <p v-if="collection.introduction">{{ collection.introduction }}</p>
          <span>{{ collection.controller_name }}</span>
        </header>

        <div class="email-feedback-fields">
          <fieldset
            v-for="block in collection.response_blocks"
            :key="block.key"
            class="email-feedback-field"
          >
            <legend>
              {{ blockLabel(block) }}
              <small v-if="block.required">{{ text.required }}</small>
            </legend>

            <div v-if="block.type === 'rating'" class="email-feedback-rating">
              <button
                v-for="value in 5"
                :key="value"
                type="button"
                :aria-label="text.ratingValue(value)"
                :aria-pressed="responseValue(block) === value"
                :data-filled="
                  typeof responseValue(block) === 'number' &&
                    Number(responseValue(block)) >= value
                "
                @click="setResponse(block, value)"
              >
                <Star
                  :size="22"
                  :fill="
                    typeof responseValue(block) === 'number' &&
                      Number(responseValue(block)) >= value
                      ? 'currentColor'
                      : 'none'
                  "
                  aria-hidden="true"
                />
              </button>
            </div>

            <textarea
              v-else-if="block.type === 'free_text'"
              :id="`email-feedback-${block.key}`"
              :aria-label="blockLabel(block)"
              :aria-invalid="Boolean(fieldErrors[`responses.${block.key}`])"
              maxlength="10000"
              rows="5"
              :value="typeof responseValue(block) === 'string' ? responseValue(block) : ''"
              @input="setResponse(block, ($event.target as HTMLTextAreaElement).value)"
            />

            <div v-else class="email-feedback-options">
              <label v-for="option in block.options || []" :key="option.value">
                <input
                  type="radio"
                  :name="`email-feedback-${block.key}`"
                  :value="option.value"
                  :checked="responseValue(block) === option.value"
                  @change="setResponse(block, option.value)"
                />
                <span>{{ optionLabel(block, option.value) }}</span>
              </label>
            </div>

            <p
              v-if="fieldErrors[`responses.${block.key}`]"
              class="email-feedback-error"
              role="alert"
            >
              {{ fieldErrors[`responses.${block.key}`] }}
            </p>
          </fieldset>

          <label v-if="collection.allow_comment" class="email-feedback-field">
            <span>{{ localization?.comment_placeholder || text.comment }}</span>
            <textarea
              v-model="comment"
              :aria-label="localization?.comment_placeholder || text.comment"
              maxlength="10000"
              rows="3"
            />
          </label>
        </div>

        <section v-if="collection.collect_contact" class="email-feedback-contact">
          <label class="email-feedback-consent">
            <input v-model="contactConsent" type="checkbox" />
            <span>
              <strong>{{ localization?.consent_label || text.contactIntro }}</strong>
              <small>{{ localization?.privacy_purpose_summary }}</small>
            </span>
          </label>

          <label v-if="contactConsent" class="email-feedback-field">
            <span>{{ localization?.contact_label || text.contactFallback }}</span>
            <input
              v-model.trim="reporterEmail"
              type="email"
              autocomplete="email"
              :aria-label="localization?.contact_label || text.contactFallback"
              :aria-invalid="Boolean(fieldErrors.reporter_email)"
            />
            <span v-if="fieldErrors.reporter_email" class="email-feedback-error" role="alert">
              {{ fieldErrors.reporter_email }}
            </span>
          </label>

          <div class="email-feedback-privacy">
            <ShieldCheck :size="17" aria-hidden="true" />
            <p>
              <span>{{ localization?.privacy_lawful_basis_summary }}</span>
              <span>{{ localization?.privacy_retention_summary }}</span>
            </p>
            <a
              :href="collection.privacy_policy_url"
              target="_blank"
              rel="noopener noreferrer"
            >
              {{ text.privacyPolicy }} <ExternalLink :size="13" />
            </a>
          </div>
        </section>

        <p v-if="collection.ai_processing.enabled" class="email-feedback-ai">
          {{ text.aiNotice }}
          <a
            v-if="collection.ai_processing.privacy_policy_url"
            :href="collection.ai_processing.privacy_policy_url"
            target="_blank"
            rel="noopener noreferrer"
          >
            {{ text.privacyPolicy }} <ExternalLink :size="12" />
          </a>
        </p>

        <p v-if="submitError" class="email-feedback-error email-feedback-submit-error" role="alert">
          {{ submitError }}
        </p>

        <button type="submit" class="primary-button email-feedback-submit" :disabled="submitting">
          {{ submitting ? text.submitting : submitLabel }}
        </button>
      </form>
    </main>

    <footer>{{ text.footer }}</footer>
  </div>
</template>

<style scoped>
.email-feedback-page {
  display: flex;
  min-height: 100vh;
  flex-direction: column;
}

.email-feedback-topbar {
  display: flex;
  min-height: 56px;
  align-items: center;
  justify-content: center;
  padding: 0 16px;
  background: var(--surface);
  border-bottom: 1px solid var(--line);
}

.email-feedback-topbar .app-brand {
  display: block;
  width: auto;
}

.email-feedback-topbar .app-brand img {
  width: auto;
  height: 22px;
}

main {
  display: grid;
  flex: 1 0 auto;
  padding: 32px 16px;
  place-items: start center;
}

.email-feedback-form,
.email-feedback-state {
  width: min(100%, 620px);
}

.email-feedback-form {
  padding: 28px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  box-shadow: 0 12px 32px var(--shadow-soft);
}

.email-feedback-eyebrow {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--teal);
  font-size: 11px;
  font-weight: 750;
  text-transform: uppercase;
}

.email-feedback-heading h1,
.email-feedback-state h1 {
  margin-top: 8px;
  font-size: 23px;
  line-height: 1.3;
}

.email-feedback-heading > p:not(.email-feedback-eyebrow) {
  margin-top: 10px;
  color: var(--ink-soft);
  font-size: 13px;
  line-height: 1.65;
}

.email-feedback-heading > span {
  display: block;
  margin-top: 9px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 750;
}

.email-feedback-fields {
  display: grid;
  gap: 22px;
  margin-top: 26px;
}

.email-feedback-field {
  display: grid;
  min-width: 0;
  gap: 8px;
  padding: 0;
  border: 0;
}

.email-feedback-field legend,
.email-feedback-field > span:first-child {
  color: var(--ink);
  font-size: 12px;
  font-weight: 750;
}

.email-feedback-field legend small {
  margin-left: 6px;
  color: var(--muted);
  font-size: 9px;
  font-weight: 650;
}

.email-feedback-field textarea,
.email-feedback-field input[type='email'] {
  width: 100%;
  min-width: 0;
  padding: 10px 11px;
  background: var(--surface);
  color: var(--ink);
  border: 1px solid var(--line-strong);
  border-radius: 5px;
  font: inherit;
  font-size: 13px;
  line-height: 1.5;
  resize: vertical;
}

.email-feedback-field textarea:focus,
.email-feedback-field input[type='email']:focus {
  border-color: var(--teal);
  outline: 3px solid var(--focus-ring);
}

.email-feedback-field [aria-invalid='true'] {
  border-color: var(--red);
}

.email-feedback-rating {
  display: grid;
  width: min(100%, 280px);
  grid-template-columns: repeat(5, minmax(44px, 1fr));
  gap: 8px;
}

.email-feedback-rating button {
  display: grid;
  width: 100%;
  aspect-ratio: 1;
  place-items: center;
  padding: 0;
  background: var(--surface-soft);
  color: var(--muted);
  border: 1px solid var(--line);
  border-radius: 5px;
  cursor: pointer;
}

.email-feedback-rating button:hover,
.email-feedback-rating button[data-filled='true'] {
  background: var(--amber-soft);
  color: var(--amber);
  border-color: var(--amber-line);
}

.email-feedback-rating button:focus-visible {
  outline: 3px solid var(--focus-ring);
  outline-offset: 2px;
}

.email-feedback-options {
  display: grid;
  gap: 8px;
}

.email-feedback-options label {
  display: grid;
  min-height: 42px;
  grid-template-columns: 18px minmax(0, 1fr);
  align-items: center;
  gap: 9px;
  padding: 8px 10px;
  background: var(--surface-soft);
  border: 1px solid var(--line);
  border-radius: 5px;
  cursor: pointer;
  font-size: 12px;
}

.email-feedback-contact {
  display: grid;
  gap: 16px;
  margin-top: 26px;
  padding-top: 22px;
  border-top: 1px solid var(--line);
}

.email-feedback-consent {
  display: grid;
  grid-template-columns: 18px minmax(0, 1fr);
  align-items: start;
  gap: 10px;
  cursor: pointer;
}

.email-feedback-consent input {
  width: 16px;
  height: 16px;
  margin-top: 2px;
  accent-color: var(--teal-fill);
}

.email-feedback-consent span {
  display: grid;
  gap: 4px;
}

.email-feedback-consent strong {
  font-size: 12px;
}

.email-feedback-consent small {
  color: var(--muted);
  font-size: 10px;
  line-height: 1.5;
}

.email-feedback-privacy {
  display: grid;
  grid-template-columns: 17px minmax(0, 1fr) auto;
  align-items: start;
  gap: 9px;
  color: var(--muted);
  font-size: 10px;
  line-height: 1.5;
}

.email-feedback-privacy > svg {
  color: var(--green);
}

.email-feedback-privacy p {
  display: grid;
  gap: 3px;
}

.email-feedback-privacy a,
.email-feedback-ai a {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--teal);
  font-weight: 700;
}

.email-feedback-ai {
  margin-top: 20px;
  color: var(--muted);
  font-size: 10px;
  line-height: 1.55;
}

.email-feedback-error {
  color: var(--red);
  font-size: 10px;
  line-height: 1.5;
}

.email-feedback-submit-error {
  margin-top: 18px;
}

.email-feedback-submit {
  width: 100%;
  margin-top: 22px;
}

.email-feedback-state {
  display: grid;
  min-height: 300px;
  align-content: center;
  justify-items: center;
  gap: 8px;
  padding: 28px 20px;
  text-align: center;
}

.email-feedback-state > svg {
  color: var(--amber);
}

.email-feedback-state p {
  max-width: 380px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.6;
}

.email-feedback-state button {
  margin-top: 10px;
}

.email-feedback-done > svg {
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
    padding: 20px 10px;
  }

  .email-feedback-form {
    padding: 20px 16px;
  }

  .email-feedback-heading h1 {
    font-size: 21px;
  }

  .email-feedback-rating {
    width: 100%;
    gap: 6px;
  }

  .email-feedback-privacy {
    grid-template-columns: 17px minmax(0, 1fr);
  }

  .email-feedback-privacy a {
    grid-column: 2;
  }
}
</style>
