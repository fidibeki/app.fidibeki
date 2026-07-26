<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Quote } from '@lucide/vue'
import { extractAPIError } from '~/utils/safe-api-error'

definePageMeta({ layout: false })

interface RevokeView {
  quote: string
  revoked: boolean
}

const route = useRoute()
const { $apiFetch } = useNuxtApp()
const { t } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const token = computed(() => String(route.params.token || ''))

const reason = ref('')
const sending = ref(false)
const submitError = ref('')
const done = ref(false)
const confirmation = ref<HTMLElement | null>(null)

const { data, status, error } = await useAsyncData(
  () => `testimonial-revoke:${token.value}`,
  () => $apiFetch<{ testimonial: RevokeView }>(
    `/v1/public/testimonial-revocations/${encodeURIComponent(token.value)}`,
  ),
  { watch: [token], server: false },
)

const testimonial = computed(() => data.value?.testimonial)
const isUnusable = computed(() => extractAPIError(error.value)?.code === 'not_found')
const alreadyRevoked = computed(() => testimonial.value?.revoked || done.value)

async function revoke() {
  if (sending.value) return
  sending.value = true
  submitError.value = ''
  try {
    await $apiFetch(`/v1/public/testimonial-revocations/${encodeURIComponent(token.value)}`, {
      method: 'POST',
      body: { reason: reason.value.trim() },
    })
    done.value = true
    await nextTick()
    confirmation.value?.focus()
  } catch (failure) {
    submitError.value = apiErrorMessage(failure, 'revokeTestimonial.failed')
  } finally {
    sending.value = false
  }
}

useHead(() => ({
  title: t('revokeTestimonial.pageTitle'),
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
          <div><span class="app-spinner" /><h1>{{ t('revokeTestimonial.loading') }}</h1></div>
        </section>

        <section v-else-if="isUnusable" class="app-surface app-state" role="alert">
          <div>
            <AlertTriangle :size="24" />
            <h1>{{ t('revokeTestimonial.unusableTitle') }}</h1>
            <p>{{ t('revokeTestimonial.unusableDescription') }}</p>
          </div>
        </section>

        <section v-else-if="error || !testimonial" class="app-surface app-state" role="alert">
          <div>
            <AlertTriangle :size="24" />
            <h1>{{ t('revokeTestimonial.errorTitle') }}</h1>
            <p>{{ apiErrorMessage(error, 'revokeTestimonial.errorDescription') }}</p>
          </div>
        </section>

        <!-- Returning to an old link months later lands here, not on an error.
             Being told it is already withdrawn is the reassurance they came for. -->
        <section v-else-if="alreadyRevoked" ref="confirmation" class="app-surface collect-done" tabindex="-1" role="status">
          <CheckCircle2 :size="26" />
          <h1>{{ t('revokeTestimonial.doneTitle') }}</h1>
          <p>{{ t('revokeTestimonial.doneDescription') }}</p>
        </section>

        <!-- One decision, so the page is shaped like one: their words, then the
             act, and nothing competing with either. -->
        <section v-else class="app-surface revoke-card">
          <p class="app-eyebrow"><Quote :size="13" /> {{ t('revokeTestimonial.eyebrow') }}</p>
          <h1>{{ t('revokeTestimonial.title') }}</h1>

          <!-- Shown before the decision, so nobody withdraws the wrong thing. -->
          <blockquote class="revoke-quote">{{ testimonial.quote }}</blockquote>
          <p class="revoke-lede">{{ t('revokeTestimonial.subtitle') }}</p>

          <!-- Optional, so it stays out of the way until it is wanted. -->
          <details class="revoke-reason">
            <summary>{{ t('revokeTestimonial.reasonLabel') }}</summary>
            <textarea v-model="reason" maxlength="2000" :aria-label="t('revokeTestimonial.reasonLabel')" />
            <span>{{ t('revokeTestimonial.reasonHint') }}</span>
          </details>

          <p v-if="submitError" class="revoke-error" role="alert">{{ submitError }}</p>

          <footer class="revoke-actions">
            <p>{{ t('revokeTestimonial.cacheNotice') }}</p>
            <button type="button" class="danger-button" :disabled="sending" @click="revoke">
              {{ sending ? t('revokeTestimonial.sending') : t('revokeTestimonial.submit') }}
            </button>
          </footer>
        </section>
      </div>
    </main>

    <footer class="collect-footer">{{ t('collectTestimonial.footer') }}</footer>
  </div>
</template>

<style scoped>
.collect-page { display: flex; min-height: 100vh; flex-direction: column; }
.collect-topbar { position: sticky; top: 0; z-index: 10; display: flex; min-height: 56px; flex: 0 0 auto; align-items: center; justify-content: center; padding: 0 16px; background: var(--surface); border-bottom: 1px solid var(--line); }
.app-page { flex: 1 0 auto; }
/* Narrower than the collection form. That page is a form to fill in; this is a
   single decision, and a wide column makes it look like there is more to do. */
.collect-container { max-width: 460px; margin: 0 auto; }
.collect-container .app-state { min-height: 300px; }
.revoke-card { padding: 20px; }
.revoke-card .app-eyebrow { display: flex; align-items: center; gap: 5px; }
.revoke-card h1 { margin: 6px 0 0; font-size: 18px; line-height: 1.3; }
/* Their words carry the page, so they are set as a quotation rather than in the
   grey of a disabled field, which is what a filled box reads as. */
.revoke-quote { position: relative; margin: 16px 0 0; padding: 0 0 0 14px; border-left: 2px solid var(--teal); color: var(--ink); font-size: 13px; line-height: 1.6; }
.revoke-lede { margin: 12px 0 0; color: var(--muted); font-size: 10px; line-height: 1.6; }
.revoke-reason { margin-top: 16px; }
.revoke-reason summary { color: var(--muted); cursor: pointer; font-size: 10px; font-weight: 700; }
.revoke-reason summary:focus-visible { outline: 2px solid var(--teal); outline-offset: 2px; }
.revoke-reason textarea { display: block; width: 100%; min-height: 68px; margin-top: 8px; padding: 8px 10px; background: var(--surface); border: 1px solid var(--line-strong); border-radius: var(--radius); color: var(--ink); font: inherit; font-size: 11px; line-height: 1.55; resize: vertical; }
.revoke-reason textarea:focus-visible { border-color: var(--teal); outline: 0; box-shadow: 0 0 0 3px var(--accent-glow); }
.revoke-reason span { display: block; margin-top: 5px; color: var(--muted); font-size: 9px; line-height: 1.55; }
.revoke-error { margin: 12px 0 0; color: var(--red); font-size: 10px; }
/* The caveat sits with the button because it qualifies the button, not the page. */
.revoke-actions { display: grid; gap: 10px; margin-top: 18px; padding-top: 14px; border-top: 1px solid var(--line); }
.revoke-actions p { margin: 0; color: var(--muted); font-size: 9px; line-height: 1.6; }
.revoke-actions .danger-button { justify-self: end; min-height: 34px; font-size: 10px; }
.collect-done { display: grid; justify-items: center; gap: 8px; padding: 34px 20px; color: var(--green); text-align: center; }
.collect-done h1 { color: var(--ink); font-size: 18px; }
.collect-done p { max-width: 380px; color: var(--muted); font-size: 11px; line-height: 1.6; }
.collect-footer { flex: 0 0 auto; padding: 16px; color: var(--muted); font-size: 9px; text-align: center; border-top: 1px solid var(--line); }
@media (max-width: 600px) {
  .revoke-actions .danger-button { width: 100%; justify-self: stretch; }
}
</style>
