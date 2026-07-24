<script setup lang="ts">
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock3,
  Download,
  FileArchive,
  LockKeyhole,
  ListTree,
  MailCheck,
  MessageSquareText,
  MoveRight,
  PencilLine,
  Play,
  RefreshCw,
  Send,
  ShieldAlert,
  ShieldCheck,
  UnlockKeyhole,
  X,
} from '@lucide/vue'
import type {
  EffectiveRetention,
  Member,
  PrivacyCase,
  PrivacyCaseCommunication,
  PrivacyCaseCommunicationKind,
  PrivacyCaseDashboard,
  PrivacyCaseEvent,
  PrivacyCaseRectification,
  PrivacyCaseStatus,
  PrivacyControllerRole,
  PrivacyExport,
  PrivacyFeedbackRectification,
  PrivacyIntegrationAction,
  PrivacyPortabilityDataCategory,
  PrivacyPortabilityDecision,
  PrivacyRecipientAction,
  PrivacyRightType,
  PrivacySubjectType,
  RectifiableFeedback,
  ReporterPrivacyAction,
  ReporterPrivacyRequest,
  RetentionHealth,
  RetentionOperationHealth,
  RetentionPolicy,
  ReviewDecisionStatus,
  ReviewItem,
} from '~/types/api'
import { triggerSignedDownload } from '~/utils/download'

const { $apiFetch } = useNuxtApp()
const { can, canWrite } = useAuth()
const { t } = useI18n()
const localizedError = useLocalizedAPIError()
const { dateTime, decimal, plural } = useRegionalFormat()
const props = defineProps<{ members: Member[] }>()
const emit = defineEmits<{ memberChanged: [member: Member] }>()
const canReadRetention = computed(() => can('tenant:read'))
const canManagePrivacy = computed(() => can('tenant:update'))

const { data, status, error, refresh: refreshData } = await useAsyncData('privacy-settings-data', async () => {
  const retention = canReadRetention.value
    ? $apiFetch<{ retention: EffectiveRetention, health: RetentionHealth }>('/v1/privacy/retention')
    : Promise.resolve<{ retention?: EffectiveRetention, health?: RetentionHealth }>({})
  const exports = canManagePrivacy.value
    ? $apiFetch<{ exports: PrivacyExport[] }>('/v1/privacy/exports')
    : Promise.resolve({ exports: [] as PrivacyExport[] })
  const requests = canManagePrivacy.value
    ? $apiFetch<{ requests: ReporterPrivacyRequest[] }>('/v1/privacy/requests')
    : Promise.resolve({ requests: [] as ReporterPrivacyRequest[] })
  const cases = canManagePrivacy.value
    ? $apiFetch<{ cases: PrivacyCase[] }>('/v1/privacy/cases')
    : Promise.resolve({ cases: [] as PrivacyCase[] })
  const dashboard = canManagePrivacy.value
    ? $apiFetch<{ dashboard: PrivacyCaseDashboard }>('/v1/privacy/cases/dashboard')
    : Promise.resolve({ dashboard: {
        overdue: 0, due_soon: 0, awaiting_manual: 0,
        awaiting_legal_review: 0, failed_external: 0, completed: 0,
      } })
  const [retentionResult, exportsResult, requestsResult, casesResult, dashboardResult] = await Promise.all([
    retention, exports, requests, cases, dashboard,
  ])
  return {
    retention: retentionResult.retention,
    retentionHealth: retentionResult.health,
    exports: exportsResult.exports,
    requests: requestsResult.requests,
    cases: casesResult.cases,
    dashboard: dashboardResult.dashboard,
  }
})

const actionPending = ref('')
const actionError = ref('')
const actionNotice = ref('')
const reporterEmail = ref('')
const reporterAction = ref<ReporterPrivacyAction>('export')
const expandedRetentionCategories = ref(new Set<string>())
const anonymizeDialogOpen = ref(false)
const anonymizeConfirmation = ref('')
const caseSubjectType = ref<PrivacySubjectType>('reporter')
const caseRightType = ref<PrivacyRightType>('access')
const caseControllerRole = ref<PrivacyControllerRole>('customer_controller_assistance')
const caseReporterEmail = ref('')
const caseSubjectMemberID = ref('')
const caseOwnerMemberID = ref('')
const expandedCases = ref<Record<string, boolean>>({})
const holdReasons = ref<Record<string, string>>({})
const holdReferences = ref<Record<string, string>>({})
const releaseReasons = ref<Record<string, string>>({})
const portabilityDecisions = ref<Record<string, PrivacyPortabilityDecision>>({})
const portabilityReasons = ref<Record<string, string>>({})
const portabilitySelections = ref<Record<string, PrivacyPortabilityDataCategory[]>>({})
type MemberRectificationField = 'name' | 'locale' | 'timezone'
type MemberRectificationDraft = {
  selected: Record<MemberRectificationField, boolean>
  values: Record<MemberRectificationField, string>
  reason: string
}
const rectificationFields: MemberRectificationField[] = ['name', 'locale', 'timezone']
const rectificationDrafts = ref<Record<string, MemberRectificationDraft>>({})
type FeedbackRectificationDraft = {
  feedbackID: string
  description: string
  reason: string
}
const rectifiableFeedbacks = ref<Record<string, RectifiableFeedback[]>>({})
const feedbackRectificationDrafts = ref<Record<string, FeedbackRectificationDraft>>({})
const rectifiableFeedbackLoading = ref<Record<string, boolean>>({})
type RecipientActionDraft = {
  recipientName: string
  recipientEmail: string
  instructionSummary: string
  integrationActionID: string
}
const recipientActionDrafts = ref<Record<string, RecipientActionDraft>>({})
const integrationActionNotes = ref<Record<string, string>>({})
type CaseCommunicationDraft = {
  kind: PrivacyCaseCommunicationKind
  subjectLine: string
  messageBody: string
}
const caseCommunicationDrafts = ref<Record<string, CaseCommunicationDraft>>({})
const caseDetailLoading = ref<Record<string, boolean>>({})
type CaseDecisionDraft = {
  status: PrivacyCaseStatus
  decisionSummary: string
  legalReviewNotes: string
  exceptionReason: string
}
const caseDecisionDrafts = ref<Record<string, CaseDecisionDraft>>({})
const portabilityCategoryOptions: PrivacyPortabilityDataCategory[] = [
  'feedback',
  'issues',
  'attachments',
  'replay',
  'ai_processing',
  'consent',
  'delivery',
  'widget_events',
]

const activeMembers = computed(() => props.members.filter(member => member.status === 'active'))

async function refresh() {
  await refreshData()
  const openCaseIDs = Object.entries(expandedCases.value)
    .filter(([, expanded]) => expanded)
    .map(([caseID]) => caseID)
  await Promise.all(openCaseIDs.map(async (caseID) => {
    const item = data.value?.cases.find(candidate => candidate.id === caseID)
    if (item) await loadCaseDetail(item)
  }))
}

function memberLabel(memberID?: string) {
  if (!memberID) return '—'
  const member = props.members.find(item => item.id === memberID)
  return member?.name || member?.email || memberID.slice(0, 8)
}

function controllerRoleLabel(role: PrivacyControllerRole) {
  return t(role === 'controller'
    ? 'admin.privacy.settings.cases.controller'
    : 'admin.privacy.settings.cases.assistance')
}

async function createPrivacyCase() {
  if (!canWrite('tenant:update')) return
  const subjectEmail = caseReporterEmail.value.trim()
  if (caseSubjectType.value === 'reporter' && !subjectEmail) return
  if (caseSubjectType.value === 'member' && !caseSubjectMemberID.value) return
  await runAction('create-case', async () => {
    await $apiFetch('/v1/privacy/cases', {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: {
        subject_type: caseSubjectType.value,
        right_type: caseRightType.value,
        controller_role: caseControllerRole.value,
        subject_email: caseSubjectType.value === 'reporter' ? subjectEmail : '',
        subject_member_id: caseSubjectType.value === 'member' ? caseSubjectMemberID.value : '',
        owner_member_id: caseOwnerMemberID.value,
        scope: { intake_channel: 'backoffice' },
      },
    })
    actionNotice.value = caseSubjectType.value === 'reporter'
      ? t('admin.privacy.settings.cases.verificationQueued', { email: subjectEmail })
      : t('admin.privacy.settings.cases.created')
    caseReporterEmail.value = ''
    caseSubjectMemberID.value = ''
    await refresh()
  })
}

function activeCaseHolds(item: PrivacyCase) {
  return item.legal_holds.filter(hold => hold.active)
}

function caseIsTerminal(item: PrivacyCase) {
  return ['partially_fulfilled', 'completed', 'refused', 'cancelled'].includes(item.status)
}

function erasureExecutionSettled(item: PrivacyCase) {
  return item.execution?.request_status === 'completed'
    && item.execution.job_status === 'completed'
    && item.execution.erasure_review != null
    && item.execution.erasure_review.pending === 0
}

function caseTransitionOptions(item: PrivacyCase): PrivacyCaseStatus[] {
  if (caseIsTerminal(item) || item.status === 'pending_verification') return []
  let options: PrivacyCaseStatus[]
  if (item.status === 'received') {
    options = ['in_progress']
  } else if (item.status === 'in_progress') {
    options = [
      'awaiting_clarification',
      'paused',
      'awaiting_legal_review',
      'completed',
      'partially_fulfilled',
      'refused',
      'cancelled',
    ]
  } else {
    options = ['in_progress', 'completed', 'partially_fulfilled', 'refused', 'cancelled']
  }
  if (item.right_type !== 'erasure') return options
  const review = item.execution?.erasure_review
  const settled = erasureExecutionSettled(item)
  return options.filter((status) => {
    if (status === 'refused') return !item.execution
    if (status === 'completed') {
      return settled && review?.retained_exception === 0 && activeCaseHolds(item).length === 0
    }
    if (status === 'partially_fulfilled') {
      return settled && Boolean(review?.retained_exception || activeCaseHolds(item).length)
    }
    return true
  })
}

function ensureCaseDecisionDraft(item: PrivacyCase) {
  if (caseDecisionDrafts.value[item.id]) return
  const options = caseTransitionOptions(item)
  caseDecisionDrafts.value = {
    ...caseDecisionDrafts.value,
    [item.id]: {
      status: options[0] || 'in_progress',
      decisionSummary: item.decision_summary || '',
      legalReviewNotes: item.legal_review_notes || '',
      exceptionReason: item.exception_reason || '',
    },
  }
}

function caseDecisionNeedsReason(item: PrivacyCase) {
  const status = caseDecisionDrafts.value[item.id]?.status
  return status === 'partially_fulfilled' || status === 'refused'
}

function caseDecisionReady(item: PrivacyCase) {
  const draft = caseDecisionDrafts.value[item.id]
  if (!draft || !caseTransitionOptions(item).includes(draft.status)) return false
  if (caseDecisionNeedsReason(item) && !draft.decisionSummary.trim()) return false
  if (item.right_type === 'erasure' && caseDecisionNeedsReason(item)) {
    return Boolean(draft.legalReviewNotes.trim() && draft.exceptionReason.trim())
  }
  return true
}

async function transitionCase(item: PrivacyCase) {
  if (!canWrite('tenant:update') || !caseDecisionReady(item)) return
  const draft = caseDecisionDrafts.value[item.id]
  await runAction(`transition-case:${item.id}`, async () => {
    await $apiFetch(`/v1/privacy/cases/${item.id}/transition`, {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: {
        status: draft.status,
        decision_summary: draft.decisionSummary.trim(),
        legal_review_notes: draft.legalReviewNotes.trim(),
        exception_reason: draft.exceptionReason.trim(),
      },
    })
    actionNotice.value = t('admin.privacy.settings.cases.decisionRecorded')
    await refresh()
  })
}

function caseCanExecute(item: PrivacyCase) {
  const portabilityApproved = item.right_type !== 'portability_assessment'
    || ['approved', 'partially_approved'].includes(item.portability_assessment?.decision || '')
  const supportedSubject = item.subject_type === 'reporter'
    || (item.subject_type === 'member' && item.right_type === 'access')
  return supportedSubject
    && Boolean(item.verified_at)
    && !item.execution
    && ['access', 'erasure', 'portability_assessment'].includes(item.right_type)
    && portabilityApproved
    && !['partially_fulfilled', 'completed', 'refused', 'cancelled'].includes(item.status)
}

async function toggleCase(item: PrivacyCase) {
  const opening = !expandedCases.value[item.id]
  expandedCases.value = {
    ...expandedCases.value,
    [item.id]: opening,
  }
  if (!opening) return
  await loadCaseDetail(item)
  ensureCaseDecisionDraft(item)
  ensureCaseCommunicationDraft(item)
  if (item.right_type === 'portability_assessment' && !portabilityDecisions.value[item.id]) {
    portabilityDecisions.value = { ...portabilityDecisions.value, [item.id]: 'approved' }
    portabilitySelections.value = {
      ...portabilitySelections.value,
      [item.id]: [...portabilityCategoryOptions],
    }
  }
  if (item.right_type === 'rectification' && item.subject_type === 'member') {
    ensureRectificationDraft(item)
  }
  if (caseCanRectifyReporter(item) && !rectifiableFeedbacks.value[item.id]) {
    await loadRectifiableFeedbacks(item)
  }
  if (item.right_type === 'rectification' || item.right_type === 'erasure') {
    ensureRecipientActionDraft(item)
  }
}

async function loadCaseDetail(item: PrivacyCase) {
  caseDetailLoading.value = { ...caseDetailLoading.value, [item.id]: true }
  try {
    const response = await $apiFetch<{ case: PrivacyCase, events: PrivacyCaseEvent[] }>(
      `/v1/privacy/cases/${item.id}`,
    )
    Object.assign(item, response.case, { events: response.events })
  } catch (operationError) {
    actionError.value = localizedError.message(operationError, 'admin.privacy.settings.error')
  } finally {
    caseDetailLoading.value = { ...caseDetailLoading.value, [item.id]: false }
  }
}

function caseCommunicationKinds(item: PrivacyCase): PrivacyCaseCommunicationKind[] {
  if (!item.verified_at) return []
  if (caseIsTerminal(item)) return ['decision_notice']
  if (item.status === 'awaiting_clarification') {
    return ['clarification_request', 'progress_update']
  }
  return ['progress_update']
}

function ensureCaseCommunicationDraft(item: PrivacyCase) {
  const kinds = caseCommunicationKinds(item)
  const existing = caseCommunicationDrafts.value[item.id]
  if (existing && kinds.includes(existing.kind)) return
  const kind = kinds[0] || 'progress_update'
  caseCommunicationDrafts.value = {
    ...caseCommunicationDrafts.value,
    [item.id]: {
      kind,
      subjectLine: t(`admin.privacy.settings.cases.communicationSubjects.${kind}`, {
        case: item.public_id,
      }),
      messageBody: '',
    },
  }
}

function updateCaseCommunicationSubject(item: PrivacyCase) {
  const draft = caseCommunicationDrafts.value[item.id]
  if (!draft) return
  draft.subjectLine = t(`admin.privacy.settings.cases.communicationSubjects.${draft.kind}`, {
    case: item.public_id,
  })
}

function caseCommunicationReady(item: PrivacyCase) {
  const draft = caseCommunicationDrafts.value[item.id]
  return Boolean(
    draft
    && caseCommunicationKinds(item).includes(draft.kind)
    && draft.subjectLine.trim()
    && draft.messageBody.trim(),
  )
}

async function createCaseCommunication(item: PrivacyCase) {
  if (!canWrite('tenant:update') || !caseCommunicationReady(item)) return
  const draft = caseCommunicationDrafts.value[item.id]
  await runAction(`case-communication:${item.id}`, async () => {
    await $apiFetch<{ communication: PrivacyCaseCommunication }>(
      `/v1/privacy/cases/${item.id}/communications`, {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
        body: {
          communication_kind: draft.kind,
          subject_line: draft.subjectLine.trim(),
          message_body: draft.messageBody.trim(),
        },
      },
    )
    draft.messageBody = ''
    actionNotice.value = t('admin.privacy.settings.cases.communicationQueued')
    await refresh()
  })
}

async function retryCaseCommunication(item: PrivacyCase, communication: PrivacyCaseCommunication) {
  if (!canWrite('tenant:update') || communication.status !== 'failed') return
  await runAction(`case-communication-retry:${communication.id}`, async () => {
    await $apiFetch(
      `/v1/privacy/cases/${item.id}/communications/${communication.id}/retry`, {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
      },
    )
    actionNotice.value = t('admin.privacy.settings.cases.communicationRetryQueued')
    await refresh()
  })
}

function caseScopeEntries(item: PrivacyCase) {
  return Object.entries(item.scope || {}).sort(([left], [right]) => left.localeCompare(right))
}

function caseScopeValue(value: unknown) {
  if (value == null || value === '') return '—'
  return typeof value === 'string' ? value : JSON.stringify(value)
}

function caseEventLabel(event: PrivacyCaseEvent) {
  const key = event.event_type.replace(/^privacy_case\./, '')
  return t(`admin.privacy.settings.cases.timelineEvents.${key}`, event.event_type)
}

