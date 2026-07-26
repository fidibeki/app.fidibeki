<script setup lang="ts">
import { AlertTriangle, Check, Copy, LayoutGrid, MessageSquareQuote, Sparkles, X } from '@lucide/vue'
import type {
  Testimonial, TestimonialCandidate, TestimonialPublishResult, TestimonialWall,
} from '~/types/api'

const { $apiFetch } = useNuxtApp()
const { canWrite } = useAuth()
const { t } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { plural } = useRegionalFormat()

type Tab = 'suggested' | 'library' | 'walls'
const tab = ref<Tab>('suggested')
const mutationError = ref('')
const notice = ref('')
const busyID = ref('')
const copiedID = ref('')

// The request dialog. The recipient address is typed by the customer rather
// than taken from the feedback record: that address was collected to send
// updates about the feedback, not to ask for a testimonial.
const requestFor = ref<TestimonialCandidate | null>(null)
const requestEmail = ref('')
const requestPrompt = ref('')
const requestLink = ref('')

const newWallName = ref('')
const newWallSlug = ref('')

const { data, status, error, refresh } = await useAsyncData('testimonials', async () => {
  const [candidates, testimonials, walls] = await Promise.all([
    $apiFetch<{ candidates: TestimonialCandidate[] }>('/v1/testimonial-candidates'),
    $apiFetch<{ testimonials: Testimonial[] }>('/v1/testimonials'),
    $apiFetch<{ walls: TestimonialWall[] }>('/v1/testimonial-walls'),
  ])
  return {
    candidates: candidates.candidates,
    testimonials: testimonials.testimonials,
    walls: walls.walls,
  }
})

const candidates = computed(() => data.value?.candidates || [])
const testimonials = computed(() => data.value?.testimonials || [])
const walls = computed(() => data.value?.walls || [])

/** Reason keys come from the scoring engine so the score is never a bare number. */
function reasonLabel(key: string) {
  const short = key.replace('testimonials.reason.', '')
  return t(`testimonials.reason.${short}`)
}

function consentLabel(state: Testimonial['consent_state']) {
  const keys: Record<Testimonial['consent_state'], string> = {
    pending: 'testimonials.consentPending',
    granted: 'testimonials.consentGranted',
    pending_reconsent: 'testimonials.consentPendingReconsent',
    revoked: 'testimonials.consentRevoked',
    expired: 'testimonials.consentExpired',
  }
  return t(keys[state])
}

/** Maps onto the existing status colours rather than introducing new ones. */
function consentPillClass(state: Testimonial['consent_state']) {
  if (state === 'granted') return 'status-ready'
  if (state === 'revoked' || state === 'expired') return 'status-rejected'
  return 'status-pending'
}

function embedSnippet(wall: TestimonialWall) {
  const host = window.location.origin.replace('//app.', '//widget.')
  return `<script async src="${host}/v1/testimonials.js"><\/script>\n<fidibeki-wall wall="${wall.public_id}"><\/fidibeki-wall>`
}

function openRequest(candidate: TestimonialCandidate) {
  requestFor.value = candidate
  requestEmail.value = ''
  requestPrompt.value = ''
  requestLink.value = ''
  mutationError.value = ''
}

async function submitRequest() {
  const candidate = requestFor.value
  if (!candidate || busyID.value) return
  busyID.value = candidate.feedback_id
  mutationError.value = ''
  try {
    const created = await $apiFetch<{ token: string }>(
      `/v1/testimonial-candidates/${candidate.feedback_id}/request`,
      { method: 'POST', body: { recipient_email: requestEmail.value, prompt: requestPrompt.value || null } },
    )
    // Shown once. The plaintext token is never stored, so it cannot be recovered.
    requestLink.value = `${window.location.origin}/testimonials/collect/${created.token}`
    notice.value = t('testimonials.requestCreated')
  } catch (requestError) {
    mutationError.value = apiErrorMessage(requestError, 'testimonials.requestFailed')
  } finally {
    busyID.value = ''
  }
}

