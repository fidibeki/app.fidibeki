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
  <div class="replays-workspace">
    <section class="replays-stream-panel">
      <header class="replays-stream-head">
        <div><h1>{{ t('replays.title') }}</h1><span>{{ scopeLine }}</span></div>
      </header>

      <div class="replays-filter-bar">
        <select v-if="showProjectName" v-model="project" :aria-label="t('replays.filters.project')">
          <option value="">{{ t('replays.filters.allProjects') }}</option>
          <option v-for="item in projectData?.projects" :key="item.id" :value="item.id">{{ item.name }}</option>
        </select>
        <select v-model="sessionStatus" :aria-label="t('replays.filters.status')">
          <option value="">{{ t('replays.filters.allStatuses') }}</option>
          <option v-for="value in ['ready', 'recording', 'processing', 'failed', 'expired']" :key="value" :value="value">{{ statusLabel(value) }}</option>
        </select>
        <select v-model="feedback" :aria-label="t('replays.filters.feedback')">
          <option value="">{{ t('replays.filters.anyFeedback') }}</option>
          <option value="linked">{{ t('replays.filters.withFeedback') }}</option>
          <option value="unlinked">{{ t('replays.filters.withoutFeedback') }}</option>
        </select>
      </div>

      <p v-if="loadMoreError" class="replays-inline-error" role="alert"><AlertTriangle :size="15" /> {{ loadMoreError }}</p>
      <div v-if="status === 'pending'" class="replays-list-state"><span class="app-spinner" /><p>{{ t('replays.states.loading') }}</p></div>
      <div v-else-if="error" class="replays-list-state" role="alert"><AlertTriangle :size="22" /><p>{{ apiErrorMessage(error, 'replays.errors.load') }}</p><button class="app-button" @click="refresh()">{{ t('replays.actions.tryAgain') }}</button></div>
      <div v-else-if="sessions.length === 0" class="replays-list-state"><MonitorPlay :size="22" /><p>{{ t('replays.states.empty') }}</p><button v-if="project || sessionStatus || feedback" class="app-button" @click="clearFilters">{{ t('replays.filters.clear') }}</button></div>
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
          <span class="replays-row-open" aria-hidden="true"><ChevronRight :size="16" /></span>
        </article>
        <button v-if="hasMore" class="replays-load-more" type="button" :disabled="loadingMore" @click="loadMore">
          <LoaderCircle v-if="loadingMore" :size="15" /> {{ loadingMore ? t('replays.states.loadingMore') : t('replays.states.loadMore') }}
        </button>
      </div>
    </section>

    <aside v-if="selected" class="replays-inspector">
      <header>
        <div class="replays-inspector-title">
          <span class="app-eyebrow">{{ statusLabel(selected.status) }}</span>
          <h2>{{ pageLabel(selected) }}</h2>
        </div>
        <div>
          <NuxtLink
            v-if="selected.feedback_id && can('feedback:read')"
            :to="`/feedback/${selected.feedback_id}`"
            :title="t('replays.actions.openFeedback')"
            :aria-label="t('replays.actions.openFeedback')"
          ><ExternalLink :size="16" /></NuxtLink>
          <button type="button" :title="t('replays.actions.close')" :aria-label="t('replays.actions.close')" @click="selectedId = ''"><X :size="16" /></button>
        </div>
      </header>
      <div class="replays-inspector-body">
        <dl class="replays-meta">
          <div><dt>{{ t('replays.meta.started') }}</dt><dd>{{ formatAge(selected.started_at) }}</dd></div>
          <div><dt>{{ t('replays.meta.duration') }}</dt><dd>{{ durationLabel(selected) }}</dd></div>
          <div><dt>{{ t('replays.meta.events') }}</dt><dd>{{ integer(selected.events_count) }}</dd></div>
          <div v-if="showProjectName"><dt>{{ t('replays.meta.project') }}</dt><dd>{{ projectNames.get(selected.project_id) || t('replays.common.unknownPage') }}</dd></div>
        </dl>
        <ReplayPlayer v-if="selected.status === 'ready' && selected.replay_url" :key="selected.id" :url="selected.replay_url" />
        <div v-else class="replays-placeholder" :data-failed="selected.status === 'failed' || selected.status === 'expired'" aria-live="polite">
          <AlertTriangle v-if="selected.status === 'failed' || selected.status === 'expired'" :size="28" />
          <MonitorPlay v-else :size="28" />
          <p>{{ t(`replays.placeholders.${selected.status}`) }}</p>
        </div>
      </div>
    </aside>
  </div>
</template>