function ensureRectificationDraft(item: PrivacyCase) {
  if (rectificationDrafts.value[item.id]) return
  const member = props.members.find(candidate => candidate.id === item.subject_member_id)
  rectificationDrafts.value = {
    ...rectificationDrafts.value,
    [item.id]: {
      selected: { name: false, locale: false, timezone: false },
      values: {
        name: member?.name || '',
        locale: member?.locale || '',
        timezone: member?.timezone || '',
      },
      reason: '',
    },
  }
}

function caseCanRectify(item: PrivacyCase) {
  return item.subject_type === 'member'
    && item.right_type === 'rectification'
    && Boolean(item.verified_at)
    && !['partially_fulfilled', 'completed', 'refused', 'cancelled'].includes(item.status)
}

function caseCanRectifyReporter(item: PrivacyCase) {
  return item.subject_type === 'reporter'
    && item.right_type === 'rectification'
    && Boolean(item.verified_at)
    && !['partially_fulfilled', 'completed', 'refused', 'cancelled'].includes(item.status)
}

async function loadRectifiableFeedbacks(item: PrivacyCase) {
  rectifiableFeedbackLoading.value = {
    ...rectifiableFeedbackLoading.value,
    [item.id]: true,
  }
  try {
    const response = await $apiFetch<{ feedbacks: RectifiableFeedback[] }>(
      `/v1/privacy/cases/${item.id}/rectifiable-feedbacks`,
    )
    rectifiableFeedbacks.value = {
      ...rectifiableFeedbacks.value,
      [item.id]: response.feedbacks,
    }
    const selected = response.feedbacks[0]
    feedbackRectificationDrafts.value = {
      ...feedbackRectificationDrafts.value,
      [item.id]: {
        feedbackID: selected?.id || '',
        description: selected?.description || '',
        reason: '',
      },
    }
  } catch (operationError) {
    actionError.value = localizedError.message(operationError, 'admin.privacy.settings.error')
  } finally {
    rectifiableFeedbackLoading.value = {
      ...rectifiableFeedbackLoading.value,
      [item.id]: false,
    }
  }
}

function selectRectifiableFeedback(item: PrivacyCase) {
  const draft = feedbackRectificationDrafts.value[item.id]
  const selected = (rectifiableFeedbacks.value[item.id] || [])
    .find(feedback => feedback.id === draft?.feedbackID)
  if (draft) draft.description = selected?.description || ''
}

function feedbackRectificationReady(item: PrivacyCase) {
  const draft = feedbackRectificationDrafts.value[item.id]
  const selected = (rectifiableFeedbacks.value[item.id] || [])
    .find(feedback => feedback.id === draft?.feedbackID)
  return caseCanRectifyReporter(item)
    && Boolean(draft?.feedbackID)
    && Boolean(draft?.reason.trim())
    && Boolean(draft?.description.trim())
    && draft?.description !== (selected?.description || '')
}

function rectificationReady(item: PrivacyCase) {
  const draft = rectificationDrafts.value[item.id]
  return caseCanRectify(item)
    && Boolean(draft?.reason.trim())
    && rectificationFields.some(field => draft?.selected[field])
}

function rectificationValue(value: string | null | undefined) {
  return value || t('admin.privacy.settings.cases.rectificationCleared')
}

function rectificationChangedFields(rectification: PrivacyCaseRectification) {
  return rectificationFields.filter(field =>
    Object.prototype.hasOwnProperty.call(rectification.changes, field),
  )
}

async function applyMemberRectification(item: PrivacyCase) {
  if (!canWrite('tenant:update') || !rectificationReady(item)) return
  const draft = rectificationDrafts.value[item.id]
  const changes: Partial<Record<MemberRectificationField, string | null>> = {}
  for (const field of rectificationFields) {
    if (!draft.selected[field]) continue
    const value = draft.values[field].trim()
    changes[field] = value || null
  }
  await runAction(`rectification:${item.id}`, async () => {
    const response = await $apiFetch<{ rectification: PrivacyCaseRectification }>(
      `/v1/privacy/cases/${item.id}/rectifications`, {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
        body: { changes, reason: draft.reason.trim() },
      },
    )
    const member = props.members.find(candidate => candidate.id === item.subject_member_id)
    if (member) {
      const updated = { ...member }
      for (const field of rectificationChangedFields(response.rectification)) {
        updated[field] = response.rectification.changes[field] ?? null
      }
      emit('memberChanged', updated)
    }
    draft.selected = { name: false, locale: false, timezone: false }
    draft.reason = ''
    actionNotice.value = t('admin.privacy.settings.cases.rectificationApplied')
    await refresh()
  })
}

async function applyFeedbackRectification(item: PrivacyCase) {
  if (!canWrite('tenant:update') || !feedbackRectificationReady(item)) return
  const draft = feedbackRectificationDrafts.value[item.id]
  await runAction(`feedback-rectification:${item.id}`, async () => {
    await $apiFetch<{ feedback_rectification: PrivacyFeedbackRectification }>(
      `/v1/privacy/cases/${item.id}/feedback-rectifications`, {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
        body: {
          feedback_id: draft.feedbackID,
          description: draft.description,
          reason: draft.reason.trim(),
        },
      },
    )
    actionNotice.value = t('admin.privacy.settings.cases.feedbackRectificationApplied')
    await Promise.all([refresh(), loadRectifiableFeedbacks(item)])
  })
}

function caseRecipientActionEligible(item: PrivacyCase) {
  if (!item.verified_at) return false
  if (item.right_type === 'rectification') {
    return Boolean(item.rectifications?.length || item.feedback_rectifications?.length)
  }
  return item.right_type === 'erasure'
    && ['completed', 'partially_fulfilled'].includes(item.status)
    && erasureExecutionSettled(item)
}

function ensureRecipientActionDraft(item: PrivacyCase) {
  if (recipientActionDrafts.value[item.id]) return
  recipientActionDrafts.value = {
    ...recipientActionDrafts.value,
    [item.id]: {
      recipientName: '',
      recipientEmail: '',
      instructionSummary: '',
      integrationActionID: '',
    },
  }
}

function recipientActionReady(item: PrivacyCase) {
  const draft = recipientActionDrafts.value[item.id]
  return caseRecipientActionEligible(item)
    && Boolean(draft?.recipientName.trim())
    && Boolean(draft?.recipientEmail.trim())
    && Boolean(draft?.instructionSummary.trim())
}

async function createRecipientAction(item: PrivacyCase) {
  if (!canWrite('tenant:update') || !recipientActionReady(item)) return
  const draft = recipientActionDrafts.value[item.id]
  await runAction(`recipient-action:${item.id}`, async () => {
    await $apiFetch<{ recipient_action: PrivacyRecipientAction }>(
      `/v1/privacy/cases/${item.id}/recipient-actions`, {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
        body: {
          action_kind: item.right_type,
          recipient_name: draft.recipientName.trim(),
          recipient_email: draft.recipientEmail.trim(),
          instruction_summary: draft.instructionSummary.trim(),
          integration_action_id: draft.integrationActionID || null,
        },
      },
    )
    recipientActionDrafts.value = {
      ...recipientActionDrafts.value,
      [item.id]: {
        recipientName: '',
        recipientEmail: '',
        instructionSummary: '',
        integrationActionID: '',
      },
    }
    actionNotice.value = t('admin.privacy.settings.cases.recipientQueued')
    await refresh()
  })
}

function caseIntegrationActionsEligible(item: PrivacyCase) {
  return Boolean(item.verified_at)
    && ['rectification', 'erasure'].includes(item.right_type)
    && !['pending_verification', 'cancelled', 'refused'].includes(item.status)
}

async function refreshIntegrationActions(item: PrivacyCase) {
  if (!canWrite('tenant:update') || !caseIntegrationActionsEligible(item)) return
  await runAction(`integration-actions-refresh:${item.id}`, async () => {
    const response = await $apiFetch<{
      integration_actions: PrivacyIntegrationAction[]
      created: number
      reopened: number
    }>(`/v1/privacy/cases/${item.id}/integration-actions/refresh`, { method: 'POST' })
    item.integration_actions = response.integration_actions
    actionNotice.value = response.created || response.reopened
      ? t('admin.privacy.settings.cases.integrationActionsUpdated', {
          created: response.created,
          reopened: response.reopened,
        })
      : t('admin.privacy.settings.cases.integrationActionsCurrent')
  })
}

function prepareIntegrationRecipientAction(
  item: PrivacyCase,
  integrationAction: PrivacyIntegrationAction,
) {
  ensureRecipientActionDraft(item)
  const draft = recipientActionDrafts.value[item.id]
  if (!draft) return
  recipientActionDrafts.value = {
    ...recipientActionDrafts.value,
    [item.id]: {
      ...draft,
      integrationActionID: integrationAction.id,
      recipientName: integrationAction.destination_name,
      instructionSummary: t('admin.privacy.settings.cases.integrationActionInstruction', {
        action: t(`admin.privacy.settings.cases.recipientKinds.${integrationAction.action_kind}`),
        destination: integrationAction.destination_name,
      }),
    },
  }
}

async function completeIntegrationAction(
  item: PrivacyCase,
  integrationAction: PrivacyIntegrationAction,
) {
  const note = integrationActionNotes.value[integrationAction.id]?.trim()
  if (!canWrite('tenant:update') || !caseRecipientActionEligible(item) || !note) return
  await runAction(`integration-action-complete:${integrationAction.id}`, async () => {
    const response = await $apiFetch<{ integration_action: PrivacyIntegrationAction }>(
      `/v1/privacy/cases/${item.id}/integration-actions/${integrationAction.id}/complete`, {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
        body: { completion_note: note },
      },
    )
    const index = item.integration_actions?.findIndex(
      candidate => candidate.id === integrationAction.id,
    ) ?? -1
    if (index >= 0) item.integration_actions?.splice(index, 1, response.integration_action)
    integrationActionNotes.value = { ...integrationActionNotes.value, [integrationAction.id]: '' }
    actionNotice.value = t('admin.privacy.settings.cases.integrationActionCompleted')
  })
}

async function retryRecipientAction(item: PrivacyCase, action: PrivacyRecipientAction) {
  if (!canWrite('tenant:update') || action.status !== 'failed') return
  await runAction(`recipient-action-retry:${action.id}`, async () => {
    await $apiFetch(
      `/v1/privacy/cases/${item.id}/recipient-actions/${action.id}/retry`, {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
      },
    )
    actionNotice.value = t('admin.privacy.settings.cases.recipientRetryQueued')
    await refresh()
  })
}

async function startCaseExecution(item: PrivacyCase) {
  if (!canWrite('tenant:update') || !caseCanExecute(item)) return
  if (item.right_type === 'erasure'
    && !window.confirm(t('admin.privacy.settings.cases.confirmErasure'))) return
  await runAction(`execute-case:${item.id}`, async () => {
    await $apiFetch(`/v1/privacy/cases/${item.id}/execute`, {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
    })
    actionNotice.value = t(item.right_type === 'erasure'
      ? 'admin.privacy.settings.cases.erasureQueued'
      : item.right_type === 'portability_assessment'
        ? 'admin.privacy.settings.cases.portabilityQueued'
        : 'admin.privacy.settings.cases.exportQueued')
    await refresh()
  })
}

async function downloadCaseExecution(item: PrivacyCase) {
  if (!['access', 'portability_assessment'].includes(item.execution?.right_type || '')
    || item.execution?.job_status !== 'completed') return
  await runAction(`download-case:${item.id}`, async () => {
    const response = await $apiFetch<{ url: string }>(
      `/v1/privacy/cases/${item.id}/execution/download`,
      { method: 'POST' },
    )
    const kind = item.right_type === 'portability_assessment' ? 'portability' : 'access'
    triggerSignedDownload(response.url, `fidibeki-${item.public_id.toLowerCase()}-${kind}-export.zip`)
  })
}

async function recordPortabilityAssessment(item: PrivacyCase) {
  if (!canWrite('tenant:update') || item.right_type !== 'portability_assessment') return
  const decision = portabilityDecisions.value[item.id] || 'approved'
  const reason = (portabilityReasons.value[item.id] || '').trim()
  const approved = ['approved', 'partially_approved'].includes(decision)
  const categories = approved ? (portabilitySelections.value[item.id] || []) : []
  if (!reason || (approved && categories.length === 0)) return
  await runAction(`portability:${item.id}`, async () => {
    await $apiFetch(`/v1/privacy/cases/${item.id}/portability-assessments`, {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: {
        decision,
        data_categories: categories,
        export_format: approved ? 'jsonl_zip' : '',
        reason,
      },
    })
    portabilityReasons.value = { ...portabilityReasons.value, [item.id]: '' }
    actionNotice.value = t('admin.privacy.settings.cases.portabilityRecorded')
    await refresh()
  })
}

async function placeCaseHold(item: PrivacyCase) {
  if (!canWrite('tenant:update')) return
  const reason = (holdReasons.value[item.id] || '').trim()
  if (!reason) return
  await runAction(`hold-case:${item.id}`, async () => {
    await $apiFetch(`/v1/privacy/cases/${item.id}/legal-holds`, {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: {
        reason,
        legal_reference: (holdReferences.value[item.id] || '').trim(),
      },
    })
    holdReasons.value = { ...holdReasons.value, [item.id]: '' }
    holdReferences.value = { ...holdReferences.value, [item.id]: '' }
    actionNotice.value = t('admin.privacy.settings.cases.holdPlaced')
    await refresh()
  })
}

async function releaseCaseHold(item: PrivacyCase, holdID: string) {
  if (!canWrite('tenant:update')) return
  const reason = (releaseReasons.value[holdID] || '').trim()
  if (!reason) return
  await runAction(`release-hold:${holdID}`, async () => {
    await $apiFetch(`/v1/privacy/cases/${item.id}/legal-holds/${holdID}/release`, {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: { reason },
    })
    releaseReasons.value = { ...releaseReasons.value, [holdID]: '' }
    actionNotice.value = t('admin.privacy.settings.cases.holdReleased')
    await refresh()
  })
}

function statusLabel(value: string) {
  return t(`admin.statuses.${value}`)
}

function statusTone(value: string) {
  if (value === 'completed' || value === 'sent') return 'success'
  if (value === 'failed' || value === 'expired') return 'danger'
  return 'pending'
}

function formatBytes(value?: number) {
  if (!value) return '—'
  if (value < 1024) return `${value} B`
  const units = ['KB', 'MB', 'GB']
  let amount = value / 1024
  let index = 0
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024
    index += 1
  }
  return `${decimal(amount, { maximumFractionDigits: amount >= 10 ? 0 : 1 })} ${units[index]}`
}

function retentionLabel(policy: RetentionPolicy) {
  return retentionCategoryLabel(policy.category)
}

function retentionCategoryLabel(category: string) {
  return t(`admin.privacy.settings.category.${category}`)
}

function retentionDuration(policy: RetentionPolicy) {
  if (policy.status === 'decision_required') return t('admin.privacy.settings.duration.decisionRequired')
  if (policy.status === 'external_control') return t('admin.privacy.settings.duration.externalConfiguration')
  if (policy.category === 'auth_magic_links') return t('admin.privacy.settings.duration.magicLinkExpiry')
  if (policy.category === 'unverified_requests') {
    return plural('admin.privacy.settings.duration.hours', 48, { count: 48 })
  }
  if (policy.category === 'widget_recurrence') return t('admin.privacy.settings.duration.ruleControlled')
  if (policy.category === 'ai_provider_files') return t('admin.privacy.settings.duration.providerFileExpiry')
  if (policy.duration_days != null) {
    return plural('admin.privacy.settings.duration.days', policy.duration_days, { count: policy.duration_days })
  }
  return policy.duration
}

function retentionSource(source: string) {
  if (source === 'Plan storage limit') return t('admin.privacy.settings.source.planStorageLimit')
  if (source === 'Policy decision required') return t('admin.privacy.settings.source.policyDecision')
  if (source === 'Widget rule') return t('admin.privacy.settings.source.widgetRule')
  if (source === 'Platform and provider boundary') return t('admin.privacy.settings.source.platformProvider')
  if (source === 'External configuration') return t('admin.privacy.settings.source.externalConfiguration')
  return t('admin.privacy.settings.source.platformDefault')
}

function retentionStatusTone(status: RetentionPolicy['status']) {
  if (status === 'enforced') return 'success'
  if (status === 'external_control') return 'external'
  return 'pending'
}

function retentionStatusLabel(status: RetentionPolicy['status']) {
  return t(`admin.privacy.settings.retentionStatus.${status}`)
}

function retentionLifecycleAction(status: RetentionPolicy['status']) {
  return t(`admin.privacy.settings.retentionAction.${status}`)
}

function retentionCadence(cadence: string) {
  const key = cadence.toLowerCase().replaceAll(' ', '_').replaceAll('-', '_')
  return t(`admin.privacy.settings.cadence.${key}`)
}

function retentionHold(hold: RetentionPolicy['hold_behavior']) {
  return t(`admin.privacy.settings.hold.${hold}`)
}

