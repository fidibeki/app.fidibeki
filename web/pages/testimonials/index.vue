<script setup lang="ts">
import {
  AlertTriangle, Check, Copy, GripVertical, LayoutGrid, MessageSquareQuote, Sparkles, X,
} from '@lucide/vue'
import type {
  Testimonial, TestimonialCandidate, TestimonialPublishResult, TestimonialWall,
} from '~/types/api'

const { $apiFetch } = useNuxtApp()
const { canWrite } = useAuth()
const { t } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { plural } = useRegionalFormat()
const runtime = useRuntimeConfig()

type Tab = 'suggested' | 'library' | 'walls'
const tab = ref<Tab>('suggested')
const mutationError = ref('')
const notice = ref('')
const busyID = ref('')
const copiedID = ref('')

const requestFor = ref<TestimonialCandidate | null>(null)
const requestEmail = ref('')
const requestPrompt = ref('')
const requestLink = ref('')

const newWallName = ref('')
// The wall being edited, and the ordered selection inside it. Membership is
// explicit: the platform ranks candidates, but which testimonials go public and
// in what order is the customer's decision, never inferred.
const editingWall = ref<TestimonialWall | null>(null)
const selection = ref<string[]>([])

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
/** Only an approved, consented testimonial can be put on a wall. */
const publishable = computed(() => testimonials.value.filter(item =>
  item.status === 'approved' && item.consent_state === 'granted'))
const selected = computed(() => selection.value
  .map(id => publishable.value.find(item => item.id === id))
  .filter((item): item is Testimonial => Boolean(item)))
const unselected = computed(() => publishable.value.filter(item => !selection.value.includes(item.id)))

function reasonLabel(key: string) {
  return t(`testimonials.reason.${key.replace('testimonials.reason.', '')}`)
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

/** New states map onto the existing status colours rather than adding any. */
function consentPillClass(state: Testimonial['consent_state']) {
  if (state === 'granted') return 'status-ready'
  if (state === 'revoked' || state === 'expired') return 'status-rejected'
  return 'status-pending'
}

function statusLabel(value: Testimonial['status']) {
  return t(`testimonials.status.${value}`)
}

function authorLine(item: Testimonial) {
  return [item.author.name, item.author.role, item.author.company].filter(Boolean).join(' · ')
}

/**
 * The embed host is configuration, not something to guess from the browser's
 * origin: the assets live on a different host from the app, and a wrong guess
 * hands the customer a script tag that resolves to nothing.
 */
function embedSnippet(wall: TestimonialWall) {
  const host = String(runtime.public.widgetUrl || '')
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
    // Shown once. The plaintext is never stored, so it cannot be recovered.
    requestLink.value = `${window.location.origin}/testimonials/collect/${created.token}`
  } catch (requestError) {
    mutationError.value = apiErrorMessage(requestError, 'testimonials.requestFailed')
  } finally {
    busyID.value = ''
  }
}

async function run(id: string, work: () => Promise<void>, fallbackKey: string) {
  if (busyID.value) return
  busyID.value = id
  mutationError.value = ''
  try {
    await work()
    await refresh()
  } catch (failure) {
    mutationError.value = apiErrorMessage(failure, fallbackKey)
  } finally {
    busyID.value = ''
  }
}

const dismiss = (candidate: TestimonialCandidate) => run(candidate.feedback_id, async () => {
  await $apiFetch(`/v1/testimonial-candidates/${candidate.feedback_id}/dismiss`, { method: 'POST' })
}, 'testimonials.dismissFailed')

const approve = (item: Testimonial) => run(item.id, async () => {
  await $apiFetch(`/v1/testimonials/${item.id}/approve`, { method: 'POST' })
}, 'testimonials.approveFailed')

const createWall = () => run('new-wall', async () => {
  const name = newWallName.value.trim()
  if (!name) return
  await $apiFetch('/v1/testimonial-walls', { method: 'POST', body: { name, slug: slugify(name) } })
  newWallName.value = ''
}, 'testimonials.wallCreateFailed')

async function openWall(wall: TestimonialWall) {
  editingWall.value = wall
  mutationError.value = ''
  try {
    const detail = await $apiFetch<{ cards: Array<{ id: string, position: number }> }>(
      `/v1/testimonial-walls/${wall.id}`,
    )
    selection.value = [...detail.cards]
      .sort((left, right) => left.position - right.position)
      .map(card => card.id)
  } catch (failure) {
    mutationError.value = apiErrorMessage(failure, 'testimonials.wallLoadFailed')
    selection.value = []
  }
}

