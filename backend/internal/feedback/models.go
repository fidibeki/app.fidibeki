package feedback

import (
	"encoding/json"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/triage"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetanalytics"
)

type Status string

const (
	StatusOpen     Status = "open"
	StatusSpam     Status = "spam"
	StatusArchived Status = "archived"
)

func (s Status) Valid() bool {
	return s == StatusOpen || s == StatusSpam || s == StatusArchived
}

type SortOrder string

const (
	SortNewest     SortOrder = "newest"
	SortOldest     SortOrder = "oldest"
	SortRatingLow  SortOrder = "rating_low"
	SortRatingHigh SortOrder = "rating_high"
)

func (s SortOrder) Valid() bool {
	return s == SortNewest || s == SortOldest || s == SortRatingLow || s == SortRatingHigh
}

type AttachmentKind string

const (
	AttachmentScreenshot AttachmentKind = "screenshot"
	AttachmentAudio      AttachmentKind = "audio"
	AttachmentVideo      AttachmentKind = "video"
	AttachmentReplay     AttachmentKind = "replay"
)

type AttachmentSafetyStatus string

const (
	AttachmentSafetyPending     AttachmentSafetyStatus = "pending"
	AttachmentSafetyCleaning    AttachmentSafetyStatus = "cleaning"
	AttachmentSafetyQuarantined AttachmentSafetyStatus = "quarantined"
	AttachmentSafetySafe        AttachmentSafetyStatus = "safe"
	AttachmentSafetyRejected    AttachmentSafetyStatus = "rejected"
	AttachmentSafetyExpired     AttachmentSafetyStatus = "expired"
)

type MetadataInput struct {
	URL            string `json:"url"`
	Referrer       string `json:"referrer"`
	UserAgent      string `json:"user_agent"`
	OS             string `json:"os"`
	Browser        string `json:"browser"`
	Device         string `json:"device"`
	ViewportWidth  int    `json:"viewport_width"`
	ViewportHeight int    `json:"viewport_height"`
	Language       string `json:"language"`
}

type ElementAttachmentInput struct {
	Selector string            `json:"selector"`
	HTML     string            `json:"html"`
	Styles   map[string]string `json:"styles"`
}

type AttachmentInput struct {
	ClientAttachmentID string         `json:"client_attachment_id"`
	Kind               AttachmentKind `json:"kind"`
	ContentType        string         `json:"content_type"`
	SizeBytes          int64          `json:"size_bytes"`
	DurationSeconds    *int           `json:"duration_seconds"`
}

type SubmissionRequest struct {
	ClientFeedbackID string                     `json:"client_feedback_id"`
	WidgetRuleID     string                     `json:"widget_rule_id"`
	ReplaySessionID  *string                    `json:"session_id"`
	Rating           *int                       `json:"rating"`
	Text             *string                    `json:"text"`
	Responses        map[string]json.RawMessage `json:"responses"`
	Comment          *string                    `json:"comment"`
	ReporterEmail    *string                    `json:"reporter_email"`
	NotifyReporter   bool                       `json:"notify_reporter"`
	Consent          *ReporterConsentInput      `json:"consent"`
	Element          *ElementAttachmentInput    `json:"element_attachment"`
	Attachments      []AttachmentInput          `json:"attachments"`
	Metadata         MetadataInput              `json:"metadata"`
	Analytics        *SubmissionAnalyticsInput  `json:"analytics"`
	Honeypot         string                     `json:"company_website"`
}

// ReporterConsentInput is the structured, auditable evidence the widget must submit alongside an
// opt-in for reporter update emails. It is only valid when notify_reporter is true; the widget must
// omit it entirely (or send null) for anonymous and opted-out submissions.
type ReporterConsentInput struct {
	Purpose       string `json:"purpose"`
	NoticeVersion string `json:"notice_version"`
	Locale        string `json:"locale"`
	Granted       bool   `json:"granted"`
}

type SubmissionAnalyticsInput struct {
	EventID    string    `json:"event_id"`
	VisitorID  string    `json:"visitor_id"`
	PageViewID string    `json:"page_view_id"`
	RuleID     string    `json:"rule_id"`
	PageURL    string    `json:"page_url"`
	OccurredAt time.Time `json:"occurred_at"`
}

type SubmissionValues struct {
	ClientFeedbackID             string
	WidgetRuleID                 string
	ReplaySessionID              *string
	Rating                       *int
	Text                         *string
	Responses                    string
	Comment                      *string
	DefinitionVersion            *int
	ReporterEmail                *string
	NotifyReporter               bool
	ReporterConsentPurpose       *string
	ReporterConsentNoticeVersion *string
	ReporterConsentLocale        *string
	ReporterConsentSource        *string
	PageURL                      string
	Referrer                     *string
	UserAgent                    *string
	OS                           *string
	Browser                      *string
	Device                       *string
	ViewportWidth                *int
	ViewportHeight               *int
	Language                     *string
	DOMSnapshotHTML              *string
	DOMSnapshotStyles            *string
	Attachments                  []AttachmentValues
	Analytics                    *widgetanalytics.EventValues
}

