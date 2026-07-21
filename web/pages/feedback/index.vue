<script setup lang="ts">
import {
  AlertTriangle,
  Archive,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Inbox,
  Link2,
  LoaderCircle,
  PanelRightOpen,
  RotateCcw,
  Search,
  ShieldAlert,
} from '@lucide/vue'
import type { FeedbackItem, FeedbackPage, FeedbackStatus, Project } from '~/types/api'
import { apiErrorMessage } from '~/utils/api-fetch'

const route = useRoute()
const router = useRouter()
const { $apiFetch } = useNuxtApp()
const { can } = useAuth()

const query = ref(typeof route.query.q === 'string' ? route.query.q : '')
const project = ref(typeof route.query.project_id === 'string' ? route.query.project_id : '')
const feedbackStatus = ref(typeof route.query.status === 'string' ? route.query.status : 'open')
const rating = ref(typeof route.query.rating === 'string' ? route.query.rating : '')
const attachment = ref(typeof route.query.attachment === 'string' ? route.query.attachment : '')
const moderation = ref(typeof route.query.moderation === 'string' ? route.query.moderation : '')
const issue = ref(typeof route.query.issue === 'string' ? route.query.issue : '')
const processing = ref(typeof route.query.processing === 'string' ? route.query.processing : '')
const sentiment = ref(typeof route.query.sentiment === 'string' ? route.query.sentiment : '')
const dateFrom = ref(typeof route.query.date_from === 'string' ? route.query.date_from : '')
const dateTo = ref(typeof route.query.date_to === 'string' ? route.query.date_to : '')
const sort = ref(typeof route.query.sort === 'string' ? route.query.sort : 'newest')
const selectedId = ref(typeof route.query.id === 'string' ? route.query.id : '')
const selectedIds = ref<string[]>([])
const inspectorTab = ref<'feedback' | 'evidence' | 'replay'>('feedback')
const items = ref<FeedbackItem[]>([])
const nextCursor = ref<string | null>(null)
const hasMore = ref(false)
const loadingMore = ref(false)
const mutationPending = ref(false)
const notice = ref('')
const mutationError = ref('')

const filterQuery = computed(() => {
  const params = new URLSearchParams()
  if (query.value.trim()) params.set('q', query.value.trim())
  if (project.value) params.set('project_id', project.value)
  if (feedbackStatus.value) params.set('status', feedbackStatus.value)
  if (rating.value) params.set('rating', rating.value)
  if (attachment.value) params.set('attachment', attachment.value)
  if (moderation.value) params.set('moderation', moderation.value)
  if (issue.value) params.set('issue', issue.value)
  if (processing.value) params.set('processing', processing.value)
  if (sentiment.value) params.set('sentiment', sentiment.value)
  if (dateFrom.value) params.set('date_from', dateFrom.value)
  if (dateTo.value) params.set('date_to', dateTo.value)
  if (sort.value && sort.value !== 'newest') params.set('sort', sort.value)
  params.set('limit', '50')
  return params.toString()
})

const { data, status, error, refresh } = await useAsyncData(
  'feedback-queue',
  () => $apiFetch<FeedbackPage>(`/v1/feedbacks?${filterQuery.value}`),
  { watch: [filterQuery] },
)

const { data: projectData } = await useAsyncData(
  'feedback-project-options',
  () => can('project:read')
    ? $apiFetch<{ projects: Project[] }>('/v1/projects')
    : Promise.resolve({ projects: [] }),
)

watch(data, (page) => {
  items.value = page?.feedbacks || []
  nextCursor.value = page?.pagination.next_cursor || null
  hasMore.value = page?.pagination.has_more || false
  selectedIds.value = []
  if (!items.value.some((item) => item.id === selectedId.value)) {
    selectedId.value = items.value[0]?.id || ''
  }
}, { immediate: true })

watch(
  [query, project, feedbackStatus, rating, attachment, moderation, issue, processing, sentiment, dateFrom, dateTo, sort],
  () => {
    const params = Object.fromEntries(new URLSearchParams(filterQuery.value).entries())
    delete params.limit
    if (selectedId.value) params.id = selectedId.value
    router.replace({ query: params })
  },
)