<style scoped>
.replays-workspace { display: grid; min-height: calc(100vh - 56px); grid-template-columns: minmax(520px, 1fr) minmax(330px, 40%); background: var(--surface); }
.replays-stream-panel { min-width: 0; border-right: 1px solid var(--line); }
.replays-stream-head { display: flex; min-height: 58px; align-items: center; justify-content: space-between; gap: 18px; padding: 10px 20px; border-bottom: 1px solid var(--line); }
.replays-stream-head > div { display: flex; min-width: 0; align-items: baseline; gap: 10px; }
.replays-stream-head h1 { margin: 0; flex: 0 0 auto; font-size: 17px; }
.replays-stream-head div > span { overflow: hidden; color: var(--muted); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.replays-filter-bar { position: sticky; top: 56px; z-index: 30; display: flex; min-height: 54px; align-items: center; gap: 7px; padding: 8px 12px; background: var(--surface-subtle); border-bottom: 1px solid var(--line); }
.replays-filter-bar select { height: 34px; min-width: 0; padding: 0 8px; background: var(--surface); border: 1px solid var(--line); border-radius: 4px; color: var(--ink-soft); font-size: 11px; }
.replays-inline-error { display: flex; align-items: center; gap: 7px; margin: 0; padding: 9px 12px; background: var(--red-soft); color: var(--red); border-bottom: 1px solid var(--red-line); font-size: 11px; }
.replays-list-state { display: flex; min-height: 330px; align-items: center; justify-content: center; flex-direction: column; gap: 10px; color: var(--muted); font-size: 11px; text-align: center; }
.replays-stream article { display: grid; min-height: 58px; grid-template-columns: 34px minmax(190px, 1fr) auto auto max-content 22px; align-items: center; gap: 8px; padding: 0 12px; border-bottom: 1px solid var(--line); cursor: pointer; }
.replays-stream article:hover { background: var(--surface-subtle); }
.replays-stream article[data-selected='true'] { background: var(--teal-soft); box-shadow: inset 3px 0 var(--teal); }
.replays-row-icon { display: grid; place-items: center; color: var(--muted); }
.replays-row-copy { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.replays-row-copy > strong { overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.replays-row-context { display: flex; min-width: 0; align-items: center; gap: 5px; }
.replays-row-context > span { min-width: 0; overflow: hidden; color: var(--muted); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.replays-feedback-link { color: var(--ink-soft); font-size: 11px; text-decoration: underline; text-underline-offset: 2px; }
.replays-feedback-link:hover { color: var(--teal); }
.replays-feedback-none { color: var(--muted); font-size: 11px; }
.replays-stream article > .status-pill { justify-self: start; }
.replays-stream time { color: var(--muted); font-size: 11px; white-space: nowrap; font-variant-numeric: tabular-nums; text-align: right; }
.replays-row-open { display: grid; width: 26px; height: 30px; place-items: center; color: var(--muted); }
.replays-load-more { display: flex; width: 100%; min-height: 44px; align-items: center; justify-content: center; gap: 7px; background: var(--surface-subtle); color: var(--teal); border: 0; border-top: 1px solid var(--line); font-size: 11px; font-weight: 750; }
.replays-load-more svg { animation: app-spin 700ms linear infinite; }

.replays-inspector { position: sticky; top: 56px; display: flex; height: calc(100vh - 56px); min-width: 0; flex-direction: column; background: var(--surface); }
.replays-inspector > header { display: flex; min-height: 65px; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 14px; border-bottom: 1px solid var(--line); }
.replays-inspector-title { min-width: 0; }
.replays-inspector h2 { margin: 3px 0 0; overflow: hidden; font-size: 14px; text-overflow: ellipsis; white-space: nowrap; }
.replays-inspector > header > div:last-child { display: flex; flex: 0 0 auto; gap: 3px; }
.replays-inspector > header button,
.replays-inspector > header a { display: grid; width: 31px; height: 31px; place-items: center; background: transparent; color: var(--ink-soft); border: 1px solid var(--line); border-radius: 4px; cursor: pointer; }
.replays-inspector-body { flex: 1; overflow-y: auto; padding: 14px; }
.replays-meta { display: grid; margin: 0 0 14px; }
.replays-meta div { display: flex; min-height: 38px; align-items: center; justify-content: space-between; gap: 16px; border-bottom: 1px solid var(--line); }
.replays-meta dt { color: var(--muted); font-size: 10px; }
.replays-meta dd { margin: 0; font-size: 10px; text-align: right; }
.replays-placeholder { display: grid; min-height: 220px; align-content: center; justify-items: center; gap: 10px; background: var(--surface-soft); border-radius: var(--radius); color: var(--muted); text-align: center; }
.replays-placeholder[data-failed='true'] { color: var(--red); }
.replays-placeholder p { max-width: 280px; margin: 0; font-size: 11px; line-height: 1.5; }

@media (max-width: 1240px) {
  .replays-workspace { grid-template-columns: minmax(420px, 1fr) 330px; }
  .replays-stream article { grid-template-columns: 30px minmax(140px, 1fr) auto max-content 20px; }
  .replays-stream article .replays-feedback-none { display: none; }
}

@media (max-width: 1100px) {
  /* Replays have no standalone detail page, so unlike the feedback workspace
     the inspector cannot disappear on narrow viewports — it stacks under the
     stream instead and keeps the player reachable. */
  .replays-workspace { display: block; min-height: 0; }
  .replays-stream-panel { border: 0; }
  .replays-inspector { position: static; height: auto; border-top: 2px solid var(--line-strong); }
  .replays-inspector-body { overflow: visible; }
}

@media (max-width: 700px) {
  .replays-stream-head { min-height: 72px; padding: 12px 14px; }
  .replays-filter-bar { overflow-x: auto; }
  .replays-filter-bar select { flex: 0 0 auto; }
  .replays-stream article { grid-template-columns: 30px minmax(0, 1fr) 20px; gap: 7px; padding: 0 8px; }
  .replays-stream article .status-pill,
  .replays-stream article time,
  .replays-stream article .replays-feedback-link,
  .replays-stream article .replays-feedback-none { display: none; }
}
</style>