type AttachmentValues struct {
	ClientAttachmentID string
	Kind               AttachmentKind
	ContentType        string
	SizeBytes          int64
	DurationSeconds    *int
}

type Attachment struct {
	ID                     string
	FeedbackID             string
	ClientAttachmentID     string
	Kind                   AttachmentKind
	StorageKey             string
	ContentType            string
	SizeBytes              int64
	DurationSeconds        *int
	CompleteTokenHash      *string
	CompletedAt            *time.Time
	SafetyStatus           AttachmentSafetyStatus
	UploadExpiresAt        time.Time
	SafetyCheckedAt        *time.Time
	SafetyErrorReason      *string
	DetectedContentType    *string
	VerificationGeneration int
	VerificationEnqueuedAt *time.Time
	VerificationAttempts   int
}

type AttachmentDetail struct {
	ID                  string                 `json:"id"`
	Kind                AttachmentKind         `json:"kind"`
	ContentType         string                 `json:"content_type"`
	SizeBytes           int64                  `json:"size_bytes"`
	DurationSeconds     *int                   `json:"duration_seconds,omitempty"`
	ProcessingStatus    string                 `json:"processing_status"`
	ErrorMessage        *string                `json:"error_message,omitempty"`
	Transcription       *string                `json:"transcription,omitempty"`
	SentimentScore      *float64               `json:"sentiment_score,omitempty"`
	ModerationFlags     []string               `json:"moderation_flags"`
	ProcessingAttempts  int                    `json:"processing_attempts"`
	CompletedAt         *time.Time             `json:"completed_at,omitempty"`
	ProcessedAt         *time.Time             `json:"processed_at,omitempty"`
	RetentionDeletedAt  *time.Time             `json:"retention_deleted_at,omitempty"`
	SafetyStatus        AttachmentSafetyStatus `json:"safety_status"`
	SafetyCheckedAt     *time.Time             `json:"safety_checked_at,omitempty"`
	SafetyErrorReason   *string                `json:"safety_error_reason,omitempty"`
	DetectedContentType *string                `json:"detected_content_type,omitempty"`
	CreatedAt           time.Time              `json:"created_at"`
	ContentURL          *string                `json:"content_url,omitempty"`
}

type AttachmentObject struct {
	ID          string
	Kind        AttachmentKind
	StorageKey  string
	ContentType string
	SizeBytes   int64
	CompletedAt time.Time
	CreatedAt   time.Time
}

type SessionDetail struct {
	ID                 string     `json:"id"`
	Status             string     `json:"status"`
	PageURL            *string    `json:"page_url,omitempty"`
	StartedAt          time.Time  `json:"started_at"`
	EndedAt            *time.Time `json:"ended_at,omitempty"`
	EventsCount        int        `json:"events_count"`
	BatchesCount       int        `json:"batches_count"`
	RRWebVersion       string     `json:"rrweb_version"`
	ReplayReady        bool       `json:"replay_ready"`
	MergeError         *string    `json:"merge_error,omitempty"`
	RetentionDeletedAt *time.Time `json:"retention_deleted_at,omitempty"`
	ReplayURL          *string    `json:"replay_url,omitempty"`
}

