<script setup lang="ts">
import {
  AlertTriangle,
  ChevronRight,
  ExternalLink,
  LoaderCircle,
  MonitorPlay,
  X,
} from '@lucide/vue'
import type { Project, ReplaySessionItem, ReplaySessionPage } from '~/types/api'
import {
  replaySessionDurationSeconds,
  replaySessionListQuery,
  replaySessionStatusClass,
} from '~/utils/replay-sessions'

const route = useRoute()
const router = useRouter()
const { $apiFetch } = useNuxtApp()
const { can } = useAuth()
const { t } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { integer, plural, relativeTime } = useRegionalFormat()

const project = ref(typeof route.query.project_id === 'string' ? route.query.project_id : '')
const sessionStatus = ref(typeof route.query.status === 'string' ? route.query.status : '')
const feedback = ref(typeof route.query.feedback === 'string' ? route.query.feedback : '')
const selectedId = ref(typeof route.query.id === 'string' ? route.query.id : '')
const sessions = ref<ReplaySessionItem[]>([])
const nextCursor = ref<string | null>(null)
const hasMore = ref(false)
const loadingMore = ref(false)
const loadMoreError = ref('')

const filterQuery = computed(() => replaySessionListQuery({
  projectId: project.value,
  status: sessionStatus.value,
  feedback: feedback.value,
}))

const { data, status, error, refresh } = await useAsyncData(
  'replay-sessions',
  () => $apiFetch<ReplaySessionPage>(`/v1/sessions?${filterQuery.value}`),
  { watch: [filterQuery] },
)

const { data: projectData } = await useAsyncData(
  'replay-session-projects',
  () => can('project:read')
    ? $apiFetch<{ projects: Project[] }>('/v1/projects')
    : Promise.resolve({ projects: [] }),
)

watch(data, (page) => {
  sessions.value = page?.sessions || []
  nextCursor.value = page?.pagination.next_cursor || null
  hasMore.value = page?.pagination.has_more || false
  loadMoreError.value = ''
}, { immediate: true })

// The address bar mirrors the filters and selection so a filtered view or an
// open session survives a reload and can be shared as a link — and external
// navigation onto this page (palette, back button) flows back into the state.
watch([project, sessionStatus, feedback, selectedId], () => {
  const query: Record<string, string> = {}
  if (project.value) query.project_id = project.value
  if (sessionStatus.value) query.status = sessionStatus.value
  if (feedback.value) query.feedback = feedback.value
  if (selectedId.value) query.id = selectedId.value
  void router.replace({ query })
})
watch(() => route.query, (query) => {
  project.value = typeof query.project_id === 'string' ? query.project_id : ''
  sessionStatus.value = typeof query.status === 'string' ? query.status : ''
  feedback.value = typeof query.feedback === 'string' ? query.feedback : ''
  selectedId.value = typeof query.id === 'string' ? query.id : ''
})

// The selected session usually sits in the loaded list, but a shared link can
// point at one that is pages away — then the detail endpoint supplies it.
const selected = ref<ReplaySessionItem | null>(null)
watch([selectedId, sessions], async () => {
  const id = selectedId.value
  if (!id) {
    selected.value = null
    return
  }
  const found = sessions.value.find(item => item.id === id)
  if (found) {
    selected.value = found
    return
  }
  if (selected.value?.id === id) return
  // The previous selection must not linger while a different one loads, and a
  // fetch that finishes after the user moved on must not overwrite where they
  // went — the id check after the await keeps only the current request's result.
  selected.value = null
  try {
    const fetched = await $apiFetch<ReplaySessionItem>(`/v1/sessions/${id}`)
    if (selectedId.value === id) selected.value = fetched
  } catch {
    // A vanished or foreign id simply leaves the inspector closed.
  }
}, { immediate: true })

async function loadMore() {
  if (!nextCursor.value || loadingMore.value) return
  const requestQuery = filterQuery.value
  const cursor = nextCursor.value
  loadingMore.value = true
  loadMoreError.value = ''
  try {
    const page = await $apiFetch<ReplaySessionPage>(`/v1/sessions?${replaySessionListQuery({
      projectId: project.value,
      status: sessionStatus.value,
      feedback: feedback.value,
      cursor,
    })}`)
    // A filter change while this page was in flight owns the list now; merging
    // this response would mix results from two different queries.
    if (filterQuery.value !== requestQuery || nextCursor.value !== cursor) return
    sessions.value = [...sessions.value, ...page.sessions]
    nextCursor.value = page.pagination.next_cursor || null
    hasMore.value = page.pagination.has_more
  } catch (error) {
    // An error from a superseded request would sit under a list it never
    // belonged to; only the request that still owns the list may report.
    if (filterQuery.value === requestQuery && nextCursor.value === cursor) {
      loadMoreError.value = apiErrorMessage(error, 'replays.errors.loadMore')
    }
  } finally {
    loadingMore.value = false
  }
}

function clearFilters() {
  project.value = ''
  sessionStatus.value = ''
  feedback.value = ''
}

