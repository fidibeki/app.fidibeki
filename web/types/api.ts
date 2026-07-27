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
  privacy_contact_email?: string | null
  privacy_rights_email?: string | null
  privacy_profile_version?: number
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
  privacy_contact_email?: string | null
  privacy_rights_email?: string | null
  privacy_profile_version: number
  terms_url?: string | null
  logo_version?: string | null
}

export type TwoFactorPolicyMode = 'optional' | 'required'
export type TwoFactorRequirement = 'challenge_required' | 'enrollment_required'

export interface TwoFactorRequirementResponse {
  status: TwoFactorRequirement
}

/**
 * A session that still owes its two-factor challenge is answered with the
 * requirement alone: no access token and no workspace metadata.
 */
export interface AuthResponse {
  access_token: string
  token_type: 'Bearer'
  expires_in: number
  member: Member
  tenant: TenantSummary
  two_factor?: TwoFactorRequirementResponse
}

export type AuthOrTwoFactorResponse = AuthResponse | { two_factor: TwoFactorRequirementResponse }

export function twoFactorRequirement(
  response: AuthOrTwoFactorResponse | null | undefined,
): TwoFactorRequirement | null {
  const status = response && 'two_factor' in response ? response.two_factor?.status : null
  return status === 'challenge_required' || status === 'enrollment_required' ? status : null
}

export function isAuthResponse(response: AuthOrTwoFactorResponse): response is AuthResponse {
  return typeof (response as AuthResponse).access_token === 'string'
}

export interface AuthSession {
  accessToken: string | null
  expiresAt: number | null
  member: Member | null
  tenant: TenantSummary | null
}

export interface TwoFactorStatus {
  policy: TwoFactorPolicyMode
  enrolled: boolean
  confirmed_at?: string | null
  recovery_codes_remaining: number
  can_remove: boolean
  authentication_method: 'local' | 'saml'
}

export interface TwoFactorEnrollment {
  secret: string
  provisioning_uri: string
  qr_code_svg: string
}

export interface TwoFactorPolicy {
  policy: TwoFactorPolicyMode
  updated_at?: string | null
  updated_by?: string | null
  /** Required single sign-on has delegated multi-factor assurance to the IdP. */
  managed_by_idp: boolean
}