type Item struct {
	ID                           string           `json:"id"`
	PublicID                     string           `json:"public_id"`
	ProjectID                    string           `json:"project_id"`
	ProjectName                  string           `json:"project_name"`
	WidgetRuleID                 *string          `json:"widget_rule_id,omitempty"`
	SessionID                    *string          `json:"session_id,omitempty"`
	IssueID                      *string          `json:"issue_id,omitempty"`
	IssuePublicID                *string          `json:"issue_public_id,omitempty"`
	IssueStatus                  *string          `json:"issue_status,omitempty"`
	Rating                       *int             `json:"rating,omitempty"`
	Responses                    map[string]any   `json:"responses"`
	Comment                      *string          `json:"comment,omitempty"`
	DefinitionVersion            *int             `json:"widget_definition_version,omitempty"`
	Title                        string           `json:"title"`
	Description                  *string          `json:"description,omitempty"`
	Text                         *string          `json:"text,omitempty"`
	TextCleaned                  *string          `json:"text_cleaned,omitempty"`
	TextModerationFlags          []string         `json:"-"`
	AISuggestedTitle             *string          `json:"ai_suggested_title,omitempty"`
	AIActionability              *string          `json:"ai_actionability,omitempty"`
	AICategory                   *string          `json:"ai_category,omitempty"`
	AIPriority                   *string          `json:"ai_priority,omitempty"`
	Priority                     *triage.Priority `json:"priority"`
	Assignee                     *triage.Assignee `json:"assignee"`
	Tags                         []triage.Tag     `json:"tags"`
	ReporterEmail                *string          `json:"reporter_email,omitempty"`
	NotifyReporter               bool             `json:"notify_reporter"`
	ReporterConsentGrantedAt     *time.Time       `json:"reporter_consent_granted_at,omitempty"`
	ReporterConsentWithdrawnAt   *time.Time       `json:"reporter_consent_withdrawn_at,omitempty"`
	ReporterConsentPurpose       *string          `json:"reporter_consent_purpose,omitempty"`
	ReporterConsentNoticeVersion *string          `json:"reporter_consent_notice_version,omitempty"`
	ReporterConsentLocale        *string          `json:"reporter_consent_locale,omitempty"`
	ReporterConsentSource        *string          `json:"reporter_consent_source,omitempty"`
	PageURL                      string           `json:"page_url"`
	Referrer                     *string          `json:"referrer,omitempty"`
	UserAgent                    *string          `json:"user_agent,omitempty"`
	OS                           *string          `json:"os,omitempty"`
	Browser                      *string          `json:"browser,omitempty"`
	Device                       *string          `json:"device,omitempty"`
	ViewportWidth                *int             `json:"viewport_width,omitempty"`
	ViewportHeight               *int             `json:"viewport_height,omitempty"`
	Language                     *string          `json:"language,omitempty"`
	Status                       Status           `json:"status"`
	AIStatus                     string           `json:"ai_status"`
	SentimentScore               *float64         `json:"sentiment_score,omitempty"`
	ModerationFlags              []string         `json:"moderation_flags"`
	AttachmentKinds              []AttachmentKind `json:"attachment_kinds"`
	HasReplay                    bool             `json:"has_replay"`
	CreatedAt                    time.Time        `json:"created_at"`
	UpdatedAt                    time.Time        `json:"updated_at"`
}

type Detail struct {
	Item
	DOMSnapshotHTML   *string            `json:"dom_snapshot_html,omitempty"`
	DOMSnapshotStyles *string            `json:"dom_snapshot_styles,omitempty"`
	Attachments       []AttachmentDetail `json:"attachments"`
	Session           *SessionDetail     `json:"session,omitempty"`
	AIProcessedAt     *time.Time         `json:"ai_processed_at,omitempty"`
}

type ListCursor struct {
	Sort      SortOrder
	CreatedAt time.Time
	ID        string
	RatingKey int
}

type ListOptions struct {
	IDs        []string
	Query      string
	ProjectID  string
	Status     Status
	Rating     int
	Attachment string
	Processing string
	Moderation string
	Issue      string
	DateFrom   *time.Time
	DateTo     *time.Time
	Sentiment  string
	Sort       SortOrder
	Limit      int
	Cursor     *ListCursor
}

type TenantMutation struct {
	Status       *Status
	Title        *string
	SetPriority  bool
	Priority     *triage.Priority
	SetAssignee  bool
	AssigneeID   *string
	AddTagIDs    []string
	RemoveTagIDs []string
}

type TrendPoint struct {
	Date          time.Time `json:"date"`
	FeedbackCount int       `json:"feedback_count"`
	Satisfaction  *float64  `json:"satisfaction,omitempty"`
}

type Dashboard struct {
	RangeDays            int          `json:"range_days"`
	FeedbackCount        int          `json:"feedback_count"`
	FeedbackDeltaPercent *float64     `json:"feedback_delta_percent,omitempty"`
	OpenIssueCount       int          `json:"open_issue_count"`
	Satisfaction         *float64     `json:"satisfaction,omitempty"`
	SatisfactionDelta    *float64     `json:"satisfaction_delta,omitempty"`
	AttentionCount       int          `json:"attention_count"`
	Trend                []TrendPoint `json:"trend"`
}

type Submission struct {
	ID             string
	PublicID       string
	IssuePublicID  string
	NotifyReporter bool
	Attachments    []Attachment
}

type AttachmentUpload struct {
	AttachmentID       string            `json:"attachment_id"`
	ClientAttachmentID string            `json:"client_attachment_id"`
	PresignedURL       string            `json:"presigned_url"`
	Method             string            `json:"method"`
	Headers            map[string]string `json:"headers"`
	CompleteToken      string            `json:"complete_token"`
	ExpiresAt          time.Time         `json:"expires_at"`
}

type SubmissionResponse struct {
	FeedbackID       string              `json:"feedback_id"`
	FeedbackPublicID string              `json:"feedback_public_id"`
	IssuePublicID    string              `json:"issue_public_id,omitempty"`
	NotifyReporter   bool                `json:"notify_reporter"`
	Attachments      []AttachmentUpload  `json:"attachments"`
	Warnings         []SubmissionWarning `json:"warnings,omitempty"`
}

type SubmissionWarning struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	Reason            string `json:"reason"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
