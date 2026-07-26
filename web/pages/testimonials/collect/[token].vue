<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Quote } from '@lucide/vue'
import { extractAPIError } from '~/utils/safe-api-error'

definePageMeta({ layout: false })

interface CollectView {
  kind: 'collection' | 'reconsent'
  prompt: string | null
  original_text: string
  proposed_quote: string | null
  submitted: boolean
}

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { t, locale } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const token = computed(() => String(route.params.token || ''))

const quote = ref('')
const authorName = ref('')
const authorRole = ref('')
const authorCompany = ref('')
const authorCompanyURL = ref('')
// Each scope is granted separately: someone may happily publish their words and
// not their name. Only the quote is pre-ticked, because without it there is
// nothing to publish at all.
const consentQuote = ref(true)
const consentIdentity = ref(false)
const consentCompany = ref(false)
const sending = ref(false)
const submitError = ref('')
const done = ref(false)
const revocationToken = ref('')
const confirmation = ref<HTMLElement | null>(null)

const { data, status, error } = await useAsyncData(
  () => `testimonial-collect:${token.value}`,
  () => $apiFetch<{ request: CollectView }>(
    `/v1/public/testimonial-requests/${encodeURIComponent(token.value)}`,
  ),
  { watch: [token], server: false },
)

const request = computed(() => data.value?.request)
const isUnusable = computed(() => extractAPIError(error.value)?.code === 'not_found')
const alreadySubmitted = computed(() => request.value?.submitted || done.value)

watch(request, (value) => {
  if (!value) return
  // Pre-fill with what they already wrote, so the ask is "confirm and expand",
  // not "produce a testimonial from nothing".
  quote.value = value.proposed_quote || value.original_text || ''
}, { immediate: true })

// Built from the configured public app URL rather than window.location, so it
// is identical whether this page rendered on the server or in the browser.
const runtime = useRuntimeConfig()
const revocationLink = computed(() =>
  `${runtime.public.appUrl || ''}/testimonials/revoke/${revocationToken.value}`)

const canSubmit = computed(() =>
  Boolean(quote.value.trim()) &&
  consentQuote.value &&
  (!consentIdentity.value || Boolean(authorName.value.trim())) &&
  !sending.value)

async function submit() {
  if (!canSubmit.value) return
  sending.value = true
  submitError.value = ''
  try {
    const result = await $apiFetch<{ revocation_token: string }>(
      `/v1/public/testimonial-requests/${encodeURIComponent(token.value)}`,
      {
        method: 'POST',
        body: {
          quote: quote.value.trim(),
          author_name: authorName.value.trim(),
          author_role: authorRole.value.trim(),
          author_company: authorCompany.value.trim(),
          author_company_url: authorCompanyURL.value.trim(),
          consent: {
            quote: consentQuote.value,
            identity: consentIdentity.value,
            company: consentCompany.value,
            avatar: false,
            video: false,
          },
          notice_version: 'v1',
          locale: String(locale.value),
        },
      },
    )
    revocationToken.value = result.revocation_token
    done.value = true
    await nextTick()
    confirmation.value?.focus()
  } catch (requestError) {
    submitError.value = apiErrorMessage(requestError, 'collectTestimonial.submitFailed')
  } finally {
    sending.value = false
  }
}

useHead(() => ({
  title: t('collectTestimonial.pageTitle'),
  meta: [{ name: 'robots', content: 'noindex,nofollow' }],
}))
</script>