export interface TwoFactorCompliance {
  member_id: string
  email: string
  name?: string | null
  enrolled: boolean
  confirmed_at?: string | null
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
export type AIInputCategory = 'feedback_text' | 'screenshot' | 'audio' | 'video'
export type AnalyticsConsentMode = 'none' | 'fidibeki_managed' | 'customer_cmp'
export type RecurrenceMode = 'cooldown' | 'session' | 'never'
export type WidgetDefinitionStatus = 'draft' | 'active' | 'paused' | 'archived'
export type WidgetPresentation = 'launcher' | 'inline' | 'reaction' | 'prompt'
export type WidgetLauncherVariant = 'edge_tab' | 'floating_button'
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
  privacy_purpose_summary?: string
  privacy_lawful_basis_summary?: string
  privacy_retention_summary?: string
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

export interface AIProcessingConfig {
  enabled: boolean
  policy_version?: string
  provider?: 'google_gemini'
  purposes?: string[]
  input_categories?: AIInputCategory[]
  privacy_policy_url?: string
  sensitive_data_mode?: 'prohibit_and_quarantine'
  acknowledged?: boolean
  acknowledged_at?: string
  acknowledged_by_member_id?: string
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

export interface AnalyticsConsentNotice {
  title: string
  description: string
  accept_label: string
  reject_label: string
  withdraw_label: string
}

export interface AnalyticsConsentConfig {
  mode: AnalyticsConsentMode
  notice_version: string
  controller_name: string
  privacy_policy_url: string
  supported_locales: string[]
  retention_days: number
  expiry_days: number
  notices: Record<string, AnalyticsConsentNotice>
}

export interface AnalyticsConsentDiagnostic {
  widget_rule_id: string
  consent_mode: AnalyticsConsentMode
  notice_version: string
  active_count: number
  expiring_soon_count: number
  denied_count: number
  withdrawn_count: number
  expired_count: number
  recent_count: number
  next_expires_at?: string
  last_activity_at?: string
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
  analytics_consent: AnalyticsConsentConfig
  recurrence_mode: RecurrenceMode
  recurrence_days?: number | null
  definition_key: string
  definition_version: number
  definition_status: WidgetDefinitionStatus
  presentation: WidgetPresentation
  launcher_variant: WidgetLauncherVariant
  objective: WidgetObjective
  response_blocks: WidgetResponseBlock[]
  localizations: Record<string, WidgetLocalization>
  allow_comment: boolean
  collect_contact: boolean
  ai_processing: AIProcessingConfig
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
  /** Rides with `ai`: without moderation the Suggested queue can never rank. */
  testimonials: boolean
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
  yearly_price_cents?: number | null
  yearly_discount_percent?: number | null
  pricing_model?: 'per_seat' | 'custom'
  price_unit?: 'seat' | null
  limits: PlanLimits
  features: PlanFeatures
}

export interface PlanCatalogResponse {
  plans: PlanDefinition[]
}

export interface SignupResponse {
  tenant: TenantSummary
}

/**
 * Deployment-level facts, not workspace settings. They exist so the UI never offers an
 * action this deployment cannot carry out.
 */
export interface DeploymentCapabilities {
  /** False when no Slack app is configured, which hides the one-click install button. */
  slack_one_click_install: boolean
}

export interface TenantConfiguration {
  tenant: TenantSummary
  plan: PlanDefinition
  capabilities?: DeploymentCapabilities
}

export type SSOMode = 'disabled' | 'optional' | 'required'

/** Organization-level SSO policy the login page reads before any session exists. */
export interface SSOPolicy {
  mode: SSOMode
  start_url?: string
  local_login_enabled: boolean
  break_glass_available: boolean
}

export interface SAMLCertificate {
  id: string
  certificate: string
  fingerprint: string
  subject: string
  not_before: string
  not_after: string
  created_at: string
}

export interface SAMLConfiguration {
  mode: SSOMode
  idp_entity_id: string
  idp_sso_url: string
  idp_metadata_url?: string
  email_domains: string[]
  allow_idp_initiated: boolean
  sp_certificate: string
  last_tested_at?: string
  last_test_error?: string
  last_successful_login_at?: string
  created_at: string
  updated_at: string
  updated_by_member_id?: string
  certificates: SAMLCertificate[]
}

/** The service-provider values an administrator copies into their IdP. */
export interface SAMLServiceProvider {
  entity_id: string
  acs_url: string
  metadata_url: string
  start_url: string
  certificate: string
}

export type SAMLWarning = 'no_valid_certificate' | 'certificate_expiring' | 'mfa_managed_by_idp'

export interface SAMLSettings {
  configured: boolean
  sso: SAMLConfiguration | null
  service_provider: SAMLServiceProvider
  can_require?: boolean
  warnings?: SAMLWarning[]
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

export type PrivacyRightType =
  | 'access'
  | 'rectification'
  | 'erasure'
  | 'restriction'
  | 'objection'
  | 'portability_assessment'
  | 'consent_withdrawal'
export type PrivacyCaseStatus =
  | 'received'
  | 'pending_verification'
  | 'in_progress'
  | 'awaiting_clarification'
  | 'paused'
  | 'awaiting_legal_review'
  | 'partially_fulfilled'
  | 'completed'
  | 'refused'
  | 'cancelled'

export interface ReporterPrivacyCaseView {
  public_id: string
  right_type: PrivacyRightType
  status: PrivacyCaseStatus
  received_at: string
  deadline_at: string
}

export type PrivacySubjectType = 'reporter' | 'member'
export type PrivacyControllerRole = 'controller' | 'customer_controller_assistance'
export type PrivacyProcessingPurpose =
  | 'ai'
  | 'integration'
  | 'reporter_email'
  | 'replay'
  | 'analytics'
  | 'background_processing'

export interface PrivacyProcessingRestriction {
  id: string
  privacy_case_id: string
  purposes: PrivacyProcessingPurpose[]
  active: boolean
  reason: string
  activated_at: string
  deactivated_at?: string
  deactivated_by_member_id?: string
}

export interface PrivacyCaseLegalHold {
  id: string
  privacy_case_id: string
  reason: string
  legal_reference?: string
  active: boolean
  placed_by_member_id: string
  placed_at: string
  released_by_member_id?: string
  released_at?: string
  release_reason?: string
  created_at: string
  updated_at: string
}

export interface PrivacyCaseExecution {
  right_type: PrivacyRightType
  request_status?: ReporterPrivacyRequest['status']
  job_id?: string
  job_status?: PrivacyExport['status']
  job_expires_at?: string
  record_count?: number
  manual_review_count?: number
  error_message?: string
  started_at: string
  completed_at?: string
  erasure_review?: PrivacyErasureReviewEvidence
}

export interface PrivacyErasureReviewEvidence {
  pending: number
  redacted: number
  deleted: number
  not_personal_data: number
  retained_exception: number
  items: ReviewItem[]
}

export type PrivacyPortabilityDecision =
  | 'approved'
  | 'partially_approved'
  | 'not_applicable'
  | 'refused'
export type PrivacyPortabilityDataCategory =
  | 'feedback'
  | 'issues'
  | 'attachments'
  | 'replay'
  | 'ai_processing'
  | 'consent'
  | 'delivery'
  | 'widget_events'

export interface PrivacyPortabilityAssessment {
  id: string
  privacy_case_id: string
  decision: PrivacyPortabilityDecision
  data_categories: PrivacyPortabilityDataCategory[]
  export_format?: 'jsonl_zip'
  reason: string
  assessed_by_member_id: string
  assessed_at: string
  created_at: string
}

export interface PrivacyCaseRectification {
  id: string
  privacy_case_id: string
  subject_member_id: string
  changes: Partial<Record<'name' | 'locale' | 'timezone', string | null>>
  previous_values: Partial<Record<'name' | 'locale' | 'timezone', string | null>>
  reason: string
  applied_by_member_id: string
  applied_at: string
  created_at: string
}

export interface RectifiableFeedback {
  id: string
  public_id: string
  title: string
  description?: string
  page_url: string
  text_process_status: string
  created_at: string
}

export interface PrivacyFeedbackRectification {
  id: string
  privacy_case_id: string
  feedback_id: string
  previous_description?: string
  description: string
  reason: string
  derived_data_invalidated: boolean
  applied_by_member_id: string
  applied_at: string
  created_at: string
}

export type PrivacyRecipientActionKind = 'rectification' | 'erasure'
export type PrivacyRecipientActionStatus = 'pending' | 'processing' | 'sent' | 'failed'

export interface PrivacyRecipientDelivery {
  id: string
  attempt_number: number
  status: PrivacyRecipientActionStatus
  provider_message_id?: string
  error_message?: string
  started_at: string
  completed_at?: string
}

export interface PrivacyRecipientAction {
  id: string
  privacy_case_id: string
  action_kind: PrivacyRecipientActionKind
  recipient_name: string
  recipient_email: string
  instruction_summary: string
  status: PrivacyRecipientActionStatus
  attempts: number
  last_error?: string
  provider_message_id?: string
  last_attempt_at?: string
  sent_at?: string
  created_by_member_id: string
  integration_action_id?: string
  created_at: string
  updated_at: string
  delivery_attempts: PrivacyRecipientDelivery[]
}

export type PrivacyIntegrationActionHandlingMode = 'manual' | 'api'
export type PrivacyIntegrationActionStatus = 'manual_required' | 'api_pending' | 'completed' | 'failed'

export interface PrivacyIntegrationAction {
  id: string
  privacy_case_id: string
  integration_id?: string
  destination_key: string
  destination_kind?: IntegrationKind
  destination_name: string
  action_kind: PrivacyRecipientActionKind
  handling_mode: PrivacyIntegrationActionHandlingMode
  status: PrivacyIntegrationActionStatus
  capability_snapshot: {
    message_update?: boolean
    message_delete?: boolean
    message_recall?: boolean
    fidibeki_links_require_authentication?: boolean
    fidibeki_links_access_controlled?: boolean
  }
  data_categories: string[]
  disclosure_count: number
  first_disclosed_at: string
  last_disclosed_at: string
  completed_by_member_id?: string
  completed_at?: string
  completion_note?: string
  linked_recipient_action_ids: string[]
  created_at: string
  updated_at: string
}

export type PrivacyCaseCommunicationKind =
  | 'progress_update'
  | 'clarification_request'
  | 'decision_notice'
export type PrivacyCaseCommunicationStatus = 'pending' | 'processing' | 'sent' | 'failed'

export interface PrivacyCaseCommunicationDelivery {
  id: string
  attempt_number: number
  status: PrivacyCaseCommunicationStatus
  provider_message_id?: string
  error_message?: string
  started_at: string
  completed_at?: string
}

export interface PrivacyCaseCommunication {
  id: string
  privacy_case_id: string
  communication_kind: PrivacyCaseCommunicationKind
  recipient_email: string
  subject_line: string
  message_body: string
  status: PrivacyCaseCommunicationStatus
  attempts: number
  last_error?: string
  provider_message_id?: string
  last_attempt_at?: string
  sent_at?: string
  created_by_member_id: string
  created_at: string
  updated_at: string
  delivery_attempts: PrivacyCaseCommunicationDelivery[]
}

export interface PrivacyCaseEvent {
  id: string
  sequence: number
  privacy_case_id: string
  event_type: string
  actor_member_id?: string
  previous_status?: PrivacyCaseStatus
  new_status?: PrivacyCaseStatus
  occurred_at: string
}

export interface PrivacyCase {
  id: string
  public_id: string
  subject_type: PrivacySubjectType
  right_type: PrivacyRightType
  controller_role: PrivacyControllerRole
  status: PrivacyCaseStatus
  subject_email?: string
  subject_member_id?: string
  requested_by_member_id: string
  owner_member_id?: string
  verification_expires_at?: string
  verification_method?: string
  verified_at?: string
  received_at: string
  deadline_at: string
  pause_started_at?: string
  paused_seconds: number
  completed_at?: string
  scope: Record<string, unknown>
  decision_summary?: string
  legal_review_notes?: string
  exception_reason?: string
  restriction?: PrivacyProcessingRestriction
  legal_holds: PrivacyCaseLegalHold[]
  execution?: PrivacyCaseExecution
  portability_assessment?: PrivacyPortabilityAssessment
  rectifications?: PrivacyCaseRectification[]
  feedback_rectifications?: PrivacyFeedbackRectification[]
  integration_actions?: PrivacyIntegrationAction[]
  recipient_actions?: PrivacyRecipientAction[]
  communications?: PrivacyCaseCommunication[]
  events?: PrivacyCaseEvent[]
  created_at: string
  updated_at: string
}

export interface PrivacyCaseDashboard {
  overdue: number
  due_soon: number
  awaiting_manual: number
  awaiting_legal_review: number
  failed_external: number
  completed: number
}

export interface RetentionPolicy {
  category: string
  label: string
  duration: string
  duration_days?: number
  source: string
  purpose: string
  role: 'controller' | 'processor' | 'controller_and_processor'
  status: 'enforced' | 'configurable' | 'external_control' | 'decision_required'
  trigger: string
  action: string
  cadence: string
  hold_behavior: 'case_scoped' | 'supported' | 'not_applicable' | 'decision_required'
  implementation?: string
  resource_counts: Array<{
    kind: 'postgres_table' | 'database_scope' | 'object_prefix' | 'queue' | 'browser_key' | 'provider'
    count: number
  }>
}

export interface EffectiveRetention {
  plan: TenantPlan
  registry_version: number
  summary: {
    enforced: number
    configurable: number
    external_control: number
    decision_required: number
  }
  policies: RetentionPolicy[]
}

export type RetentionHealthState = 'healthy' | 'attention' | 'failed' | 'overdue' | 'not_observed'
export type RetentionHealthAlert =
  | 'none'
  | 'budget_exhausted'
  | 'last_run_failed'
  | 'cleanup_overdue'
  | 'not_observed'

export type RetentionHealthOperation =
  | 'tenant_magic_links'
  | 'platform_admin_magic_links'
  | 'attachments'
  | 'privacy_export_archives'
  | 'reporter_privacy_requests'
  | 'replay_sessions'
  | 'widget_funnel_events'
  | 'widget_funnel_daily'
  | 'abuse_diagnostics'
  | 'ai_budget_ledgers'
  | 'identity_asset_deletions'

export interface RetentionOperationHealth {
  operation: RetentionHealthOperation
  category: string
  state: RetentionHealthState
  alert_reason: RetentionHealthAlert
  registered_at: string
  expected_interval_seconds: number
  alert_grace_seconds: number
  last_attempt_at?: string
  last_success_at?: string
  next_expected_at: string
  last_result?: 'succeeded' | 'failed'
  last_processed: number
  total_processed: number
  total_runs: number
  total_failures: number
  consecutive_failures: number
  budget_exhausted: boolean
  failure_code?: string
}

export interface RetentionHealth {
  generated_at: string
  summary: {
    tracked: number
    healthy: number
    attention: number
    failed: number
    overdue: number
    not_observed: number
  }
  operations: RetentionOperationHealth[]
}

export type AccessEventActorType = 'human' | 'api_key' | 'platform_admin' | 'system'
export type AccessEventOutcome = 'success' | 'denied' | 'not_found'

export interface AccessEvent {
  id: string
  kind: string
  actor_type: AccessEventActorType
  actor_id?: string
  actor_member_id?: string
  actor_api_key_id?: string
  actor_platform_admin_id?: string
  resource_type?: string
  resource_id?: string
  action?: string
  permission_path?: string
  request_id?: string
  outcome: AccessEventOutcome
  data: Record<string, unknown>
  created_at: string
}

export type AccessEventSignalType = 'bulk_access' | 'revoked_credential' | 'unusual_api_key' | 'support_access_volume'

export interface AccessEventSignal {
  type: AccessEventSignalType
  actor_type: AccessEventActorType
  actor_id: string
  event_count: number
  distinct_resources?: number
  denied_count?: number
  detail?: string
  last_access_at: string
}

export interface AccessEventPage {
  events: AccessEvent[]
  signals: AccessEventSignal[]
  pagination: {
    next_cursor?: string
    has_more: boolean
    limit: number
  }
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
/** How a destination was obtained: pasted by hand, or granted through a one-click install. */
export type IntegrationInstallSource = 'manual' | 'oauth'
/** Outcome the Slack callback reports back on the settings URL. */
export type SlackInstallResult = 'installed' | 'denied' | 'failed'
export type IntegrationEvent = 'feedback.created' | 'issue.created' | 'issue.updated' | 'issue.resolved'
export type IntegrationPrivacyField =
  | 'description'
  | 'page'
  | 'rating'
  | 'classification'
  | 'workflow'
  | 'attachments'

export interface Integration {
  id: string
  kind: IntegrationKind
  name: string
  webhook_url: string
  channel?: string
  webhook_secret_fingerprint?: string
  custom_header_names: string[]
  events: IntegrationEvent[]
  privacy_fields: IntegrationPrivacyField[]
  privacy_profile_version: number
  destination_capabilities: {
    message_update: boolean
    message_delete: boolean
    message_recall: boolean
    fidibeki_links_require_authentication: boolean
    fidibeki_links_access_controlled: boolean
  }
  install_source: IntegrationInstallSource
  slack_team_name?: string
  is_active: boolean
  created_at: string
  updated_at: string
}

export type IssueTrackerProvider = 'jira' | 'linear'
export type ExternalLinkStatus = 'pending' | 'created' | 'failed'

export interface IssueTrackerConnection {
  id: string
  provider: IssueTrackerProvider
  site_url: string
  account_label: string
  credential_fingerprint: string
  destination_id: string
  destination_key: string
  destination_name: string
  issue_type_id: string
  issue_type_name: string
  sub_destination_id: string
  sub_destination_name: string
  is_enabled: boolean
  auto_create: boolean
  validated_at?: string | null
  last_error: string
  created_at: string
  updated_at: string
}

export interface IssueTrackerDestinationRef {
  id: string
  name: string
}

export interface IssueTrackerDestination {
  id: string
  key: string
  name: string
  issue_types?: IssueTrackerDestinationRef[]
  projects?: IssueTrackerDestinationRef[]
}

export interface IssueExternalLink {
  id: string
  issue_id: string
  provider: IssueTrackerProvider
  status: ExternalLinkStatus
  request_title: string
  request_description: string
  request_priority: string
  destination_id: string
  destination_name: string
  issue_type_id: string
  external_id: string
  external_key: string
  external_url: string
  last_error: string
  attempt_count: number
  created_at: string
  updated_at: string
}

export interface IssueExternalPage {
  external_links: IssueExternalLink[]
  available_providers: IssueTrackerConnection[]
}

export type BillingInterval = 'monthly' | 'yearly'

export interface BillingSummary {
  plan: TenantPlan
  pending_plan?: TenantPlan | null
  status: TenantStatus
  billing_interval?: BillingInterval | null
  trial_ends_at?: string | null
  has_customer: boolean
  has_subscription: boolean
  billing_model?: 'legacy_flat' | 'per_seat'
  billable_seats?: number
  confirmed_seats?: number
  seat_sync_status?: 'legacy' | 'not_subscribed' | 'syncing' | 'failed' | 'synced'
  seat_sync_attempted_at?: string | null
  seat_synced_at?: string | null
  seat_sync_error?: boolean
  unit_price_cents?: number | null
  estimated_total_cents?: number | null
  deletion_requested_at?: string | null
}

export type NotificationCategory =
  | 'assignments'
  | 'mentions'
  | 'comments'
  | 'issue_changes'
  | 'feedback_summary'

export type NotificationMode = 'immediate' | 'daily' | 'weekly' | 'off'

export type NotificationCadence = 'immediate' | 'daily' | 'weekly'

export type NotificationDeliveryStatus = 'claimed' | 'sent' | 'suppressed' | 'failed'

export interface NotificationPreferences {
  assignments: NotificationMode
  mentions: NotificationMode
  comments: NotificationMode
  issue_changes: NotificationMode
  feedback_summary: NotificationMode
  digest_hour: number
  digest_weekday: number
  timezone: string
  timezone_source: 'member' | 'workspace' | 'default'
  updated_at?: string | null
}

export interface NotificationDelivery {
  cadence: NotificationCadence
  status: NotificationDeliveryStatus
  event_count: number
  window_start: string
  window_end: string
  reason?: string | null
  sent_at?: string | null
  created_at: string
}

export interface NotificationPreferencesResponse {
  notification_preferences: NotificationPreferences
  categories: NotificationCategory[]
  supported_modes: Record<NotificationCategory, NotificationMode[]>
  deliveries: NotificationDelivery[]
}

export type TestimonialStatus = 'draft' | 'pending_review' | 'approved' | 'archived'
export type TestimonialConsentState =
  | 'pending' | 'granted' | 'pending_reconsent' | 'revoked' | 'expired'
/** 'custom' is not an arrangement: the wall is rendered by the customer's own
 *  code from the public JSON, and the screen offers them the endpoint instead of
 *  a script tag. */
export type TestimonialWallLayout =
  | 'grid' | 'masonry' | 'carousel' | 'marquee' | 'list' | 'single' | 'custom'

export interface TestimonialAuthor {
  name?: string
  role?: string
  company?: string
  company_url?: string
  avatar_url?: string
  video_url?: string
}

/** Which fields the author agreed to publish, each granted separately. */
export interface TestimonialConsentScopes {
  quote?: boolean
  identity?: boolean
  company?: boolean
  avatar?: boolean
  video?: boolean
}

export interface TestimonialCandidate {
  feedback_id: string
  project_id: string
  public_id: string
  text: string
  rating: number | null
  base_score: number
  decayed_score: number
  /** i18n keys naming the strongest contributors to the score. */
  reasons: string[]
  signals: Record<string, number>
  suggested_excerpt: string | null
  /** The reporter's own address, present only when they asked to be contacted. */
  reporter_email: string | null
  contact_permitted: boolean
  created_at: string
}

export interface Testimonial {
  id: string
  public_id: string
  project_id: string | null
  source: 'feedback' | 'form' | 'manual'
  /** The wording the author agreed to. Never edited. */
  consented_text: string
  /** What is displayed. Drifting from consented_text suspends consent. */
  quote: string
  headline: string | null
  author: TestimonialAuthor
  rating: number | null
  status: TestimonialStatus
  consent_state: TestimonialConsentState
  consent_scopes: TestimonialConsentScopes
  review_note: string | null
  /** Null when there is no recording. */
  video_status?: 'pending' | 'processing' | 'ready' | 'failed' | null
  video_reviewed?: boolean | null
  tag_ids: string[]
  collected_at: string | null
  approved_at: string | null
  revoked_at: string | null
  created_at: string
  updated_at: string
}

export interface TestimonialWall {
  id: string
  public_id: string
  project_id: string | null
  name: string
  slug: string
  layout: TestimonialWallLayout
  show_rating: boolean
  show_avatar: boolean
  show_company: boolean
  show_video: boolean
  max_items: number
  allowed_origins: string[]
  status: 'draft' | 'published'
  item_count: number
  /** Whether the selection differs from the copy that is live. */
  has_unpublished_changes: boolean
  published_at: string | null
  created_at: string
  updated_at: string
}

export interface TestimonialPublishResult {
  version: number
  etag: string
  published: number
  /** Cards dropped because they were not publishable. Surfaced, never silent. */
  skipped: number
  public_id: string
  published_at: string
}