function add(item: Testimonial) {
  if (!selection.value.includes(item.id)) selection.value = [...selection.value, item.id]
}

function remove(id: string) {
  selection.value = selection.value.filter(value => value !== id)
}

function move(id: string, offset: number) {
  const index = selection.value.indexOf(id)
  const next = index + offset
  if (index < 0 || next < 0 || next >= selection.value.length) return
  const reordered = [...selection.value]
  const [moved] = reordered.splice(index, 1)
  reordered.splice(next, 0, moved)
  selection.value = reordered
}

const saveSelection = () => {
  const wall = editingWall.value
  if (!wall) return Promise.resolve()
  return run(wall.id, async () => {
    await $apiFetch(`/v1/testimonial-walls/${wall.id}/items`, {
      method: 'PUT',
      body: { items: selection.value.map((id, position) => ({ testimonial_id: id, position, pinned: false })) },
    })
    notice.value = t('testimonials.selectionSaved')
  }, 'testimonials.selectionFailed')
}

const publish = (wall: TestimonialWall) => run(wall.id, async () => {
  const result = await $apiFetch<TestimonialPublishResult>(
    `/v1/testimonial-walls/${wall.id}/publish`, { method: 'POST' },
  )
  // Skipped cards are surfaced, never silent: a customer who is one testimonial
  // short should be told, not left to notice.
  notice.value = result.skipped > 0
    ? t('testimonials.publishSkipped', { count: result.published, skipped: result.skipped })
    : t('testimonials.publishSucceeded', { count: result.published })
}, 'testimonials.publishFailed')

function slugify(value: string) {
  return value.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'wall'
}

async function copy(text: string, id: string) {
  try {
    await navigator.clipboard.writeText(text)
    copiedID.value = id
    setTimeout(() => { if (copiedID.value === id) copiedID.value = '' }, 2000)
  } catch {
    // Clipboard access can be refused; the value stays selectable either way.
  }
}

useHead(() => ({ title: t('testimonials.pageTitle') }))
</script>

