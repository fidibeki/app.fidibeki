<script setup lang="ts">
import {
  AlertTriangle,
  Archive,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Inbox,
  Link2,
  ListFilter,
  LoaderCircle,
  PanelRightOpen,
  Plus,
  Search,
  X,
} from '@lucide/vue'
import type { LocationQueryRaw } from 'vue-router'
import type {
  FeedbackItem,
  FeedbackPage,
  FeedbackStatus,
  Issue,
  Member,
  Project,
  TenantConfiguration,
  TriagePriority,
  TriageTag,
} from '~/types/api'

const route = useRoute()
const router = useRouter()
const { $apiFetch } = useNuxtApp()
const { can, canWrite } = useAuth()
const { t } = useI18n()
const { message: apiErrorMessage } = useLocalizedAPIError()
const { integer, plural, relativeTime } = useRegionalFormat()

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
const localQueryTargets = new Set<string>()
const createIssueOpen = ref(false)
const linkIssueOpen = ref(false)
const issueTitle = ref('')
const issueDescription = ref('')
const linkTargetID = ref('')
const mobileFiltersEl = ref<HTMLDetailsElement | null>(null)

function replaceLocalQuery(query: LocationQueryRaw) {
  const target = router.resolve({ query }).fullPath
  if (target === route.fullPath) return

  localQueryTargets.add(target)
  const cleanup = () => localQueryTargets.delete(target)
  void router.replace({ query }).then(cleanup, cleanup)
}

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
const { data: configuration } = await useAsyncData(
  'tenant-configuration',
  () => $apiFetch<TenantConfiguration>('/v1/config'),
)
const { data: tagData } = await useAsyncData(
  'feedback-triage-tags',
  () => $apiFetch<{ tags: TriageTag[] }>('/v1/tags'),
)
const { data: memberData } = await useAsyncData(
  'feedback-triage-members',
  () => can('member:read')
    ? $apiFetch<{ members: Member[] }>('/v1/members')
    : Promise.resolve({ members: [] }),
)
const { data: issueData } = await useAsyncData(
  'feedback-issue-options',
  () => can('issue:read')
    ? $apiFetch<{ issues: Issue[] }>('/v1/issues')
    : Promise.resolve({ issues: [] }),
)
const aiEnabled = computed(() => configuration.value?.plan.features.ai === true)

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
    replaceLocalQuery(params)
  },
)

watch(() => route.fullPath, (fullPath) => {
  if (localQueryTargets.delete(fullPath)) return

  const value = (key: string, fallback = '') => {
    const queryValue = route.query[key]
    return typeof queryValue === 'string' ? queryValue : fallback
  }

  selectedId.value = value('id')
  query.value = value('q')
  project.value = value('project_id')
  feedbackStatus.value = value('status', 'open')
  rating.value = value('rating')
  attachment.value = value('attachment')
  moderation.value = value('moderation')
  issue.value = value('issue')
  processing.value = value('processing')
  sentiment.value = value('sentiment')
  dateFrom.value = value('date_from')
  dateTo.value = value('date_to')
  sort.value = value('sort', 'newest')
}, { flush: 'sync' })

const selected = computed(() => items.value.find((item) => item.id === selectedId.value) || items.value[0])
const triageTags = computed(() => {
  const options = [...(tagData.value?.tags || [])]
  for (const tag of selected.value?.tags || []) {
    if (!options.some(option => option.id === tag.id)) options.push(tag)
  }
  return options.sort((left, right) => left.name.localeCompare(right.name))
})
const triageMembers = computed(() => {
  const options = [...(memberData.value?.members || [])]
  const assignee = selected.value?.assignee
  if (assignee && !options.some(option => option.id === assignee.id)) {
    options.push({
      id: assignee.id, tenant_id: '', email: assignee.email, name: assignee.name || undefined,
      avatar_version: assignee.avatar_version,
      permissions: [], status: 'active', role: 'member', verified: false,
    })
  }
  return options
})
const selectedItems = computed(() => items.value.filter(item => selectedIds.value.includes(item.id)))
const selectedProjectIDs = computed(() => new Set(selectedItems.value.map(item => item.project_id)))
const selectedProjectID = computed(() => selectedProjectIDs.value.size === 1 ? selectedItems.value[0]?.project_id : undefined)
const selectedHasLinkedFeedback = computed(() => selectedItems.value.some(item => Boolean(item.issue_id)))
const issueActionReason = computed(() => {
  if (selectedProjectIDs.value.size > 1) return t('feedback.bulk.reasonMixedProjects')
  if (selectedHasLinkedFeedback.value) return t('feedback.bulk.reasonAlreadyLinked')
  return ''
})
const canCreateOrLinkIssue = computed(() => Boolean(
  selectedItems.value.length && selectedProjectID.value && !selectedHasLinkedFeedback.value,
))
const canUnlinkIssue = computed(() => selectedHasLinkedFeedback.value)
const linkIssueOptions = computed(() => (issueData.value?.issues || []).filter(issue => (
  issue.project_id === selectedProjectID.value && !issue.merged_into_id
)))

function selectItem(item: FeedbackItem) {
  selectedId.value = item.id
  inspectorTab.value = 'feedback'
  replaceLocalQuery({ ...route.query, id: item.id })
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
    mutationError.value = apiErrorMessage(loadError, 'feedback.errors.loadMore')
  } finally {
    loadingMore.value = false
  }
}

