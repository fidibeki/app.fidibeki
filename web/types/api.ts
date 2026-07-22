export type TenantPlan = 'starter' | 'growth' | 'scale' | 'enterprise'
export type TenantStatus = 'active' | 'trial' | 'suspended' | 'cancelled'
export type FeedbackStatus = 'open' | 'spam' | 'archived'
export type ProcessingStatus = 'pending' | 'processing' | 'completed' | 'failed' | 'skipped'
export type IssueStatus = 'open' | 'in_progress' | 'resolved' | 'closed'
export type TriagePriority = 'low' | 'medium' | 'high' | 'urgent'

export interface TriageAssignee {
  id: string
  name?: string | null
  email: string
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
  name?: string
  permissions: string[]
}

export interface TenantSummary {
  id: string
  name: string
  slug: string
  plan: TenantPlan
  status: TenantStatus
  trial_ends_at?: string | null
  deletion_requested_at?: string | null
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
    sentry_event_id?: string
  }
}

export interface Project {
  id: string
  tenant_id: string
  name: string
  public_key: string
  allowed_domains: string[]
  widget_last_seen_at?: string | null
  created_at: string
  updated_at: string
}

export type MatcherType = 'all_pages' | 'url_exact' | 'url_prefix' | 'url_regex'
export type TriggerType = 'delay' | 'scroll' | 'exit_intent'
export type WidgetPosition = 'bottom-left' | 'bottom-right'
export type WidgetTheme = 'light' | 'dark' | 'auto'
export type ReplayMode = 'none' | 'full' | 'sampling'
export type RecurrenceMode = 'cooldown' | 'session' | 'never'

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
  recurrence_mode: RecurrenceMode
  recurrence_days?: number | null
  created_at: string
  updated_at: string
}

export type WidgetRuleInput = Omit<
  WidgetRule,
  'id' | 'project_id' | 'created_at' | 'updated_at' | 'include_conditions' | 'exclude_conditions'
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
  error_message?: string
  transcription?: string
  sentiment_score?: number
  moderation_flags: string[]
  processing_attempts: number
  completed_at?: string
  processed_at?: string
  created_at: string
	content_url?: string
}

export interface FeedbackSession {
  id: string
  status: 'recording' | 'processing' | 'ready' | 'failed'
  page_url?: string
  started_at: string
  ended_at?: string
  events_count: number
  batches_count: number
  rrweb_version: string
  replay_ready: boolean
  merge_error?: string
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