function retentionRole(role: RetentionPolicy['role']) {
  return t(`admin.privacy.settings.role.${role}`)
}

function retentionResources(policy: RetentionPolicy) {
  return policy.resource_counts.map((resource) => {
    return plural(
      `admin.privacy.settings.resources.${resource.kind}`,
      resource.count,
      { count: resource.count },
    )
  }).join(' · ')
}

function retentionHealthTone(state: RetentionOperationHealth['state']) {
  if (state === 'healthy') return 'success'
  if (state === 'failed' || state === 'overdue') return 'danger'
  return 'pending'
}

function retentionHealthLabel(state: RetentionOperationHealth['state']) {
  return t(`admin.privacy.settings.health.status.${state}`)
}

function retentionHealthAlert(alert: RetentionOperationHealth['alert_reason']) {
  return t(`admin.privacy.settings.health.alert.${alert}`)
}

function retentionOperationLabel(operation: RetentionOperationHealth['operation']) {
  return t(`admin.privacy.settings.health.operation.${operation}`)
}

function toggleRetentionDetails(category: string) {
  const expanded = new Set(expandedRetentionCategories.value)
  if (expanded.has(category)) expanded.delete(category)
  else expanded.add(category)
  expandedRetentionCategories.value = expanded
}

function actionLabel(action: ReporterPrivacyAction) {
  return t(action === 'anonymize'
    ? 'admin.privacy.settings.actionAnonymize'
    : 'admin.privacy.settings.actionExport')
}

async function runAction(key: string, operation: () => Promise<void>) {
  if (actionPending.value) return
  actionPending.value = key
  actionError.value = ''
  actionNotice.value = ''
  try {
    await operation()
  } catch (operationError) {
    actionError.value = localizedError.message(operationError, 'admin.privacy.settings.error')
  } finally {
    actionPending.value = ''
  }
}

async function createWorkspaceExport() {
  if (!canWrite('tenant:update')) return
  await runAction('create-export', async () => {
    const response = await $apiFetch<{ created: boolean }>('/v1/privacy/exports', { method: 'POST' })
    actionNotice.value = response.created
      ? t('admin.privacy.settings.notices.exportQueued')
      : t('admin.privacy.settings.notices.exportExists')
    await refresh()
  })
}

async function downloadWorkspaceExport(item: PrivacyExport) {
  await runAction(`download:${item.id}`, async () => {
    const response = await $apiFetch<{ url: string }>(`/v1/privacy/exports/${item.id}/download`, { method: 'POST' })
    triggerSignedDownload(response.url, 'fidibeki-workspace-export.zip')
  })
}

async function retryJob(jobID: string) {
  if (!canWrite('tenant:update')) return
  await runAction(`retry:${jobID}`, async () => {
    await $apiFetch(`/v1/privacy/exports/${jobID}/retry`, { method: 'POST' })
    actionNotice.value = t('admin.privacy.settings.notices.retryQueued')
    await refresh()
  })
}

async function startReporterRequest() {
  if (!reporterEmail.value.trim() || !canWrite('tenant:update')) return
  if (reporterAction.value === 'anonymize') {
    anonymizeConfirmation.value = ''
    anonymizeDialogOpen.value = true
    return
  }
  await createReporterRequest()
}

async function createReporterRequest() {
  const email = reporterEmail.value.trim()
  if (!email || !canWrite('tenant:update')) return
  await runAction('create-request', async () => {
    await $apiFetch('/v1/privacy/requests', {
      method: 'POST',
      body: { email, action: reporterAction.value },
    })
    actionNotice.value = t('admin.privacy.settings.notices.verificationQueued', { email })
    anonymizeDialogOpen.value = false
    anonymizeConfirmation.value = ''
    reporterEmail.value = ''
    reporterAction.value = 'export'
    await refresh()
  })
}

const reviewExpanded = ref<Record<string, boolean>>({})
const reviewItems = ref<Record<string, ReviewItem[]>>({})
const reviewReasons = ref<Record<string, string>>({})

function resourceLabel(item: ReviewItem) {
  return t(`admin.privacy.settings.review.resource.${item.resource_type}`)
}

function fieldsLabel(item: ReviewItem) {
  return item.fields.map(field => t(`admin.privacy.settings.review.field.${field}`, field)).join(', ')
}

function disclosedDestinationsLabel(item: ReviewItem) {
  const destinations = item.disclosed_destinations
  if (!destinations) return ''
  return Object.entries(destinations)
    .map(([kind, count]) => `${t(`admin.privacy.settings.review.destination.${kind}`, kind)} (${count})`)
    .join(', ')
}

async function loadReviewItems(request: ReporterPrivacyRequest) {
  const response = await $apiFetch<{ review_items: ReviewItem[] }>(
    `/v1/privacy/requests/${request.id}/review-items`,
  )
  reviewItems.value = { ...reviewItems.value, [request.id]: response.review_items }
}

async function toggleReview(request: ReporterPrivacyRequest) {
  const isOpen = Boolean(reviewExpanded.value[request.id])
  if (isOpen) {
    reviewExpanded.value = { ...reviewExpanded.value, [request.id]: false }
    return
  }
  await runAction(`review:${request.id}`, async () => {
    await loadReviewItems(request)
    reviewExpanded.value = { ...reviewExpanded.value, [request.id]: true }
  })
}

async function decideReviewItem(request: ReporterPrivacyRequest, item: ReviewItem, status: ReviewDecisionStatus) {
  if (!canWrite('tenant:update')) return
  const reason = (reviewReasons.value[item.id] || '').trim()
  if (status === 'retained_exception' && !reason) {
    actionError.value = t('admin.privacy.settings.review.reasonRequired')
    return
  }
  await runAction(`decide:${item.id}`, async () => {
    await $apiFetch(`/v1/privacy/requests/${request.id}/review-items/${item.id}/decide`, {
      method: 'POST',
      body: { status, reason },
    })
    await loadReviewItems(request)
    await refresh()
  })
}

async function finalizeRequest(request: ReporterPrivacyRequest) {
  if (!canWrite('tenant:update')) return
  await runAction(`finalize:${request.id}`, async () => {
    const response = await $apiFetch<{ completed: boolean, pending: number }>(
      `/v1/privacy/requests/${request.id}/finalize`,
      { method: 'POST' },
    )
    actionNotice.value = response.completed
      ? t('admin.privacy.settings.notices.reviewCompleted')
      : t('admin.privacy.settings.notices.reviewPending', { count: response.pending })
    await loadReviewItems(request)
    await refresh()
  })
}
</script>