async function updateFeedbacks(
  ids: string[],
  body: Record<string, unknown>,
  actionKey: string,
  parameters: Record<string, unknown> = {},
) {
  if (!canWrite('feedback:update') || !ids.length || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    const response = await $apiFetch<{ updated_count: number, feedbacks: FeedbackItem[] }>('/v1/feedbacks/bulk', {
      method: 'PATCH', body: { feedback_ids: ids, ...body },
    })
    for (const updated of response.feedbacks) {
      const index = items.value.findIndex(item => item.id === updated.id)
      if (index >= 0) items.value[index] = updated
    }
    notice.value = plural(`feedback.notices.${actionKey}`, response.updated_count, {
      count: integer(response.updated_count),
      ...parameters,
    })
    selectedIds.value = []
    await refresh()
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, 'feedback.errors.update')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

function updateStatus(ids: string[], nextStatus: FeedbackStatus) {
  const actionKey = nextStatus === 'spam'
    ? 'bulkSpam'
    : nextStatus === 'open' ? 'bulkReopened' : 'bulkArchived'
  return updateFeedbacks(ids, { status: nextStatus }, actionKey)
}

function updateSelectedFeedbacks(
  body: Record<string, unknown>,
  actionKey: string,
  parameters?: Record<string, unknown>,
) {
  return updateFeedbacks([...selectedIds.value], body, actionKey, parameters)
}

function openCreateIssue() {
  if (!canCreateOrLinkIssue.value) return
  const first = selectedItems.value[0]
  issueTitle.value = first?.ai_suggested_title || first?.title || ''
  issueDescription.value = selectedItems.value.map(item => (
    `${item.public_id}: ${item.description || item.text || item.title}`
  )).join('\n\n')
  mutationError.value = ''
  createIssueOpen.value = true
}

function openLinkIssue() {
  if (!canCreateOrLinkIssue.value) return
  linkTargetID.value = ''
  mutationError.value = ''
  linkIssueOpen.value = true
}

async function createIssueFromSelection() {
  const feedbackIDs = [...selectedIds.value]
  const title = issueTitle.value.trim()
  if (!canWrite('issue:update') || !feedbackIDs.length || !title || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    const response = await $apiFetch<{ issue: Issue }>('/v1/issues/from-feedback', {
      method: 'POST',
      body: { feedback_ids: feedbackIDs, title, description: issueDescription.value.trim() },
    })
    createIssueOpen.value = false
    selectedIds.value = []
    await router.push(`/issues/${response.issue.id}`)
  } catch (createError) {
    mutationError.value = apiErrorMessage(createError, 'feedback.errors.createIssue')
  } finally {
    mutationPending.value = false
  }
}

async function mutateFeedbackLinks(path: '/v1/feedbacks/link' | '/v1/feedbacks/unlink', issueID?: string) {
  const feedbackIDs = [...selectedIds.value]
  if (!canWrite('issue:update') || !feedbackIDs.length || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    const response = await $apiFetch<{ updated_count: number, feedbacks: FeedbackItem[] }>(path, {
      method: 'POST', body: { feedback_ids: feedbackIDs, ...(issueID ? { issue_id: issueID } : {}) },
    })
    for (const updated of response.feedbacks) {
      const index = items.value.findIndex(item => item.id === updated.id)
      if (index >= 0) items.value[index] = updated
    }
    selectedIds.value = []
    if (issueID) {
      linkIssueOpen.value = false
      await router.push(`/issues/${issueID}`)
    } else {
      notice.value = plural('feedback.notices.unlinked', response.updated_count, {
        count: integer(response.updated_count),
      })
      await refresh()
    }
  } catch (linkError) {
    mutationError.value = apiErrorMessage(linkError, 'feedback.errors.links')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

function linkSelectedFeedback() {
  if (!linkTargetID.value) return
  return mutateFeedbackLinks('/v1/feedbacks/link', linkTargetID.value)
}

function unlinkSelectedFeedback() {
  return mutateFeedbackLinks('/v1/feedbacks/unlink')
}

async function updateSelectedTriage(body: Record<string, unknown>, successKey: string) {
  if (!canWrite('feedback:update') || !selected.value || mutationPending.value) return
  const feedbackID = selected.value.id
  mutationPending.value = true
  mutationError.value = ''
  try {
    const response = await $apiFetch<{ feedback: FeedbackItem }>(`/v1/feedbacks/${feedbackID}`, {
      method: 'PATCH', body,
    })
    const index = items.value.findIndex(item => item.id === feedbackID)
    if (index >= 0) items.value[index] = response.feedback
    notice.value = t(successKey)
  } catch (updateError) {
    mutationError.value = apiErrorMessage(updateError, 'feedback.errors.triage')
  } finally {
    mutationPending.value = false
    window.setTimeout(() => { notice.value = '' }, 3000)
  }
}

function setSelectedPriority(priority: TriagePriority | null) {
  return updateSelectedTriage({ priority: priority || 'none' }, 'feedback.notices.priorityUpdated')
}

function setSelectedAssignee(memberID: string | null) {
  return updateSelectedTriage(memberID ? { assignee_id: memberID } : { unassign: true }, 'feedback.notices.assigneeUpdated')
}

function toggleSelectedTag(tagID: string, checked: boolean) {
  return updateSelectedTriage(checked ? { add_tag_ids: [tagID] } : { remove_tag_ids: [tagID] }, 'feedback.notices.tagsUpdated')
}

function priorityLabel(priority?: TriagePriority | null) {
  return priority ? t(`feedback.priority.${priority}`) : ''
}

async function reprocess(ids: string[]) {
  if (!canWrite('feedback:update') || !aiEnabled.value || !ids.length || mutationPending.value) return
  mutationPending.value = true
  mutationError.value = ''
  try {
    await Promise.all(ids.map((id) => $apiFetch(`/v1/feedbacks/${id}/reprocess`, { method: 'POST' })))
    notice.value = plural('feedback.notices.aiQueued', ids.length, { count: integer(ids.length) })
    selectedIds.value = []
    await refresh()
  } catch (reprocessError) {
    mutationError.value = apiErrorMessage(reprocessError, 'feedback.errors.ai')
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

const activeFilterCount = computed(() => [
  project.value,
  feedbackStatus.value,
  rating.value,
  attachment.value,
  moderation.value,
  issue.value,
  processing.value,
  sentiment.value,
  dateFrom.value,
  dateTo.value,
].filter(Boolean).length)

const mobileFiltersSummaryLabel = computed(() => activeFilterCount.value > 0
  ? plural('feedback.filters.mobileTriggerActive', activeFilterCount.value, { count: integer(activeFilterCount.value) })
  : t('feedback.filters.mobileTrigger'))

function closeMobileFilters() {
  const details = mobileFiltersEl.value
  if (!details) return
  details.open = false
  details.querySelector('summary')?.focus()
}

function copy(item: FeedbackItem) {
  return item.title || t('feedback.common.feedbackWithoutDetail')
}

function formatAge(value: string) {
  const hours = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 3_600_000))
  if (hours < 1) return t('feedback.common.now')
  if (hours < 24) return relativeTime(-hours, 'hour', { numeric: 'always', style: 'short' })
  return relativeTime(-Math.floor(hours / 24), 'day', { numeric: 'always', style: 'short' })
}

function statusLabel(value: string) {
  return t(`feedback.status.${value}`)
}

useHead(() => ({ title: t('feedback.pageTitle') }))
</script>

<template>
  <div class="feedback-workspace">
    <section class="feedback-stream-panel">
      <header class="feedback-stream-head">
        <div><p class="app-eyebrow">{{ t('feedback.queueEyebrow') }}</p><h1>{{ t('feedback.title') }}</h1><span>{{ plural('feedback.loaded', items.length, { count: integer(items.length) }) }}</span></div>
        <select v-model="sort" class="app-select" :aria-label="t('feedback.sorting.label')">
          <option value="newest">{{ t('feedback.sorting.newest') }}</option><option value="oldest">{{ t('feedback.sorting.oldest') }}</option>
          <option value="rating_low">{{ t('feedback.sorting.ratingLow') }}</option><option value="rating_high">{{ t('feedback.sorting.ratingHigh') }}</option>
        </select>
      </header>

      <div class="feedback-filter-bar">
        <label class="feedback-search"><Search :size="15" /><input v-model.trim="query" :placeholder="t('feedback.filters.search')" :aria-label="t('feedback.filters.search')" /></label>
        <div class="feedback-filter-bar-desktop">
          <select v-model="project" :aria-label="t('feedback.filters.project')"><option value="">{{ t('feedback.filters.allProjects') }}</option><option v-for="item in projectData?.projects" :key="item.id" :value="item.id">{{ item.name }}</option></select>
          <select v-model="feedbackStatus" :aria-label="t('feedback.filters.status')"><option value="">{{ t('feedback.filters.allStatuses') }}</option><option value="open">{{ statusLabel('open') }}</option><option value="archived">{{ statusLabel('archived') }}</option><option value="spam">{{ statusLabel('spam') }}</option></select>
          <select v-model="rating" :aria-label="t('feedback.filters.rating')"><option value="">{{ t('feedback.filters.anyRating') }}</option><option v-for="value in 5" :key="value" :value="String(value)">{{ plural('feedback.filters.star', value, { count: integer(value) }) }}</option></select>
          <select v-model="attachment" :aria-label="t('feedback.filters.evidence')"><option value="">{{ t('feedback.filters.anyEvidence') }}</option><option value="none">{{ t('feedback.filters.noEvidence') }}</option><option value="screenshot">{{ t('feedback.evidenceKinds.screenshot') }}</option><option value="audio">{{ t('feedback.evidenceKinds.audio') }}</option><option value="video">{{ t('feedback.evidenceKinds.video') }}</option><option value="replay">{{ t('feedback.evidenceKinds.replay') }}</option></select>
          <details class="feedback-more-filters">
            <summary>{{ t('feedback.filters.more') }}</summary>
            <div>
              <label>{{ t('feedback.filters.moderation') }}<select v-model="moderation"><option value="">{{ t('feedback.filters.any') }}</option><option value="flagged">{{ t('feedback.filters.flagged') }}</option><option value="clean">{{ t('feedback.filters.clean') }}</option></select></label>
              <label>{{ t('feedback.filters.issue') }}<select v-model="issue"><option value="">{{ t('feedback.filters.any') }}</option><option value="linked">{{ t('feedback.filters.linked') }}</option><option value="unlinked">{{ t('feedback.filters.unlinked') }}</option></select></label>
              <label>{{ t('feedback.filters.aiStatus') }}<select v-model="processing"><option value="">{{ t('feedback.filters.any') }}</option><option v-for="value in ['failed', 'processing', 'deferred', 'pending', 'completed', 'skipped']" :key="value" :value="value">{{ statusLabel(value) }}</option></select></label>
              <label>{{ t('feedback.filters.sentiment') }}<select v-model="sentiment"><option value="">{{ t('feedback.filters.any') }}</option><option value="negative">{{ t('feedback.sentiment.negative') }}</option><option value="neutral">{{ t('feedback.sentiment.neutral') }}</option><option value="positive">{{ t('feedback.sentiment.positive') }}</option></select></label>
              <label>{{ t('feedback.filters.from') }}<input v-model="dateFrom" type="date" /></label><label>{{ t('feedback.filters.to') }}<input v-model="dateTo" type="date" /></label>
              <button type="button" @click="clearFilters">{{ t('feedback.filters.clear') }}</button>
            </div>
          </details>
        </div>
        <details ref="mobileFiltersEl" class="feedback-mobile-filters">
          <summary :aria-label="mobileFiltersSummaryLabel">
            <ListFilter :size="14" aria-hidden="true" /> {{ t('feedback.filters.mobileTrigger') }}
            <span v-if="activeFilterCount > 0" class="feedback-mobile-filters-count" aria-hidden="true">{{ activeFilterCount }}</span>
          </summary>
          <div class="feedback-mobile-filters-panel">
            <label>{{ t('feedback.filters.project') }}<select v-model="project"><option value="">{{ t('feedback.filters.allProjects') }}</option><option v-for="item in projectData?.projects" :key="item.id" :value="item.id">{{ item.name }}</option></select></label>
            <label>{{ t('feedback.filters.status') }}<select v-model="feedbackStatus"><option value="">{{ t('feedback.filters.allStatuses') }}</option><option value="open">{{ statusLabel('open') }}</option><option value="archived">{{ statusLabel('archived') }}</option><option value="spam">{{ statusLabel('spam') }}</option></select></label>
            <label>{{ t('feedback.filters.rating') }}<select v-model="rating"><option value="">{{ t('feedback.filters.anyRating') }}</option><option v-for="value in 5" :key="value" :value="String(value)">{{ plural('feedback.filters.star', value, { count: integer(value) }) }}</option></select></label>
            <label>{{ t('feedback.filters.evidence') }}<select v-model="attachment"><option value="">{{ t('feedback.filters.anyEvidence') }}</option><option value="none">{{ t('feedback.filters.noEvidence') }}</option><option value="screenshot">{{ t('feedback.evidenceKinds.screenshot') }}</option><option value="audio">{{ t('feedback.evidenceKinds.audio') }}</option><option value="video">{{ t('feedback.evidenceKinds.video') }}</option><option value="replay">{{ t('feedback.evidenceKinds.replay') }}</option></select></label>
            <label>{{ t('feedback.filters.moderation') }}<select v-model="moderation"><option value="">{{ t('feedback.filters.any') }}</option><option value="flagged">{{ t('feedback.filters.flagged') }}</option><option value="clean">{{ t('feedback.filters.clean') }}</option></select></label>
            <label>{{ t('feedback.filters.issue') }}<select v-model="issue"><option value="">{{ t('feedback.filters.any') }}</option><option value="linked">{{ t('feedback.filters.linked') }}</option><option value="unlinked">{{ t('feedback.filters.unlinked') }}</option></select></label>
            <label>{{ t('feedback.filters.aiStatus') }}<select v-model="processing"><option value="">{{ t('feedback.filters.any') }}</option><option v-for="value in ['failed', 'processing', 'deferred', 'pending', 'completed', 'skipped']" :key="value" :value="value">{{ statusLabel(value) }}</option></select></label>
            <label>{{ t('feedback.filters.sentiment') }}<select v-model="sentiment"><option value="">{{ t('feedback.filters.any') }}</option><option value="negative">{{ t('feedback.sentiment.negative') }}</option><option value="neutral">{{ t('feedback.sentiment.neutral') }}</option><option value="positive">{{ t('feedback.sentiment.positive') }}</option></select></label>
            <label>{{ t('feedback.filters.from') }}<input v-model="dateFrom" type="date" /></label>
            <label>{{ t('feedback.filters.to') }}<input v-model="dateTo" type="date" /></label>
            <div class="feedback-mobile-filters-actions">
              <button type="button" class="feedback-mobile-filters-clear" @click="clearFilters">{{ t('feedback.filters.clear') }}</button>
              <button type="button" class="app-button app-button--primary" @click="closeMobileFilters">{{ t('feedback.filters.done') }}</button>
            </div>
          </div>
        </details>
      </div>

      <FeedbackBulkToolbar
        v-if="selectedIds.length && canWrite('feedback:update')"
        :count="selectedIds.length"
        :selected-items="selectedItems"
        :members="triageMembers"
        :tags="tagData?.tags || []"
        :pending="mutationPending"
        :ai-enabled="aiEnabled"
        :show-a-i-upgrade="Boolean(configuration && !aiEnabled)"
        :issue-actions-available="canWrite('issue:update') && can('issue:read')"
        :can-create-or-link="canCreateOrLinkIssue"
        :can-unlink="canUnlinkIssue"
        :issue-action-reason="issueActionReason"
        @mutate="updateSelectedFeedbacks"
        @reprocess="reprocess([...selectedIds])"
        @create-issue="openCreateIssue"
        @link-issue="openLinkIssue"
        @unlink-issue="unlinkSelectedFeedback"
      />

      <p v-if="mutationError" class="feedback-inline-error" role="alert"><AlertTriangle :size="15" /> {{ mutationError }}</p>
      <div v-if="status === 'pending'" class="feedback-list-state"><span class="app-spinner" /><p>{{ t('feedback.states.loading') }}</p></div>
      <div v-else-if="error" class="feedback-list-state" role="alert"><AlertTriangle :size="22" /><p>{{ apiErrorMessage(error, 'feedback.errors.load') }}</p><button class="app-button" @click="refresh()">{{ t('feedback.actions.tryAgain') }}</button></div>
      <div v-else-if="items.length === 0" class="feedback-list-state"><Inbox :size="22" /><p>{{ t('feedback.states.noMatches') }}</p><button class="app-button" @click="clearFilters">{{ t('feedback.filters.clear') }}</button></div>
      <div v-else class="feedback-stream" role="list">
        <article v-for="item in items" :key="item.id" role="listitem" :data-selected="selected?.id === item.id" @click="selectItem(item)">
          <label v-if="canWrite('feedback:update')" class="feedback-row-check" @click.stop><input type="checkbox" :checked="selectedIds.includes(item.id)" :aria-label="t('feedback.actions.select', { feedback: item.public_id })" @change="toggleSelected(item.id)" /></label>
          <span v-else class="feedback-row-check" aria-hidden="true" />
          <RatingMark :rating="item.rating" compact />
          <div class="feedback-row-copy"><strong>{{ copy(item) }}</strong><span>{{ item.public_id }} · {{ item.project_name }} · {{ item.page_url }}</span></div>
          <div class="feedback-triage-markers">
            <span v-if="item.priority" class="feedback-priority-mark" :data-priority="item.priority">{{ priorityLabel(item.priority) }}</span>
            <span v-if="item.assignee" class="feedback-assignee-mark" :title="item.assignee.name || item.assignee.email"><MemberAvatar :member-id="item.assignee.id" :name="item.assignee.name" :email="item.assignee.email" :avatar-version="item.assignee.avatar_version" /></span>
            <span v-if="item.tags?.length" class="feedback-tag-marks" :title="item.tags.map(tag => tag.name).join(', ')"><i v-for="tag in item.tags.slice(0, 3)" :key="tag.id" :style="{ backgroundColor: tag.color }" /></span>
            <span v-if="!item.priority && !item.assignee && !item.tags?.length" class="muted">{{ t('feedback.common.untriaged') }}</span>
          </div>
          <AttachmentIcons :attachments="item.attachment_kinds" />
          <span class="status-pill" :class="`status-${item.ai_status}`">{{ statusLabel(item.ai_status) }}</span>
          <span v-if="item.issue_public_id" class="issue-label">{{ item.issue_public_id }}</span><span v-else class="muted">{{ t('feedback.common.unlinked') }}</span>
          <time>{{ formatAge(item.created_at) }}</time>
          <NuxtLink class="feedback-mobile-open" :to="`/feedback/${item.id}`" :aria-label="t('feedback.actions.open', { feedback: item.public_id })" @click.stop><ChevronRight :size="17" /></NuxtLink>
        </article>
        <button v-if="hasMore" class="feedback-load-more" type="button" :disabled="loadingMore" @click="loadMore">
          <LoaderCircle v-if="loadingMore" :size="15" /> {{ loadingMore ? t('feedback.states.loadingMore') : t('feedback.states.loadMore') }}
        </button>
      </div>
    </section>

    <aside v-if="selected" class="feedback-inspector">
      <header><div><span class="app-eyebrow">{{ selected.project_name }}</span><h2>{{ selected.public_id }}</h2></div><div><button :title="t('feedback.actions.previous')" :aria-label="t('feedback.actions.previous')" @click="moveSelection(-1)"><ChevronLeft :size="16" /></button><button :title="t('feedback.actions.next')" :aria-label="t('feedback.actions.next')" @click="moveSelection(1)"><ChevronRight :size="16" /></button><NuxtLink :to="`/feedback/${selected.id}`" :title="t('feedback.actions.openDetail')" :aria-label="t('feedback.actions.openDetail')"><ExternalLink :size="16" /></NuxtLink></div></header>
      <div class="feedback-inspector-summary"><RatingMark :rating="selected.rating" /><div><b>{{ selected.page_url }}</b><span>{{ formatAge(selected.created_at) }} · {{ selected.browser || t('feedback.common.unknownBrowser') }}</span></div><span class="status-pill" :class="`status-${selected.status}`">{{ statusLabel(selected.status) }}</span></div>
      <nav :aria-label="t('feedback.inspector.sections')"><button :data-active="inspectorTab === 'feedback'" @click="inspectorTab = 'feedback'">{{ t('feedback.inspector.feedback') }}</button><button :data-active="inspectorTab === 'evidence'" @click="inspectorTab = 'evidence'">{{ t('feedback.inspector.evidence') }}</button><button :data-active="inspectorTab === 'replay'" @click="inspectorTab = 'replay'">{{ t('feedback.inspector.replay') }}</button></nav>
      <div v-if="inspectorTab === 'feedback'" class="feedback-inspector-body">
        <p class="feedback-quote">{{ selected.description || selected.text || t('feedback.common.noWrittenFeedback') }}</p>
        <OperationalTriageControls
          :priority="selected.priority"
          :assignee="selected.assignee"
          :tags="selected.tags || []"
          :tag-options="triageTags"
          :members="triageMembers"
          :editable="canWrite('feedback:update')"
          :pending="mutationPending"
          @set-priority="setSelectedPriority"
          @set-assignee="setSelectedAssignee"
          @toggle-tag="toggleSelectedTag"
        />
        <dl><div><dt>{{ t('feedback.common.reporter') }}</dt><dd>{{ selected.reporter_email || t('feedback.common.anonymous') }}</dd></div><div><dt>{{ t('feedback.common.sentiment') }}</dt><dd><SentimentBadge :score="selected.sentiment_score" /></dd></div><div><dt>{{ t('feedback.common.issue') }}</dt><dd>{{ selected.issue_public_id || t('feedback.common.notLinked') }}</dd></div><div><dt>{{ t('feedback.common.ai') }}</dt><dd><span class="status-pill" :class="`status-${selected.ai_status}`">{{ statusLabel(selected.ai_status) }}</span></dd></div></dl>
      </div>
      <div v-else-if="inspectorTab === 'evidence'" class="feedback-inspector-body feedback-inspector-evidence"><AttachmentIcons :attachments="selected.attachment_kinds" /><p>{{ selected.attachment_kinds.length ? t('feedback.inspector.evidenceAvailable') : t('feedback.inspector.evidenceEmpty') }}</p></div>
      <div v-else class="feedback-inspector-body feedback-inspector-evidence"><PanelRightOpen :size="24" /><p>{{ selected.has_replay ? t('feedback.inspector.replayAvailable') : t('feedback.inspector.replayEmpty') }}</p></div>
      <footer><template v-if="canWrite('feedback:update')"><button v-if="selected.status !== 'open'" class="app-button" :disabled="mutationPending" @click="updateStatus([selected.id], 'open')">{{ t('feedback.actions.reopen') }}</button><button v-else class="app-button" :disabled="mutationPending" @click="updateStatus([selected.id], 'archived')"><Archive :size="15" /> {{ t('feedback.actions.archive') }}</button></template><NuxtLink class="app-button app-button--primary" :to="selected.issue_id ? `/issues/${selected.issue_id}` : `/feedback/${selected.id}`"><Link2 :size="15" /> {{ selected.issue_id ? t('feedback.actions.openIssue') : t('feedback.actions.triage') }}</NuxtLink></footer>
    </aside>

    <AppDialog :open="createIssueOpen" aria-labelledby="bulk-create-issue-title" @close="createIssueOpen = false">
      <form class="feedback-issue-dialog app-modal" @submit.prevent="createIssueFromSelection">
        <header><div><p class="app-eyebrow">{{ t('feedback.dialogs.createEyebrow') }}</p><h2 id="bulk-create-issue-title">{{ plural('feedback.dialogs.createMany', selectedIds.length, { count: integer(selectedIds.length) }) }}</h2></div><button type="button" class="app-icon-button" :title="t('feedback.actions.close')" :aria-label="t('feedback.actions.close')" @click="createIssueOpen = false"><X :size="16" /></button></header>
        <div class="feedback-issue-dialog-body app-modal__body">
          <label><span>{{ t('feedback.dialogs.title') }}</span><input v-model="issueTitle" class="app-input" :aria-label="t('feedback.dialogs.issueTitle')" required maxlength="80" autofocus /></label>
          <label><span>{{ t('feedback.dialogs.description') }}</span><textarea v-model="issueDescription" class="app-input" :aria-label="t('feedback.dialogs.issueDescription')" rows="6" maxlength="10000" /></label>
          <p v-if="mutationError" class="feedback-dialog-error" role="alert">{{ mutationError }}</p>
        </div>
        <footer><button type="button" class="app-button" :disabled="mutationPending" @click="createIssueOpen = false">{{ t('feedback.actions.cancel') }}</button><button class="app-button app-button--primary" :disabled="mutationPending || !issueTitle.trim()" type="submit"><Plus :size="14" /> {{ mutationPending ? t('feedback.actions.creating') : t('feedback.actions.createIssue') }}</button></footer>
      </form>
    </AppDialog>

    <AppDialog :open="linkIssueOpen" aria-labelledby="bulk-link-issue-title" @close="linkIssueOpen = false">
      <form class="feedback-issue-dialog feedback-link-dialog app-modal" @submit.prevent="linkSelectedFeedback">
        <header><div><p class="app-eyebrow">{{ t('feedback.dialogs.linkEyebrow') }}</p><h2 id="bulk-link-issue-title">{{ plural('feedback.dialogs.linkMany', selectedIds.length, { count: integer(selectedIds.length) }) }}</h2></div><button type="button" class="app-icon-button" :title="t('feedback.actions.close')" :aria-label="t('feedback.actions.close')" @click="linkIssueOpen = false"><X :size="16" /></button></header>
        <div class="feedback-issue-dialog-body app-modal__body">
          <label><span>{{ t('feedback.common.issue') }}</span><select v-model="linkTargetID" class="app-select" :aria-label="t('feedback.common.issue')" required><option value="">{{ t('feedback.dialogs.chooseIssue') }}</option><option v-for="issueOption in linkIssueOptions" :key="issueOption.id" :value="issueOption.id">{{ issueOption.public_id }} · {{ issueOption.title }}</option></select></label>
          <p v-if="linkIssueOptions.length === 0" class="feedback-dialog-note">{{ t('feedback.dialogs.noIssue') }}</p>
          <p v-if="mutationError" class="feedback-dialog-error" role="alert">{{ mutationError }}</p>
        </div>
        <footer><button type="button" class="app-button" :disabled="mutationPending" @click="linkIssueOpen = false">{{ t('feedback.actions.cancel') }}</button><button class="app-button app-button--primary" :disabled="mutationPending || !linkTargetID" type="submit"><Link2 :size="14" /> {{ mutationPending ? t('feedback.actions.linking') : t('feedback.actions.linkFeedback') }}</button></footer>
      </form>
    </AppDialog>

    <p v-if="notice" class="feedback-toast" role="status">{{ notice }}</p>
  </div>
</template>

<style scoped>
.feedback-workspace { display: grid; min-height: calc(100vh - 56px); grid-template-columns: minmax(520px, 1fr) minmax(330px, 36%); background: var(--surface); }
.feedback-stream-panel { min-width: 0; border-right: 1px solid var(--line); }
.feedback-stream-head { display: flex; min-height: 84px; align-items: flex-end; justify-content: space-between; gap: 18px; padding: 18px 20px 14px; border-bottom: 1px solid var(--line); }
.feedback-stream-head h1 { margin: 2px 0; font-size: 20px; }
.feedback-stream-head div > span { color: var(--muted); font-size: 10px; }
.feedback-filter-bar { position: sticky; top: 56px; z-index: 30; display: flex; min-height: 54px; align-items: center; gap: 7px; padding: 8px 12px; background: var(--surface-subtle); border-bottom: 1px solid var(--line); }
.feedback-filter-bar-desktop { display: contents; }
.feedback-filter-bar select,
.feedback-filter-bar input { height: 34px; min-width: 0; padding: 0 8px; background: var(--surface); border: 1px solid var(--line); border-radius: 4px; color: var(--ink-soft); font-size: 10px; }
.feedback-search { display: flex; width: min(190px, 26%); height: 34px; align-items: center; gap: 6px; padding: 0 8px; background: var(--surface); border: 1px solid var(--line); border-radius: 4px; color: var(--muted); }
.feedback-search input { width: 100%; height: auto; padding: 0; border: 0; outline: 0; }
.feedback-more-filters { position: relative; }
.feedback-more-filters summary { display: flex; height: 34px; align-items: center; padding: 0 9px; background: var(--surface); border: 1px solid var(--line); border-radius: 4px; color: var(--ink-soft); cursor: pointer; font-size: 10px; font-weight: 700; list-style: none; white-space: nowrap; }
.feedback-more-filters summary::-webkit-details-marker { display: none; }
.feedback-more-filters > div { position: absolute; top: 40px; right: 0; z-index: 60; display: grid; width: 360px; grid-template-columns: 1fr 1fr; gap: 10px; padding: 14px; background: var(--surface); border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.feedback-more-filters label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.feedback-more-filters label select,
.feedback-more-filters label input { width: 100%; }
.feedback-more-filters button { min-height: 34px; grid-column: 1 / -1; background: var(--surface-soft); border: 0; border-radius: 4px; color: var(--teal); font-size: 10px; font-weight: 750; }
.feedback-mobile-filters { display: none; }
.feedback-mobile-filters summary { display: flex; align-items: center; gap: 6px; height: 34px; padding: 0 10px; background: var(--surface); border: 1px solid var(--line); border-radius: 4px; color: var(--ink-soft); cursor: pointer; font-size: 10px; font-weight: 700; list-style: none; white-space: nowrap; }
.feedback-mobile-filters summary::-webkit-details-marker { display: none; }
.feedback-mobile-filters summary:focus { outline: 2px solid var(--teal); outline-offset: 2px; }
.feedback-mobile-filters-count { display: inline-flex; min-width: 15px; height: 15px; align-items: center; justify-content: center; padding: 0 4px; background: var(--teal-fill); color: var(--on-accent); border-radius: 8px; font-size: 9px; font-weight: 800; }
.feedback-mobile-filters-panel { position: fixed; z-index: 60; top: 108px; right: 10px; left: 10px; display: grid; max-height: calc(100dvh - 120px); grid-template-columns: 1fr 1fr; align-content: start; gap: 10px; overflow-y: auto; padding: 14px; background: var(--surface); border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.feedback-mobile-filters-panel label { display: flex; flex-direction: column; gap: 5px; color: var(--muted); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.feedback-mobile-filters-panel label select,
.feedback-mobile-filters-panel label input { width: 100%; }
.feedback-mobile-filters-actions { display: flex; grid-column: 1 / -1; gap: 8px; }
.feedback-mobile-filters-clear { flex: 1; min-height: 34px; background: var(--surface-soft); border: 0; border-radius: 4px; color: var(--teal); font-size: 10px; font-weight: 750; }
.feedback-mobile-filters-actions .app-button--primary { flex: 1; }
.feedback-inline-error { display: flex; align-items: center; gap: 7px; margin: 0; padding: 9px 12px; background: var(--red-soft); color: var(--red); border-bottom: 1px solid var(--red-line); font-size: 11px; }
.feedback-list-state { display: flex; min-height: 330px; align-items: center; justify-content: center; flex-direction: column; gap: 10px; color: var(--muted); font-size: 11px; text-align: center; }
.feedback-stream article { display: grid; min-height: 58px; grid-template-columns: 24px 36px minmax(190px, 1fr) 82px 74px 94px 78px 36px 22px; align-items: center; gap: 8px; padding: 0 10px; border-bottom: 1px solid var(--line); cursor: pointer; }
.feedback-stream article:hover { background: var(--surface-subtle); }
.feedback-stream article[data-selected='true'] { background: var(--teal-soft); box-shadow: inset 3px 0 var(--teal); }
.feedback-row-check { display: grid; place-items: center; }
.feedback-row-copy { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.feedback-row-copy strong,
.feedback-row-copy span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.feedback-row-copy strong { font-size: 11px; }
.feedback-row-copy span { color: var(--muted); font-size: 9px; }
.feedback-triage-markers { display: flex; min-width: 0; height: 28px; align-items: center; gap: 4px; overflow: hidden; }
.feedback-priority-mark { flex: 0 0 auto; padding: 3px 5px; color: var(--ink-soft); background: var(--teal-soft); border-radius: 3px; font-size: 8px; font-weight: 800; text-transform: uppercase; }
.feedback-priority-mark[data-priority='high'] { color: var(--amber); background: var(--amber-soft); }
.feedback-priority-mark[data-priority='urgent'] { color: var(--red); background: var(--red-soft); }
.feedback-assignee-mark { display: block; width: 23px; height: 23px; flex: 0 0 23px; overflow: hidden; border: 1px solid var(--teal-line); border-radius: 50%; font-size: 8px; }
.feedback-tag-marks { display: flex; min-width: 10px; gap: 2px; }
.feedback-tag-marks i { width: 6px; height: 18px; flex: 0 0 6px; border-radius: 2px; }
.feedback-triage-markers > .muted { overflow: hidden; font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }
.feedback-stream article > .status-pill,
.feedback-stream article > .issue-label,
.feedback-stream article > .muted { min-width: 0; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.feedback-stream article > .status-pill { justify-self: start; overflow: visible; text-overflow: clip; }
.feedback-stream time { color: var(--muted); font-size: 9px; font-variant-numeric: tabular-nums; text-align: right; }
.feedback-mobile-open { display: grid; width: 26px; height: 30px; place-items: center; color: var(--muted); border-radius: 4px; }
.feedback-load-more { display: flex; width: 100%; min-height: 44px; align-items: center; justify-content: center; gap: 7px; background: var(--surface-subtle); color: var(--teal); border: 0; border-top: 1px solid var(--line); font-size: 11px; font-weight: 750; }
.feedback-load-more svg { animation: app-spin 700ms linear infinite; }

.feedback-inspector { position: sticky; top: 56px; display: flex; height: calc(100vh - 56px); min-width: 0; flex-direction: column; background: var(--surface); }
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
.feedback-issue-dialog { width: min(580px, calc(100vw - 32px)); }
.feedback-issue-dialog textarea { min-height: 130px; padding: 10px; resize: vertical; font: inherit; font-size: 11px; line-height: 1.5; }
.feedback-issue-dialog select { width: 100%; }
.feedback-dialog-error { margin: 0; padding: 9px 10px; background: var(--red-soft); color: var(--red); border: 1px solid var(--red-line); border-radius: 4px; font-size: 10px; }
.feedback-dialog-note { margin: 0; color: var(--muted); font-size: 10px; }

@media (max-width: 1240px) {
  .feedback-workspace { grid-template-columns: minmax(480px, 1fr) 320px; }
  .feedback-stream article { grid-template-columns: 22px 36px minmax(140px, 1fr) 70px 94px 34px 20px; }
  .feedback-stream article :deep(.attachment-icons) { display: none; }
  .feedback-stream article .issue-label,
  .feedback-stream article > .muted { display: none; }
}

@media (max-width: 1100px) {
  .feedback-workspace { display: block; min-height: 0; }
  .feedback-stream-panel { border: 0; }
  .feedback-inspector { display: none; }
  .feedback-filter-bar { overflow-x: auto; }
  .feedback-filter-bar > *,
  .feedback-filter-bar-desktop > * { flex: 0 0 auto; }
  .feedback-search { width: 180px; }
}

@media (max-width: 700px) {
  .feedback-stream-head { min-height: 72px; padding: 12px 14px; }
  .feedback-filter-bar { top: 56px; padding: 7px 10px; overflow-x: visible; }
  .feedback-filter-bar-desktop { display: none; }
  .feedback-mobile-filters { display: block; }
  .feedback-search { width: auto; flex: 1 1 120px; }
  .feedback-stream article { grid-template-columns: 22px 34px minmax(0, 1fr) 34px 20px; gap: 7px; padding: 0 8px; }
  .feedback-stream article .feedback-triage-markers,
  .feedback-stream article .status-pill,
  .feedback-stream article time { display: none; }
}

@media (max-width: 360px) {
  .feedback-mobile-filters-panel { grid-template-columns: 1fr; }
}
</style>