watch(() => route.query.q, (value) => {
  const next = typeof value === 'string' ? value : ''
  if (next !== query.value) query.value = next
})

const selected = computed(() => items.value.find((item) => item.id === selectedId.value) || items.value[0])

function selectItem(item: FeedbackItem) {
  selectedId.value = item.id
  inspectorTab.value = 'feedback'
  router.replace({ query: { ...route.query, id: item.id } })
}

function moveSelection(direction: number) {
  const index = items.value.findIndex((item) => item.id === selected.value?.id)
  const next = items.value[Math.min(items.value.length - 1, Math.max(0, index + direction))]
  if (next) selectItem(next)
}

function toggleSelected(id: string) {
  selectedIds.value = selectedIds.value.includes(id)
    ? selectedIds.value.filter((value) => value !== id)
    : [...selectedIds.value, id]
}

async function loadMore() {
  if (!nextCursor.value || loadingMore.value) return
  loadingMore.value = true
  mutationError.value = ''
  try {
    const page = await $apiFetch<FeedbackPage>(`/v1/feedbacks?${filterQuery.value}&cursor=${encodeURIComponent(nextCursor.value)}`)
    items.value.push(...page.feedbacks)
    nextCursor.value = page.pagination.next_cursor || null
    hasMore.value = page.pagination.has_more
  } catch (loadError) {
    mutationError.value = apiErrorMessage(loadError, 'More feedback could not be loaded.')
  } finally {
    loadingMore.value = false
  }
}

