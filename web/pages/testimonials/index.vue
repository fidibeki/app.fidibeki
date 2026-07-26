<script setup lang="ts">
import {
  AlertTriangle, ArrowDown, ArrowUp, Check, Copy, GripVertical, LayoutGrid, MessageSquareQuote,
  Plus, Sparkles, Trash2, X,
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
const tabs: Tab[] = ['suggested', 'library', 'walls']

// The tab lives in the URL so a refresh lands where you were and a colleague can
// be sent straight to the queue being discussed.
const route = useRoute()
const router = useRouter()
const tab = computed<Tab>({
  get: () => tabs.find(value => value === route.query.tab) || 'suggested',
  set: (value) => {
    // Replace, not push: flicking between queues is not navigation history.
    router.replace({ query: { ...route.query, tab: value === 'suggested' ? undefined : value } })
  },
})
const mutationError = ref('')
const notice = ref('')
const busyID = ref('')
const copiedID = ref('')

const requestFor = ref<TestimonialCandidate | null>(null)
const inviting = ref(false)
const requestEmail = ref('')
const requestPrompt = ref('')
const requestLink = ref('')

const newWallName = ref('')
// The wall being edited, and the ordered selection inside it. Membership is
// explicit: the platform ranks candidates, but which testimonials go public and
// in what order is the customer's decision, never inferred.
const editingWall = ref<TestimonialWall | null>(null)
const selection = ref<string[]>([])
// What the wall was last saved as, so the editor can say whether there is
// anything to save rather than offering the button unconditionally.
const savedSelection = ref<string[]>([])
const unsaved = computed(() => selection.value.join() !== savedSelection.value.join())

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
const tabCounts = computed<Record<Tab, number>>(() => ({
  suggested: candidates.value.length,
  library: testimonials.value.length,
  walls: walls.value.length,
}))
/**
 * What the queue's pending action does, stated once rather than on every row —
 * and only while something is actually waiting for it, so it explains a button
 * that is on screen rather than a rule with nothing to apply to.
 */
const tabHint = computed(() => {
  if (tab.value === 'suggested' && candidates.value.length) return t('testimonials.requestHint')
  if (tab.value === 'library' && testimonials.value.some(item => item.status === 'pending_review')) {
    return t('testimonials.approveHint')
  }
  return ''
})
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

function resetRequest() {
  requestEmail.value = ''
  requestPrompt.value = ''
  requestLink.value = ''
  mutationError.value = ''
}

function openRequest(candidate: TestimonialCandidate) {
  inviting.value = false
  requestFor.value = candidate
  resetRequest()
}

// Asking someone who never left feedback. There is no record to draw an address
// from, so this is the one path where the customer supplies one.
function openInvite() {
  requestFor.value = null
  inviting.value = true
  resetRequest()
}

function closeRequest() {
  requestFor.value = null
  inviting.value = false
}

const requestOpen = computed(() => Boolean(requestFor.value) || inviting.value)
const canSubmitRequest = computed(() => !busyID.value &&
  (!inviting.value || requestEmail.value.includes('@')))

async function submitRequest() {
  const candidate = requestFor.value
  if (!requestOpen.value || !canSubmitRequest.value) return
  busyID.value = candidate?.feedback_id || 'invite'
  mutationError.value = ''
  try {
    // Two endpoints, because they rest on different justifications: one reaches
    // the reporter at the address they left, the other reaches a contact only
    // the customer has. Neither publishes anything without the author's consent.
    const created = candidate
      ? await $apiFetch<{ token: string }>(
        `/v1/testimonial-candidates/${candidate.feedback_id}/request`,
        { method: 'POST', body: { prompt: requestPrompt.value || null } },
      )
      : await $apiFetch<{ token: string }>('/v1/testimonial-requests', {
        method: 'POST',
        body: { recipient_email: requestEmail.value.trim(), prompt: requestPrompt.value || null },
      })
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

// Dismissing is permanent: the scoring sweep deliberately leaves dismissed_at
// alone on re-score, and there is no endpoint to undo it. An action with no way
// back asks first.
const dismissFor = ref<TestimonialCandidate | null>(null)

async function confirmDismiss() {
  const candidate = dismissFor.value
  if (!candidate) return
  await run(candidate.feedback_id, async () => {
    await $apiFetch(`/v1/testimonial-candidates/${candidate.feedback_id}/dismiss`, { method: 'POST' })
  }, 'testimonials.dismissFailed')
  // run() reports failure through mutationError, so the dialog stays open with
  // the reason rather than closing as though it had worked.
  if (!mutationError.value) dismissFor.value = null
}

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
    savedSelection.value = [...selection.value]
  } catch (failure) {
    mutationError.value = apiErrorMessage(failure, 'testimonials.wallLoadFailed')
    selection.value = []
    savedSelection.value = []
  }
}

function add(item: Testimonial) {
  if (!selection.value.includes(item.id)) selection.value = [...selection.value, item.id]
}

function remove(id: string) {
  selection.value = selection.value.filter(value => value !== id)
}

function move(id: string, offset: number) {
  reorder(selection.value.indexOf(id), selection.value.indexOf(id) + offset)
}

function reorder(from: number, to: number) {
  if (from < 0 || to < 0 || from === to || to >= selection.value.length) return
  const reordered = [...selection.value]
  const [moved] = reordered.splice(from, 1)
  reordered.splice(to, 0, moved)
  selection.value = reordered
}

// Dragging is the obvious gesture for an ordered list, so the grip has to
// actually drag. The arrow buttons stay as the keyboard path — a pointer-only
// reorder would lock out anyone not using one.
const draggingID = ref('')
const dropTargetID = ref('')

function startDrag(id: string, event: DragEvent) {
  draggingID.value = id
  if (!event.dataTransfer) return
  event.dataTransfer.effectAllowed = 'move'
  // Firefox refuses to start a drag without payload, even one nothing reads.
  event.dataTransfer.setData('text/plain', id)
}

function dragOver(id: string) {
  if (draggingID.value && id !== draggingID.value) dropTargetID.value = id
}

function drop(id: string) {
  reorder(selection.value.indexOf(draggingID.value), selection.value.indexOf(id))
  endDrag()
}

function endDrag() {
  draggingID.value = ''
  dropTargetID.value = ''
}

const saveSelection = () => {
  const wall = editingWall.value
  if (!wall) return Promise.resolve()
  return run(wall.id, async () => {
    await $apiFetch(`/v1/testimonial-walls/${wall.id}/items`, {
      method: 'PUT',
      body: { items: selection.value.map((id, position) => ({ testimonial_id: id, position, pinned: false })) },
    })
    savedSelection.value = [...selection.value]
    notice.value = t('testimonials.selectionSaved')
  }, 'testimonials.selectionFailed')
}

// Deleting a published wall takes it off the customer's site, so the dialog says
// which of the two things is about to happen rather than one warning for both.
const deleteFor = ref<TestimonialWall | null>(null)

async function confirmDeleteWall() {
  const wall = deleteFor.value
  if (!wall) return
  await run(wall.id, async () => {
    await $apiFetch(`/v1/testimonial-walls/${wall.id}`, { method: 'DELETE' })
    if (editingWall.value?.id === wall.id) editingWall.value = null
    notice.value = t('testimonials.wallDeleted', { name: wall.name })
  }, 'testimonials.wallDeleteFailed')
  if (!mutationError.value) deleteFor.value = null
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
      <!-- Not everyone worth quoting left feedback first. -->
      <button
        v-if="canWrite('testimonial:create')" type="button" class="app-button app-button--primary"
        @click="openInvite"
      >
        <Plus :size="13" /> {{ t('testimonials.inviteAction') }}
      </button>
    </header>

    <nav class="testimonial-tabs" :aria-label="t('testimonials.tabsLabel')">
      <button
        v-for="value in tabs" :key="value"
        type="button" class="filter-chip" :aria-pressed="tab === value" @click="tab = value"
      >
        {{ t(`testimonials.tab${value.charAt(0).toUpperCase()}${value.slice(1)}`) }}
        <!-- The size of each queue is the reason to open it, so it does not
             require opening it to find out. -->
        <b v-if="tabCounts[value]">{{ tabCounts[value] }}</b>
      </button>
    </nav>

    <p v-if="mutationError" class="testimonial-alert" role="alert">{{ mutationError }}</p>
    <p v-else-if="notice" class="testimonial-notice" role="status">{{ notice }}</p>
    <!-- Said once for the whole queue. Repeating an invariant sentence on every
         row doubled the row height to say nothing new. -->
    <p v-if="tabHint" class="testimonial-lede">{{ tabHint }}</p>

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
      <section v-if="tab === 'suggested'" class="testimonial-list">
        <div v-if="!candidates.length" class="app-surface app-state">
          <div>
            <Sparkles :size="25" />
            <h2>{{ t('testimonials.suggestedEmptyTitle') }}</h2>
            <p>{{ t('testimonials.suggestedEmptyDescription') }}</p>
          </div>
        </div>
        <article
          v-for="candidate in candidates" :key="candidate.feedback_id"
          class="app-surface candidate-row"
        >
          <div class="candidate-main">
            <span class="candidate-score">
              <b>{{ Math.round(candidate.decayed_score * 100) }}</b>
              <small>{{ t('testimonials.score') }}</small>
            </span>
            <div class="candidate-body">
              <p>{{ candidate.suggested_excerpt || candidate.text }}</p>
              <div class="candidate-reasons">
                <RatingMark :rating="candidate.rating" stars compact />
                <!-- Stated on the row, because it decides whether this one can be
                     asked at all. -->
                <span v-if="!candidate.contact_permitted" class="tag tag--muted">
                  {{ t('testimonials.anonymousAuthor') }}
                </span>
                <span v-for="reason in candidate.reasons" :key="reason" class="tag">{{ reasonLabel(reason) }}</span>
              </div>
            </div>
          </div>
          <!-- Same shape as the library row: the quote keeps a full line to
               itself, and the actions sit on their own. -->
          <div v-if="canWrite('testimonial:create')" class="candidate-action">
            <button
              type="button" class="quiet-button" :disabled="busyID === candidate.feedback_id"
              @click="dismissFor = candidate"
            >
              <X :size="13" /> {{ t('testimonials.dismissAction') }}
            </button>
            <button
              type="button" class="app-button app-button--primary"
              :disabled="busyID === candidate.feedback_id || !candidate.contact_permitted"
              :title="candidate.contact_permitted ? undefined : t('testimonials.anonymousCannotAsk')"
              @click="openRequest(candidate)"
            >
              {{ t('testimonials.requestAction') }}
            </button>
          </div>
        </article>
      </section>

      <section v-else-if="tab === 'library'" class="testimonial-list">
        <div v-if="!testimonials.length" class="app-surface app-state">
          <div>
            <MessageSquareQuote :size="25" />
            <h2>{{ t('testimonials.libraryEmptyTitle') }}</h2>
            <p>{{ t('testimonials.libraryEmptyDescription') }}</p>
          </div>
        </div>
        <article v-for="item in testimonials" :key="item.id" class="app-surface library-row">
          <div class="library-main">
            <RatingMark :rating="item.rating" stars />
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
              <button
                v-if="canWrite('testimonial:publish')" type="button" class="icon-button"
                :disabled="busyID === wall.id" :aria-label="t('testimonials.wallDelete')"
                :title="t('testimonials.wallDelete')" @click="deleteFor = wall"
              >
                <Trash2 :size="14" />
              </button>
            </div>
          </header>

          <div v-if="editingWall?.id === wall.id" class="wall-editor">
            <div class="wall-column">
              <p class="section-label">
                {{ t('testimonials.onThisWall') }}
                <span>{{ t('testimonials.selectionCount', { count: selected.length, max: wall.max_items }) }}</span>
              </p>
              <p v-if="!selected.length" class="wall-hint">{{ t('testimonials.selectionEmpty') }}</p>
              <template v-else>
                <p class="wall-hint">{{ t('testimonials.reorderHint') }}</p>
                <ol class="wall-selection">
                  <li
                    v-for="(item, index) in selected" :key="item.id" draggable="true"
                    :class="{
                      'is-dragging': draggingID === item.id,
                      'is-target': dropTargetID === item.id,
                      'is-overflow': index >= wall.max_items,
                    }"
                    @dragstart="startDrag(item.id, $event)" @dragover.prevent="dragOver(item.id)"
                    @drop.prevent="drop(item.id)" @dragend="endDrag"
                  >
                    <span class="wall-handle">
                      <GripVertical :size="12" aria-hidden="true" />
                      <b>{{ index + 1 }}</b>
                    </span>
                    <div class="wall-item">
                      <p>{{ item.quote }}</p>
                      <small v-if="authorLine(item)">{{ authorLine(item) }}</small>
                    </div>
                    <span class="wall-order">
                      <button
                        type="button" class="icon-button" :disabled="index === 0"
                        :aria-label="t('testimonials.moveUp')" :title="t('testimonials.moveUp')"
                        @click="move(item.id, -1)"
                      ><ArrowUp :size="13" /></button>
                      <button
                        type="button" class="icon-button" :disabled="index === selected.length - 1"
                        :aria-label="t('testimonials.moveDown')" :title="t('testimonials.moveDown')"
                        @click="move(item.id, 1)"
                      ><ArrowDown :size="13" /></button>
                      <button
                        type="button" class="icon-button"
                        :aria-label="t('testimonials.removeFromWall')"
                        :title="t('testimonials.removeFromWall')" @click="remove(item.id)"
                      ><X :size="13" /></button>
                    </span>
                  </li>
                </ol>
                <!-- The wall has a cap, and the snapshot builder simply stops at
                     it. Say so here rather than let the customer discover it as
                     a missing card on their own site. -->
                <p v-if="selected.length > wall.max_items" class="wall-warning">
                  {{ t('testimonials.overWallLimit', { max: wall.max_items }) }}
                </p>
              </template>
            </div>
            <div class="wall-column">
              <p class="section-label">{{ t('testimonials.availableToAdd') }}</p>
              <p v-if="!unselected.length" class="wall-hint">{{ t('testimonials.nothingToAdd') }}</p>
              <ul v-else class="wall-available">
                <li v-for="item in unselected" :key="item.id">
                  <div class="wall-item">
                    <p>{{ item.quote }}</p>
                    <small v-if="authorLine(item)">{{ authorLine(item) }}</small>
                  </div>
                  <button
                    type="button" class="secondary-button"
                    :aria-label="t('testimonials.addToWall')" @click="add(item)"
                  >
                    <Plus :size="13" /> {{ t('testimonials.addToWall') }}
                  </button>
                </li>
              </ul>
            </div>
            <footer class="wall-editor-footer">
              <p v-if="unsaved">{{ t('testimonials.unsavedSelection') }}</p>
              <button
                type="button" class="app-button app-button--primary"
                :disabled="busyID === wall.id || !unsaved" @click="saveSelection"
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
      :open="requestOpen" aria-labelledby="request-testimonial-title"
      @close="closeRequest"
    >
      <section class="app-modal">
        <header>
          <div>
            <p class="app-eyebrow">{{ t('testimonials.eyebrow') }}</p>
            <h2 id="request-testimonial-title">
              {{ inviting ? t('testimonials.inviteTitle') : t('testimonials.requestTitle') }}
            </h2>
          </div>
          <button class="app-icon-button" type="button" @click="closeRequest"><X :size="18" /></button>
        </header>
        <div class="app-modal__body">
          <p class="request-help">
            {{ inviting ? t('testimonials.inviteDescription') : t('testimonials.requestDescription') }}
          </p>
          <!-- For a candidate the address is the one the reporter left when they
               asked to be contacted. It is shown, not typed: the customer never
               held it, so offering a field would be offering them to invent one. -->
          <div v-if="!inviting" class="request-recipient">
            <p class="section-label">{{ t('testimonials.requestEmailLabel') }}</p>
            <b>{{ requestFor?.reporter_email }}</b>
            <span>{{ t('testimonials.requestRecipientNote') }}</span>
          </div>
          <label v-else>
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
          <button type="button" class="app-button" @click="closeRequest">
            {{ requestLink ? t('testimonials.requestDone') : t('testimonials.requestCancel') }}
          </button>
          <button
            v-if="!requestLink" type="button" class="app-button app-button--primary"
            :disabled="!canSubmitRequest" @click="submitRequest"
          >
            {{ t('testimonials.requestSubmit') }}
          </button>
        </footer>
      </section>
    </AppDialog>

    <AppConfirmationDialog
      :open="Boolean(deleteFor)"
      :title="t('testimonials.wallDeleteTitle', { name: deleteFor?.name })"
      :description="deleteFor?.status === 'published'
        ? t('testimonials.wallDeletePublished')
        : t('testimonials.wallDeleteDraft')"
      :confirm-label="t('testimonials.wallDelete')"
      :pending-label="t('testimonials.wallDeletePending')"
      :cancel-label="t('testimonials.dismissKeep')"
      :pending="Boolean(deleteFor) && busyID === deleteFor?.id"
      :error="deleteFor ? mutationError : ''"
      @cancel="deleteFor = null"
      @confirm="confirmDeleteWall"
    />

    <AppConfirmationDialog
      :open="Boolean(dismissFor)"
      :title="t('testimonials.dismissTitle')"
      :description="t('testimonials.dismissDescription')"
      :confirm-label="t('testimonials.dismissAction')"
      :pending-label="t('testimonials.dismissPending')"
      :cancel-label="t('testimonials.dismissKeep')"
      :pending="Boolean(dismissFor) && busyID === dismissFor?.feedback_id"
      :error="dismissFor ? mutationError : ''"
      @cancel="dismissFor = null"
      @confirm="confirmDismiss"
    />
  </div>
</template>

<style scoped>
.testimonial-tabs { display: flex; gap: 6px; margin-bottom: 12px; }
.testimonial-tabs button { display: inline-flex; align-items: center; gap: 6px; }
.testimonial-tabs button[aria-pressed='true'] { background: var(--rail-active); border-color: var(--teal-line); color: var(--ink); }
.testimonial-tabs b { min-width: 15px; padding: 0 4px; background: var(--surface-soft); border-radius: 7px; color: var(--muted); font-size: 9px; font-variant-numeric: tabular-nums; line-height: 15px; text-align: center; }
.testimonial-tabs button[aria-pressed='true'] b { background: var(--surface); color: var(--ink); }
.testimonial-lede { margin: 0 0 10px; color: var(--muted); font-size: 10px; line-height: 1.5; }
.testimonial-alert,
.testimonial-notice { margin: 0 0 12px; padding: 9px 12px; border-radius: 5px; font-size: 10px; }
.testimonial-alert { background: var(--red-soft); border: 1px solid var(--red-line); color: var(--red); }
.testimonial-notice { background: var(--green-soft); border: 1px solid var(--green-line); color: var(--green); }
.testimonial-list { display: grid; gap: 12px; }
/* Both lists share one shape: the quote gets a full line, and whatever can be
   done to it sits on a second line that never competes with it for width. Each
   item is its own card, so an action bar can only read as belonging to the
   quote above it. */
.candidate-row,
.library-row { overflow: hidden; }
.candidate-row:hover,
.library-row:hover { background: var(--surface-subtle); }
.candidate-main,
.library-main { display: grid; min-height: 58px; align-items: center; gap: 14px; padding: 12px 16px; }
.candidate-main { grid-template-columns: 46px minmax(0, 1fr); }
.library-main { grid-template-columns: 74px minmax(0, 1fr) 190px; }
.candidate-action,
.library-action { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 12px; padding: 8px 16px; background: var(--surface-soft); border-top: 1px solid var(--line); }
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
/* The stars sit level with the reason chips, with a little room after them so
   they read as their own thing rather than a fourth chip. */
.candidate-reasons .rating-stars { height: 20px; margin-right: 3px; }
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
.wall-editor { display: grid; gap: 18px; margin-top: 14px; padding-top: 12px; grid-template-columns: repeat(2, minmax(0, 1fr)); border-top: 1px solid var(--line); }
.wall-column { min-width: 0; }
.wall-column .section-label { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
.wall-column .section-label span { color: var(--muted); font-weight: 500; letter-spacing: 0; text-transform: none; }
.wall-hint { margin: 5px 0 0; color: var(--muted); font-size: 9px; line-height: 1.5; }
.wall-warning { margin: 7px 0 0; padding: 7px 9px; background: var(--amber-soft); border: 1px solid var(--amber-line); border-radius: 4px; color: var(--amber-ink); font-size: 9px; line-height: 1.5; }
.wall-selection,
.wall-available { display: grid; gap: 6px; margin: 7px 0 0; padding: 0; list-style: none; }
.wall-selection li,
.wall-available li { display: grid; align-items: center; gap: 9px; padding: 8px 10px; background: var(--surface-subtle); border: 1px solid var(--line); border-radius: 5px; }
.wall-selection li { grid-template-columns: auto minmax(0, 1fr) auto; cursor: grab; }
.wall-available li { grid-template-columns: minmax(0, 1fr) auto; }
/* The quote is how you tell one apart from another, so it wraps to two lines
   rather than being clipped to a word and an ellipsis. */
.wall-item { min-width: 0; }
.wall-item p { display: -webkit-box; overflow: hidden; margin: 0; color: var(--ink-soft); font-size: 10px; line-height: 1.45; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.wall-item small { display: block; margin-top: 3px; overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.wall-handle { display: flex; align-items: center; gap: 3px; color: var(--muted); }
.wall-handle b { min-width: 13px; font-size: 10px; font-weight: 750; font-variant-numeric: tabular-nums; text-align: right; }
.wall-selection li.is-dragging { opacity: 0.45; cursor: grabbing; }
.wall-selection li.is-target { border-color: var(--teal); box-shadow: 0 0 0 3px var(--accent-glow); }
/* Past the wall's cap the snapshot builder stops, so these are dimmed to show
   they are carried but not shown. */
.wall-selection li.is-overflow { opacity: 0.55; }
.wall-order { display: flex; gap: 2px; }
.wall-order .icon-button { width: 26px; height: 26px; }
.wall-available .secondary-button { display: inline-flex; align-items: center; gap: 4px; min-height: 28px; padding: 0 10px; font-size: 9px; white-space: nowrap; }
.wall-editor-footer { display: flex; align-items: center; justify-content: flex-end; gap: 12px; grid-column: 1 / -1; }
.wall-editor-footer p { margin: 0 auto 0 0; color: var(--amber-ink); font-size: 9px; font-weight: 700; }
.wall-editor-footer .app-button { min-height: 32px; font-size: 10px; }
.wall-embed { margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--line); }
.wall-embed .secondary-button { min-height: 30px; font-size: 9px; }
.wall-embed pre,
.request-link pre { margin: 7px 0; padding: 9px 10px; overflow-x: auto; background: var(--canvas); border: 1px solid var(--line); border-radius: 4px; font-size: 9px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
.request-help { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.55; text-transform: none; }
.request-recipient { display: grid; gap: 3px; padding: 9px 11px; background: var(--surface-soft); border: 1px solid var(--line); border-radius: 5px; }
.request-recipient b { overflow-wrap: anywhere; color: var(--ink); font-size: 11px; }
.request-recipient span { color: var(--muted); font-size: 9px; line-height: 1.5; }
.tag--muted { color: var(--muted); font-style: italic; }
.request-prompt { min-height: 76px; padding: 8px 10px; background: var(--surface); border: 1px solid var(--line-strong); border-radius: var(--radius); color: var(--ink); font: inherit; font-size: 11px; line-height: 1.5; resize: vertical; text-transform: none; }
.request-prompt:focus-visible { border-color: var(--teal); outline: 0; box-shadow: 0 0 0 3px var(--accent-glow); }
.request-link .secondary-button { min-height: 30px; font-size: 9px; }
@media (max-width: 850px) {
  .candidate-main,
  .library-main { grid-template-columns: minmax(0, 1fr); gap: 10px; }
  .library-main .rating-stars { justify-self: start; }
  .candidate-score { justify-items: start; text-align: left; }
  .candidate-action,
  .library-action { justify-content: flex-start; }
  .wall-create { align-items: stretch; flex-direction: column; }
  .wall-head { align-items: flex-start; flex-direction: column; }
}
</style>
