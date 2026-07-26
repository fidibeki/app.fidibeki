<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Quote } from '@lucide/vue'
import { extractAPIError } from '~/utils/safe-api-error'

definePageMeta({ layout: false })

interface CollectView {
  kind: 'collection' | 'reconsent'
  prompt: string | null
  original_text: string
  proposed_quote: string | null
  /** The stars they already gave. Shown, not editable: it is a record of what
   *  they said, not a field to restate at publication time. */
  rating: number | null
  video_allowed: boolean
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
const consentVideo = ref(false)

// The recording, if they choose to send one. It uploads as soon as it is picked
// rather than on submit: a phone video takes a while, and asking someone to wait
// after they have already pressed the last button reads as a broken page.
const videoFile = ref<File | null>(null)
const videoState = ref<'idle' | 'uploading' | 'done' | 'failed'>('idle')
const videoError = ref('')
const videoName = computed(() => videoFile.value?.name || '')
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

// The configured public app URL, falling back to this page's own origin. The
// author is told to keep this link, and a relative path is not something anyone
// can keep — which is exactly what an unset appUrl used to produce. The fallback
// is safe because this route is client-rendered, so there is no server pass to
// disagree with.
const runtime = useRuntimeConfig()
const revocationLink = computed(() => {
  const origin = String(runtime.public.appUrl || '') ||
    (import.meta.client ? window.location.origin : '')
  return `${origin}/testimonials/revoke/${revocationToken.value}`
})

// An invitation to someone who never left feedback arrives with nothing behind
// it, so the page cannot ask them to review words they never wrote.
const fromScratch = computed(() => !request.value?.original_text && !request.value?.proposed_quote)

// Clamped rather than trusted: the score is rendered as a fixed row of five, so
// a value outside the range would otherwise draw a broken one.
const stars = computed(() => {
  const value = request.value?.rating
  if (!value || value < 1) return ''
  const filled = Math.min(5, Math.round(value))
  return '★'.repeat(filled) + '☆'.repeat(5 - filled)
})

const canSubmit = computed(() =>
  Boolean(quote.value.trim()) &&
  consentQuote.value &&
  (!consentIdentity.value || Boolean(authorName.value.trim())) &&
  !sending.value)

/**
 * Sends the recording straight to storage.
 *
 * Three steps, and the middle one does not touch our API at all: ask for a
 * destination, PUT the bytes there, then tell us it arrived. The file never
 * passes through the API, which is what keeps a slow mobile upload from holding
 * a connection the rest of the workspace needs.
 */
async function uploadVideo(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  videoFile.value = file
  videoState.value = 'uploading'
  videoError.value = ''
  try {
    const created = await $apiFetch<{ upload_url: string, media_id: string, complete_token: string, headers: Record<string, string> }>(
      `/v1/public/testimonial-requests/${encodeURIComponent(token.value)}/video`,
      { method: 'POST', body: { content_type: file.type, size_bytes: file.size } },
    )
    const stored = await fetch(created.upload_url, {
      method: 'PUT', body: file, headers: created.headers,
    })
    if (!stored.ok) throw new Error(`storage responded ${stored.status}`)
    await $apiFetch(
      `/v1/public/testimonial-requests/${encodeURIComponent(token.value)}/video/complete`,
      { method: 'POST', body: { media_id: created.media_id, complete_token: created.complete_token } },
    )
    videoState.value = 'done'
    // Sending a recording is not the same as agreeing to publish it, so the
    // permission is asked for separately and starts unticked.
    consentVideo.value = false
  } catch (failure) {
    videoState.value = 'failed'
    videoFile.value = null
    videoError.value = apiErrorMessage(failure, 'collectTestimonial.videoFailed')
  }
}

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
            video: consentVideo.value && videoState.value === 'done',
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
              <h1>{{ fromScratch ? t('collectTestimonial.blankTitle') : t('collectTestimonial.title') }}</h1>
              <span>
                {{ request.prompt ||
                  (fromScratch ? t('collectTestimonial.blankSubtitle') : t('collectTestimonial.subtitle')) }}
              </span>
            </div>
          </header>

          <form class="app-surface collect-form" @submit.prevent="submit">
            <label>
              {{ t('collectTestimonial.quoteLabel') }}
              <textarea v-model="quote" maxlength="2000" required autofocus />
              <span class="collect-hint">
                {{ fromScratch ? t('collectTestimonial.blankQuoteHint') : t('collectTestimonial.quoteHint') }}
              </span>
            </label>

            <!-- Their own stars, stated because they are published alongside the
                 words. Asking permission for something without showing it is not
                 asking. -->
            <p v-if="stars" class="collect-rating">
              <span class="collect-stars" aria-hidden="true">{{ stars }}</span>
              <span>{{ t('collectTestimonial.ratingNote', { rating: request.rating }) }}</span>
            </p>

            <!-- Only where the workspace can accept one. Offering a field that
                 would be refused after the file was chosen wastes the upload and
                 the author's patience. -->
            <fieldset v-if="request.video_allowed" class="collect-video">
              <legend>{{ t('collectTestimonial.videoTitle') }}</legend>
              <p class="collect-hint">{{ t('collectTestimonial.videoHint') }}</p>
              <input
                type="file" accept="video/mp4,video/quicktime,video/webm"
                :disabled="videoState === 'uploading'" @change="uploadVideo"
              />
              <p v-if="videoState === 'uploading'" class="collect-hint" aria-live="polite">
                <span class="app-spinner" /> {{ t('collectTestimonial.videoUploading') }}
              </p>
              <p v-else-if="videoState === 'done'" class="collect-video-ready" aria-live="polite">
                {{ t('collectTestimonial.videoReady', { name: videoName }) }}
              </p>
              <p v-else-if="videoError" class="collect-error" role="alert">{{ videoError }}</p>
            </fieldset>

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
              <!-- Only once a recording exists, and never pre-ticked: sending a
                   video is not the same act as agreeing to publish one. -->
              <label v-if="videoState === 'done'" class="collect-check">
                <input v-model="consentVideo" type="checkbox" />
                <span>{{ t('collectTestimonial.permitVideo') }}</span>
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
.collect-rating { display: flex; align-items: center; gap: 8px; margin: -6px 0 0; color: var(--muted); font-size: 9px; line-height: 1.55; }
.collect-stars { color: var(--amber-ink); font-size: 13px; letter-spacing: 1px; }
.collect-video input[type='file'] { color: var(--ink); font-size: 10px; }
.collect-video .app-spinner { width: 11px; height: 11px; margin-right: 5px; vertical-align: -1px; }
.collect-video-ready { margin: 0; color: var(--green); font-size: 10px; font-weight: 700; overflow-wrap: anywhere; }
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