<template>
  <div class="privacy-settings">
    <section v-if="status === 'pending'" class="app-surface app-state">
      <div><span class="app-spinner" /><h2>{{ t('admin.privacy.settings.loading') }}</h2></div>
    </section>
    <section v-else-if="error" class="app-surface app-state">
      <div><AlertTriangle :size="24" /><h2>{{ t('admin.privacy.settings.unavailable') }}</h2><p>{{ localizedError.message(error, 'admin.privacy.settings.error') }}</p><button class="app-button" @click="refresh()">{{ t('admin.common.tryAgain') }}</button></div>
    </section>
    <template v-else-if="data">
      <p v-if="actionError" class="privacy-alert privacy-alert--error" role="alert"><AlertTriangle :size="15" />{{ actionError }}</p>
      <p v-if="actionNotice" class="privacy-alert privacy-alert--success" role="status"><CheckCircle2 :size="15" />{{ actionNotice }}</p>

      <section class="app-surface privacy-section">
        <header>
          <div><p class="app-eyebrow">{{ t('admin.privacy.settings.cases.eyebrow') }}</p><h2>{{ t('admin.privacy.settings.cases.title') }}</h2></div>
        </header>
        <div v-if="!canManagePrivacy" class="privacy-empty"><ShieldCheck :size="22" /><h3>{{ t('admin.privacy.settings.administrationRequired') }}</h3></div>
        <template v-else>
          <div class="privacy-case-dashboard">
            <div><b>{{ data.dashboard.overdue }}</b><span>{{ t('admin.privacy.settings.cases.overdue') }}</span></div>
            <div><b>{{ data.dashboard.due_soon }}</b><span>{{ t('admin.privacy.settings.cases.dueSoon') }}</span></div>
            <div><b>{{ data.dashboard.awaiting_manual }}</b><span>{{ t('admin.privacy.settings.cases.manual') }}</span></div>
            <div><b>{{ data.dashboard.awaiting_legal_review }}</b><span>{{ t('admin.privacy.settings.cases.legal') }}</span></div>
            <div><b>{{ data.dashboard.failed_external }}</b><span>{{ t('admin.privacy.settings.cases.failedExternal') }}</span></div>
            <div><b>{{ data.dashboard.completed }}</b><span>{{ t('admin.privacy.settings.cases.completed') }}</span></div>
          </div>
          <form class="privacy-case-form" @submit.prevent="createPrivacyCase">
            <label>{{ t('admin.privacy.settings.cases.subjectType') }}
              <select v-model="caseSubjectType" class="app-select">
                <option value="reporter">{{ t('admin.privacy.settings.cases.reporter') }}</option>
                <option value="member">{{ t('admin.privacy.settings.cases.member') }}</option>
              </select>
            </label>
            <label v-if="caseSubjectType === 'reporter'">{{ t('admin.privacy.settings.reporterEmail') }}
              <input v-model.trim="caseReporterEmail" class="app-input" type="email" maxlength="320" required />
            </label>
            <label v-else>{{ t('admin.privacy.settings.cases.member') }}
              <select v-model="caseSubjectMemberID" class="app-select" required>
                <option value="" disabled>{{ t('admin.privacy.settings.cases.selectMember') }}</option>
                <option v-for="member in activeMembers" :key="member.id" :value="member.id">{{ member.name || member.email }}</option>
              </select>
            </label>
            <label>{{ t('admin.privacy.settings.requestType') }}
              <select v-model="caseRightType" class="app-select">
                <option v-for="right in ['access', 'rectification', 'erasure', 'restriction', 'objection', 'portability_assessment', 'consent_withdrawal']" :key="right" :value="right">{{ t(`admin.privacy.casePublic.rights.${right}`) }}</option>
              </select>
            </label>
            <label>{{ t('admin.privacy.settings.cases.role') }}
              <select v-model="caseControllerRole" class="app-select">
                <option value="controller">{{ t('admin.privacy.settings.cases.controller') }}</option>
                <option value="customer_controller_assistance">{{ t('admin.privacy.settings.cases.assistance') }}</option>
              </select>
            </label>
            <label>{{ t('admin.privacy.settings.cases.owner') }}
              <select v-model="caseOwnerMemberID" class="app-select">
                <option value="">{{ t('admin.privacy.settings.cases.unassigned') }}</option>
                <option v-for="member in activeMembers" :key="member.id" :value="member.id">{{ member.name || member.email }}</option>
              </select>
            </label>
            <button class="app-button app-button--primary" type="submit" :disabled="Boolean(actionPending)">
              <ShieldCheck :size="15" />{{ actionPending === 'create-case' ? t('admin.common.queuing') : t('admin.privacy.settings.cases.create') }}
            </button>
          </form>
          <div v-if="!data.cases.length" class="privacy-empty privacy-empty--compact"><ShieldCheck :size="22" /><h3>{{ t('admin.privacy.settings.cases.empty') }}</h3></div>
          <div v-else class="privacy-table privacy-case-table">
            <div class="privacy-row privacy-row--head"><span>{{ t('admin.privacy.settings.cases.case') }}</span><span>{{ t('admin.privacy.settings.cases.subject') }}</span><span>{{ t('admin.privacy.settings.columns.status') }}</span><span>{{ t('admin.privacy.settings.cases.deadline') }}</span><span>{{ t('admin.privacy.settings.cases.owner') }}</span><span /></div>
            <div v-for="item in data.cases" :key="item.id" class="privacy-case-entry">
              <div class="privacy-row">
                <span><b>{{ item.public_id }}</b><small>{{ t(`admin.privacy.casePublic.rights.${item.right_type}`) }}</small></span>
                <span><b>{{ item.subject_email || memberLabel(item.subject_member_id) }}</b><small>{{ t(`admin.privacy.settings.cases.${item.subject_type}`) }}</small></span>
                <span><span class="privacy-status" :data-tone="statusTone(item.status)"><i />{{ statusLabel(item.status) }}</span><small v-if="item.restriction?.active">{{ t('admin.privacy.settings.cases.restricted') }}</small><small v-if="activeCaseHolds(item).length">{{ t('admin.privacy.settings.cases.activeHolds', { count: activeCaseHolds(item).length }) }}</small></span>
                <span>{{ dateTime(item.deadline_at) }}</span>
                <span>{{ memberLabel(item.owner_member_id) }}</span>
                <button class="app-icon-button" type="button" :title="t('admin.privacy.settings.cases.toggleDetails')" @click="toggleCase(item)">
                  <ChevronUp v-if="expandedCases[item.id]" :size="15" />
                  <ChevronDown v-else :size="15" />
                </button>
              </div>
              <div v-if="expandedCases[item.id]" class="privacy-case-detail">
                <p v-if="caseDetailLoading[item.id]" class="privacy-case-detail-loading"><span class="app-spinner" />{{ t('admin.privacy.settings.cases.detailLoading') }}</p>
                <section v-else class="privacy-case-overview">
                  <div class="privacy-case-identity">
                    <header><ShieldCheck :size="15" /><b>{{ t('admin.privacy.settings.cases.identity') }}</b></header>
                    <dl>
                      <div><dt>{{ t('admin.privacy.settings.cases.subject') }}</dt><dd>{{ item.subject_email || memberLabel(item.subject_member_id) }}</dd></div>
                      <div><dt>{{ t('admin.privacy.settings.cases.verification') }}</dt><dd>{{ item.verified_at ? t('admin.privacy.settings.cases.verifiedAt', { date: dateTime(item.verified_at) }) : t('admin.privacy.settings.cases.notVerified') }}</dd></div>
                      <div><dt>{{ t('admin.privacy.settings.cases.verificationMethod') }}</dt><dd>{{ item.verification_method || '—' }}</dd></div>
                      <div><dt>{{ t('admin.privacy.settings.cases.controllerRole') }}</dt><dd>{{ controllerRoleLabel(item.controller_role) }}</dd></div>
                      <div><dt>{{ t('admin.privacy.settings.cases.requestedBy') }}</dt><dd>{{ memberLabel(item.requested_by_member_id) }}</dd></div>
                      <div><dt>{{ t('admin.privacy.settings.cases.owner') }}</dt><dd>{{ memberLabel(item.owner_member_id) }}</dd></div>
                      <div><dt>{{ t('admin.privacy.settings.cases.received') }}</dt><dd>{{ dateTime(item.received_at) }}</dd></div>
                      <div><dt>{{ t('admin.privacy.settings.cases.deadline') }}</dt><dd>{{ dateTime(item.deadline_at) }}</dd></div>
                    </dl>
                  </div>
                  <div class="privacy-case-scope">
                    <header><ListTree :size="15" /><b>{{ t('admin.privacy.settings.cases.scope') }}</b></header>
                    <p v-if="!caseScopeEntries(item).length">{{ t('admin.privacy.settings.cases.noScope') }}</p>
                    <dl v-else>
                      <div v-for="[key, value] in caseScopeEntries(item)" :key="key"><dt>{{ key }}</dt><dd>{{ caseScopeValue(value) }}</dd></div>
                    </dl>
                  </div>
                  <div class="privacy-case-timeline">
                    <header><Clock3 :size="15" /><b>{{ t('admin.privacy.settings.cases.timeline') }}</b></header>
                    <p v-if="!item.events?.length">{{ t('admin.privacy.settings.cases.noTimeline') }}</p>
                    <ol v-else>
                      <li v-for="event in item.events" :key="event.id">
                        <i />
                        <div>
                          <b>{{ caseEventLabel(event) }}</b>
                          <span v-if="event.previous_status && event.new_status">{{ statusLabel(event.previous_status) }} → {{ statusLabel(event.new_status) }}</span>
                          <small>{{ dateTime(event.occurred_at) }} · {{ event.actor_member_id ? memberLabel(event.actor_member_id) : t('admin.privacy.settings.cases.systemActor') }}</small>
                        </div>
                      </li>
                    </ol>
                  </div>
                </section>
                <div v-if="item.right_type === 'portability_assessment'" class="privacy-case-portability">
                  <header>
                    <div><p class="app-eyebrow">{{ t('admin.privacy.settings.cases.portability') }}</p><b>{{ t('admin.privacy.settings.cases.portabilityTitle') }}</b></div>
                    <span v-if="item.portability_assessment" class="privacy-status" :data-tone="['approved', 'partially_approved'].includes(item.portability_assessment.decision) ? 'success' : 'pending'"><i />{{ t(`admin.privacy.settings.cases.portabilityDecisions.${item.portability_assessment.decision}`) }}</span>
                  </header>
                  <template v-if="item.portability_assessment">
                    <p>{{ item.portability_assessment.reason }}</p>
                    <small>{{ dateTime(item.portability_assessment.assessed_at) }} · {{ memberLabel(item.portability_assessment.assessed_by_member_id) }}</small>
                    <div v-if="item.portability_assessment.data_categories.length" class="privacy-portability-summary">
                      <span v-for="category in item.portability_assessment.data_categories" :key="category">{{ t(`admin.privacy.settings.cases.portabilityCategories.${category}`) }}</span>
                      <span>{{ t('admin.privacy.settings.cases.portabilityFormat') }}</span>
                    </div>
                  </template>
                  <p v-else>{{ t('admin.privacy.settings.cases.portabilityPending') }}</p>
                  <form v-if="item.subject_type === 'reporter' && !item.execution" @submit.prevent="recordPortabilityAssessment(item)">
                    <label>{{ t('admin.privacy.settings.cases.portabilityDecision') }}
                      <select v-model="portabilityDecisions[item.id]" class="app-select">
                        <option v-for="decision in ['approved', 'partially_approved', 'not_applicable', 'refused']" :key="decision" :value="decision">{{ t(`admin.privacy.settings.cases.portabilityDecisions.${decision}`) }}</option>
                      </select>
                    </label>
                    <fieldset v-if="['approved', 'partially_approved'].includes(portabilityDecisions[item.id] || 'approved')">
                      <legend>{{ t('admin.privacy.settings.cases.portabilityData') }}</legend>
                      <label v-for="category in portabilityCategoryOptions" :key="category">
                        <input v-model="portabilitySelections[item.id]" type="checkbox" :value="category" />
                        {{ t(`admin.privacy.settings.cases.portabilityCategories.${category}`) }}
                      </label>
                    </fieldset>
                    <label>{{ t('admin.privacy.settings.cases.portabilityReason') }}
                      <textarea v-model.trim="portabilityReasons[item.id]" class="app-textarea" maxlength="2000" required />
                    </label>
                    <button class="app-button" type="submit" :disabled="Boolean(actionPending)"><ShieldCheck :size="14" />{{ t('admin.privacy.settings.cases.recordPortability') }}</button>
                  </form>
                  <p v-else-if="item.subject_type === 'member'" class="privacy-portability-member-note">{{ t('admin.privacy.settings.cases.memberPortabilityPending') }}</p>
                </div>
                <div v-if="item.right_type === 'rectification' && item.subject_type === 'member'" class="privacy-case-rectification">
                  <header>
                    <PencilLine :size="16" />
                    <div>
                      <p class="app-eyebrow">{{ t('admin.privacy.settings.cases.rectification') }}</p>
                      <b>{{ t('admin.privacy.settings.cases.rectificationTitle') }}</b>
                      <small>{{ t('admin.privacy.settings.cases.rectificationDescription') }}</small>
                    </div>
                  </header>
                  <form v-if="caseCanRectify(item) && rectificationDrafts[item.id]" @submit.prevent="applyMemberRectification(item)">
                    <fieldset>
                      <legend>{{ t('admin.privacy.settings.cases.rectificationFields') }}</legend>
                      <div class="privacy-rectification-field">
                        <label :for="`rectification-name-${item.id}`"><input :id="`rectification-name-${item.id}`" v-model="rectificationDrafts[item.id].selected.name" type="checkbox" />{{ t('admin.privacy.settings.cases.rectificationName') }}</label>
                        <input v-model="rectificationDrafts[item.id].values.name" class="app-input" maxlength="120" :aria-label="t('admin.privacy.settings.cases.rectificationName')" :disabled="!rectificationDrafts[item.id].selected.name" />
                      </div>
                      <div class="privacy-rectification-field">
                        <label :for="`rectification-locale-${item.id}`"><input :id="`rectification-locale-${item.id}`" v-model="rectificationDrafts[item.id].selected.locale" type="checkbox" />{{ t('admin.privacy.settings.cases.rectificationLocale') }}</label>
                        <select v-model="rectificationDrafts[item.id].values.locale" class="app-select" :aria-label="t('admin.privacy.settings.cases.rectificationLocale')" :disabled="!rectificationDrafts[item.id].selected.locale">
                          <option value="">{{ t('admin.privacy.settings.cases.rectificationDefault') }}</option>
                          <option value="en">English</option>
                          <option value="pt">Português</option>
                          <option value="pt-AO">Português (Angola)</option>
                          <option value="pt-PT">Português (Portugal)</option>
                        </select>
                      </div>
                      <div class="privacy-rectification-field">
                        <label :for="`rectification-timezone-${item.id}`"><input :id="`rectification-timezone-${item.id}`" v-model="rectificationDrafts[item.id].selected.timezone" type="checkbox" />{{ t('admin.privacy.settings.cases.rectificationTimezone') }}</label>
                        <input v-model="rectificationDrafts[item.id].values.timezone" class="app-input" maxlength="100" placeholder="Europe/Lisbon" :aria-label="t('admin.privacy.settings.cases.rectificationTimezone')" :disabled="!rectificationDrafts[item.id].selected.timezone" />
                      </div>
                    </fieldset>
                    <label>{{ t('admin.privacy.settings.cases.rectificationReason') }}
                      <textarea v-model.trim="rectificationDrafts[item.id].reason" class="app-textarea" maxlength="2000" required />
                    </label>
                    <button class="app-button" type="submit" :disabled="Boolean(actionPending) || !rectificationReady(item)">
                      <PencilLine :size="14" />{{ actionPending === `rectification:${item.id}` ? t('admin.common.saving') : t('admin.privacy.settings.cases.applyRectification') }}
                    </button>
                  </form>
                  <section class="privacy-rectification-history">
                    <header><b>{{ t('admin.privacy.settings.cases.rectificationHistory') }}</b></header>
                    <p v-if="!item.rectifications?.length">{{ t('admin.privacy.settings.cases.noRectifications') }}</p>
                    <article v-for="rectification in item.rectifications || []" :key="rectification.id">
                      <div v-for="field in rectificationChangedFields(rectification)" :key="field" class="privacy-rectification-change">
                        <b>{{ t(`admin.privacy.settings.cases.rectification${field[0].toUpperCase()}${field.slice(1)}`) }}</b>
                        <span>
                          <code>{{ rectificationValue(rectification.previous_values[field]) }}</code>
                          <MoveRight :size="13" />
                          <code>{{ rectificationValue(rectification.changes[field]) }}</code>
                        </span>
                      </div>
                      <footer>
                        <span>{{ dateTime(rectification.applied_at) }} · {{ memberLabel(rectification.applied_by_member_id) }}</span>
                        <p>{{ rectification.reason }}</p>
                      </footer>
                    </article>
                  </section>
                </div>
                <div v-if="item.right_type === 'rectification' && item.subject_type === 'reporter'" class="privacy-case-rectification privacy-case-feedback-rectification">
                  <header>
                    <PencilLine :size="16" />
                    <div>
                      <p class="app-eyebrow">{{ t('admin.privacy.settings.cases.feedbackRectification') }}</p>
                      <b>{{ t('admin.privacy.settings.cases.feedbackRectificationTitle') }}</b>
                      <small>{{ t('admin.privacy.settings.cases.feedbackRectificationDescription') }}</small>
                    </div>
                  </header>
                  <p v-if="!item.verified_at" class="privacy-rectification-note">{{ t('admin.privacy.settings.cases.feedbackRectificationVerification') }}</p>
                  <p v-else-if="rectifiableFeedbackLoading[item.id]" class="privacy-rectification-note"><span class="app-spinner" />{{ t('admin.common.loading') }}</p>
                  <form v-else-if="caseCanRectifyReporter(item) && feedbackRectificationDrafts[item.id] && (rectifiableFeedbacks[item.id] || []).length" class="privacy-feedback-rectification-form" @submit.prevent="applyFeedbackRectification(item)">
                    <label>{{ t('admin.privacy.settings.cases.feedbackRectificationFeedback') }}
                      <select v-model="feedbackRectificationDrafts[item.id].feedbackID" class="app-select" @change="selectRectifiableFeedback(item)">
                        <option v-for="feedback in rectifiableFeedbacks[item.id] || []" :key="feedback.id" :value="feedback.id">{{ feedback.public_id }} · {{ feedback.title }}</option>
                      </select>
                    </label>
                    <label class="privacy-feedback-description">{{ t('admin.privacy.settings.cases.feedbackRectificationContent') }}
                      <textarea v-model="feedbackRectificationDrafts[item.id].description" class="app-textarea" maxlength="10000" required />
                    </label>
                    <label>{{ t('admin.privacy.settings.cases.rectificationReason') }}
                      <textarea v-model.trim="feedbackRectificationDrafts[item.id].reason" class="app-textarea" maxlength="2000" required />
                    </label>
                    <button class="app-button" type="submit" :disabled="Boolean(actionPending) || !feedbackRectificationReady(item)">
                      <PencilLine :size="14" />{{ actionPending === `feedback-rectification:${item.id}` ? t('admin.common.saving') : t('admin.privacy.settings.cases.applyFeedbackRectification') }}
                    </button>
                  </form>
                  <p v-else-if="caseCanRectifyReporter(item)" class="privacy-rectification-note">{{ t('admin.privacy.settings.cases.noRectifiableFeedback') }}</p>
                  <section class="privacy-rectification-history">
                    <header><b>{{ t('admin.privacy.settings.cases.feedbackRectificationHistory') }}</b></header>
                    <p v-if="!item.feedback_rectifications?.length">{{ t('admin.privacy.settings.cases.noFeedbackRectifications') }}</p>
                    <article v-for="rectification in item.feedback_rectifications || []" :key="rectification.id" class="privacy-feedback-rectification-history">
                      <div class="privacy-rectification-change">
                        <b>{{ t('admin.privacy.settings.cases.feedbackRectificationPrevious') }}</b>
                        <code>{{ rectificationValue(rectification.previous_description) }}</code>
                      </div>
                      <MoveRight :size="14" />
                      <div class="privacy-rectification-change">
                        <b>{{ t('admin.privacy.settings.cases.feedbackRectificationCorrected') }}</b>
                        <code>{{ rectification.description }}</code>
                      </div>
                      <footer>
                        <span>{{ dateTime(rectification.applied_at) }} · {{ memberLabel(rectification.applied_by_member_id) }}</span>
                        <p>{{ rectification.reason }}</p>
                        <small v-if="rectification.derived_data_invalidated">{{ t('admin.privacy.settings.cases.feedbackRectificationAIInvalidated') }}</small>
                      </footer>
                    </article>
                  </section>
                </div>
                <section v-if="item.right_type === 'erasure' && item.execution?.erasure_review" class="privacy-case-erasure-review">
                  <header>
                    <div>
                      <p class="app-eyebrow">{{ t('admin.privacy.settings.cases.erasureEvidence') }}</p>
                      <b>{{ t('admin.privacy.settings.cases.erasureEvidenceTitle') }}</b>
                    </div>
                    <span v-if="item.execution.erasure_review.pending" class="privacy-status" data-tone="pending"><i />{{ t('admin.privacy.settings.cases.erasureReviewPending', { count: item.execution.erasure_review.pending }) }}</span>
                    <span v-else class="privacy-status" data-tone="success"><i />{{ t('admin.privacy.settings.cases.erasureReviewSettled') }}</span>
                  </header>
                  <div class="privacy-erasure-totals">
                    <span><b>{{ item.execution.erasure_review.redacted }}</b>{{ t('admin.privacy.settings.cases.erasureRedacted') }}</span>
                    <span><b>{{ item.execution.erasure_review.deleted }}</b>{{ t('admin.privacy.settings.cases.erasureDeleted') }}</span>
                    <span><b>{{ item.execution.erasure_review.not_personal_data }}</b>{{ t('admin.privacy.settings.cases.erasureNotPersonal') }}</span>
                    <span :data-exception="item.execution.erasure_review.retained_exception > 0"><b>{{ item.execution.erasure_review.retained_exception }}</b>{{ t('admin.privacy.settings.cases.erasureRetained') }}</span>
                  </div>
                  <div v-if="item.execution.erasure_review.items.length" class="privacy-erasure-items">
                    <article v-for="reviewItem in item.execution.erasure_review.items" :key="reviewItem.id">
                      <div>
                        <b>{{ resourceLabel(reviewItem) }}</b>
                        <code>{{ reviewItem.resource_id }}</code>
                        <small>{{ fieldsLabel(reviewItem) }}</small>
                      </div>
                      <div>
                        <span class="privacy-status" :data-tone="reviewItem.status === 'retained_exception' ? 'pending' : reviewItem.status === 'pending' ? 'danger' : 'success'"><i />{{ reviewItem.status === 'pending' ? statusLabel('pending') : t(`admin.privacy.settings.review.decision.${reviewItem.status}`) }}</span>
                        <small v-if="reviewItem.decided_at">{{ dateTime(reviewItem.decided_at) }} · {{ memberLabel(reviewItem.decided_by_member_id) }}</small>
                        <p v-if="reviewItem.decision_reason">{{ reviewItem.decision_reason }}</p>
                      </div>
                    </article>
                  </div>
                  <p v-else>{{ t('admin.privacy.settings.cases.erasureNoExceptions') }}</p>
                </section>
                <section v-if="item.right_type === 'rectification' || item.right_type === 'erasure'" class="privacy-integration-actions">
                  <header>
                    <div>
                      <p class="app-eyebrow">{{ t('admin.privacy.settings.cases.integrationActionsEyebrow') }}</p>
                      <b>{{ t('admin.privacy.settings.cases.integrationActionsTitle') }}</b>
                      <small>{{ t('admin.privacy.settings.cases.integrationActionsDescription') }}</small>
                    </div>
                    <button class="app-button" type="button" :disabled="Boolean(actionPending) || !canWrite('tenant:update') || !caseIntegrationActionsEligible(item)" @click="refreshIntegrationActions(item)">
                      <RefreshCw :size="14" />{{ actionPending === `integration-actions-refresh:${item.id}` ? t('admin.common.refreshing') : t('admin.privacy.settings.cases.refreshIntegrationActions') }}
                    </button>
                  </header>
                  <p v-if="!item.integration_actions?.length" class="privacy-integration-actions-empty">{{ t('admin.privacy.settings.cases.noIntegrationActions') }}</p>
                  <div v-else class="privacy-integration-action-list">
                    <article v-for="integrationAction in item.integration_actions" :key="integrationAction.id">
                      <div class="privacy-integration-action-summary">
                        <span><b>{{ integrationAction.destination_name }}</b><small>{{ integrationAction.destination_kind || t('admin.privacy.settings.cases.removedIntegration') }}</small></span>
                        <span class="privacy-status" :data-tone="integrationAction.status === 'completed' ? 'success' : 'pending'"><i />{{ t(`admin.privacy.settings.cases.integrationActionStatuses.${integrationAction.status}`) }}</span>
                      </div>
                      <p>{{ plural('admin.privacy.settings.cases.integrationDisclosureCount', integrationAction.disclosure_count, { count: integrationAction.disclosure_count }) }} · {{ t('admin.privacy.settings.cases.integrationLastDisclosure', { date: dateTime(integrationAction.last_disclosed_at) }) }}</p>
                      <div class="privacy-integration-categories">
                        <span v-for="category in integrationAction.data_categories" :key="category">{{ category.replaceAll('_', ' ') }}</span>
                      </div>
                      <p class="privacy-integration-capability">{{ t('admin.privacy.settings.cases.integrationManualBoundary') }}</p>
                      <div v-if="integrationAction.status === 'completed'" class="privacy-integration-completion">
                        <CheckCircle2 :size="15" />
                        <span><b>{{ integrationAction.completion_note }}</b><small>{{ integrationAction.completed_at ? dateTime(integrationAction.completed_at) : '—' }} · {{ memberLabel(integrationAction.completed_by_member_id) }}</small></span>
                      </div>
                      <form v-else-if="caseRecipientActionEligible(item)" @submit.prevent="completeIntegrationAction(item, integrationAction)">
                        <label>{{ t('admin.privacy.settings.cases.integrationCompletionNote') }}
                          <input v-model.trim="integrationActionNotes[integrationAction.id]" class="app-input" maxlength="2000" required :placeholder="t('admin.privacy.settings.cases.integrationCompletionPlaceholder')" />
                        </label>
                        <button class="app-button app-button--primary" type="submit" :disabled="Boolean(actionPending) || !canWrite('tenant:update') || !integrationActionNotes[integrationAction.id]?.trim()">
                          <CheckCircle2 :size="14" />{{ actionPending === `integration-action-complete:${integrationAction.id}` ? t('admin.common.saving') : t('admin.privacy.settings.cases.completeIntegrationAction') }}
                        </button>
                      </form>
                      <p v-else class="privacy-integration-evidence-gate">{{ t('admin.privacy.settings.cases.integrationEvidenceRequired') }}</p>
                      <footer>
                        <button class="app-button" type="button" :disabled="!canWrite('tenant:update') || !caseRecipientActionEligible(item)" @click="prepareIntegrationRecipientAction(item, integrationAction)">
                          <MailCheck :size="14" />{{ t('admin.privacy.settings.cases.prepareRecipientNotice') }}
                        </button>
                        <small v-if="integrationAction.linked_recipient_action_ids?.length">{{ plural('admin.privacy.settings.cases.linkedRecipientNotices', integrationAction.linked_recipient_action_ids.length, { count: integrationAction.linked_recipient_action_ids.length }) }}</small>
                      </footer>
                    </article>
                  </div>
                </section>
                <section v-if="item.right_type === 'rectification' || item.right_type === 'erasure'" class="privacy-recipient-actions">
                  <header>
                    <div>
                      <p class="app-eyebrow">{{ t('admin.privacy.settings.cases.recipientEyebrow') }}</p>
                      <b>{{ t('admin.privacy.settings.cases.recipientTitle') }}</b>
                      <small>{{ t('admin.privacy.settings.cases.recipientDescription') }}</small>
                    </div>
                    <MailCheck :size="17" />
                  </header>
                  <p v-if="!caseRecipientActionEligible(item)" class="privacy-recipient-gate">{{ t('admin.privacy.settings.cases.recipientEvidenceRequired') }}</p>
                  <form v-else-if="recipientActionDrafts[item.id]" @submit.prevent="createRecipientAction(item)">
                    <label v-if="item.integration_actions?.length">{{ t('admin.privacy.settings.cases.recipientDestination') }}
                      <select v-model="recipientActionDrafts[item.id].integrationActionID" class="app-select">
                        <option value="">{{ t('admin.privacy.settings.cases.recipientDestinationUnlinked') }}</option>
                        <option v-for="integrationAction in item.integration_actions" :key="integrationAction.id" :value="integrationAction.id">{{ integrationAction.destination_name }}</option>
                      </select>
                    </label>
                    <label>{{ t('admin.privacy.settings.cases.recipientName') }}
                      <input v-model.trim="recipientActionDrafts[item.id].recipientName" class="app-input" maxlength="200" required />
                    </label>
                    <label>{{ t('admin.privacy.settings.cases.recipientEmail') }}
                      <input v-model.trim="recipientActionDrafts[item.id].recipientEmail" class="app-input" type="email" maxlength="320" required />
                    </label>
                    <label class="privacy-recipient-instruction">{{ t('admin.privacy.settings.cases.recipientInstruction') }}
                      <textarea v-model="recipientActionDrafts[item.id].instructionSummary" class="app-textarea" maxlength="2000" required />
                    </label>
                    <button class="app-button" type="submit" :disabled="Boolean(actionPending) || !canWrite('tenant:update') || !recipientActionReady(item)">
                      <Send :size="14" />{{ actionPending === `recipient-action:${item.id}` ? t('admin.common.queuing') : t('admin.privacy.settings.cases.sendRecipientAction') }}
                    </button>
                  </form>
                  <div class="privacy-recipient-history">
                    <p v-if="!item.recipient_actions?.length">{{ t('admin.privacy.settings.cases.noRecipientActions') }}</p>
                    <article v-for="action in item.recipient_actions || []" :key="action.id">
                      <div class="privacy-recipient-summary">
                        <b>{{ action.recipient_name }}</b>
                        <span>{{ action.recipient_email }}</span>
                        <small>{{ t(`admin.privacy.settings.cases.recipientKinds.${action.action_kind}`) }} · {{ dateTime(action.created_at) }} · {{ memberLabel(action.created_by_member_id) }}</small>
                        <small v-if="action.integration_action_id">{{ t('admin.privacy.settings.cases.recipientLinkedToChecklist') }}</small>
                      </div>
                      <p>{{ action.instruction_summary }}</p>
                      <div class="privacy-recipient-delivery">
                        <span class="privacy-status" :data-tone="statusTone(action.status)"><i />{{ statusLabel(action.status) }}</span>
                        <small>{{ plural('admin.common.attempts', action.attempts, { count: action.attempts }) }}</small>
                        <small v-if="action.sent_at">{{ t('admin.privacy.settings.cases.recipientSentAt', { date: dateTime(action.sent_at) }) }}</small>
                        <small v-if="action.last_error">{{ action.last_error }}</small>
                        <button v-if="action.status === 'failed'" class="app-button" type="button" :disabled="Boolean(actionPending)" @click="retryRecipientAction(item, action)">
                          <RefreshCw :size="14" />{{ t('admin.privacy.settings.cases.retryRecipientAction') }}
                        </button>
                      </div>
                    </article>
                  </div>
                </section>
                <section class="privacy-case-communications">
                  <header>
                    <div>
                      <p class="app-eyebrow">{{ t('admin.privacy.settings.cases.communicationEyebrow') }}</p>
                      <b>{{ t('admin.privacy.settings.cases.communicationTitle') }}</b>
                      <small>{{ t('admin.privacy.settings.cases.communicationDescription') }}</small>
                    </div>
                    <MessageSquareText :size="17" />
                  </header>
                  <p v-if="!caseCommunicationKinds(item).length" class="privacy-communication-gate">{{ t('admin.privacy.settings.cases.communicationUnavailable') }}</p>
                  <form v-else-if="caseCommunicationDrafts[item.id]" @submit.prevent="createCaseCommunication(item)">
                    <label>{{ t('admin.privacy.settings.cases.communicationKind') }}
                      <select v-model="caseCommunicationDrafts[item.id].kind" class="app-select" @change="updateCaseCommunicationSubject(item)">
                        <option v-for="kind in caseCommunicationKinds(item)" :key="kind" :value="kind">{{ t(`admin.privacy.settings.cases.communicationKinds.${kind}`) }}</option>
                      </select>
                    </label>
                    <label>{{ t('admin.privacy.settings.cases.communicationSubject') }}
                      <input v-model.trim="caseCommunicationDrafts[item.id].subjectLine" class="app-input" maxlength="200" required />
                    </label>
                    <label class="privacy-communication-message">{{ t('admin.privacy.settings.cases.communicationMessage') }}
                      <textarea v-model="caseCommunicationDrafts[item.id].messageBody" class="app-textarea" maxlength="4000" required />
                    </label>
                    <button class="app-button" type="submit" :disabled="Boolean(actionPending) || !caseCommunicationReady(item)">
                      <Send :size="14" />{{ actionPending === `case-communication:${item.id}` ? t('admin.common.queuing') : t('admin.privacy.settings.cases.sendCommunication') }}
                    </button>
                  </form>
                  <div class="privacy-communication-history">
                    <p v-if="!item.communications?.length">{{ t('admin.privacy.settings.cases.noCommunications') }}</p>
                    <article v-for="communication in item.communications || []" :key="communication.id">
                      <div class="privacy-communication-summary">
                        <b>{{ communication.subject_line }}</b>
                        <span>{{ communication.recipient_email }}</span>
                        <small>{{ t(`admin.privacy.settings.cases.communicationKinds.${communication.communication_kind}`) }} · {{ dateTime(communication.created_at) }} · {{ memberLabel(communication.created_by_member_id) }}</small>
                      </div>
                      <p>{{ communication.message_body }}</p>
                      <div class="privacy-communication-delivery">
                        <span class="privacy-status" :data-tone="statusTone(communication.status)"><i />{{ statusLabel(communication.status) }}</span>
                        <small>{{ plural('admin.common.attempts', communication.attempts, { count: communication.attempts }) }}</small>
                        <small v-if="communication.sent_at">{{ t('admin.privacy.settings.cases.communicationSentAt', { date: dateTime(communication.sent_at) }) }}</small>
                        <small v-if="communication.last_error">{{ communication.last_error }}</small>
                        <button v-if="communication.status === 'failed'" class="app-button" type="button" :disabled="Boolean(actionPending)" @click="retryCaseCommunication(item, communication)">
                          <RefreshCw :size="14" />{{ t('admin.privacy.settings.cases.retryCommunication') }}
                        </button>
                      </div>
                    </article>
                  </div>
                </section>
                <section class="privacy-case-decision">
                  <header>
                    <div><p class="app-eyebrow">{{ t('admin.privacy.settings.cases.decision') }}</p><b>{{ t('admin.privacy.settings.cases.decisionTitle') }}</b></div>
                    <span class="privacy-status" :data-tone="statusTone(item.status)"><i />{{ statusLabel(item.status) }}</span>
                  </header>
                  <div v-if="caseIsTerminal(item)" class="privacy-case-decision-record">
                    <p v-if="item.decision_summary">{{ item.decision_summary }}</p>
                    <small v-if="item.legal_review_notes">{{ t('admin.privacy.settings.cases.legalReviewNotes') }}: {{ item.legal_review_notes }}</small>
                    <small v-if="item.exception_reason">{{ t('admin.privacy.settings.cases.exceptionReason') }}: {{ item.exception_reason }}</small>
                  </div>
                  <form v-else-if="caseDecisionDrafts[item.id] && caseTransitionOptions(item).length" :class="{ 'privacy-case-decision-form--evidence': item.right_type === 'erasure' && caseDecisionNeedsReason(item) }" @submit.prevent="transitionCase(item)">
                    <label>{{ t('admin.privacy.settings.cases.caseStatus') }}
                      <select v-model="caseDecisionDrafts[item.id].status" class="app-select">
                        <option v-for="target in caseTransitionOptions(item)" :key="target" :value="target">{{ statusLabel(target) }}</option>
                      </select>
                    </label>
                    <label>{{ t('admin.privacy.settings.cases.decisionSummary') }}
                      <textarea v-model="caseDecisionDrafts[item.id].decisionSummary" class="app-textarea" maxlength="4000" :required="caseDecisionNeedsReason(item)" />
                    </label>
                    <label v-if="item.right_type === 'erasure' && caseDecisionNeedsReason(item)">{{ t('admin.privacy.settings.cases.legalReviewNotes') }}
                      <textarea v-model="caseDecisionDrafts[item.id].legalReviewNotes" class="app-textarea" maxlength="4000" required />
                    </label>
                    <label v-if="item.right_type === 'erasure' && caseDecisionNeedsReason(item)">{{ t('admin.privacy.settings.cases.exceptionReason') }}
                      <textarea v-model="caseDecisionDrafts[item.id].exceptionReason" class="app-textarea" maxlength="4000" required />
                    </label>
                    <button class="app-button" type="submit" :disabled="Boolean(actionPending) || !caseDecisionReady(item)">
                      <ShieldCheck :size="14" />{{ actionPending === `transition-case:${item.id}` ? t('admin.common.saving') : t('admin.privacy.settings.cases.recordDecision') }}
                    </button>
                  </form>
                </section>
                <div class="privacy-case-execution">
                  <div>
                    <p class="app-eyebrow">{{ t('admin.privacy.settings.cases.execution') }}</p>
                    <template v-if="item.execution">
                      <b>{{ t(`admin.privacy.casePublic.rights.${item.execution.right_type}`) }}</b>
                      <span class="privacy-status" :data-tone="statusTone(item.execution.job_status || item.execution.request_status || 'pending')"><i />{{ statusLabel(item.execution.job_status || item.execution.request_status || 'pending') }}</span>
                      <small v-if="item.execution.record_count != null">{{ t('admin.privacy.settings.cases.records', { count: item.execution.record_count }) }}</small>
                      <small v-if="item.execution.manual_review_count">{{ t('admin.privacy.settings.cases.manualRecords', { count: item.execution.manual_review_count }) }}</small>
                    </template>
                    <small v-else>{{ t('admin.privacy.settings.cases.notStarted') }}</small>
                  </div>
                  <button v-if="caseCanExecute(item)" class="app-button" :class="{ 'privacy-danger-button': item.right_type === 'erasure' }" type="button" :disabled="Boolean(actionPending) || (item.right_type === 'erasure' && activeCaseHolds(item).length > 0)" @click="startCaseExecution(item)">
                    <ShieldAlert v-if="item.right_type === 'erasure'" :size="15" />
                    <Play v-else :size="15" />
                    {{ t(item.right_type === 'erasure'
                      ? 'admin.privacy.settings.cases.startErasure'
                      : item.right_type === 'portability_assessment'
                        ? 'admin.privacy.settings.cases.startPortability'
                        : 'admin.privacy.settings.cases.startExport') }}
                  </button>
                  <button v-else-if="['access', 'portability_assessment'].includes(item.execution?.right_type || '') && item.execution?.job_status === 'completed'" class="app-button" type="button" :disabled="Boolean(actionPending)" @click="downloadCaseExecution(item)">
                    <Download :size="15" />{{ t(item.right_type === 'portability_assessment' ? 'admin.privacy.settings.cases.downloadPortability' : 'admin.privacy.settings.cases.downloadExport') }}
                  </button>
                  <button v-else-if="item.execution?.job_id && item.execution.job_status === 'failed'" class="app-button" type="button" :disabled="Boolean(actionPending) || (item.right_type === 'erasure' && activeCaseHolds(item).length > 0)" @click="retryJob(item.execution.job_id)">
                    <RefreshCw :size="15" />{{ t('admin.privacy.settings.cases.retryExecution') }}
                  </button>
                </div>
                <div class="privacy-case-holds">
                  <header><LockKeyhole :size="15" /><b>{{ t('admin.privacy.settings.cases.legalHolds') }}</b></header>
                  <div v-if="!item.legal_holds.length" class="privacy-case-hold-empty">{{ t('admin.privacy.settings.cases.noHolds') }}</div>
                  <div v-for="hold in item.legal_holds" :key="hold.id" class="privacy-case-hold">
                    <div><b>{{ hold.reason }}</b><small v-if="hold.legal_reference">{{ hold.legal_reference }}</small><small>{{ dateTime(hold.placed_at) }} · {{ memberLabel(hold.placed_by_member_id) }}</small><small v-if="!hold.active && hold.release_reason">{{ t('admin.privacy.settings.cases.releasedReason', { reason: hold.release_reason }) }}</small></div>
                    <form v-if="hold.active" @submit.prevent="releaseCaseHold(item, hold.id)">
                      <input v-model.trim="releaseReasons[hold.id]" class="app-input" maxlength="2000" required :placeholder="t('admin.privacy.settings.cases.releaseReason')" />
                      <button class="app-button" type="submit" :disabled="Boolean(actionPending)"><UnlockKeyhole :size="14" />{{ t('admin.privacy.settings.cases.releaseHold') }}</button>
                    </form>
                    <span v-else class="privacy-status" data-tone="success"><i />{{ t('admin.privacy.settings.cases.released') }}</span>
                  </div>
                  <form class="privacy-case-hold-form" @submit.prevent="placeCaseHold(item)">
                    <input v-model.trim="holdReasons[item.id]" class="app-input" maxlength="2000" required :placeholder="t('admin.privacy.settings.cases.holdReason')" />
                    <input v-model.trim="holdReferences[item.id]" class="app-input" maxlength="500" :placeholder="t('admin.privacy.settings.cases.legalReference')" />
                    <button class="app-button" type="submit" :disabled="Boolean(actionPending)"><LockKeyhole :size="14" />{{ t('admin.privacy.settings.cases.placeHold') }}</button>
                  </form>
                </div>
              </div>
            </div>
          </div>
        </template>
      </section>

      <section class="app-surface privacy-section">
        <header>
          <div><p class="app-eyebrow">{{ t('admin.privacy.settings.policyEyebrow') }}</p><h2>{{ t('admin.privacy.settings.retention') }}</h2></div>
          <span v-if="data.retention" class="privacy-plan">{{ t('admin.privacy.settings.plan', { plan: t(`admin.plans.${data.retention.plan}`) }) }}</span>
        </header>
        <div v-if="!canReadRetention" class="privacy-empty"><ShieldCheck :size="22" /><h3>{{ t('admin.privacy.settings.readRequired') }}</h3></div>
        <div v-else>
          <div v-if="data.retention" class="privacy-retention-summary" :aria-label="t('admin.privacy.settings.lifecycleSummary')">
            <div><b>{{ data.retention.summary.enforced }}</b><span>{{ retentionStatusLabel('enforced') }}</span></div>
            <div><b>{{ data.retention.summary.configurable }}</b><span>{{ retentionStatusLabel('configurable') }}</span></div>
            <div><b>{{ data.retention.summary.decision_required }}</b><span>{{ retentionStatusLabel('decision_required') }}</span></div>
            <div><b>{{ data.retention.summary.external_control }}</b><span>{{ retentionStatusLabel('external_control') }}</span></div>
          </div>
          <div v-if="data.retentionHealth" class="privacy-retention-health">
            <div class="privacy-retention-health-heading">
              <span><ShieldCheck :size="17" /><b>{{ t('admin.privacy.settings.health.title') }}</b></span>
              <small>{{ t('admin.privacy.settings.health.tracked', { count: data.retentionHealth.summary.tracked }) }}</small>
            </div>
            <div class="privacy-retention-health-summary" :aria-label="t('admin.privacy.settings.health.summaryLabel')">
              <span><b>{{ data.retentionHealth.summary.healthy }}</b>{{ t('admin.privacy.settings.health.status.healthy') }}</span>
              <span><b>{{ data.retentionHealth.summary.attention }}</b>{{ t('admin.privacy.settings.health.status.attention') }}</span>
              <span><b>{{ data.retentionHealth.summary.failed + data.retentionHealth.summary.overdue }}</b>{{ t('admin.privacy.settings.health.needsAction') }}</span>
              <span><b>{{ data.retentionHealth.summary.not_observed }}</b>{{ t('admin.privacy.settings.health.status.not_observed') }}</span>
            </div>
            <div class="privacy-retention-health-table">
              <div
                v-for="operation in data.retentionHealth.operations"
                :key="operation.operation"
                class="privacy-retention-health-row"
              >
                <span>
                  <b>{{ retentionOperationLabel(operation.operation) }}</b>
                  <small>{{ retentionCategoryLabel(operation.category) }}</small>
                </span>
                <span>
                  <span class="privacy-status" :data-tone="retentionHealthTone(operation.state)"><i />{{ retentionHealthLabel(operation.state) }}</span>
                  <small>{{ retentionHealthAlert(operation.alert_reason) }}</small>
                </span>
                <span>
                  <b>{{ operation.last_success_at ? dateTime(operation.last_success_at) : t('admin.common.never') }}</b>
                  <small>{{ t('admin.privacy.settings.health.nextExpected', { date: dateTime(operation.next_expected_at) }) }}</small>
                </span>
                <span>
                  <b>{{ decimal(operation.last_processed) }} / {{ decimal(operation.total_processed) }}</b>
                  <small>
                    {{ t('admin.privacy.settings.health.processed') }}
                    <template v-if="operation.consecutive_failures">
                      · {{ plural('admin.privacy.settings.health.consecutiveFailures', operation.consecutive_failures, { count: operation.consecutive_failures }) }}
                    </template>
                  </small>
                </span>
              </div>
            </div>
          </div>
          <div class="privacy-table privacy-retention-table">
            <div class="privacy-row privacy-row--head"><span>{{ t('admin.privacy.settings.columns.category') }}</span><span>{{ t('admin.privacy.settings.columns.retention') }}</span><span>{{ t('admin.privacy.settings.columns.lifecycle') }}</span><span>{{ t('admin.privacy.settings.columns.operation') }}</span></div>
            <template v-for="policy in data.retention?.policies || []" :key="policy.category">
              <div class="privacy-row">
                <span class="privacy-retention-category">
                  <b>{{ retentionLabel(policy) }}</b>
                  <small>{{ retentionRole(policy.role) }}</small>
                </span>
                <span class="privacy-retention-duration">
                  <b>{{ retentionDuration(policy) }}</b>
                  <small>{{ retentionSource(policy.source) }}</small>
                </span>
                <span class="privacy-retention-lifecycle">
                  <span class="privacy-status" :data-tone="retentionStatusTone(policy.status)"><i />{{ retentionStatusLabel(policy.status) }}</span>
                  <small>{{ retentionLifecycleAction(policy.status) }}</small>
                </span>
                <span class="privacy-retention-operation">
                  <span>
                    <b>{{ retentionCadence(policy.cadence) }}</b>
                    <button
                      class="app-icon-button"
                      type="button"
                      :title="t('admin.privacy.settings.retentionDetails')"
                      :aria-expanded="expandedRetentionCategories.has(policy.category)"
                      @click="toggleRetentionDetails(policy.category)"
                    >
                      <ChevronUp v-if="expandedRetentionCategories.has(policy.category)" :size="14" />
                      <ListTree v-else :size="14" />
                    </button>
                  </span>
                  <small>{{ retentionHold(policy.hold_behavior) }}</small>
                  <small>{{ retentionResources(policy) }}</small>
                </span>
              </div>
              <div v-if="expandedRetentionCategories.has(policy.category)" class="privacy-retention-detail">
                <div><b>{{ t('admin.privacy.settings.detail.purpose') }}</b><p>{{ policy.purpose }}</p></div>
                <div><b>{{ t('admin.privacy.settings.detail.trigger') }}</b><p>{{ policy.trigger }}</p></div>
                <div><b>{{ t('admin.privacy.settings.detail.action') }}</b><p>{{ policy.action }}</p></div>
                <div><b>{{ t('admin.privacy.settings.detail.control') }}</b><p>{{ policy.implementation || t('admin.privacy.settings.detail.notAutomated') }}</p></div>
              </div>
            </template>
          </div>
        </div>
      </section>

      <section class="app-surface privacy-section">
        <header>
          <div><p class="app-eyebrow">{{ t('admin.privacy.settings.portabilityEyebrow') }}</p><h2>{{ t('admin.privacy.settings.exports') }}</h2></div>
          <button v-if="canWrite('tenant:update')" class="app-button app-button--primary" :disabled="Boolean(actionPending)" @click="createWorkspaceExport">
            <FileArchive :size="15" />{{ actionPending === 'create-export' ? t('admin.common.queuing') : t('admin.privacy.settings.generateExport') }}
          </button>
        </header>
        <div v-if="!canManagePrivacy" class="privacy-empty"><ShieldCheck :size="22" /><h3>{{ t('admin.privacy.settings.administrationRequired') }}</h3></div>
        <div v-else-if="!data.exports.length" class="privacy-empty"><FileArchive :size="22" /><h3>{{ t('admin.privacy.settings.noExports') }}</h3><p>{{ t('admin.privacy.settings.exportsDescription') }}</p></div>
        <div v-else class="privacy-table privacy-export-table">
          <div class="privacy-row privacy-row--head"><span>{{ t('admin.privacy.settings.columns.created') }}</span><span>{{ t('admin.privacy.settings.columns.status') }}</span><span>{{ t('admin.privacy.settings.columns.archive') }}</span><span>{{ t('admin.privacy.settings.columns.expires') }}</span><span /></div>
          <div v-for="item in data.exports" :key="item.id" class="privacy-row">
            <span><b>{{ dateTime(item.created_at) }}</b><small>{{ plural('admin.common.attempts', item.attempts, { count: item.attempts }) }}</small></span>
            <span class="privacy-status" :data-tone="statusTone(item.status)"><i />{{ statusLabel(item.status) }}</span>
            <span>{{ formatBytes(item.object_size_bytes) }}</span>
            <span>{{ item.expires_at ? dateTime(item.expires_at) : '—' }}</span>
            <span class="privacy-actions">
              <button v-if="item.status === 'completed'" class="app-icon-button" :title="t('admin.privacy.settings.downloadExport')" :disabled="Boolean(actionPending)" @click="downloadWorkspaceExport(item)"><Download :size="16" /></button>
              <button v-if="item.status === 'failed' || item.status === 'expired'" class="app-icon-button" :title="t('admin.privacy.settings.retryExport')" :disabled="Boolean(actionPending)" @click="retryJob(item.id)"><RefreshCw :size="16" /></button>
            </span>
          </div>
        </div>
      </section>

      <section class="app-surface privacy-section">
        <header><div><p class="app-eyebrow">{{ t('admin.privacy.settings.reporterEyebrow') }}</p><h2>{{ t('admin.privacy.settings.requests') }}</h2></div></header>
        <div v-if="!canManagePrivacy" class="privacy-empty"><ShieldCheck :size="22" /><h3>{{ t('admin.privacy.settings.administrationRequired') }}</h3></div>
        <template v-else>
          <form class="privacy-request-form" @submit.prevent="startReporterRequest">
            <label>{{ t('admin.privacy.settings.reporterEmail') }}<input v-model.trim="reporterEmail" class="app-input" type="email" maxlength="320" required placeholder="reporter@example.com" /></label>
            <label>{{ t('admin.privacy.settings.requestType') }}<select v-model="reporterAction" class="app-select"><option value="export">{{ t('admin.privacy.settings.exportReporter') }}</option><option value="anonymize">{{ t('admin.privacy.settings.anonymizeReporter') }}</option></select></label>
            <button class="app-button" :class="reporterAction === 'anonymize' ? 'privacy-danger-button' : 'app-button--primary'" type="submit" :disabled="Boolean(actionPending) || !reporterEmail">
              <ShieldAlert v-if="reporterAction === 'anonymize'" :size="15" /><FileArchive v-else :size="15" />
              {{ actionPending === 'create-request' ? t('admin.common.queuing') : reporterAction === 'anonymize' ? t('admin.privacy.settings.reviewAnonymization') : t('admin.privacy.settings.sendVerification') }}
            </button>
          </form>
          <div v-if="!data.requests.length" class="privacy-empty privacy-empty--compact"><ShieldCheck :size="22" /><h3>{{ t('admin.privacy.settings.noRequests') }}</h3><p>{{ t('admin.privacy.settings.noRequestsDescription') }}</p></div>
          <div v-else class="privacy-table privacy-request-table">
            <div class="privacy-row privacy-row--head"><span>{{ t('admin.privacy.settings.columns.reporter') }}</span><span>{{ t('admin.privacy.settings.columns.request') }}</span><span>{{ t('admin.privacy.settings.columns.status') }}</span><span>{{ t('admin.privacy.settings.columns.access') }}</span><span /></div>
            <template v-for="item in data.requests" :key="item.id">
              <div class="privacy-row">
                <span><b>{{ item.reporter_email || t('admin.privacy.settings.identityRemoved') }}</b><small>{{ dateTime(item.created_at) }}</small></span>
                <span class="privacy-action-label">{{ actionLabel(item.action) }}</span>
                <span>
                  <span class="privacy-status" :data-tone="statusTone(item.status)"><i />{{ statusLabel(item.status) }}</span>
                  <small v-if="item.job_status && item.job_status !== item.status">{{ t('admin.privacy.settings.jobStatus', { status: statusLabel(item.job_status) }) }}</small>
                </span>
                <span>
                  {{ item.access_expires_at ? dateTime(item.access_expires_at) : dateTime(item.verification_expires_at) }}
                  <small v-if="item.manual_review_count">{{ t('admin.privacy.settings.manualReview', { count: item.manual_review_count }) }}</small>
                </span>
                <span class="privacy-actions">
                  <button v-if="item.job_id && (item.status === 'failed' || item.job_status === 'failed')" class="app-icon-button" :title="t('admin.privacy.settings.retryRequest')" :disabled="Boolean(actionPending)" @click="retryJob(item.job_id)"><RefreshCw :size="16" /></button>
                  <button v-if="item.status === 'awaiting_manual_review'" class="app-icon-button" :title="t('admin.privacy.settings.review.toggle')" :disabled="Boolean(actionPending)" @click="toggleReview(item)">
                    <ChevronUp v-if="reviewExpanded[item.id]" :size="16" /><ChevronDown v-else :size="16" />
                  </button>
                </span>
              </div>
              <div v-if="item.status === 'awaiting_manual_review' && reviewExpanded[item.id]" class="privacy-review-queue">
                <header>
                  <h3>{{ t('admin.privacy.settings.review.title') }}</h3>
                  <button class="app-button" :disabled="Boolean(actionPending)" @click="finalizeRequest(item)">
                    {{ actionPending === `finalize:${item.id}` ? t('admin.common.queuing') : t('admin.privacy.settings.review.finalize') }}
                  </button>
                </header>
                <p v-if="!(reviewItems[item.id] || []).length" class="privacy-review-queue-empty">{{ t('admin.privacy.settings.review.empty') }}</p>
                <div v-for="reviewItem in reviewItems[item.id] || []" :key="reviewItem.id" class="privacy-review-item">
                  <div class="privacy-review-item-meta">
                    <b>{{ resourceLabel(reviewItem) }}</b>
                    <span>{{ fieldsLabel(reviewItem) }}</span>
                    <small v-if="disclosedDestinationsLabel(reviewItem)">{{ t('admin.privacy.settings.review.disclosedTo', { destinations: disclosedDestinationsLabel(reviewItem) }) }}</small>
                  </div>
                  <template v-if="reviewItem.status === 'pending'">
                    <input
                      v-model.trim="reviewReasons[reviewItem.id]"
                      class="app-input privacy-review-reason"
                      :placeholder="t('admin.privacy.settings.review.reasonPlaceholder')"
                      maxlength="2000"
                    />
                    <div class="privacy-review-item-actions">
                      <button class="app-button" :disabled="Boolean(actionPending)" @click="decideReviewItem(item, reviewItem, 'redacted')">{{ t('admin.privacy.settings.review.decisionRedacted') }}</button>
                      <button class="app-button" :disabled="Boolean(actionPending)" @click="decideReviewItem(item, reviewItem, 'deleted')">{{ t('admin.privacy.settings.review.decisionDeleted') }}</button>
                      <button class="app-button" :disabled="Boolean(actionPending)" @click="decideReviewItem(item, reviewItem, 'not_personal_data')">{{ t('admin.privacy.settings.review.decisionNotPersonalData') }}</button>
                      <button class="app-button privacy-danger-button" :disabled="Boolean(actionPending)" @click="decideReviewItem(item, reviewItem, 'retained_exception')">{{ t('admin.privacy.settings.review.decisionRetainedException') }}</button>
                    </div>
                  </template>
                  <div v-else class="privacy-review-item-decided">
                    <span class="privacy-status" :data-tone="reviewItem.status === 'retained_exception' ? 'pending' : 'success'">{{ t(`admin.privacy.settings.review.decision.${reviewItem.status}`) }}</span>
                    <small v-if="reviewItem.decision_reason">{{ reviewItem.decision_reason }}</small>
                  </div>
                </div>
              </div>
            </template>
          </div>
          <p class="privacy-review-note"><Clock3 :size="14" />{{ t('admin.privacy.settings.reviewNote') }}</p>
        </template>
      </section>
    </template>

    <AppDialog :open="anonymizeDialogOpen" aria-labelledby="privacy-anonymize-title" @close="anonymizeDialogOpen = false">
      <section class="privacy-dialog">
        <header><div><p class="app-eyebrow">{{ t('admin.privacy.settings.destructiveEyebrow') }}</p><h2 id="privacy-anonymize-title">{{ t('admin.privacy.settings.confirmAnonymization') }}</h2></div><button class="app-icon-button" :title="t('admin.common.close')" @click="anonymizeDialogOpen = false"><X :size="18" /></button></header>
        <form @submit.prevent="createReporterRequest">
          <p>{{ t('admin.privacy.settings.anonymizationDescription') }}</p>
          <label><span>{{ t('admin.privacy.settings.typeToConfirm', { value: reporterEmail }) }}</span><input v-model.trim="anonymizeConfirmation" class="app-input" autocomplete="off" required autofocus /></label>
          <footer><button class="app-button" type="button" @click="anonymizeDialogOpen = false">{{ t('admin.common.cancel') }}</button><button class="app-button privacy-danger-button" type="submit" :disabled="Boolean(actionPending) || anonymizeConfirmation !== reporterEmail">{{ actionPending === 'create-request' ? t('admin.common.queuing') : t('admin.privacy.settings.sendAnonymization') }}</button></footer>
        </form>
      </section>
    </AppDialog>
  </div>
