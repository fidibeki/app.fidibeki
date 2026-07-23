export type TenantPlan = 'starter' | 'growth' | 'scale' | 'enterprise'
export type TenantStatus = 'active' | 'trial' | 'suspended' | 'cancelled'
export type FeedbackStatus = 'open' | 'spam' | 'archived'
export type ProcessingStatus = 'pending' | 'processing' | 'deferred' | 'completed' | 'failed' | 'skipped'
export type IssueStatus = 'open' | 'in_progress' | 'needs_info' | 'resolved' | 'closed'
export type TriagePriority = 'low' | 'medium' | 'high' | 'urgent'

export interface TriageAssignee {
  id: string
  name?: string | null
  email: string
  avatar_version?: string | null
}

export interface TriageTag {
  id: string
  name: string
  color: string
}

export interface Member {
  id: string
  tenant_id: string
  email: string
  name?: string | null
  avatar_version?: string | null
  locale?: string | null
  timezone?: string | null
  permissions: string[]
  status: 'active' | 'deactivated' | 'removed'
  role: 'owner' | 'member'
  verified: boolean
}

export interface MemberDomain {
  id: string
  tenant_id: string
  domain: string
  verification_email: string
  verification_expires_at: string
  verified_at?: string
  status: 'pending' | 'verified'
  created_at: string
  updated_at: string
}

export interface TenantSummary {
  id: string
  name: string
  slug: string
  plan: TenantPlan
  status: TenantStatus
  trial_ends_at?: string | null
  deletion_requested_at?: string | null
  reporter_access_enabled: boolean
  widget_appearance_defaults?: WidgetAppearanceOverrides
  short_name?: string | null
  website_url?: string | null
  support_email?: string | null
  default_locale?: string | null
  default_timezone?: string | null
  privacy_policy_url?: string | null
  terms_url?: string | null
  logo_version?: string | null
}

export interface OrganizationProfile {
  name: string
  short_name?: string | null
  website_url?: string | null
  support_email?: string | null
  accent_color?: string | null
  default_locale?: string | null
  default_timezone?: string | null
  privacy_policy_url?: string | null
  terms_url?: string | null
  logo_version?: string | null
}

export interface AuthResponse {
  access_token: string
  token_type: 'Bearer'
  expires_in: number
  member: Member
  tenant: TenantSummary
}

export interface AuthSession {
  accessToken: string | null
  expiresAt: number | null
  member: Member | null
  tenant: TenantSummary | null
}

export interface APIErrorBody {
  error: {
    code: string
    message: string
    details?: unknown
    reason?: string
    retry_after_seconds?: number
    sentry_event_id?: string
  }
}

export interface Project {
  id: string
  tenant_id: string
  name: string
  public_key: string
  allowed_domains: string[]
  default_locale: string
  reporter_access_enabled?: boolean | null
  widget_appearance_overrides?: WidgetAppearanceOverrides
  widget_last_seen_at?: string | null
  created_at: string
  updated_at: string
}

export type MatcherType = 'all_pages' | 'url_exact' | 'url_prefix' | 'url_regex'
export type TriggerType = 'delay' | 'scroll' | 'exit_intent'
export type WidgetPosition = 'bottom-left' | 'bottom-right'
export type WidgetTheme = 'light' | 'dark' | 'auto'
export type ReplayMode = 'none' | 'full' | 'sampling'
export type ReplayConsentMode = 'none' | 'fidibeki_managed' | 'customer_cmp'
export type RecurrenceMode = 'cooldown' | 'session' | 'never'
export type WidgetDefinitionStatus = 'draft' | 'active' | 'paused' | 'archived'
export type WidgetPresentation = 'launcher' | 'inline' | 'reaction' | 'prompt'
export type WidgetObjective = 'continuous_feedback' | 'after_engagement' | 'validate_content' | 'detect_friction'
export type WidgetResponseBlockType = 'free_text' | 'rating' | 'reaction' | 'single_choice'