async function updateStatus(ids: string[], nextStatus: FeedbackStatus) {
  if (!ids.length || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    await Promise.all(ids.map((id) => $apiFetch(`/v1/feedbacks/${id}`, {
      method: 'PATCH', body: { status: nextStatus },
    })))
    notice.value = `${ids.length} feedback item${ids.length === 1 ? '' : 's'} updated.`
    selectedIds.value = []
    await refresh()
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, 'Feedback could not be updated.')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

async function reprocess(ids: string[]) {
  if (!ids.length || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    await Promise.all(ids.map((id) => $apiFetch(`/v1/feedbacks/${id}/reprocess`, { method: 'POST' })))
    notice.value = `${ids.length} AI job${ids.length === 1 ? '' : 's'} queued.`
    selectedIds.value = []
    await refresh()
  } catch (reprocessError) {
    mutationError.value = apiErrorMessage(reprocessError, 'AI processing could not be queued.')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

function clearFilters() {
  query.value = ''
  project.value = ''
  feedbackStatus.value = ''
  rating.value = ''
  attachment.value = ''
  moderation.value = ''
  issue.value = ''
  processing.value = ''
  sentiment.value = ''
  dateFrom.value = ''
  dateTo.value = ''
  sort.value = 'newest'
}

function copy(item: FeedbackItem) {
  return item.text_cleaned || item.text || 'Feedback without written detail'
}

function formatAge(value: string) {
  const hours = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 3_600_000))
  return hours < 1 ? 'Now' : hours < 24 ? `${hours}h` : `${Math.floor(hours / 24)}d`
}

useHead({ title: 'Feedback · Fidibeki' })
</script>

<template>
  <div class="feedback-workspace">
    <section class="feedback-stream-panel">
      <header class="feedback-stream-head">
        <div><p class="app-eyebrow">Feedback queue</p><h1>Feedback</h1><span>{{ items.length }} loaded</span></div>
        <select v-model="sort" class="app-select" aria-label="Sort feedback">
          <option value="newest">Newest</option><option value="oldest">Oldest</option>
          <option value="rating_low">Lowest rating</option><option value="rating_high">Highest rating</option>
        </select>
      </header>

      <div class="feedback-filter-bar">
        <label class="feedback-search"><Search :size="15" /><input v-model.trim="query" placeholder="Search feedback" aria-label="Search feedback" /></label>
        <select v-model="project" aria-label="Project filter"><option value="">All projects</option><option v-for="item in projectData?.projects" :key="item.id" :value="item.id">{{ item.name }}</option></select>
        <select v-model="feedbackStatus" aria-label="Status filter"><option value="">All status</option><option value="open">Open</option><option value="archived">Archived</option><option value="spam">Spam</option></select>
        <select v-model="rating" aria-label="Rating filter"><option value="">Any rating</option><option v-for="value in 5" :key="value" :value="String(value)">{{ value }} star</option></select>
        <select v-model="attachment" aria-label="Evidence filter"><option value="">Any evidence</option><option value="none">No evidence</option><option value="screenshot">Screenshot</option><option value="audio">Audio</option><option value="video">Video</option><option value="replay">Replay</option></select>
        <details class="feedback-more-filters">
          <summary>More filters</summary>
          <div>
            <label>Moderation<select v-model="moderation"><option value="">Any</option><option value="flagged">Flagged</option><option value="clean">Clean</option></select></label>
            <label>Issue<select v-model="issue"><option value="">Any</option><option value="linked">Linked</option><option value="unlinked">Unlinked</option></select></label>
            <label>AI status<select v-model="processing"><option value="">Any</option><option value="failed">Failed</option><option value="processing">Processing</option><option value="pending">Pending</option><option value="completed">Completed</option><option value="skipped">Skipped</option></select></label>
            <label>Sentiment<select v-model="sentiment"><option value="">Any</option><option value="negative">Negative</option><option value="neutral">Neutral</option><option value="positive">Positive</option></select></label>
            <label>From<input v-model="dateFrom" type="date" /></label><label>To<input v-model="dateTo" type="date" /></label>
            <button type="button" @click="clearFilters">Clear filters</button>
          </div>
        </details>
      </div>

      <div v-if="selectedIds.length" class="feedback-bulk-bar">
        <b>{{ selectedIds.length }} selected</b>
        <button type="button" :disabled="mutationPending" @click="updateStatus(selectedIds, 'archived')"><Archive :size="14" /> Archive</button>
        <button type="button" :disabled="mutationPending || !can('feedback:update')" @click="reprocess(selectedIds)"><RotateCcw :size="14" /> Reprocess AI</button>
        <button class="danger" type="button" :disabled="mutationPending" @click="updateStatus(selectedIds, 'spam')"><ShieldAlert :size="14" /> Spam</button>
      </div>

      <p v-if="mutationError" class="feedback-inline-error" role="alert"><AlertTriangle :size="15" /> {{ mutationError }}</p>
      <div v-if="status === 'pending'" class="feedback-list-state"><span class="app-spinner" /><p>Loading feedback</p></div>
      <div v-else-if="error" class="feedback-list-state" role="alert"><AlertTriangle :size="22" /><p>{{ apiErrorMessage(error, 'Feedback could not be loaded.') }}</p><button class="app-button" @click="refresh()">Try again</button></div>
      <div v-else-if="items.length === 0" class="feedback-list-state"><Inbox :size="22" /><p>No feedback matches these filters.</p><button class="app-button" @click="clearFilters">Clear filters</button></div>
      <div v-else class="feedback-stream" role="list">
        <article v-for="item in items" :key="item.id" role="listitem" :data-selected="selected?.id === item.id" @click="selectItem(item)">
          <label class="feedback-row-check" @click.stop><input type="checkbox" :checked="selectedIds.includes(item.id)" :aria-label="`Select ${item.public_id}`" @change="toggleSelected(item.id)" /></label>
          <RatingMark v-if="item.rating" :rating="item.rating" compact /><span v-else class="feedback-no-rating">—</span>
          <div class="feedback-row-copy"><strong>{{ copy(item) }}</strong><span>{{ item.public_id }} · {{ item.project_name }} · {{ item.page_url }}</span></div>
          <AttachmentIcons :attachments="item.attachment_kinds" />
          <span class="status-pill" :class="`status-${item.ai_status}`">{{ item.ai_status }}</span>
          <span v-if="item.issue_public_id" class="issue-label">{{ item.issue_public_id }}</span><span v-else class="muted">Unlinked</span>
          <time>{{ formatAge(item.created_at) }}</time>
          <NuxtLink class="feedback-mobile-open" :to="`/feedback/${item.id}`" :aria-label="`Open ${item.public_id}`" @click.stop><ChevronRight :size="17" /></NuxtLink>
        </article>
        <button v-if="hasMore" class="feedback-load-more" type="button" :disabled="loadingMore" @click="loadMore">
          <LoaderCircle v-if="loadingMore" :size="15" /> {{ loadingMore ? 'Loading…' : 'Load more feedback' }}
        </button>
      </div>
    </section>

    <aside v-if="selected" class="feedback-inspector">
      <header><div><span class="app-eyebrow">{{ selected.project_name }}</span><h2>{{ selected.public_id }}</h2></div><div><button title="Previous feedback" @click="moveSelection(-1)"><ChevronLeft :size="16" /></button><button title="Next feedback" @click="moveSelection(1)"><ChevronRight :size="16" /></button><NuxtLink :to="`/feedback/${selected.id}`" title="Open full detail"><ExternalLink :size="16" /></NuxtLink></div></header>
      <div class="feedback-inspector-summary"><RatingMark v-if="selected.rating" :rating="selected.rating" /><span v-else class="feedback-no-rating">No rating</span><div><b>{{ selected.page_url }}</b><span>{{ formatAge(selected.created_at) }} · {{ selected.browser || 'Unknown browser' }}</span></div><span class="status-pill" :class="`status-${selected.status}`">{{ selected.status }}</span></div>
      <nav aria-label="Feedback detail sections"><button :data-active="inspectorTab === 'feedback'" @click="inspectorTab = 'feedback'">Feedback</button><button :data-active="inspectorTab === 'evidence'" @click="inspectorTab = 'evidence'">Evidence</button><button :data-active="inspectorTab === 'replay'" @click="inspectorTab = 'replay'">Replay</button></nav>
      <div v-if="inspectorTab === 'feedback'" class="feedback-inspector-body"><p class="feedback-quote">{{ copy(selected) }}</p><dl><div><dt>Reporter</dt><dd>{{ selected.reporter_email || 'Anonymous' }}</dd></div><div><dt>Sentiment</dt><dd>{{ selected.sentiment_score?.toFixed(2) || 'Not scored' }}</dd></div><div><dt>Issue</dt><dd>{{ selected.issue_public_id || 'Not linked' }}</dd></div><div><dt>AI</dt><dd><span class="status-pill" :class="`status-${selected.ai_status}`">{{ selected.ai_status }}</span></dd></div></dl></div>
      <div v-else-if="inspectorTab === 'evidence'" class="feedback-inspector-body feedback-inspector-evidence"><AttachmentIcons :attachments="selected.attachment_kinds" /><p>{{ selected.attachment_kinds.length ? 'Open full detail to inspect captured evidence.' : 'No evidence was attached.' }}</p></div>
      <div v-else class="feedback-inspector-body feedback-inspector-evidence"><PanelRightOpen :size="24" /><p>{{ selected.has_replay ? 'A session replay is attached and available in full detail.' : 'No replay is attached.' }}</p></div>
      <footer><button v-if="selected.status !== 'open'" class="app-button" :disabled="mutationPending" @click="updateStatus([selected.id], 'open')">Reopen</button><button v-else class="app-button" :disabled="mutationPending" @click="updateStatus([selected.id], 'archived')"><Archive :size="15" /> Archive</button><NuxtLink class="app-button app-button--primary" :to="selected.issue_id ? `/issues/${selected.issue_id}` : `/feedback/${selected.id}`"><Link2 :size="15" /> {{ selected.issue_id ? 'Open issue' : 'Triage' }}</NuxtLink></footer>
    </aside>

    <p v-if="notice" class="feedback-toast" role="status">{{ notice }}</p>
  </div>
</template>

<style scoped>
.feedback-workspace { display: grid; min-height: calc(100vh - 56px); grid-template-columns: minmax(520px, 1fr) minmax(330px, 36%); background: #fff; }
.feedback-stream-panel { min-width: 0; border-right: 1px solid var(--line); }
.feedback-stream-head { display: flex; min-height: 84px; align-items: flex-end; justify-content: space-between; gap: 18px; padding: 18px 20px 14px; border-bottom: 1px solid var(--line); }
.feedback-stream-head h1 { margin: 2px 0; font-size: 20px; }
.feedback-stream-head div > span { color: var(--muted); font-size: 10px; }
.feedback-filter-bar { position: sticky; top: 56px; z-index: 30; display: flex; min-height: 54px; align-items: center; gap: 7px; padding: 8px 12px; background: #fbfcfb; border-bottom: 1px solid var(--line); }
.feedback-filter-bar select,
.feedback-filter-bar input { height: 34px; min-width: 0; padding: 0 8px; background: #fff; border: 1px solid var(--line); border-radius: 4px; color: var(--ink-soft); font-size: 10px; }
.feedback-search { display: flex; width: min(190px, 26%); height: 34px; align-items: center; gap: 6px; padding: 0 8px; background: #fff; border: 1px solid var(--line); border-radius: 4px; color: var(--muted); }
.feedback-search input { width: 100%; height: auto; padding: 0; border: 0; outline: 0; }
.feedback-more-filters { position: relative; }
.feedback-more-filters summary { display: flex; height: 34px; align-items: center; padding: 0 9px; background: #fff; border: 1px solid var(--line); border-radius: 4px; color: var(--ink-soft); cursor: pointer; font-size: 10px; font-weight: 700; list-style: none; white-space: nowrap; }
.feedback-more-filters summary::-webkit-details-marker { display: none; }
.feedback-more-filters > div { position: absolute; top: 40px; right: 0; z-index: 60; display: grid; width: 360px; grid-template-columns: 1fr 1fr; gap: 10px; padding: 14px; background: #fff; border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.feedback-more-filters label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.feedback-more-filters label select,
.feedback-more-filters label input { width: 100%; }
.feedback-more-filters button { min-height: 34px; grid-column: 1 / -1; background: var(--surface-soft); border: 0; border-radius: 4px; color: var(--teal); font-size: 10px; font-weight: 750; }
.feedback-bulk-bar { display: flex; min-height: 45px; align-items: center; gap: 6px; padding: 6px 12px; background: #17342f; color: #fff; }
.feedback-bulk-bar b { margin-right: auto; font-size: 11px; }
.feedback-bulk-bar button { display: inline-flex; height: 31px; align-items: center; gap: 5px; padding: 0 8px; background: #274b44; color: #fff; border: 1px solid #42635d; border-radius: 4px; font-size: 10px; }
.feedback-bulk-bar button.danger { background: #64323a; border-color: #85505a; }
.feedback-inline-error { display: flex; align-items: center; gap: 7px; margin: 0; padding: 9px 12px; background: var(--red-soft); color: var(--red); border-bottom: 1px solid #efc2c6; font-size: 11px; }
.feedback-list-state { display: flex; min-height: 330px; align-items: center; justify-content: center; flex-direction: column; gap: 10px; color: var(--muted); font-size: 11px; text-align: center; }
.feedback-stream article { display: grid; min-height: 58px; grid-template-columns: 24px 36px minmax(190px, 1fr) 82px 70px 94px 36px 22px; align-items: center; gap: 8px; padding: 0 10px; border-bottom: 1px solid var(--line); cursor: pointer; }
.feedback-stream article:hover { background: #f7faf9; }
.feedback-stream article[data-selected='true'] { background: #eaf3f0; box-shadow: inset 3px 0 var(--teal); }
.feedback-row-check { display: grid; place-items: center; }
.feedback-row-copy { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.feedback-row-copy strong,
.feedback-row-copy span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.feedback-row-copy strong { font-size: 11px; }
.feedback-row-copy span { color: var(--muted); font-size: 9px; }
.feedback-stream time { color: var(--muted); font-size: 9px; font-variant-numeric: tabular-nums; text-align: right; }
.feedback-no-rating { display: inline-grid; min-width: 34px; height: 28px; place-items: center; color: var(--muted); border: 1px solid var(--line); border-radius: 4px; font-size: 10px; }
.feedback-mobile-open { display: grid; width: 26px; height: 30px; place-items: center; color: var(--muted); border-radius: 4px; }
.feedback-load-more { display: flex; width: 100%; min-height: 44px; align-items: center; justify-content: center; gap: 7px; background: #f9fbfa; color: var(--teal); border: 0; border-top: 1px solid var(--line); font-size: 11px; font-weight: 750; }
.feedback-load-more svg { animation: app-spin 700ms linear infinite; }

.feedback-inspector { position: sticky; top: 56px; display: flex; height: calc(100vh - 56px); min-width: 0; flex-direction: column; background: #fff; }
.feedback-inspector > header { display: flex; min-height: 65px; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 14px; border-bottom: 1px solid var(--line); }
.feedback-inspector h2 { margin: 3px 0 0; font-size: 14px; }
.feedback-inspector > header > div:last-child { display: flex; gap: 3px; }
.feedback-inspector > header button,
.feedback-inspector > header a { display: grid; width: 31px; height: 31px; place-items: center; background: transparent; color: var(--ink-soft); border: 1px solid var(--line); border-radius: 4px; }
.feedback-inspector-summary { display: grid; min-height: 70px; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 10px; padding: 10px 14px; border-bottom: 1px solid var(--line); }
.feedback-inspector-summary > div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.feedback-inspector-summary > div b,
.feedback-inspector-summary > div span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.feedback-inspector-summary > div b { font-size: 11px; }
.feedback-inspector-summary > div span { color: var(--muted); font-size: 9px; }
.feedback-inspector > nav { display: flex; min-height: 42px; padding: 0 14px; border-bottom: 1px solid var(--line); }
.feedback-inspector > nav button { padding: 0 10px; background: transparent; color: var(--muted); border: 0; border-bottom: 2px solid transparent; font-size: 10px; font-weight: 750; }
.feedback-inspector > nav button[data-active='true'] { color: var(--teal); border-bottom-color: var(--teal); }
.feedback-inspector-body { flex: 1; overflow-y: auto; padding: 18px; }
.feedback-quote { margin: 0 0 22px; color: var(--ink); font-size: 14px; line-height: 1.65; }
.feedback-inspector dl { margin: 0; }
.feedback-inspector dl div { display: flex; min-height: 38px; align-items: center; justify-content: space-between; gap: 16px; border-bottom: 1px solid var(--line); }
.feedback-inspector dt { color: var(--muted); font-size: 10px; }
.feedback-inspector dd { margin: 0; font-size: 10px; text-align: right; }
.feedback-inspector-evidence { display: grid; align-content: center; justify-items: center; gap: 13px; color: var(--muted); text-align: center; }
.feedback-inspector-evidence p { max-width: 260px; font-size: 11px; line-height: 1.5; }
.feedback-inspector > footer { display: flex; min-height: 62px; align-items: center; justify-content: flex-end; gap: 7px; padding: 10px 14px; border-top: 1px solid var(--line); }
.feedback-toast { position: fixed; right: 20px; bottom: 20px; z-index: 90; margin: 0; padding: 10px 13px; background: #17342f; color: #fff; border-radius: 5px; box-shadow: var(--shadow-overlay); font-size: 11px; }

@media (max-width: 1240px) {
  .feedback-workspace { grid-template-columns: minmax(500px, 1fr) 340px; }
  .feedback-stream article { grid-template-columns: 22px 36px minmax(170px, 1fr) 70px 64px 34px 20px; }
  .feedback-stream article :deep(.attachment-icons) { display: none; }
  .feedback-stream article .issue-label,
  .feedback-stream article > .muted { display: none; }
}

@media (max-width: 900px) {
  .feedback-workspace { display: block; min-height: 0; }
  .feedback-stream-panel { border: 0; }
  .feedback-inspector { display: none; }
  .feedback-filter-bar { overflow-x: auto; }
  .feedback-filter-bar > * { flex: 0 0 auto; }
  .feedback-search { width: 180px; }
}

@media (max-width: 700px) {
  .feedback-stream-head { min-height: 72px; padding: 12px 14px; }
  .feedback-filter-bar { top: 56px; padding: 7px 10px; }
  .feedback-stream article { grid-template-columns: 22px 34px minmax(0, 1fr) 34px 20px; gap: 7px; padding: 0 8px; }
  .feedback-stream article .status-pill,
  .feedback-stream article time { display: none; }
  .feedback-more-filters > div { position: fixed; top: 118px; right: 10px; left: 10px; width: auto; }
  .feedback-bulk-bar b { display: none; }
}
</style>