const showProjectName = computed(() => (projectData.value?.projects.length ?? 0) > 1)
const projectNames = computed(() => new Map(
  (projectData.value?.projects || []).map(item => [item.id, item.name]),
))

function statusLabel(value: string) {
  return t(`replays.status.${value}`)
}

function pageLabel(item: ReplaySessionItem) {
  return (item.page_url || t('replays.common.unknownPage')).replace(/^https?:\/\//, '')
}

function formatAge(value: string) {
  const hours = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 3_600_000))
  if (hours < 1) return t('replays.common.now')
  if (hours < 24) return relativeTime(-hours, 'hour', { numeric: 'always', style: 'short' })
  return relativeTime(-Math.floor(hours / 24), 'day', { numeric: 'always', style: 'short' })
}

function durationLabel(item: ReplaySessionItem) {
  const seconds = replaySessionDurationSeconds(item.started_at, item.ended_at || null)
  if (seconds === null) return t('replays.common.inProgress')
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return minutes > 0
    ? t('replays.common.durationMinutes', { minutes: integer(minutes), seconds: integer(rest) })
    : t('replays.common.durationSeconds', { seconds: integer(rest) })
}

const scopeLine = computed(() =>
  plural('replays.loaded', sessions.value.length, { count: integer(sessions.value.length) }))

useHead(() => ({ title: t('replays.pageTitle') }))
</script>

<template>
  <div class="replays-page">
    <section class="replays-panel">
      <header class="replays-head">
        <div>
          <h1>{{ t('replays.title') }}</h1>
          <span>{{ scopeLine }}</span>
        </div>
        <div class="replays-filters">
          <select v-if="showProjectName" v-model="project" class="app-select" :aria-label="t('replays.filters.project')">
            <option value="">{{ t('replays.filters.allProjects') }}</option>
            <option v-for="item in projectData?.projects" :key="item.id" :value="item.id">{{ item.name }}</option>
          </select>
          <select v-model="sessionStatus" class="app-select" :aria-label="t('replays.filters.status')">
            <option value="">{{ t('replays.filters.allStatuses') }}</option>
            <option v-for="value in ['ready', 'recording', 'processing', 'failed', 'expired']" :key="value" :value="value">{{ statusLabel(value) }}</option>
          </select>
          <select v-model="feedback" class="app-select" :aria-label="t('replays.filters.feedback')">
            <option value="">{{ t('replays.filters.anyFeedback') }}</option>
            <option value="linked">{{ t('replays.filters.withFeedback') }}</option>
            <option value="unlinked">{{ t('replays.filters.withoutFeedback') }}</option>
          </select>
        </div>
      </header>

      <div v-if="status === 'pending'" class="replays-state"><span class="app-spinner" /><p>{{ t('replays.states.loading') }}</p></div>
      <div v-else-if="error" class="replays-state" role="alert"><AlertTriangle :size="22" /><p>{{ apiErrorMessage(error, 'replays.errors.load') }}</p><button class="app-button" @click="refresh()">{{ t('replays.actions.tryAgain') }}</button></div>
      <div v-else-if="sessions.length === 0" class="replays-state"><MonitorPlay :size="22" /><p>{{ t('replays.states.empty') }}</p><button v-if="project || sessionStatus || feedback" class="app-button" @click="clearFilters">{{ t('replays.filters.clear') }}</button></div>
      <div v-else class="replays-stream" role="list">
        <article
          v-for="item in sessions"
          :key="item.id"
          role="listitem"
          :data-selected="selectedId === item.id"
          @click="selectedId = item.id"
        >
          <span class="replays-row-icon"><MonitorPlay :size="16" /></span>
          <div class="replays-row-copy">
            <strong>{{ pageLabel(item) }}</strong>
            <div class="replays-row-context">
              <span v-if="showProjectName && projectNames.get(item.project_id)">{{ projectNames.get(item.project_id) }} ·</span>
              <span>{{ durationLabel(item) }}</span>
              <span>· {{ plural('replays.events', item.events_count, { count: integer(item.events_count) }) }}</span>
            </div>
          </div>
          <NuxtLink
            v-if="item.feedback_id && can('feedback:read')"
            class="replays-feedback-link"
            :to="`/feedback/${item.feedback_id}`"
            @click.stop
          >{{ t('replays.common.feedbackLinked') }}</NuxtLink>
          <span v-else class="replays-feedback-none">{{ t('replays.common.noFeedback') }}</span>
          <span class="status-pill" :class="replaySessionStatusClass(item.status)">{{ statusLabel(item.status) }}</span>
          <time>{{ formatAge(item.started_at) }}</time>
          <ChevronRight :size="16" aria-hidden="true" />
        </article>
        <p v-if="loadMoreError" class="replays-inline-error" role="alert"><AlertTriangle :size="14" /> {{ loadMoreError }}</p>
        <button v-if="hasMore" class="replays-load-more" type="button" :disabled="loadingMore" @click="loadMore">
          <LoaderCircle v-if="loadingMore" :size="15" /> {{ loadingMore ? t('replays.states.loadingMore') : t('replays.states.loadMore') }}
        </button>
      </div>
    </section>

    <aside v-if="selected" class="replays-inspector">
      <header>
        <div>
          <span class="app-eyebrow">{{ statusLabel(selected.status) }}</span>
          <h2>{{ pageLabel(selected) }}</h2>
        </div>
        <div class="replays-inspector-actions">
          <NuxtLink
            v-if="selected.feedback_id && can('feedback:read')"
            class="app-button"
            :to="`/feedback/${selected.feedback_id}`"
          >{{ t('replays.actions.openFeedback') }} <ExternalLink :size="14" /></NuxtLink>
          <button type="button" :aria-label="t('replays.actions.close')" @click="selectedId = ''"><X :size="16" /></button>
        </div>
      </header>
      <dl class="replays-meta">
        <div><dt>{{ t('replays.meta.started') }}</dt><dd>{{ formatAge(selected.started_at) }}</dd></div>
        <div><dt>{{ t('replays.meta.duration') }}</dt><dd>{{ durationLabel(selected) }}</dd></div>
        <div><dt>{{ t('replays.meta.events') }}</dt><dd>{{ integer(selected.events_count) }}</dd></div>
        <div v-if="showProjectName"><dt>{{ t('replays.meta.project') }}</dt><dd>{{ projectNames.get(selected.project_id) || t('replays.common.unknownPage') }}</dd></div>
      </dl>
      <ReplayPlayer v-if="selected.status === 'ready' && selected.replay_url" :key="selected.id" :url="selected.replay_url" />
      <div v-else class="replays-placeholder" :data-failed="selected.status === 'failed' || selected.status === 'expired'" aria-live="polite">
        <AlertTriangle v-if="selected.status === 'failed' || selected.status === 'expired'" :size="30" />
        <MonitorPlay v-else :size="30" />
        <p>{{ t(`replays.placeholders.${selected.status}`) }}</p>
      </div>
    </aside>
  </div>