export interface WidgetAppearanceOverrides {
  position?: WidgetPosition
  theme?: WidgetTheme
  accent_color?: string
  surface_color?: string
  text_color?: string
  logo_url?: string
}

export interface WidgetAppearance {
  position: WidgetPosition
  theme: WidgetTheme
  accent_color: string
  accent_foreground: string
  surface_color?: string
  text_color?: string
  logo_url?: string
}

export interface WidgetResponseOption {
  value: string
}

export interface WidgetResponseBlock {
  key: string
  type: WidgetResponseBlockType
  required: boolean
  options?: WidgetResponseOption[]
}

export interface WidgetLocalization {
  launcher_label: string
  prompt: string
  comment_placeholder: string
  contact_label: string
  consent_label: string
  submit_label: string
  success_message: string
  block_labels?: Record<string, string>
  option_labels?: Record<string, Record<string, string>>
}

export interface ReplayConsentNotice {
  title: string
  description: string
  accept_label: string
  reject_label: string
  withdraw_label: string
}

export interface ReplayConsentConfig {
  mode: ReplayConsentMode
  notice_version: string
  controller_name: string
  privacy_policy_url: string
  supported_locales: string[]
  retention_days: number
  expiry_days: number
  notices: Record<string, ReplayConsentNotice>
}

export interface ReplayConsentDiagnostic {
  widget_rule_id: string
  replay_mode: ReplayMode
  consent_mode: ReplayConsentMode
  notice_version: string
  state: 'not_observed' | 'active' | 'denied' | 'withdrawn' | 'expired'
  locale?: string
  source?: ReplayConsentMode
  accepted_at?: string
  withdrawn_at?: string
  expires_at?: string
  latest_session_started_at?: string
}

export interface WidgetTargetCondition {
  id: string
  matcher_type: MatcherType
  matcher_value?: string | null
  position: number
}

export interface WidgetTargetConditionInput {
  matcher_type: MatcherType
  matcher_value: string | null
}

export interface WidgetRule {
  id: string
  project_id: string
  name: string
  priority: number
  enabled: boolean
  matcher_type: MatcherType
  matcher_value?: string | null
  include_conditions?: WidgetTargetCondition[]
  exclude_conditions?: WidgetTargetCondition[]
  trigger_type: TriggerType
  trigger_delay_seconds?: number | null
  trigger_scroll_percent?: number | null
  can_rate: boolean
  can_write_text: boolean
  can_record_audio: boolean
  can_record_video: boolean
  can_attach_element: boolean
  can_attach_screenshot: boolean
  greeting?: string | null
  position: WidgetPosition
  theme: WidgetTheme
  accent_color?: string | null
  replay_mode: ReplayMode
  replay_sampling_rate: number
  replay_max_duration_seconds: number
  replay_max_events: number
  replay_mask_all_inputs: boolean
  replay_mask_text_selector?: string | null
  replay_record_canvas: boolean
  replay_canvas_fps: number
  replay_consent: ReplayConsentConfig
  recurrence_mode: RecurrenceMode
  recurrence_days?: number | null
  definition_key: string
  definition_version: number
  definition_status: WidgetDefinitionStatus
  presentation: WidgetPresentation
  objective: WidgetObjective
  response_blocks: WidgetResponseBlock[]
  localizations: Record<string, WidgetLocalization>
  allow_comment: boolean
  collect_contact: boolean
  appearance_overrides?: WidgetAppearanceOverrides
  appearance?: WidgetAppearance
  created_at: string
  updated_at: string
}

export type WidgetRuleInput = Omit<
  WidgetRule,
  'id' | 'project_id' | 'definition_version' | 'created_at' | 'updated_at' | 'include_conditions' | 'exclude_conditions'
> & {
  include_conditions?: WidgetTargetConditionInput[]
  exclude_conditions?: WidgetTargetConditionInput[]
}