<template>
  <div class="collect-page">
    <header class="collect-topbar">
      <span class="app-brand"><img src="/brand/fidibeki-color.png" alt="Fidibeki" /></span>
    </header>

    <main class="app-page">
      <div class="collect-container">
        <section v-if="status === 'idle' || status === 'pending'" class="app-surface app-state" aria-live="polite">
          <div><span class="app-spinner" /><h1>{{ t('collectTestimonial.loading') }}</h1></div>
        </section>

        <section v-else-if="isUnusable" class="app-surface app-state" role="alert">
          <div>
            <AlertTriangle :size="24" />
            <h1>{{ t('collectTestimonial.unusableTitle') }}</h1>
            <p>{{ t('collectTestimonial.unusableDescription') }}</p>
          </div>
        </section>

        <section v-else-if="error || !request" class="app-surface app-state" role="alert">
          <div>
            <AlertTriangle :size="24" />
            <h1>{{ t('collectTestimonial.errorTitle') }}</h1>
            <p>{{ apiErrorMessage(error, 'collectTestimonial.errorDescription') }}</p>
          </div>
        </section>

        <section v-else-if="alreadySubmitted" ref="confirmation" class="app-surface collect-done" tabindex="-1" role="status">
          <CheckCircle2 :size="26" />
          <h1>{{ t('collectTestimonial.thanksTitle') }}</h1>
          <p>{{ t('collectTestimonial.thanksDescription') }}</p>
          <!-- The way out is given at the same moment permission is given, not
               buried in a policy page. -->
          <div v-if="revocationToken" class="collect-revoke">
            <p class="section-label">{{ t('collectTestimonial.revokeTitle') }}</p>
            <p>{{ t('collectTestimonial.revokeDescription') }}</p>
            <pre>{{ revocationLink }}</pre>
          </div>
        </section>

        <template v-else>
          <header class="app-page-head collect-heading">
            <div>
              <p class="app-eyebrow"><Quote :size="13" /> {{ t('collectTestimonial.eyebrow') }}</p>
              <h1>{{ t('collectTestimonial.title') }}</h1>
              <span>{{ request.prompt || t('collectTestimonial.subtitle') }}</span>
            </div>
          </header>

          <form class="app-surface collect-form" @submit.prevent="submit">
            <label>
              {{ t('collectTestimonial.quoteLabel') }}
              <textarea v-model="quote" maxlength="2000" required autofocus />
              <span class="collect-hint">{{ t('collectTestimonial.quoteHint') }}</span>
            </label>

            <fieldset>
              <legend>{{ t('collectTestimonial.aboutYou') }}</legend>
              <label>{{ t('collectTestimonial.nameLabel') }}<input v-model="authorName" class="app-input" maxlength="120" /></label>
              <label>{{ t('collectTestimonial.roleLabel') }}<input v-model="authorRole" class="app-input" maxlength="120" /></label>
              <label>{{ t('collectTestimonial.companyLabel') }}<input v-model="authorCompany" class="app-input" maxlength="120" /></label>
              <label>{{ t('collectTestimonial.companyURLLabel') }}<input v-model="authorCompanyURL" class="app-input" type="url" maxlength="2048" /></label>
            </fieldset>

            <fieldset class="collect-consent">
              <legend>{{ t('collectTestimonial.permissionTitle') }}</legend>
              <p class="collect-hint">{{ t('collectTestimonial.permissionDescription') }}</p>
              <label class="collect-check">
                <input v-model="consentQuote" type="checkbox" />
                <span>{{ t('collectTestimonial.permitQuote') }}</span>
              </label>
              <label class="collect-check">
                <input v-model="consentIdentity" type="checkbox" />
                <span>{{ t('collectTestimonial.permitIdentity') }}</span>
              </label>
              <label class="collect-check">
                <input v-model="consentCompany" type="checkbox" />
                <span>{{ t('collectTestimonial.permitCompany') }}</span>
              </label>
              <!-- Stated before consent is given, not after: cached copies
                   genuinely outlive a withdrawal by a few minutes. -->
              <p class="collect-notice">{{ t('collectTestimonial.revocationNotice') }}</p>
            </fieldset>

            <p v-if="submitError" class="collect-error" role="alert">{{ submitError }}</p>

            <button type="submit" class="app-button app-button--primary" :disabled="!canSubmit">
              {{ sending ? t('collectTestimonial.sending') : t('collectTestimonial.submit') }}
            </button>
          </form>
        </template>
      </div>
    </main>

    <footer class="collect-footer">{{ t('collectTestimonial.footer') }}</footer>
  </div>
</template>

<style scoped>
.collect-page { display: flex; min-height: 100vh; flex-direction: column; }
.collect-topbar { position: sticky; top: 0; z-index: 10; display: flex; min-height: 56px; flex: 0 0 auto; align-items: center; justify-content: center; padding: 0 16px; background: var(--surface); border-bottom: 1px solid var(--line); }
.app-page { flex: 1 0 auto; }
.collect-container { max-width: 620px; margin: 0 auto; }
.collect-container .app-state { min-height: 300px; }
.collect-heading h1 { max-width: 520px; font-size: 21px; line-height: 1.25; }
.collect-heading .app-eyebrow { display: flex; align-items: center; gap: 5px; }
.collect-heading > div > span { display: block; margin-top: 5px; color: var(--muted); font-size: 10px; line-height: 1.55; }
.collect-form { display: grid; gap: 14px; padding: 16px; }
.collect-form > label,
.collect-form fieldset label { display: grid; gap: 5px; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.collect-form textarea { min-height: 112px; padding: 9px 11px; background: var(--surface); border: 1px solid var(--line-strong); border-radius: var(--radius); color: var(--ink); font: inherit; font-size: 11px; line-height: 1.55; resize: vertical; text-transform: none; }
.collect-form textarea:focus-visible { border-color: var(--teal); outline: 0; box-shadow: 0 0 0 3px var(--accent-glow); }
.collect-form fieldset { display: grid; gap: 10px; margin: 0; padding: 12px 13px; border: 1px solid var(--line); border-radius: var(--radius); }
.collect-form legend { padding: 0 5px; color: var(--ink); font-size: 10px; font-weight: 750; }
.collect-hint { color: var(--muted); font-size: 9px; font-weight: 400; line-height: 1.55; text-transform: none; }
.collect-check { display: flex !important; align-items: flex-start; gap: 8px; color: var(--ink) !important; font-size: 11px !important; font-weight: 500 !important; text-transform: none !important; }
.collect-check input { margin-top: 1px; }
.collect-notice { margin: 0; padding: 9px 10px; background: var(--surface-soft); border-radius: 4px; color: var(--muted); font-size: 9px; line-height: 1.6; }
.collect-error { margin: 0; color: var(--red); font-size: 10px; }
.collect-form > button { justify-self: end; min-height: 34px; font-size: 10px; }
.collect-done { display: grid; justify-items: center; gap: 8px; padding: 34px 20px; color: var(--green); text-align: center; }
.collect-done h1 { color: var(--ink); font-size: 18px; }
.collect-done p { max-width: 420px; color: var(--muted); font-size: 11px; line-height: 1.6; }
.collect-revoke { width: 100%; margin-top: 16px; padding-top: 14px; border-top: 1px solid var(--line); text-align: left; }
.collect-revoke p { max-width: none; margin: 4px 0 0; font-size: 10px; }
.collect-revoke pre { margin: 7px 0 0; padding: 9px 10px; overflow-x: auto; background: var(--canvas); border: 1px solid var(--line); border-radius: 4px; font-size: 9px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
.collect-footer { flex: 0 0 auto; padding: 16px; color: var(--muted); font-size: 9px; text-align: center; border-top: 1px solid var(--line); }
@media (max-width: 600px) {
  .collect-heading h1 { font-size: 18px; }
  .collect-form > button { width: 100%; justify-self: stretch; }
}
</style>