<template>
  <div class="app-page">
    <header class="app-page-head">
      <div>
        <p class="app-eyebrow">{{ t('testimonials.eyebrow') }}</p>
        <h1>{{ t('testimonials.title') }}</h1>
        <span>{{ t('testimonials.subtitle') }}</span>
      </div>
    </header>

    <nav class="testimonial-tabs" :aria-label="t('testimonials.tabsLabel')">
      <button
        v-for="value in (['suggested', 'library', 'walls'] as Tab[])" :key="value"
        type="button" class="filter-chip" :aria-pressed="tab === value"
        @click="tab = value; editingWall = null"
      >
        {{ t(`testimonials.tab${value.charAt(0).toUpperCase()}${value.slice(1)}`) }}
      </button>
    </nav>

    <p v-if="mutationError" class="testimonial-alert" role="alert">{{ mutationError }}</p>
    <p v-else-if="notice" class="testimonial-notice" role="status">{{ notice }}</p>

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
      <!-- Suggested: the screen that has to feel smart, so every score arrives
           with the reasons that produced it. -->
      <section v-if="tab === 'suggested'" class="app-surface testimonial-list">
        <div v-if="!candidates.length" class="app-state">
          <div>
            <Sparkles :size="25" />
            <h2>{{ t('testimonials.suggestedEmptyTitle') }}</h2>
            <p>{{ t('testimonials.suggestedEmptyDescription') }}</p>
          </div>
        </div>
        <article v-for="candidate in candidates" :key="candidate.feedback_id" class="candidate-row">
          <div class="candidate-main">
            <span class="candidate-score">
              <b>{{ Math.round(candidate.decayed_score * 100) }}</b>
              <small>{{ t('testimonials.score') }}</small>
            </span>
            <div class="candidate-body">
              <p>{{ candidate.suggested_excerpt || candidate.text }}</p>
              <div class="candidate-reasons">
                <RatingMark :rating="candidate.rating" compact />
                <span v-for="reason in candidate.reasons" :key="reason" class="tag">{{ reasonLabel(reason) }}</span>
              </div>
            </div>
          </div>
          <!-- Same shape as the library row: the quote keeps a full line to
               itself, and the actions carry what they actually do. -->
          <div v-if="canWrite('testimonial:create')" class="candidate-action">
            <p>{{ t('testimonials.requestHint') }}</p>
            <button
              type="button" class="quiet-button" :disabled="busyID === candidate.feedback_id"
              @click="dismiss(candidate)"
            >
              <X :size="13" /> {{ t('testimonials.dismissAction') }}
            </button>
            <button
              type="button" class="app-button app-button--primary"
              :disabled="busyID === candidate.feedback_id" @click="openRequest(candidate)"
            >
              {{ t('testimonials.requestAction') }}
            </button>
          </div>
        </article>
      </section>

      <section v-else-if="tab === 'library'" class="app-surface testimonial-list">
        <div v-if="!testimonials.length" class="app-state">
          <div>
            <MessageSquareQuote :size="25" />
            <h2>{{ t('testimonials.libraryEmptyTitle') }}</h2>
            <p>{{ t('testimonials.libraryEmptyDescription') }}</p>
          </div>
        </div>
        <article v-for="item in testimonials" :key="item.id" class="library-row">
          <div class="library-main">
            <RatingMark :rating="item.rating" compact />
            <div class="candidate-body">
              <p>{{ item.quote }}</p>
              <small v-if="authorLine(item)">{{ authorLine(item) }}</small>
              <!-- An edit that drifted from the consented wording is stated here,
                   not discovered when publishing fails. -->
              <small v-if="item.consent_state === 'pending_reconsent'" class="library-warning">
                {{ t('testimonials.quoteEdited') }}
              </small>
            </div>
            <div class="library-state">
              <span class="status-pill" :class="`status-${item.status}`">{{ statusLabel(item.status) }}</span>
              <span class="status-pill" :class="consentPillClass(item.consent_state)">
                {{ consentLabel(item.consent_state) }}
              </span>
            </div>
          </div>
          <!-- The action sits on its own row so the quote line keeps its shape,
               and it carries the reason it exists: nothing reaches a wall until
               someone here says so. -->
          <div
            v-if="canWrite('testimonial:update') && item.status === 'pending_review'"
            class="library-action"
          >
            <p>{{ t('testimonials.approveHint') }}</p>
            <button
              type="button" class="app-button app-button--primary"
              :disabled="busyID === item.id" @click="approve(item)"
            >
              <Check :size="13" /> {{ t('testimonials.approve') }}
            </button>
          </div>
        </article>
      </section>

      <section v-else>
        <form
          v-if="canWrite('testimonial:create')" class="app-surface wall-create"
          @submit.prevent="createWall"
        >
          <label>
            {{ t('testimonials.wallNameLabel') }}
            <input v-model="newWallName" class="app-input" maxlength="120" required />
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

        <section v-for="wall in walls" :key="wall.id" class="app-surface wall-card">
          <header class="wall-head">
            <div>
              <b>{{ wall.name }}</b>
              <span class="status-pill" :class="wall.status === 'published' ? 'status-ready' : 'status-pending'">
                {{ wall.status === 'published' ? t('testimonials.published') : t('testimonials.draft') }}
              </span>
              <span class="tag">{{ plural('testimonials.wallItems', wall.item_count) }}</span>
            </div>
            <div class="wall-actions">
              <button
                v-if="canWrite('testimonial:update')" type="button" class="secondary-button"
                @click="editingWall?.id === wall.id ? editingWall = null : openWall(wall)"
              >
                {{ editingWall?.id === wall.id ? t('testimonials.closeEditor') : t('testimonials.chooseItems') }}
              </button>
              <!-- Publishing is a separate grant from editing: it is the step
                   that reaches the public internet. -->
              <button
                v-if="canWrite('testimonial:publish')" type="button" class="app-button app-button--primary"
                :disabled="busyID === wall.id" @click="publish(wall)"
              >
                {{ t('testimonials.publish') }}
              </button>
            </div>
          </header>

          <div v-if="editingWall?.id === wall.id" class="wall-editor">
            <div class="wall-column">
              <p class="section-label">{{ t('testimonials.onThisWall') }}</p>
              <p v-if="!selected.length" class="wall-hint">{{ t('testimonials.selectionEmpty') }}</p>
              <ol v-else class="wall-selection">
                <li v-for="(item, index) in selected" :key="item.id">
                  <GripVertical :size="13" aria-hidden="true" />
                  <span>{{ item.quote }}</span>
                  <span class="wall-order">
                    <button
                      type="button" class="icon-button" :disabled="index === 0"
                      :title="t('testimonials.moveUp')" @click="move(item.id, -1)"
                    >↑</button>
                    <button
                      type="button" class="icon-button" :disabled="index === selected.length - 1"
                      :title="t('testimonials.moveDown')" @click="move(item.id, 1)"
                    >↓</button>
                    <button
                      type="button" class="icon-button" :title="t('testimonials.removeFromWall')"
                      @click="remove(item.id)"
                    ><X :size="13" /></button>
                  </span>
                </li>
              </ol>
            </div>
            <div class="wall-column">
              <p class="section-label">{{ t('testimonials.availableToAdd') }}</p>
              <p v-if="!unselected.length" class="wall-hint">{{ t('testimonials.nothingToAdd') }}</p>
              <ul v-else class="wall-available">
                <li v-for="item in unselected" :key="item.id">
                  <span>{{ item.quote }}</span>
                  <button type="button" class="secondary-button" @click="add(item)">
                    {{ t('testimonials.addToWall') }}
                  </button>
                </li>
              </ul>
            </div>
            <footer class="wall-editor-footer">
              <button
                type="button" class="app-button app-button--primary"
                :disabled="busyID === wall.id" @click="saveSelection"
              >
                {{ t('testimonials.saveSelection') }}
              </button>
            </footer>
          </div>

          <div v-if="wall.status === 'published'" class="wall-embed">
            <p class="section-label">{{ t('testimonials.embedTitle') }}</p>
            <p class="wall-hint">{{ t('testimonials.embedDescription') }}</p>
            <pre>{{ embedSnippet(wall) }}</pre>
            <button type="button" class="secondary-button" @click="copy(embedSnippet(wall), wall.id)">
              <Copy :size="13" />
              {{ copiedID === wall.id ? t('testimonials.copied') : t('testimonials.copyLink') }}
            </button>
          </div>
        </section>
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
              <Copy :size="13" />
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
.testimonial-tabs { display: flex; gap: 6px; margin-bottom: 12px; }
.testimonial-tabs button[aria-pressed='true'] { background: var(--rail-active); border-color: var(--teal-line); color: var(--ink); }
.testimonial-alert,
.testimonial-notice { margin: 0 0 12px; padding: 9px 12px; border-radius: 5px; font-size: 10px; }
.testimonial-alert { background: var(--red-soft); border: 1px solid var(--red-line); color: var(--red); }
.testimonial-notice { background: var(--green-soft); border: 1px solid var(--green-line); color: var(--green); }
.testimonial-list { overflow: hidden; }
/* Both lists share one shape: the quote gets a full line, and whatever can be
   done to it sits on a second line that never competes with it for width. */