export interface DashboardTrendPoint {
  date: string
  feedback_count: number
  satisfaction?: number
}

export interface DashboardSummary {
  range_days: number
  feedback_count: number
  feedback_delta_percent?: number
  open_issue_count: number
  satisfaction?: number
  satisfaction_delta?: number
  attention_count: number
  trend: DashboardTrendPoint[]
}

export interface WidgetAnalyticsSummary {
  matched: number
  impressions: number
  interactions: number
  submissions: number
  dismissals: number
  impression_rate: number | null
  interaction_rate: number | null
  completion_rate: number | null
  submit_rate: number | null
}

export interface WidgetAnalyticsTrendPoint {
  date: string
  impressions: number
  interactions: number
  submissions: number
}

export interface WidgetAnalyticsBreakdown extends WidgetAnalyticsSummary {
  project_id?: string
  project_name?: string
  rule_id?: string
  rule_name?: string
  page_url?: string
  deleted?: boolean
}

export interface WidgetAnalyticsResponse {
  range: '7d' | '30d' | '90d'
  period: {
    start_date: string
    end_date: string
    timezone: 'UTC'
  }
  filters: {
    project_id?: string
    rule_id?: string
    page?: string
  }
  summary: WidgetAnalyticsSummary
  trend: WidgetAnalyticsTrendPoint[]
  projects: WidgetAnalyticsBreakdown[]
  rules: WidgetAnalyticsBreakdown[]
  pages: WidgetAnalyticsBreakdown[]
  options: {
    projects: Array<{ id: string, name: string }>
    rules: Array<{ id: string, project_id: string, name: string, deleted: boolean }>
    pages: Array<{ url: string }>
  }
  metadata: {
    projects: WidgetAnalyticsResultMetadata
    rules: WidgetAnalyticsResultMetadata
    pages: WidgetAnalyticsResultMetadata
    project_options: WidgetAnalyticsResultMetadata
    rule_options: WidgetAnalyticsResultMetadata
    page_options: WidgetAnalyticsResultMetadata
  }
}

export interface WidgetAnalyticsResultMetadata {
  total: number
  returned: number
  limit: number
  truncated: boolean
}

export type SubmissionHealthCategory =
  | 'rate_limit'
  | 'spam'
  | 'plan_quota'
  | 'upload_safety'
  | 'ai_budget'
  | 'provider'

export interface SubmissionHealthTotal {
  category: SubmissionHealthCategory
  count: number
}

export interface SubmissionHealthPoint {
  project_id: string
  bucket_start: string
  category: SubmissionHealthCategory
  reason: string
  count: number
}

export interface SubmissionHealthResponse {
  range: '24h' | '7d' | '30d'
  period: {
    start_at: string
    end_at: string
    timezone: 'UTC'
  }
  filters: {
    project_id?: string
  }
  total: number
  totals: SubmissionHealthTotal[]
  trend: SubmissionHealthPoint[]
}

export interface FeedbackItem {
  id: string
  public_id: string
  project_id: string
  project_name: string
  widget_rule_id?: string
  session_id?: string
  issue_id?: string
  issue_public_id?: string
  issue_status?: IssueStatus
  rating?: number
  responses?: Record<string, string | number>
  comment?: string
  widget_definition_version?: number
  title: string
  description?: string
  text?: string
  text_cleaned?: string
  ai_suggested_title?: string
  ai_actionability?: 'actionable' | 'not_actionable' | 'uncertain'
  ai_category?: string
  ai_priority?: 'low' | 'medium' | 'high' | 'urgent'
  priority?: TriagePriority | null
  assignee?: TriageAssignee | null
  tags: TriageTag[]
  reporter_email?: string
  notify_reporter: boolean
  page_url: string
  referrer?: string
  user_agent?: string
  os?: string
  browser?: string
  device?: string
  viewport_width?: number
  viewport_height?: number
  language?: string
  status: FeedbackStatus
  ai_status: ProcessingStatus
  sentiment_score?: number
  moderation_flags: string[]
  attachment_kinds: Array<'screenshot' | 'audio' | 'video' | 'replay'>
  has_replay: boolean
  created_at: string
  updated_at: string
}