async function dismiss(candidate: TestimonialCandidate) {
  if (busyID.value) return
  busyID.value = candidate.feedback_id
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/testimonial-candidates/${candidate.feedback_id}/dismiss`, { method: 'POST' })
    await refresh()
  } catch (dismissError) {
    mutationError.value = apiErrorMessage(dismissError, 'testimonials.dismissFailed')
  } finally {
    busyID.value = ''
  }
}

async function approve(item: Testimonial) {
  if (busyID.value) return
  busyID.value = item.id
  mutationError.value = ''
  try {
    await $apiFetch(`/v1/testimonials/${item.id}/approve`, { method: 'POST' })
    await refresh()
  } catch (approveError) {
    mutationError.value = apiErrorMessage(approveError, 'testimonials.approveFailed')
  } finally {
    busyID.value = ''
  }
}

async function createWall() {
  if (busyID.value || !newWallName.value.trim()) return
  busyID.value = 'new-wall'
  mutationError.value = ''
  try {
    await $apiFetch('/v1/testimonial-walls', {
      method: 'POST',
      body: { name: newWallName.value.trim(), slug: newWallSlug.value.trim() || slugify(newWallName.value) },
    })
    newWallName.value = ''
    newWallSlug.value = ''
    await refresh()
  } catch (createError) {
    mutationError.value = apiErrorMessage(createError, 'testimonials.wallCreateFailed')
  } finally {
    busyID.value = ''
  }
}

async function publish(wall: TestimonialWall) {
  if (busyID.value) return
  busyID.value = wall.id
  mutationError.value = ''
  try {
    const result = await $apiFetch<TestimonialPublishResult>(
      `/v1/testimonial-walls/${wall.id}/publish`, { method: 'POST' },
    )
    // Skipped cards are surfaced, never silent: the customer should know a
    // testimonial did not go out and why they are one short.
    notice.value = result.skipped > 0
      ? t('testimonials.publishSkipped', { count: result.published, skipped: result.skipped })
      : t('testimonials.publishSucceeded', { count: result.published })
    await refresh()
  } catch (publishError) {
    mutationError.value = apiErrorMessage(publishError, 'testimonials.publishFailed')
  } finally {
    busyID.value = ''
  }
}

function slugify(value: string) {
  return value.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
}

async function copy(text: string, id: string) {
  try {
    await navigator.clipboard.writeText(text)
    copiedID.value = id
    setTimeout(() => { if (copiedID.value === id) copiedID.value = '' }, 2000)
  } catch {
    // Clipboard access can be refused; the value is selectable either way.
  }
}

useHead(() => ({ title: t('testimonials.pageTitle') }))
</script>

<template>
  <div class="app-page testimonials-page">
    <header class="app-page-head">
      <div>
        <p class="app-eyebrow">{{ t('testimonials.eyebrow') }}</p>
        <h1>{{ t('testimonials.title') }}</h1>
        <span>{{ t('testimonials.subtitle') }}</span>
      </div>
    </header>

    <nav class="testimonials-tabs" :aria-label="t('testimonials.tabsLabel')">
      <button
        v-for="value in (['suggested', 'library', 'walls'] as Tab[])" :key="value"
        type="button" class="filter-chip" :aria-pressed="tab === value" @click="tab = value"
      >
        {{ t(`testimonials.tab${value.charAt(0).toUpperCase()}${value.slice(1)}`) }}
      </button>
    </nav>

    <p v-if="mutationError" class="testimonials-alert" role="alert">{{ mutationError }}</p>
    <p v-else-if="notice" class="testimonials-notice" role="status">{{ notice }}</p>

    <section v-if="status === 'pending'" class="app-surface app-state" aria-live="polite">
      <div><span class="app-spinner" /><h2>{{ t('testimonials.loading') }}</h2></div>
    </section>
    <section v-else-if="error" class="app-surface app-state" role="alert">
      <div>
        <AlertTriangle :size="24" />
        <h2>{{ t('testimonials.unavailable') }}</h2>
        <p>{{ apiErrorMessage(error, 'testimonials.loadFailed') }}</p>
        <button class="app-button" type="button" @click="refresh()">{{ t('testimonials.tryAgain') }}</button>
      </div>
    </section>

    <template v-else>
      <!-- Suggested: the screen that has to feel smart, so the score always
           carries the reasons that produced it. -->
      <section v-if="tab === 'suggested'">
        <div v-if="!candidates.length" class="app-surface app-state">
          <div>
            <Sparkles :size="25" />
            <h2>{{ t('testimonials.suggestedEmptyTitle') }}</h2>
            <p>{{ t('testimonials.suggestedEmptyDescription') }}</p>
          </div>
        </div>
        <ul v-else class="candidate-list">
          <li v-for="candidate in candidates" :key="candidate.feedback_id" class="app-surface candidate">
            <div class="candidate-score">
              <span class="metric-value">{{ Math.round(candidate.decayed_score * 100) }}</span>
              <span class="section-label">{{ t('testimonials.score') }}</span>
            </div>
            <div class="candidate-body">
              <blockquote>{{ candidate.suggested_excerpt || candidate.text }}</blockquote>
              <div class="candidate-reasons">
                <span v-for="reason in candidate.reasons" :key="reason" class="tag">{{ reasonLabel(reason) }}</span>
              </div>
            </div>
            <div class="candidate-actions">
              <button
                v-if="canWrite('testimonial:create')" type="button" class="app-button app-button--primary"
                :disabled="busyID === candidate.feedback_id" @click="openRequest(candidate)"
              >
                {{ t('testimonials.requestAction') }}
              </button>
              <button
                v-if="canWrite('testimonial:create')" type="button" class="quiet-button"
                :disabled="busyID === candidate.feedback_id" @click="dismiss(candidate)"
              >
                <X :size="14" /> {{ t('testimonials.dismissAction') }}
              </button>
            </div>
          </li>
        </ul>
      </section>

      <section v-else-if="tab === 'library'">
        <div v-if="!testimonials.length" class="app-surface app-state">
          <div>
            <MessageSquareQuote :size="25" />
            <h2>{{ t('testimonials.libraryEmptyTitle') }}</h2>
            <p>{{ t('testimonials.libraryEmptyDescription') }}</p>
          </div>
        </div>
        <ul v-else class="candidate-list">
          <li v-for="item in testimonials" :key="item.id" class="app-surface candidate">
            <div class="candidate-body">
              <blockquote>{{ item.quote }}</blockquote>
              <p v-if="item.author.name" class="candidate-author">
                {{ item.author.name }}<template v-if="item.author.role"> · {{ item.author.role }}</template>
              </p>
              <!-- An edit that drifted from the consented wording is stated
                   plainly, not left for the customer to discover at publish. -->
              <p v-if="item.consent_state === 'pending_reconsent'" class="candidate-warning">
                {{ t('testimonials.quoteEdited') }}
              </p>
              <div class="candidate-reasons">
                <span class="status-pill" :class="`status-${item.status}`">{{ item.status }}</span>
                <span class="status-pill" :class="consentPillClass(item.consent_state)">
                  {{ consentLabel(item.consent_state) }}
                </span>
              </div>
            </div>
            <div class="candidate-actions">
              <button
                v-if="canWrite('testimonial:update') && item.status === 'pending_review'"
                type="button" class="app-button app-button--primary"
                :disabled="busyID === item.id" @click="approve(item)"
              >
                <Check :size="14" /> {{ t('testimonials.approve') }}
              </button>
            </div>
          </li>
        </ul>
      </section>

      <section v-else>
        <form v-if="canWrite('testimonial:create')" class="app-surface wall-form" @submit.prevent="createWall">
          <label>
            {{ t('testimonials.wallNameLabel') }}
            <input v-model="newWallName" class="field" maxlength="120" required />
          </label>
          <button type="submit" class="app-button app-button--primary" :disabled="busyID === 'new-wall'">
            {{ t('testimonials.wallCreate') }}
          </button>
        </form>

        <div v-if="!walls.length" class="app-surface app-state">
          <div>
            <LayoutGrid :size="25" />
            <h2>{{ t('testimonials.wallsEmptyTitle') }}</h2>
            <p>{{ t('testimonials.wallsEmptyDescription') }}</p>
          </div>
        </div>
        <ul v-else class="candidate-list">
          <li v-for="wall in walls" :key="wall.id" class="app-surface candidate">
            <div class="candidate-body">
              <h2 class="wall-name">{{ wall.name }}</h2>
              <div class="candidate-reasons">
                <span class="status-pill" :class="wall.status === 'published' ? 'status-ready' : 'status-pending'">
                  {{ wall.status === 'published' ? t('testimonials.published') : t('testimonials.draft') }}
                </span>
                <span class="tag">{{ plural('testimonials.wallItems', wall.item_count) }}</span>
              </div>
              <div v-if="wall.status === 'published'" class="wall-embed">
                <p class="section-label">{{ t('testimonials.embedTitle') }}</p>
                <p class="wall-embed-help">{{ t('testimonials.embedDescription') }}</p>
                <pre>{{ embedSnippet(wall) }}</pre>
                <button type="button" class="secondary-button" @click="copy(embedSnippet(wall), wall.id)">
                  <Copy :size="14" />
                  {{ copiedID === wall.id ? t('testimonials.copied') : t('testimonials.copyLink') }}
                </button>
              </div>
            </div>
            <div class="candidate-actions">
              <!-- Publishing is a separate permission from editing, because it
                   is the action that reaches the public internet. -->
              <button
                v-if="canWrite('testimonial:publish')" type="button" class="app-button app-button--primary"
                :disabled="busyID === wall.id" @click="publish(wall)"
              >
                {{ t('testimonials.publish') }}
              </button>
            </div>
          </li>
        </ul>
      </section>
    </template>

    <AppDialog
      :open="Boolean(requestFor)" aria-labelledby="request-testimonial-title"
      @close="requestFor = null"
    >
      <section class="app-modal">
        <header>
          <div>
            <p class="app-eyebrow">{{ t('testimonials.eyebrow') }}</p>
            <h2 id="request-testimonial-title">{{ t('testimonials.requestTitle') }}</h2>
          </div>
          <button class="app-icon-button" type="button" @click="requestFor = null"><X :size="18" /></button>
        </header>
        <div class="app-modal__body">
          <p class="request-help">{{ t('testimonials.requestDescription') }}</p>
          <label>
            {{ t('testimonials.requestEmailLabel') }}
            <input v-model.trim="requestEmail" class="app-input" type="email" required autofocus />
          </label>
          <label>
            {{ t('testimonials.requestPromptLabel') }}
            <textarea v-model="requestPrompt" class="request-prompt" maxlength="2000" />
          </label>
          <div v-if="requestLink" class="request-link">
            <p class="section-label">{{ t('testimonials.requestCreated') }}</p>
            <pre>{{ requestLink }}</pre>
            <button type="button" class="secondary-button" @click="copy(requestLink, 'request')">
              <Copy :size="14" />
              {{ copiedID === 'request' ? t('testimonials.copied') : t('testimonials.copyLink') }}
            </button>
          </div>
        </div>
        <footer>
          <button type="button" class="app-button" @click="requestFor = null">
            {{ t('testimonials.requestCancel') }}
          </button>
          <button
            v-if="!requestLink" type="button" class="app-button app-button--primary"
            :disabled="!requestEmail || Boolean(busyID)" @click="submitRequest"
          >
            {{ t('testimonials.requestSubmit') }}
          </button>
        </footer>
      </section>
    </AppDialog>
  </div>
</template>

<style scoped>
.testimonials-tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
}

.testimonials-tabs button[aria-pressed='true'] {
  background: var(--rail-active);
  border-color: var(--teal-line);
  color: var(--ink);
}

.testimonials-alert,
.testimonials-notice {
  margin: 0 0 14px;
  padding: 10px 14px;
  border-radius: var(--radius);
  font-size: 12px;
}

.testimonials-alert {
  background: var(--red-soft);
  border: 1px solid var(--red-line);
  color: var(--red);
}

.testimonials-notice {
  background: var(--green-soft);
  border: 1px solid var(--green-line);
  color: var(--green);
}

.candidate-list {
  display: grid;
  gap: 12px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.candidate {
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr) auto;
  gap: 16px;
  align-items: start;
  padding: 16px;
}

.candidate-score {
  display: grid;
  gap: 2px;
  justify-items: center;
  text-align: center;
}

.candidate-body {
  min-width: 0;
}

.candidate blockquote {
  margin: 0;
  overflow-wrap: anywhere;
  font-size: 14px;
  line-height: 1.55;
}

.candidate-author {
  margin: 8px 0 0;
  color: var(--muted);
  font-size: 12px;
}

.candidate-warning {
  margin: 8px 0 0;
  color: var(--amber-ink);
  font-size: 12px;
  font-weight: 650;
}

.candidate-reasons {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}

.candidate-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: stretch;
}

.wall-name {
  margin: 0;
  font-size: 15px;
}

.wall-form {
  display: flex;
  gap: 12px;
  align-items: flex-end;
  margin-bottom: 12px;
  padding: 16px;
}

.wall-form label {
  display: grid;
  flex: 1 1 auto;
  gap: 6px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.wall-embed {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}

.wall-embed-help {
  margin: 4px 0 8px;
  color: var(--muted);
  font-size: 11px;
}

.wall-embed pre,
.request-link pre {
  margin: 0 0 8px;
  padding: 10px;
  overflow-x: auto;
  background: var(--canvas);
  border: 1px solid var(--line);
  border-radius: 5px;
  font-size: 11px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.request-help {
  margin: 0;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.55;
}

.request-prompt {
  min-height: 90px;
  padding: 10px 12px;
  font: inherit;
  font-size: 12px;
  resize: vertical;
}

@media (max-width: 720px) {
  .candidate {
    grid-template-columns: minmax(0, 1fr);
  }

  .candidate-score {
    justify-items: start;
    text-align: left;
  }

  .wall-form {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
