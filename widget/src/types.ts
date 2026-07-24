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
export type WidgetObjective = 'continuous_feedback' | 'after_engagement' | 'validate_content' | 'detect_friction'
export type WidgetResponseBlockType = 'free_text' | 'rating' | 'reaction' | 'single_choice'

export interface WidgetAppearance {
  position: WidgetPosition
  theme: WidgetTheme
  accent_color: string
  accent_foreground?: string
  surface_color?: string
  text_color?: string
  logo_url?: string
}

export interface WidgetResponseBlock {
  key: string
  type: WidgetResponseBlockType
  required: boolean
  options?: Array<{ value: string }>
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
  provider?: 'google_gemini'
  input_categories?: AIInputCategory[]
  privacy_policy_url?: string
  policy_version?: string
}

// AnalyticsConsentNotice/AnalyticsConsentConfig intentionally mirror the shape of their replay
// counterparts above but stay separate types: widget_analytics is an independent purpose with its
// own evidence and must never be inferred from, or coupled to, session_replay consent (issue #58).
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

export interface TargetCondition {
  id?: string
  matcher_type: MatcherType
  matcher_value: string | null
  position?: number
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

export interface WidgetRule {
  id: string
  name: string
  priority: number
  matcher_type: MatcherType
  matcher_value: string | null
  include_conditions?: TargetCondition[]
  exclude_conditions?: TargetCondition[]
  trigger_type: TriggerType
  trigger_delay_seconds: number | null
  trigger_scroll_percent: number | null
  can_rate: boolean
  can_write_text: boolean
  can_record_audio: boolean
  can_record_video: boolean
  can_attach_element: boolean
  can_attach_screenshot: boolean
  greeting: string | null
  position: WidgetPosition
  theme: WidgetTheme
  accent_color: string | null
  replay_mode: ReplayMode
  replay_sampling_rate: number
  replay_max_duration_seconds: number
  replay_max_events: number
  replay_mask_all_inputs: boolean
  replay_mask_text_selector: string | null
  replay_record_canvas: boolean
  replay_canvas_fps: number
  replay_consent: ReplayConsentConfig
  analytics_consent: AnalyticsConsentConfig
  recurrence_mode: RecurrenceMode
  recurrence_days: number | null
  definition_key?: string
  definition_version?: number
  definition_status?: WidgetDefinitionStatus
  presentation?: WidgetPresentation
  objective?: WidgetObjective
  response_blocks?: WidgetResponseBlock[]
  localizations?: Record<string, WidgetLocalization>
  allow_comment?: boolean
  collect_contact?: boolean
  ai_processing?: AIProcessingConfig
  appearance?: WidgetAppearance
}

export interface ControllerProfile {
  version: number
  name: string
  privacy_contact_email: string
  privacy_rights_email: string
  privacy_policy_url: string
}

export interface WidgetConfiguration {
  schema_version?: number
  project_id: string
  default_locale?: string
  controller_profile?: ControllerProfile | null
  features: PlanFeatures
  rules: WidgetRule[]
  definitions?: WidgetRule[]
  refresh_after_seconds: number
  error_reporting_dsn?: string
}

export interface ElementAttachment {
  selector: string
  html: string
  styles: Record<string, string>
}

export interface FeedbackMetadata {
  url: string
  referrer: string
  user_agent: string
  browser: string
  os: string
	device: 'desktop' | 'tablet' | 'mobile'
  viewport_width: number
  viewport_height: number
	language: string
}

export type AttachmentKind = 'screenshot' | 'audio' | 'video'

export interface PendingAttachment {
	clientAttachmentID: string
	kind: AttachmentKind
	blob: Blob
}

export interface AttachmentDeclaration {
	client_attachment_id: string
	kind: AttachmentKind
	content_type: string
	size_bytes: number
}

export interface AttachmentUpload {
	attachment_id: string
	client_attachment_id: string
	presigned_url: string
	method: 'PUT'
	headers: Record<string, string>
	complete_token: string
	expires_at: string
}

export interface SubmissionResponse {
	feedback_id: string
	feedback_public_id?: string
	issue_public_id?: string
	notify_reporter?: boolean
	attachments?: AttachmentUpload[]
	warnings?: SubmissionWarning[]
}

export interface SubmissionWarning {
	code: string
	message: string
	reason?: string
	retry_after_seconds?: number
}