</template>

<style scoped>
.privacy-settings { display: grid; gap: 16px; }
.privacy-section { overflow: hidden; }
.privacy-section > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; gap: 14px; padding: 11px 16px; border-bottom: 1px solid var(--line); }
.privacy-section h2 { margin: 3px 0 0; font-size: 14px; }
.privacy-plan { color: var(--teal); font-size: 9px; font-weight: 800; text-transform: uppercase; }
.privacy-alert { display: flex; min-height: 38px; align-items: center; gap: 8px; margin: 0; padding: 8px 11px; border-radius: 4px; font-size: 10px; font-weight: 700; }
.privacy-alert--error { background: var(--red-soft); color: var(--red); }
.privacy-alert--success { background: var(--green-soft); color: var(--green); }
.privacy-table { width: 100%; }
.privacy-row { display: grid; min-height: 60px; align-items: center; gap: 14px; padding: 7px 15px; border-bottom: 1px solid var(--line); color: var(--muted); font-size: 10px; }
.privacy-row:last-child { border-bottom: 0; }
.privacy-row--head { min-height: 36px; padding-top: 0; padding-bottom: 0; background: #f8faf9; font-size: 9px; font-weight: 750; text-transform: uppercase; }
.privacy-row > span { min-width: 0; }
.privacy-row b { display: block; overflow: hidden; color: var(--ink-soft); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.privacy-row small { display: block; margin-top: 3px; overflow: hidden; color: var(--muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.privacy-retention-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border-bottom: 1px solid var(--line); background: var(--surface-soft); }
.privacy-retention-summary div { display: flex; min-height: 46px; align-items: baseline; gap: 7px; padding: 10px 15px; border-right: 1px solid var(--line); }
.privacy-retention-summary div:last-child { border-right: 0; }
.privacy-retention-summary b { color: var(--ink); font-size: 16px; }
.privacy-retention-summary span { color: var(--muted); font-size: 9px; font-weight: 700; }
.privacy-retention-health { border-bottom: 1px solid var(--line); }
.privacy-retention-health-heading { display: flex; min-height: 45px; align-items: center; justify-content: space-between; gap: 12px; padding: 0 15px; }
.privacy-retention-health-heading > span { display: flex; align-items: center; gap: 7px; }
.privacy-retention-health-heading b { font-size: 11px; }
.privacy-retention-health-heading small { color: var(--muted); font-size: 9px; }
.privacy-retention-health-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); background: var(--surface-soft); border-block: 1px solid var(--line); }
.privacy-retention-health-summary > span { display: flex; min-height: 38px; align-items: baseline; gap: 6px; padding: 9px 15px; border-right: 1px solid var(--line); color: var(--muted); font-size: 9px; font-weight: 700; }
.privacy-retention-health-summary > span:last-child { border-right: 0; }
.privacy-retention-health-summary b { color: var(--ink); font-size: 14px; }
.privacy-retention-health-row { display: grid; min-height: 58px; grid-template-columns: minmax(180px, 1.1fr) minmax(150px, .8fr) minmax(210px, 1fr) minmax(130px, .6fr); align-items: center; border-top: 1px solid var(--line); }
.privacy-retention-health-row:first-child { border-top: 0; }
.privacy-retention-health-row > span { display: grid; min-width: 0; gap: 3px; padding: 9px 15px; }
.privacy-retention-health-row b { overflow: hidden; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.privacy-retention-health-row small { overflow: hidden; color: var(--muted); font-size: 8px; line-height: 1.4; text-overflow: ellipsis; white-space: nowrap; }
.privacy-retention-table .privacy-row { grid-template-columns: minmax(190px, 1.15fr) minmax(135px, .7fr) minmax(170px, .9fr) minmax(180px, 1fr); }
.privacy-retention-lifecycle { display: grid; align-content: center; }
.privacy-retention-operation { text-align: right; }
.privacy-retention-operation > span { display: flex; align-items: center; justify-content: flex-end; gap: 6px; }
.privacy-retention-operation .app-icon-button { width: 26px; height: 26px; min-height: 26px; }
.privacy-retention-operation small { overflow: visible; text-overflow: clip; white-space: normal; }
.privacy-retention-detail { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); background: var(--surface-soft); border-bottom: 1px solid var(--line); }
.privacy-retention-detail > div { min-width: 0; padding: 11px 15px; border-right: 1px solid var(--line); }
.privacy-retention-detail > div:last-child { border-right: 0; }
.privacy-retention-detail b { color: var(--muted); font-size: 8px; text-transform: uppercase; }
.privacy-retention-detail p { margin: 4px 0 0; overflow-wrap: anywhere; color: var(--ink-soft); font-size: 9px; line-height: 1.5; }
.privacy-export-table .privacy-row { grid-template-columns: minmax(190px, 1.1fr) 110px 90px minmax(170px, .9fr) 38px; }
.privacy-request-table .privacy-row { grid-template-columns: minmax(220px, 1.2fr) 90px 140px minmax(180px, .9fr) 38px; }
.privacy-status { display: inline-flex; align-items: center; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 700; text-transform: capitalize; }
.privacy-status i { width: 7px; height: 7px; background: #b98a26; border-radius: 50%; }
.privacy-status[data-tone='success'] i { background: var(--green); }
.privacy-status[data-tone='danger'] i { background: var(--red); }
.privacy-status[data-tone='external'] i { background: #3375a7; }
.privacy-action-label { color: var(--ink-soft); font-weight: 700; text-transform: capitalize; }
.privacy-actions { display: flex; justify-content: flex-end; }
.privacy-request-form { display: grid; grid-template-columns: minmax(220px, 1.4fr) minmax(190px, .8fr) auto; align-items: end; gap: 12px; padding: 14px 15px; background: var(--surface-soft); border-bottom: 1px solid var(--line); }
.privacy-request-form label, .privacy-case-form label, .privacy-dialog label { display: flex; flex-direction: column; gap: 6px; color: var(--ink-soft); font-size: 10px; font-weight: 750; }
.privacy-case-dashboard { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); border-bottom: 1px solid var(--line); }
.privacy-case-dashboard > div { display: grid; gap: 3px; padding: 13px 15px; border-right: 1px solid var(--line); }
.privacy-case-dashboard > div:last-child { border-right: 0; }
.privacy-case-dashboard b { color: var(--ink); font-size: 16px; }
.privacy-case-dashboard span { color: var(--muted); font-size: 9px; }
.privacy-case-form { display: grid; grid-template-columns: repeat(5, minmax(130px, 1fr)) auto; align-items: end; gap: 10px; padding: 14px 15px; background: var(--surface-soft); border-bottom: 1px solid var(--line); }
.privacy-case-form .app-button { white-space: nowrap; }
.privacy-case-table .privacy-row { grid-template-columns: 110px minmax(180px, 1.2fr) 150px minmax(160px, .8fr) minmax(120px, .7fr) 34px; }
.privacy-case-entry { border-bottom: 1px solid var(--line); }
.privacy-case-entry > .privacy-row { border-bottom: 0; }
.privacy-case-entry .app-icon-button { align-self: center; justify-self: end; }
.privacy-case-detail { display: grid; grid-template-columns: minmax(220px, .75fr) minmax(0, 1.5fr); border-top: 1px solid var(--line); background: var(--surface-soft); }
.privacy-case-detail-loading { display: flex; grid-column: 1 / -1; min-height: 76px; align-items: center; justify-content: center; gap: 8px; margin: 0; color: var(--muted); font-size: 9px; }
.privacy-case-overview { display: grid; grid-column: 1 / -1; grid-template-columns: minmax(250px, .9fr) minmax(180px, .65fr) minmax(300px, 1.2fr); background: var(--surface); border-bottom: 1px solid var(--line); }
.privacy-case-overview > div { min-width: 0; padding: 14px 15px; border-right: 1px solid var(--line); }
.privacy-case-overview > div:last-child { border-right: 0; }
.privacy-case-overview header { display: flex; align-items: center; gap: 7px; margin-bottom: 10px; color: var(--teal); }
.privacy-case-overview header b { color: var(--ink); font-size: 10px; }
.privacy-case-overview dl { display: grid; gap: 7px; margin: 0; }
.privacy-case-identity dl { grid-template-columns: 1fr 1fr; }
.privacy-case-overview dl div { min-width: 0; }
.privacy-case-overview dt { color: var(--muted); font-size: 8px; font-weight: 750; text-transform: uppercase; }
.privacy-case-overview dd { margin: 2px 0 0; overflow-wrap: anywhere; color: var(--ink-soft); font-size: 9px; line-height: 1.4; }
.privacy-case-scope > p, .privacy-case-timeline > p { margin: 0; color: var(--muted); font-size: 9px; }
.privacy-case-timeline ol { display: grid; gap: 0; margin: 0; padding: 0; list-style: none; }
.privacy-case-timeline li { position: relative; display: grid; grid-template-columns: 9px minmax(0, 1fr); gap: 8px; min-height: 42px; }
.privacy-case-timeline li::before { position: absolute; top: 10px; bottom: -4px; left: 3px; width: 1px; background: var(--line); content: ''; }
.privacy-case-timeline li:last-child::before { display: none; }
.privacy-case-timeline li > i { position: relative; z-index: 1; width: 7px; height: 7px; margin-top: 4px; background: var(--teal); border: 2px solid var(--surface); border-radius: 50%; box-sizing: content-box; }
.privacy-case-timeline li > div { display: grid; align-content: start; gap: 2px; }
.privacy-case-timeline li b { color: var(--ink-soft); font-size: 9px; }
.privacy-case-timeline li span, .privacy-case-timeline li small { color: var(--muted); font-size: 8px; }
.privacy-case-portability { display: grid; grid-column: 1 / -1; gap: 8px; padding: 14px 15px; background: var(--surface); border-bottom: 1px solid var(--line); }
.privacy-case-portability > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.privacy-case-portability > header > div { display: grid; gap: 4px; }
.privacy-case-portability > header b { color: var(--ink); font-size: 11px; }
.privacy-case-portability > p { margin: 0; color: var(--ink-soft); font-size: 10px; line-height: 1.5; }
.privacy-case-portability > small { color: var(--muted); font-size: 9px; }
.privacy-portability-summary { display: flex; flex-wrap: wrap; gap: 6px; }
.privacy-portability-summary span { padding: 4px 7px; background: var(--surface-soft); border: 1px solid var(--line); border-radius: 4px; color: var(--ink-soft); font-size: 9px; }
.privacy-case-portability form { display: grid; grid-template-columns: minmax(150px, .55fr) minmax(280px, 1.2fr) minmax(220px, 1fr) auto; align-items: end; gap: 10px; margin-top: 3px; }
.privacy-case-portability form > label { display: grid; gap: 5px; color: var(--ink-soft); font-size: 9px; font-weight: 750; }
.privacy-case-portability fieldset { display: flex; min-width: 0; flex-wrap: wrap; gap: 6px 12px; padding: 7px 9px; border: 1px solid var(--line); border-radius: 4px; }
.privacy-case-portability legend { padding: 0 4px; color: var(--muted); font-size: 8px; font-weight: 750; }
.privacy-case-portability fieldset label { display: inline-flex; align-items: center; gap: 5px; color: var(--ink-soft); font-size: 9px; }
.privacy-case-portability .app-textarea { min-height: 62px; resize: vertical; }
.privacy-portability-member-note { color: var(--muted) !important; }
.privacy-case-rectification { display: grid; grid-column: 1 / -1; gap: 12px; padding: 14px 15px; background: var(--surface); border-bottom: 1px solid var(--line); }
.privacy-case-rectification > header { display: flex; align-items: flex-start; gap: 8px; color: var(--ink-soft); }
.privacy-case-rectification > header > div { display: grid; gap: 4px; }
.privacy-case-rectification > header b { color: var(--ink); font-size: 11px; }
.privacy-case-rectification > header small { max-width: 680px; color: var(--muted); font-size: 9px; line-height: 1.5; }
.privacy-case-rectification > form { display: grid; grid-template-columns: minmax(0, 1.6fr) minmax(220px, .8fr) auto; align-items: end; gap: 10px; }
.privacy-case-rectification > form > label { display: grid; gap: 5px; color: var(--ink-soft); font-size: 9px; font-weight: 750; }
.privacy-case-rectification > form .app-textarea { min-height: 72px; resize: vertical; }
.privacy-case-rectification fieldset { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 9px; min-width: 0; padding: 9px; border: 1px solid var(--line); border-radius: 4px; }
.privacy-case-rectification legend { padding: 0 4px; color: var(--muted); font-size: 8px; font-weight: 750; }
.privacy-rectification-field { display: grid; align-content: end; gap: 6px; min-width: 0; }
.privacy-rectification-field label { display: flex; align-items: center; gap: 5px; color: var(--ink-soft); font-size: 9px; font-weight: 750; }
.privacy-rectification-history { display: grid; gap: 8px; padding-top: 2px; }
.privacy-rectification-history > header b { color: var(--ink-soft); font-size: 10px; }
.privacy-rectification-history > p { margin: 0; color: var(--muted); font-size: 9px; }
.privacy-rectification-history article { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; padding: 9px 0; border-top: 1px solid var(--line); }
.privacy-rectification-change { display: grid; gap: 4px; min-width: 0; }
.privacy-rectification-change > b { color: var(--muted); font-size: 8px; text-transform: uppercase; }
.privacy-rectification-change > span { display: grid; grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr); align-items: center; gap: 6px; color: var(--muted); }
.privacy-rectification-change code { overflow-wrap: anywhere; color: var(--ink-soft); font-family: inherit; font-size: 9px; }
.privacy-rectification-history article footer { display: grid; grid-column: 1 / -1; gap: 3px; }
.privacy-rectification-history article footer span { color: var(--muted); font-size: 8px; }
.privacy-rectification-history article footer p { margin: 0; color: var(--ink-soft); font-size: 9px; line-height: 1.5; }
.privacy-rectification-note { display: flex; align-items: center; gap: 7px; margin: 0; color: var(--muted); font-size: 9px; }
.privacy-feedback-rectification-form { grid-template-columns: minmax(180px, .7fr) minmax(280px, 1.4fr) minmax(220px, .9fr) auto !important; }
.privacy-feedback-rectification-form > label { display: grid; align-content: end; gap: 5px; color: var(--ink-soft); font-size: 9px; font-weight: 750; }
.privacy-feedback-rectification-form .app-textarea { min-height: 92px !important; white-space: pre-wrap; }
.privacy-feedback-rectification-history { grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr) !important; align-items: center; }
.privacy-feedback-rectification-history > svg { color: var(--muted); }
.privacy-feedback-rectification-history code { display: block; max-height: 150px; overflow: auto; white-space: pre-wrap; }
.privacy-feedback-rectification-history footer small { color: var(--teal); font-size: 8px; }
.privacy-case-erasure-review, .privacy-case-decision { display: grid; grid-column: 1 / -1; gap: 10px; padding: 14px 15px; background: var(--surface); border-bottom: 1px solid var(--line); }
.privacy-case-erasure-review > header, .privacy-case-decision > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.privacy-case-erasure-review > header > div, .privacy-case-decision > header > div { display: grid; gap: 4px; }
.privacy-case-erasure-review > header b, .privacy-case-decision > header b { color: var(--ink); font-size: 11px; }
.privacy-case-erasure-review > p { margin: 0; color: var(--muted); font-size: 9px; }
.privacy-erasure-totals { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid var(--line); border-radius: 4px; }
.privacy-erasure-totals span { display: grid; gap: 2px; padding: 9px; color: var(--muted); border-right: 1px solid var(--line); font-size: 8px; }
.privacy-erasure-totals span:last-child { border-right: 0; }
.privacy-erasure-totals span[data-exception='true'] { background: #fff9e9; color: #765d1d; }
.privacy-erasure-totals b { color: var(--ink-soft); font-size: 13px; }
.privacy-erasure-items { display: grid; border-top: 1px solid var(--line); }
.privacy-erasure-items article { display: grid; grid-template-columns: minmax(180px, .8fr) minmax(0, 1.2fr); gap: 14px; padding: 9px 0; border-bottom: 1px solid var(--line); }
.privacy-erasure-items article > div { display: grid; align-content: start; gap: 3px; min-width: 0; }
.privacy-erasure-items b { color: var(--ink-soft); font-size: 10px; }
.privacy-erasure-items code { overflow-wrap: anywhere; color: var(--muted); font-family: inherit; font-size: 8px; }
.privacy-erasure-items small { color: var(--muted); font-size: 8px; }
.privacy-erasure-items p { margin: 2px 0 0; color: var(--ink-soft); font-size: 9px; line-height: 1.5; }
.privacy-integration-actions { display: grid; grid-column: 1 / -1; gap: 11px; padding: 14px 15px; background: var(--surface); border-bottom: 1px solid var(--line); }
.privacy-integration-actions > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.privacy-integration-actions > header > div { display: grid; gap: 4px; }
.privacy-integration-actions > header b { color: var(--ink); font-size: 11px; }
.privacy-integration-actions > header small { max-width: 700px; color: var(--muted); font-size: 9px; line-height: 1.5; }
.privacy-integration-actions-empty, .privacy-integration-evidence-gate { margin: 0; color: var(--muted); font-size: 9px; line-height: 1.5; }
.privacy-integration-action-list { display: grid; border-top: 1px solid var(--line); }
.privacy-integration-action-list > article { display: grid; gap: 8px; padding: 11px 0; border-bottom: 1px solid var(--line); }
.privacy-integration-action-list > article:last-child { border-bottom: 0; }
.privacy-integration-action-summary { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.privacy-integration-action-summary > span:first-child { display: grid; gap: 2px; }
.privacy-integration-action-summary b { color: var(--ink-soft); font-size: 10px; }
.privacy-integration-action-summary small, .privacy-integration-action-list article > p { margin: 0; color: var(--muted); font-size: 8px; line-height: 1.45; }
.privacy-integration-categories { display: flex; flex-wrap: wrap; gap: 5px; }
.privacy-integration-categories span { padding: 3px 5px; background: var(--surface-soft); color: var(--muted); border: 1px solid var(--line); border-radius: 3px; font-size: 8px; }
.privacy-integration-action-list .privacy-integration-capability { color: #795d1d; }
.privacy-integration-completion { display: flex; align-items: flex-start; gap: 7px; color: var(--green); }
.privacy-integration-completion > span { display: grid; gap: 3px; }
.privacy-integration-completion b { color: var(--ink-soft); font-size: 9px; }
.privacy-integration-completion small { color: var(--muted); font-size: 8px; }
.privacy-integration-action-list form { display: grid; grid-template-columns: minmax(260px, 1fr) auto; align-items: end; gap: 9px; }
.privacy-integration-action-list form label { display: grid; gap: 5px; color: var(--ink-soft); font-size: 9px; font-weight: 750; }
.privacy-integration-action-list footer { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.privacy-integration-action-list footer small { color: var(--muted); font-size: 8px; }
.privacy-recipient-actions { display: grid; grid-column: 1 / -1; gap: 11px; padding: 14px 15px; background: var(--surface); border-bottom: 1px solid var(--line); }
.privacy-recipient-actions > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; color: var(--teal); }
.privacy-recipient-actions > header > div { display: grid; gap: 4px; }
.privacy-recipient-actions > header b { color: var(--ink); font-size: 11px; }
.privacy-recipient-actions > header small { max-width: 700px; color: var(--muted); font-size: 9px; line-height: 1.5; }
.privacy-recipient-actions > form { display: grid; grid-template-columns: minmax(150px, .65fr) minmax(150px, .65fr) minmax(200px, .8fr) minmax(260px, 1.4fr) auto; align-items: end; gap: 10px; }
.privacy-recipient-actions > form > label { display: grid; gap: 5px; color: var(--ink-soft); font-size: 9px; font-weight: 750; }
.privacy-recipient-actions .app-textarea { min-height: 68px; resize: vertical; }
.privacy-recipient-gate { margin: 0; color: var(--muted); font-size: 9px; line-height: 1.5; }
.privacy-recipient-history { display: grid; border-top: 1px solid var(--line); }
.privacy-recipient-history > p { margin: 9px 0 0; color: var(--muted); font-size: 9px; }
.privacy-recipient-history article { display: grid; grid-template-columns: minmax(190px, .7fr) minmax(260px, 1.3fr) minmax(150px, .55fr); align-items: start; gap: 14px; padding: 10px 0; border-bottom: 1px solid var(--line); }
.privacy-recipient-history article:last-child { border-bottom: 0; }
.privacy-recipient-history article > p { margin: 0; color: var(--ink-soft); font-size: 9px; line-height: 1.55; white-space: pre-wrap; }
.privacy-recipient-summary, .privacy-recipient-delivery { display: grid; gap: 4px; min-width: 0; }
.privacy-recipient-summary b { overflow-wrap: anywhere; color: var(--ink-soft); font-size: 10px; }
.privacy-recipient-summary span { overflow-wrap: anywhere; color: var(--muted); font-size: 9px; }
.privacy-recipient-summary small, .privacy-recipient-delivery small { color: var(--muted); font-size: 8px; line-height: 1.4; }
.privacy-recipient-delivery .app-button { justify-self: start; margin-top: 3px; }
.privacy-case-communications { display: grid; grid-column: 1 / -1; gap: 11px; padding: 14px 15px; background: var(--surface); border-bottom: 1px solid var(--line); }
.privacy-case-communications > header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; color: var(--teal); }
.privacy-case-communications > header > div { display: grid; gap: 4px; }
.privacy-case-communications > header b { color: var(--ink); font-size: 11px; }
.privacy-case-communications > header small { max-width: 700px; color: var(--muted); font-size: 9px; line-height: 1.5; }
.privacy-case-communications > form { display: grid; grid-template-columns: minmax(150px, .55fr) minmax(220px, .85fr) minmax(280px, 1.4fr) auto; align-items: end; gap: 10px; }
.privacy-case-communications > form > label { display: grid; gap: 5px; color: var(--ink-soft); font-size: 9px; font-weight: 750; }
.privacy-case-communications .app-textarea { min-height: 72px; resize: vertical; }
.privacy-communication-gate { margin: 0; color: var(--muted); font-size: 9px; line-height: 1.5; }
.privacy-communication-history { display: grid; border-top: 1px solid var(--line); }
.privacy-communication-history > p { margin: 9px 0 0; color: var(--muted); font-size: 9px; }
.privacy-communication-history article { display: grid; grid-template-columns: minmax(210px, .85fr) minmax(280px, 1.35fr) minmax(150px, .55fr); align-items: start; gap: 14px; padding: 10px 0; border-bottom: 1px solid var(--line); }
.privacy-communication-history article:last-child { border-bottom: 0; }
.privacy-communication-history article > p { margin: 0; color: var(--ink-soft); font-size: 9px; line-height: 1.55; white-space: pre-wrap; }
.privacy-communication-summary, .privacy-communication-delivery { display: grid; gap: 4px; min-width: 0; }
.privacy-communication-summary b, .privacy-communication-summary span { overflow-wrap: anywhere; }
.privacy-communication-summary b { color: var(--ink-soft); font-size: 10px; }
.privacy-communication-summary span { color: var(--muted); font-size: 9px; }
.privacy-communication-summary small, .privacy-communication-delivery small { color: var(--muted); font-size: 8px; line-height: 1.4; }
.privacy-communication-delivery .app-button { justify-self: start; margin-top: 3px; }
.privacy-case-decision form { display: grid; grid-template-columns: minmax(150px, .5fr) minmax(240px, 1fr) auto; align-items: end; gap: 10px; }
.privacy-case-decision .privacy-case-decision-form--evidence { grid-template-columns: minmax(150px, .5fr) repeat(3, minmax(210px, 1fr)) auto; }
.privacy-case-decision form > label { display: grid; gap: 5px; color: var(--ink-soft); font-size: 9px; font-weight: 750; }
.privacy-case-decision .app-textarea { min-height: 66px; resize: vertical; }
.privacy-case-decision-record { display: grid; gap: 5px; }
.privacy-case-decision-record p { margin: 0; color: var(--ink-soft); font-size: 10px; line-height: 1.5; }
.privacy-case-decision-record small { color: var(--muted); font-size: 9px; line-height: 1.5; }
.privacy-case-execution, .privacy-case-holds { padding: 14px 15px; }
.privacy-case-execution { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; border-right: 1px solid var(--line); }
.privacy-case-execution > div { display: grid; gap: 6px; }
.privacy-case-execution b { color: var(--ink); font-size: 11px; }
.privacy-case-execution small { color: var(--muted); font-size: 9px; }
.privacy-case-holds { display: grid; gap: 9px; }
.privacy-case-holds > header { display: flex; align-items: center; gap: 7px; color: var(--ink-soft); font-size: 10px; }
.privacy-case-hold-empty { color: var(--muted); font-size: 9px; }
.privacy-case-hold { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 10px; padding: 9px; background: var(--surface); border: 1px solid var(--line); border-radius: 4px; }
.privacy-case-hold > div { display: grid; gap: 3px; }
.privacy-case-hold b { color: var(--ink); font-size: 10px; }
.privacy-case-hold small { color: var(--muted); font-size: 9px; }
.privacy-case-hold form { display: flex; align-items: center; gap: 7px; }
.privacy-case-hold form .app-input { width: min(260px, 30vw); }
.privacy-case-hold-form { display: grid; grid-template-columns: minmax(180px, 1fr) minmax(130px, .65fr) auto; gap: 7px; }
.privacy-request-form .app-button { white-space: nowrap; }
.privacy-danger-button { background: var(--red); color: #fff; border-color: var(--red); }
.privacy-danger-button:hover:not(:disabled) { background: #962f3a; color: #fff; border-color: #962f3a; }
.privacy-review-note { display: flex; min-height: 42px; align-items: center; gap: 8px; margin: 0; padding: 8px 15px; background: #fff9e9; color: #765d1d; border-top: 1px solid #eadcad; font-size: 9px; line-height: 1.5; }
.privacy-review-queue { display: grid; gap: 10px; padding: 14px 15px; background: #fff9e9; border-top: 1px solid #eadcad; border-bottom: 1px solid #eadcad; }
.privacy-review-queue > header { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.privacy-review-queue h3 { margin: 0; color: #765d1d; font-size: 11px; }
.privacy-review-queue-empty { margin: 0; color: #765d1d; font-size: 10px; }
.privacy-review-item { display: grid; gap: 8px; padding: 10px; background: #fff; border: 1px solid #eadcad; border-radius: 4px; }
.privacy-review-item-meta { display: flex; flex-wrap: wrap; align-items: baseline; gap: 8px; font-size: 10px; }
.privacy-review-item-meta b { color: var(--ink-soft); }
.privacy-review-item-meta span { color: var(--muted); }
.privacy-review-item-meta small { flex-basis: 100%; color: var(--muted); }
.privacy-review-reason { font-size: 10px; }
.privacy-review-item-actions { display: flex; flex-wrap: wrap; gap: 6px; }
.privacy-review-item-actions .app-button { padding: 0 10px; font-size: 9px; }
.privacy-review-item-decided { display: flex; align-items: center; gap: 8px; }
.privacy-review-item-decided small { color: var(--muted); font-size: 10px; }
.privacy-empty { display: grid; min-height: 190px; place-items: center; align-content: center; gap: 7px; padding: 20px; color: var(--muted); text-align: center; }
.privacy-empty--compact { min-height: 130px; }
.privacy-empty h3, .privacy-empty p { margin: 0; }
.privacy-empty h3 { color: var(--ink-soft); font-size: 12px; }
.privacy-empty p { max-width: 420px; font-size: 9px; line-height: 1.5; }
.privacy-dialog { width: min(560px, 100%); overflow: hidden; background: #fff; border: 1px solid var(--line); border-radius: 6px; box-shadow: var(--shadow-overlay); }
.privacy-dialog > header { display: flex; min-height: 68px; align-items: center; justify-content: space-between; gap: 14px; padding: 10px 15px; border-bottom: 1px solid var(--line); }
.privacy-dialog h2 { margin: 3px 0 0; font-size: 15px; }
.privacy-dialog form { display: grid; gap: 15px; padding: 16px; }
.privacy-dialog form > p { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.65; }
.privacy-dialog label b { color: var(--red); }
.privacy-dialog footer { display: flex; justify-content: flex-end; gap: 8px; margin: 1px -16px -16px; padding: 12px 16px; background: #f8faf9; border-top: 1px solid var(--line); }
@media (max-width: 900px) {
  .privacy-row--head { display: none; }
  .privacy-retention-table .privacy-row { grid-template-columns: minmax(0, 1fr) auto; }
  .privacy-retention-health-row { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .privacy-retention-lifecycle { grid-column: 1; }
  .privacy-retention-operation { grid-column: 2; }
  .privacy-retention-detail { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .privacy-retention-detail > div:nth-child(2) { border-right: 0; }
  .privacy-retention-detail > div:nth-child(-n + 2) { border-bottom: 1px solid var(--line); }
  .privacy-export-table .privacy-row, .privacy-request-table .privacy-row { grid-template-columns: minmax(0, 1fr) auto 38px; }
  .privacy-export-table .privacy-row > span:nth-child(3), .privacy-export-table .privacy-row > span:nth-child(4), .privacy-request-table .privacy-row > span:nth-child(4) { display: none; }
  .privacy-request-form { grid-template-columns: 1fr 1fr; }
  .privacy-case-dashboard { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .privacy-case-form { grid-template-columns: 1fr 1fr; }
  .privacy-case-table .privacy-row { grid-template-columns: minmax(0, 1fr) 140px 120px 34px; }
  .privacy-case-table .privacy-row > span:nth-child(2), .privacy-case-table .privacy-row > span:nth-child(5) { display: none; }
  .privacy-case-detail { grid-template-columns: 1fr; }
  .privacy-case-overview { grid-template-columns: 1fr 1fr; }
  .privacy-case-timeline { grid-column: 1 / -1; border-top: 1px solid var(--line); }
  .privacy-case-overview > div:nth-child(2) { border-right: 0; }
  .privacy-case-portability form { grid-template-columns: 1fr 1fr; }
  .privacy-case-portability form .app-button { justify-self: end; }
  .privacy-case-rectification > form { grid-template-columns: 1fr; }
  .privacy-feedback-rectification-form { grid-template-columns: 1fr 1fr !important; }
  .privacy-feedback-rectification-form .privacy-feedback-description { grid-column: 1 / -1; }
  .privacy-case-rectification > form .app-button { justify-self: end; }
  .privacy-recipient-actions > form { grid-template-columns: 1fr 1fr; }
  .privacy-integration-action-list form { grid-template-columns: 1fr auto; }
  .privacy-recipient-instruction { grid-column: 1 / -1; }
  .privacy-recipient-actions > form .app-button { justify-self: end; }
  .privacy-case-communications > form { grid-template-columns: 1fr 1fr; }
  .privacy-communication-message { grid-column: 1 / -1; }
  .privacy-case-communications > form .app-button { justify-self: end; }
  .privacy-case-decision form, .privacy-case-decision .privacy-case-decision-form--evidence { grid-template-columns: 1fr 1fr; }
  .privacy-case-decision form .app-button { justify-self: end; }
  .privacy-case-execution { border-right: 0; border-bottom: 1px solid var(--line); }
  .privacy-case-hold-form { grid-template-columns: 1fr; }
  .privacy-request-form .app-button { grid-column: 1 / -1; justify-self: end; }
}
@media (max-width: 650px) {
  .privacy-section > header { align-items: flex-start; flex-direction: column; }
  .privacy-section > header .app-button { width: 100%; }
  .privacy-row b { overflow: visible; line-height: 1.35; text-overflow: clip; white-space: normal; }
  .privacy-retention-table .privacy-row { grid-template-columns: minmax(0, 1fr) auto; }
  .privacy-retention-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .privacy-retention-summary div:nth-child(2) { border-right: 0; }
  .privacy-retention-summary div:nth-child(-n + 2) { border-bottom: 1px solid var(--line); }
  .privacy-retention-detail { grid-template-columns: 1fr; }
  .privacy-retention-health-heading { align-items: flex-start; flex-direction: column; justify-content: center; padding-block: 9px; }
  .privacy-retention-health-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .privacy-retention-health-summary > span:nth-child(2) { border-right: 0; }
  .privacy-retention-health-summary > span:nth-child(-n + 2) { border-bottom: 1px solid var(--line); }
  .privacy-retention-health-row { grid-template-columns: repeat(2, minmax(0, 1fr)); padding-block: 4px; }
  .privacy-retention-health-row b,
  .privacy-retention-health-row small { overflow: visible; text-overflow: clip; white-space: normal; }
  .privacy-retention-detail > div { border-right: 0; border-bottom: 1px solid var(--line); }
  .privacy-retention-detail > div:last-child { border-bottom: 0; }
  .privacy-export-table .privacy-row, .privacy-request-table .privacy-row { grid-template-columns: minmax(0, 1fr) 38px; }
  .privacy-export-table .privacy-row > span:not(:first-child):not(:last-child), .privacy-request-table .privacy-row > span:not(:first-child):not(:last-child) { display: none; }
  .privacy-request-form { grid-template-columns: 1fr; }
  .privacy-case-dashboard { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .privacy-case-form { grid-template-columns: 1fr; }
  .privacy-case-form .app-button { width: 100%; }
  .privacy-case-table .privacy-row { grid-template-columns: minmax(0, 1fr) auto 34px; }
  .privacy-case-table .privacy-row > span:not(:first-child):not(:nth-child(3)) { display: none; }
  .privacy-case-overview { grid-template-columns: 1fr; }
  .privacy-case-overview > div { border-right: 0; border-bottom: 1px solid var(--line); }
  .privacy-case-overview > div:last-child { border-bottom: 0; }
  .privacy-case-timeline { grid-column: auto; border-top: 0; }
  .privacy-case-identity dl { grid-template-columns: 1fr; }
  .privacy-case-execution { align-items: stretch; flex-direction: column; }
  .privacy-case-execution .app-button { width: 100%; }
  .privacy-case-portability > header { flex-direction: column; }
  .privacy-case-portability form { grid-template-columns: 1fr; }
  .privacy-case-portability form .app-button { width: 100%; }
  .privacy-case-rectification fieldset, .privacy-rectification-history article { grid-template-columns: 1fr; }
  .privacy-feedback-rectification-form, .privacy-feedback-rectification-history { grid-template-columns: 1fr !important; }
  .privacy-feedback-rectification-form .privacy-feedback-description { grid-column: auto; }
  .privacy-feedback-rectification-history > svg { transform: rotate(90deg); }
  .privacy-case-rectification > form .app-button { width: 100%; }
  .privacy-case-erasure-review > header, .privacy-case-decision > header { flex-direction: column; }
  .privacy-recipient-actions > header { flex-direction: row; }
  .privacy-integration-actions > header { align-items: stretch; flex-direction: column; }
  .privacy-integration-actions > header .app-button { width: 100%; }
  .privacy-integration-action-summary { align-items: flex-start; flex-direction: column; }
  .privacy-integration-action-list form { grid-template-columns: 1fr; }
  .privacy-integration-action-list form .app-button { width: 100%; }
  .privacy-integration-action-list footer { align-items: stretch; flex-direction: column; }
  .privacy-integration-action-list footer .app-button { width: 100%; }
  .privacy-recipient-actions > form, .privacy-recipient-history article { grid-template-columns: 1fr; }
  .privacy-recipient-instruction { grid-column: auto; }
  .privacy-recipient-actions > form .app-button { width: 100%; }
  .privacy-case-communications > header { flex-direction: row; }
  .privacy-case-communications > form, .privacy-communication-history article { grid-template-columns: 1fr; }
  .privacy-communication-message { grid-column: auto; }
  .privacy-case-communications > form .app-button { width: 100%; }
  .privacy-erasure-totals { grid-template-columns: 1fr 1fr; }
  .privacy-erasure-totals span:nth-child(2) { border-right: 0; }
  .privacy-erasure-totals span:nth-child(-n+2) { border-bottom: 1px solid var(--line); }
  .privacy-erasure-items article, .privacy-case-decision form, .privacy-case-decision .privacy-case-decision-form--evidence { grid-template-columns: 1fr; }
  .privacy-case-decision form .app-button { width: 100%; }
  .privacy-case-hold { grid-template-columns: 1fr; }
  .privacy-case-hold form { align-items: stretch; flex-direction: column; }
  .privacy-case-hold form .app-input { width: 100%; }
  .privacy-request-form .app-button { grid-column: auto; width: 100%; }
  .privacy-dialog footer { flex-direction: column-reverse; }
  .privacy-dialog footer .app-button { width: 100%; }
}
</style>