</template>

<style scoped>
.replays-page {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 18px;
  align-items: start;
}
.replays-page:has(.replays-inspector) {
  grid-template-columns: minmax(0, 1fr) minmax(320px, 440px);
}
.replays-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}
.replays-head h1 { margin: 0; font-size: 1.15rem; }
.replays-head > div > span { color: var(--color-text-muted); font-size: 0.82rem; }
.replays-filters { display: flex; gap: 8px; flex-wrap: wrap; }
.replays-state {
  display: grid;
  justify-items: center;
  gap: 8px;
  padding: 48px 16px;
  color: var(--color-text-muted);
  text-align: center;
}
.replays-stream { display: grid; gap: 2px; }
.replays-stream article {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto auto auto auto;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-radius: 10px;
  cursor: pointer;
}
.replays-stream article:hover { background: var(--color-surface-raised, rgba(127, 127, 127, 0.08)); }
.replays-stream article[data-selected='true'] { background: var(--color-surface-raised, rgba(127, 127, 127, 0.12)); }
.replays-row-icon { display: inline-flex; color: var(--color-text-muted); }
.replays-row-copy { min-width: 0; display: grid; gap: 2px; }
.replays-row-copy strong {
  font-size: 0.88rem;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.replays-row-context {
  display: flex;
  gap: 4px;
  color: var(--color-text-muted);
  font-size: 0.78rem;
  flex-wrap: wrap;
}
.replays-feedback-link { font-size: 0.78rem; text-decoration: underline; }
.replays-feedback-none { font-size: 0.78rem; color: var(--color-text-muted); }
.replays-stream time { font-size: 0.78rem; color: var(--color-text-muted); white-space: nowrap; }
.replays-inline-error {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 8px 0 0;
  color: var(--color-danger, #c0392b);
  font-size: 0.8rem;
}
.replays-load-more {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  margin-top: 10px;
  padding: 8px;
  border: 0;
  background: none;
  color: inherit;
  cursor: pointer;
  font: inherit;
}
.replays-inspector {
  border: 1px solid var(--color-border, rgba(127, 127, 127, 0.25));
  border-radius: 12px;
  padding: 14px;
  display: grid;
  gap: 12px;
  align-content: start;
  position: sticky;
  top: 12px;
}
.replays-inspector header {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: 10px;
}
.replays-inspector h2 {
  margin: 2px 0 0;
  font-size: 0.95rem;
  overflow-wrap: anywhere;
}
.replays-inspector-actions { display: flex; align-items: center; gap: 8px; }
.replays-inspector-actions button {
  border: 0;
  background: none;
  color: inherit;
  cursor: pointer;
  display: inline-flex;
  padding: 4px;
}
.replays-meta {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(110px, 1fr));
  gap: 8px;
  margin: 0;
}
.replays-meta dt { font-size: 0.72rem; color: var(--color-text-muted); text-transform: uppercase; letter-spacing: 0.04em; }
.replays-meta dd { margin: 2px 0 0; font-size: 0.85rem; }
.replays-placeholder {
  display: grid;
  justify-items: center;
  gap: 8px;
  padding: 40px 14px;
  color: var(--color-text-muted);
  text-align: center;
}
@media (max-width: 900px) {
  .replays-page:has(.replays-inspector) { grid-template-columns: minmax(0, 1fr); }
}
</style>