.candidate-row,
.library-row { border-bottom: 1px solid var(--line); }
.candidate-row:last-child,
.library-row:last-child { border-bottom: 0; }
.candidate-row:hover,
.library-row:hover { background: var(--surface-subtle); }
.candidate-main,
.library-main { display: grid; min-height: 58px; align-items: center; gap: 14px; padding: 12px 16px; }
.candidate-main { grid-template-columns: 46px minmax(0, 1fr); }
.library-main { grid-template-columns: 46px minmax(0, 1fr) 190px; }
.candidate-action,
.library-action { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 12px; padding: 8px 16px; background: var(--surface-soft); border-top: 1px solid var(--line); }
.candidate-action p,
.library-action p { margin: 0 auto 0 0; color: var(--muted); font-size: 9px; }
.candidate-action .app-button,
.candidate-action .quiet-button,
.library-action .app-button { min-height: 30px; font-size: 10px; }
.candidate-score { display: grid; justify-items: center; gap: 1px; text-align: center; }
.candidate-score b { font-size: 15px; font-weight: 750; letter-spacing: 0; }
.candidate-score small { color: var(--muted); font-size: 8px; font-weight: 750; text-transform: uppercase; }
.candidate-body { min-width: 0; }
.candidate-body p { margin: 0; overflow-wrap: anywhere; color: var(--ink-soft); font-size: 11px; line-height: 1.5; }
.candidate-body small { display: block; margin-top: 4px; color: var(--muted); font-size: 9px; }
.library-warning { color: var(--amber-ink) !important; font-weight: 700; }
.candidate-reasons { display: flex; flex-wrap: wrap; align-items: center; gap: 5px; margin-top: 7px; }
.candidate-reasons .tag { min-height: 20px; font-size: 9px; }
/* The shared rating badge, sized down to sit level with the reason chips. */
.candidate-reasons .rating-mark { height: 20px; min-width: 30px; padding: 0 6px; font-size: 10px; line-height: 18px; }
.library-state { display: flex; flex-wrap: wrap; gap: 5px; }
.library-state .status-pill { min-height: 20px; font-size: 9px; }
.wall-create { display: flex; gap: 10px; align-items: flex-end; margin-bottom: 12px; padding: 12px 16px; }
.wall-create label { display: grid; flex: 1 1 auto; gap: 5px; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.wall-card { margin-bottom: 12px; padding: 14px 16px; }
.wall-head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; }
.wall-head > div { display: flex; flex-wrap: wrap; align-items: center; gap: 7px; }
.wall-head b { font-size: 12px; }
.wall-head .status-pill,
.wall-head .tag { min-height: 20px; font-size: 9px; }
.wall-actions { display: flex; gap: 7px; }
.wall-actions .app-button,
.wall-actions .secondary-button { min-height: 32px; font-size: 10px; }
.wall-editor { display: grid; gap: 14px; margin-top: 14px; padding-top: 12px; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); border-top: 1px solid var(--line); }
.wall-column { min-width: 0; }
.wall-hint { margin: 5px 0 0; color: var(--muted); font-size: 9px; line-height: 1.5; }
.wall-selection,
.wall-available { display: grid; gap: 5px; margin: 7px 0 0; padding: 0; list-style: none; }
.wall-selection li,
.wall-available li { display: grid; align-items: center; gap: 8px; padding: 7px 9px; background: var(--surface-subtle); border: 1px solid var(--line); border-radius: 4px; }
.wall-selection li { grid-template-columns: 14px minmax(0, 1fr) auto; }
.wall-available li { grid-template-columns: minmax(0, 1fr) auto; }
.wall-selection li > span,
.wall-available li > span { overflow: hidden; color: var(--ink-soft); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.wall-selection li > svg { color: var(--muted); }
.wall-order { display: flex; gap: 2px; }
.wall-order .icon-button { width: 24px; height: 24px; font-size: 10px; }
.wall-available .secondary-button { min-height: 26px; padding: 0 9px; font-size: 9px; }
.wall-editor-footer { display: flex; justify-content: flex-end; grid-column: 1 / -1; }
.wall-editor-footer .app-button { min-height: 32px; font-size: 10px; }
.wall-embed { margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--line); }
.wall-embed .secondary-button { min-height: 30px; font-size: 9px; }
.wall-embed pre,
.request-link pre { margin: 7px 0; padding: 9px 10px; overflow-x: auto; background: var(--canvas); border: 1px solid var(--line); border-radius: 4px; font-size: 9px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
.request-help { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.55; text-transform: none; }
.request-prompt { min-height: 76px; padding: 8px 10px; background: var(--surface); border: 1px solid var(--line-strong); border-radius: var(--radius); color: var(--ink); font: inherit; font-size: 11px; line-height: 1.5; resize: vertical; text-transform: none; }
.request-prompt:focus-visible { border-color: var(--teal); outline: 0; box-shadow: 0 0 0 3px var(--accent-glow); }
.request-link .secondary-button { min-height: 30px; font-size: 9px; }
@media (max-width: 850px) {
  .candidate-main,
  .library-main { grid-template-columns: minmax(0, 1fr); gap: 10px; }
  .library-main .rating-mark { justify-self: start; }
  .candidate-score { justify-items: start; text-align: left; }
  /* The hint would push the buttons off the row at this width, so it takes a
     line of its own rather than shrinking them. */
  .candidate-action p,
  .library-action p { flex: 1 0 100%; margin: 0; }
  .wall-create { align-items: stretch; flex-direction: column; }
  .wall-head { align-items: flex-start; flex-direction: column; }
}
</style>
