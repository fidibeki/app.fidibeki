export type MatcherType = 'all_pages' | 'url_exact' | 'url_prefix' | 'url_regex'
export type TriggerType = 'delay' | 'scroll' | 'exit_intent'
export type WidgetPosition = 'bottom-left' | 'bottom-right'
export type WidgetTheme = 'light' | 'dark' | 'auto'
export type ReplayMode = 'none' | 'full' | 'sampling'
export type RecurrenceMode = 'cooldown' | 'session' | 'never'

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
  recurrence_mode: RecurrenceMode
  recurrence_days: number | null
}

export interface WidgetConfiguration {
  project_id: string
  features: PlanFeatures
  rules: WidgetRule[]
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
}
