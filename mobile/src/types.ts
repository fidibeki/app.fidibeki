import type { ReactNode } from 'react'

export type MobilePlatform = 'ios' | 'android'
export type ResponseBlockType = 'rating' | 'reaction' | 'single_choice' | 'free_text'
export type RecurrenceMode = 'session' | 'cooldown' | 'never'
export type FeedbackStatus = 'idle' | 'loading' | 'ready' | 'open' | 'submitting' | 'submitted' | 'ineligible' | 'error'

export interface MobileResponseOption {
  value: string
}

export interface MobileResponseBlock {
  key: string
  type: ResponseBlockType
  required: boolean
  options?: MobileResponseOption[]
}

export interface MobileLocalization {
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

export interface MobileControllerNotice {
  notice_version: string
  controller_profile_version: number
  widget_definition_version: number
  controller_name: string
  privacy_contact_email: string
  privacy_rights_email: string
  privacy_policy_url: string
  locale: string
  purpose_summary: string
  lawful_basis_summary: string
  retention_summary: string
}

export interface MobileAIDisclosure {
  enabled: boolean
  provider?: string
  privacy_policy_url?: string
  policy_version?: string
  input_categories?: Array<'feedback_text' | 'screenshot' | 'audio' | 'video'>
}

export interface MobileAppearance {
  position?: 'bottom-left' | 'bottom-right'
  theme?: 'light' | 'dark' | 'auto'
  accent_color?: string
  accent_foreground?: string
  surface_color?: string
  text_color?: string
  logo_url?: string
}

export interface MobileWidgetDefinition {
  id: string
  definition_key: string
  definition_version: number
  presentation: 'launcher' | 'prompt' | 'inline' | 'reaction'
  launcher_variant: 'edge_tab' | 'floating_button'
  objective: 'continuous_feedback' | 'after_engagement' | 'validate_content' | 'detect_friction'
  recurrence_mode: RecurrenceMode
  recurrence_days?: number | null
  response_blocks: MobileResponseBlock[]
  localization: MobileLocalization
  allow_comment: boolean
  collect_contact: boolean
  can_attach_screenshot: boolean
  ai_processing?: MobileAIDisclosure
  appearance: MobileAppearance
}

export interface MobileControllerProfile {
  version: number
  name: string
  privacy_contact_email: string
  privacy_rights_email: string
  privacy_policy_url: string
}

export interface MobileCaptureConfiguration {
  schema_version: 1
  project_id: string
  display_locale: string
  definition: MobileWidgetDefinition
  controller_profile?: MobileControllerProfile
  contact_notice?: MobileControllerNotice
}

export interface MobileCaptureSession {
  token: string
  submit_expires_at: string
  configuration: MobileCaptureConfiguration
}

export interface SessionProviderRequest {
  platform: MobilePlatform
  screen: string
  display_locale: string
}

export type SessionProvider = (request: SessionProviderRequest) => Promise<MobileCaptureSession>

export interface StorageAdapter {
  getItem(key: string): Promise<string | null>
  setItem(key: string, value: string): Promise<void>
  removeItem(key: string): Promise<void>
}

export interface ScreenshotAsset {
  uri: string
  contentType: 'image/png' | 'image/jpeg' | 'image/webp'
  size: number
  read(): Promise<Blob>
  dispose?(): void | Promise<void>
}

export type ScreenshotProvider = () => Promise<ScreenshotAsset | null>

export interface PreparedScreenshot {
  clientAttachmentId: string
  contentType: ScreenshotAsset['contentType']
  size: number
  blob: Blob
  dispose(): Promise<void>
}

export type ResponseValue = string | number
export type FormResponses = Record<string, ResponseValue>

export interface FeedbackFormState {
  responses: FormResponses
  comment: string
  reporterEmail: string
  consentGranted: boolean
}

export interface ReporterConsentInput {
  purpose: 'feedback_updates'
  notice_version: string
  controller_profile_version: number
  widget_definition_version: number
  locale: string
  granted: true
}

export interface ScreenshotDeclaration {
  client_attachment_id: string
  content_type: ScreenshotAsset['contentType']
  size_bytes: number
}

export interface MobileFeedbackRequest {
  client_feedback_id: string
  responses: FormResponses
  comment?: string
  reporter_email?: string
  notify_reporter?: true
  consent?: ReporterConsentInput
  screenshot?: ScreenshotDeclaration
  app_version: string
  os_version: string
  device_locale?: string
}

export interface AttachmentGrant {
  attachment_id: string
  client_attachment_id: string
  presigned_url: string
  method: 'PUT'
  headers: Record<string, string>
  complete_token: string
  expires_at: string
}

export interface MobileFeedbackAccepted {
  feedback_id: string
  feedback_public_id: string
  attachments: AttachmentGrant[]
}

export interface AttachmentOutcome {
  attachmentId: string
  status: 'uploaded' | 'failed'
  stage?: 'upload' | 'complete'
  error?: string
}

export interface MobileSubmissionResult {
  feedbackId: string
  feedbackPublicId: string
  clientFeedbackId: string
  accepted: true
  attachments: AttachmentOutcome[]
}

export interface MobileClientOptions {
  apiBaseUrl: string
  fetch?: typeof globalThis.fetch
}

export interface MobileTheme {
  mode?: 'light' | 'dark'
  accentColor?: string
  backgroundColor?: string
  textColor?: string
  mutedTextColor?: string
  borderColor?: string
  dangerColor?: string
}

export interface FidibekiProviderProps {
  apiBaseUrl: string
  sessionProvider: SessionProvider
  storage: StorageAdapter
  screen: string
  appVersion: string
  osVersion: string
  locale?: string
  deviceLocale?: string
  appSessionId?: string
  screenshotProvider?: ScreenshotProvider
  theme?: MobileTheme
  children: ReactNode
}

export type FeedbackDraftPolicy = 'reset' | 'reconcile'

export interface UseFidibekiFeedbackResult {
  open(): Promise<void>
  close(): void
  status: FeedbackStatus
  error: MobileClientError | null
  eligible: boolean
}

export type MobileClientErrorCode =
  | 'access_denied'
  | 'configuration_changed'
  | 'validation_error'
  | 'network_error'
  | 'invalid_response'
  | 'retry_payload_changed'
  | 'screenshot_error'
  | 'session_expired'

export class MobileClientError extends Error {
  readonly code: MobileClientErrorCode
  readonly retryable: boolean
  readonly fields: Readonly<Record<string, string>>

  constructor(
    code: MobileClientErrorCode,
    message: string,
    options: {
      retryable?: boolean
      fields?: Record<string, string>
      cause?: unknown
    } = {},
  ) {
    super(message)
    if (options.cause !== undefined) {
      ;(this as Error & { cause?: unknown }).cause = options.cause
    }
    this.name = 'MobileClientError'
    this.code = code
    this.retryable = options.retryable ?? false
    this.fields = options.fields ?? {}
  }
}