export interface FeedbackAttachment {
  id: string
  kind: 'screenshot' | 'audio' | 'video' | 'replay'
  content_type: string
  size_bytes: number
  duration_seconds?: number
  processing_status: ProcessingStatus
  safety_status: 'pending' | 'quarantined' | 'safe' | 'rejected' | 'expired'
  safety_checked_at?: string
  safety_error_reason?: string
  detected_content_type?: string
  error_message?: string
  transcription?: string
  sentiment_score?: number
  moderation_flags: string[]
  processing_attempts: number
  completed_at?: string
  processed_at?: string
  created_at: string
  retention_deleted_at?: string
  content_url?: string
}

export interface FeedbackSession {
  id: string
  status: 'recording' | 'processing' | 'ready' | 'failed' | 'expired'
  page_url?: string
  started_at: string
  ended_at?: string
  events_count: number
  batches_count: number
  rrweb_version: string
  replay_ready: boolean
  merge_error?: string
  retention_deleted_at?: string
  replay_url?: string
}

export interface FeedbackDetail extends FeedbackItem {
  dom_snapshot_html?: string
  dom_snapshot_styles?: string
  attachments: FeedbackAttachment[]
  session?: FeedbackSession
  ai_processed_at?: string
}

export interface FeedbackPage {
  feedbacks: FeedbackItem[]
  pagination: {
    next_cursor?: string | null
    has_more: boolean
    limit: number
  }
}

export interface Issue {
  id: string
  tenant_id: string
  project_id: string
  public_id: string
  title: string
  description: string
  status: IssueStatus
  priority?: TriagePriority | null
  assignee?: TriageAssignee | null
  tags: TriageTag[]
  merged_into_id?: string
  merged_at?: string
  reporter_email?: string
  notify_reporter: boolean
  reporter_access_revoked_at?: string | null
  resolved_at?: string
  feedback_count: number
  created_at: string
  updated_at: string
}

export interface IssueUpdate {
  id: string
  issue_id: string
  member_id?: string
  old_status?: IssueStatus
  new_status: IssueStatus
  comment?: string
  created_at: string
}

export interface ActivityActor {
  id: string
  name?: string | null
  email: string
  avatar_version?: string | null
}

export interface IssueActivity {
  id: string
  issue_id: string
  actor?: ActivityActor | null
  kind: string
  visibility: 'internal' | 'reporter'
  data: Record<string, unknown>
  created_at: string
}

export interface IssueComment {
  id: string
  issue_id: string
  author?: ActivityActor | null
  body: string
  mentions: ActivityActor[]
  created_at: string
}

export interface ReporterProgressEvent {
  from_status?: IssueStatus | null
  to_status: IssueStatus
  created_at: string
}

export interface ReporterClarification {
  prompt: string
  responded: boolean
  responded_at?: string | null
}

export interface ReporterProgress {
  public_id: string
  title: string
  status: IssueStatus
  created_at: string
  resolved_at?: string | null
  events: ReporterProgressEvent[]
  clarification?: ReporterClarification | null
}

export interface MemberNotification {
  id: string
  kind: string
  issue_id: string
  public_id: string
  issue_title: string
  actor?: ActivityActor | null
  body: string
  read_at?: string | null
  created_at: string
}

export interface PlanLimits {
  projects: number
  feedback_per_month: number
  users: number
  storage_days: number
}

export interface PlanFeatures {
  ai: boolean
  session_replay: boolean
  slack: boolean
  video: boolean
  audio: boolean
  dom_capture: boolean
  custom_domain: boolean
}

export interface PlanDefinition {
  plan: TenantPlan
  monthly_price_cents?: number | null
  yearly_discount_percent?: number | null
  limits: PlanLimits
  features: PlanFeatures
}

export interface PlanCatalogResponse {
  plans: PlanDefinition[]
}

export interface SignupResponse {
  tenant: TenantSummary
}

export interface TenantConfiguration {
  tenant: TenantSummary
  plan: PlanDefinition
}

export type PrivacyJobKind = 'workspace_export' | 'reporter_export' | 'reporter_anonymize'
export type PrivacyJobStatus =
  | 'pending'
  | 'processing'
  | 'awaiting_manual_review'
  | 'completed'
  | 'failed'
  | 'expired'
export type ReporterPrivacyAction = 'export' | 'anonymize'
export type ReporterPrivacyStatus =
  | 'pending_verification'
  | 'processing'
  | 'awaiting_manual_review'
  | 'completed'
  | 'failed'
  | 'expired'
export type ReviewItemResourceType = 'feedback' | 'issue' | 'issue_comment' | 'clarification_request' | 'legacy_unlinked'
export type ReviewItemStatus = 'pending' | 'redacted' | 'deleted' | 'not_personal_data' | 'retained_exception'
export type ReviewDecisionStatus = Exclude<ReviewItemStatus, 'pending'>

export interface ReviewItem {
  id: string
  privacy_job_id?: string
  resource_type: ReviewItemResourceType
  resource_id: string
  fields: string[]
  status: ReviewItemStatus
  decision_reason?: string
  decided_by_member_id?: string
  decided_at?: string
  disclosed_destinations?: Record<string, number>
  created_at: string
  updated_at: string
}

export interface PrivacyExport {
  id: string
  kind: PrivacyJobKind
  status: PrivacyJobStatus
  actor_member_id?: string
  attempts: number
  started_at?: string
  completed_at?: string
  expires_at?: string
  object_size_bytes?: number
  error_message?: string
  created_at: string
  updated_at: string
}

export interface ReporterPrivacyRequest {
  id: string
  action: ReporterPrivacyAction
  status: ReporterPrivacyStatus
  reporter_email?: string
  actor_member_id?: string
  verification_expires_at: string
  verified_at?: string
  access_expires_at?: string
  completed_at?: string
  record_count?: number
  manual_review_count?: number
  error_message?: string
  job_id?: string
  job_status?: PrivacyJobStatus
  job_expires_at?: string
  created_at: string
  updated_at: string
}

export interface ReporterPrivacyView {
  action: ReporterPrivacyAction
  status: ReporterPrivacyStatus
  completed_at?: string
  record_count?: number
  manual_review_count?: number
  job_status?: PrivacyJobStatus
  download_available: boolean
  access_expires_at: string
}

export interface RetentionPolicy {
  category: string
  label: string
  duration: string
  duration_days?: number
  source: string
}

export interface EffectiveRetention {
  plan: TenantPlan
  policies: RetentionPolicy[]
}

export interface APIKey {
  id: string
  tenant_id: string
  name: string
  token_prefix: string
  permissions: string[]
  rate_limit_per_min: number
  last_used_at?: string | null
  expires_at?: string | null
  created_at: string
  updated_at: string
}

export type IntegrationKind = 'slack' | 'teams' | 'webhook'
export type IntegrationEvent = 'feedback.created' | 'issue.created' | 'issue.updated' | 'issue.resolved'

export interface Integration {
  id: string
  kind: IntegrationKind
  name: string
  webhook_url: string
  channel?: string
  webhook_secret_fingerprint?: string
  custom_header_names: string[]
  events: IntegrationEvent[]
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface BillingSummary {
  plan: TenantPlan
  pending_plan?: TenantPlan | null
  status: TenantStatus
  trial_ends_at?: string | null
  has_customer: boolean
  has_subscription: boolean
  deletion_requested_at?: string | null
}
