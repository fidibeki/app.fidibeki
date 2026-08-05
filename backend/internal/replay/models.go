package replay

import (
	"errors"
	"time"
)

const (
	RRWebVersion                = "2.1.1"
	CurrentReplayPrivacyVersion = 2
)

const (
	terminalMergeAttempts    = 12
	unavailableReplayMessage = "No replay events were uploaded before the recording ended."
)

var (
	ErrNotFound          = errors.New("replay session not found")
	ErrBatchComplete     = errors.New("replay batch is already complete")
	ErrSessionFinalized  = errors.New("replay session is already finalized")
	ErrIncompleteBatches = errors.New("replay session has incomplete batches")
	ErrEventLimit        = errors.New("replay session event limit exceeded")
	ErrBatchLimit        = errors.New("replay session batch limit exceeded")
	ErrByteLimit         = errors.New("replay session byte limit exceeded")
	ErrUploadExpired     = errors.New("replay batch upload expired")
	ErrPrivacyContract   = errors.New("replay session predates the anonymous privacy contract")
	ErrConsentRequired   = errors.New("active replay consent is required")
	ErrConsentExpired    = errors.New("replay consent expired")
	ErrNotRetryable      = errors.New("replay session cannot be retried")
)

type ConsentState string

const (
	ConsentNotObserved ConsentState = "not_observed"
	ConsentActive      ConsentState = "active"
	ConsentDenied      ConsentState = "denied"
	ConsentWithdrawn   ConsentState = "withdrawn"
	ConsentExpired     ConsentState = "expired"
)

type Consent struct {
	ID                       string       `json:"id"`
	TenantID                 string       `json:"tenant_id"`
	ProjectID                string       `json:"project_id"`
	WidgetRuleID             string       `json:"widget_rule_id"`
	Purpose                  string       `json:"purpose"`
	NoticeVersion            string       `json:"notice_version"`
	Locale                   string       `json:"locale"`
	Source                   string       `json:"source"`
	CustomerConsentReference *string      `json:"customer_consent_reference,omitempty"`
	State                    ConsentState `json:"state"`
	AcceptedAt               *time.Time   `json:"accepted_at,omitempty"`
	WithdrawnAt              *time.Time   `json:"withdrawn_at,omitempty"`
	ExpiresAt                *time.Time   `json:"expires_at,omitempty"`
	CreatedAt                time.Time    `json:"created_at"`
	UpdatedAt                time.Time    `json:"updated_at"`
}

type BatchSafetyStatus string

const (
	BatchSafetyPending     BatchSafetyStatus = "pending"
	BatchSafetyCleaning    BatchSafetyStatus = "cleaning"
	BatchSafetyQuarantined BatchSafetyStatus = "quarantined"
	BatchSafetySafe        BatchSafetyStatus = "safe"
	BatchSafetyRejected    BatchSafetyStatus = "rejected"
	BatchSafetyExpired     BatchSafetyStatus = "expired"
)

type FailureCode string

const (
	FailureContractViolation FailureCode = "contract_violation"
	FailureUnsafePayload     FailureCode = "unsafe_payload"
	FailureUploadExpired     FailureCode = "upload_expired"
	FailureProcessingFailed  FailureCode = "processing_failed"
	FailureUnavailable       FailureCode = "unavailable"
)

type FailureDetail struct {
	Code      FailureCode `json:"code"`
	Message   string      `json:"message"`
	Retryable bool        `json:"retryable"`
}

func FailureDetailForCode(code FailureCode, retryable bool) *FailureDetail {
	var message string
	switch code {
	case FailureContractViolation:
		message = "The recorder format was not supported. The rejected replay data was deleted."
	case FailureUnsafePayload:
		message = "The recorded data did not meet the replay safety requirements and was deleted."
	case FailureUploadExpired:
		message = "Part of the recording expired before its upload could be completed."
	case FailureProcessingFailed:
		message = "Replay processing failed after several attempts. The recorded data is still available."
	case FailureUnavailable:
		message = "No usable replay data is available for this session."
	default:
		return nil
	}
	return &FailureDetail{
		Code: code, Message: message,
		Retryable: retryable && code == FailureProcessingFailed,
	}
}

type Session struct {
	ID                  string       `json:"id"`
	TenantID            string       `json:"tenant_id"`
	ProjectID           string       `json:"project_id"`
	FeedbackID          *string      `json:"feedback_id,omitempty"`
	ClientSessionID     string       `json:"session_id"`
	PageURL             *string      `json:"page_url,omitempty"`
	StartedAt           time.Time    `json:"started_at"`
	EndedAt             *time.Time   `json:"ended_at,omitempty"`
	EventsCount         int          `json:"events_count"`
	BatchesCount        int          `json:"batches_count"`
	StorageKey          *string      `json:"-"`
	RRWebVersion        string       `json:"rrweb_version"`
	MergedAt            *time.Time   `json:"merged_at,omitempty"`
	MergeRequestedAt    *time.Time   `json:"-"`
	FinalizeRequestedAt *time.Time   `json:"-"`
	MergeEnqueuedAt     *time.Time   `json:"-"`
	MergeAttempts       int          `json:"merge_attempts"`
	MergeError          *string      `json:"-"`
	MergeGeneration     int          `json:"-"`
	FailureCode         *FailureCode `json:"-"`
	RetentionDeletedAt  *time.Time   `json:"retention_deleted_at,omitempty"`
	PrivacyVersion      int          `json:"-"`
	MaxDurationSeconds  int          `json:"-"`
	MaxEvents           int          `json:"-"`
	ReplayConsentID     *string      `json:"-"`
	CreatedAt           time.Time    `json:"created_at"`
}

func (s Session) Status() string {
	if s.RetentionDeletedAt != nil || s.PrivacyVersion < CurrentReplayPrivacyVersion {
		return "expired"
	}
	if s.MergedAt != nil && s.StorageKey != nil {
		return "ready"
	}
	if s.EndedAt != nil && s.FailureCode != nil {
		return "failed"
	}
	if s.EndedAt != nil || s.FinalizeRequestedAt != nil {
		return "processing"
	}
	return "recording"
}

type Batch struct {
	ID                     string
	TenantID               string
	ProjectID              string
	SessionID              string
	Index                  int
	StorageKey             string
	EventsCount            int
	SizeBytes              int64
	ContentType            string
	CompleteTokenHash      *string
	CompletedAt            *time.Time
	SafetyStatus           BatchSafetyStatus
	UploadExpiresAt        time.Time
	SafetyCheckedAt        *time.Time
	SafetyErrorReason      *string
	Generation             int
	VerificationEnqueuedAt *time.Time
	VerificationTries      int
}

type BatchUpload struct {
	BatchIndex    int               `json:"batch_index"`
	PresignedURL  string            `json:"presigned_url"`
	Method        string            `json:"method"`
	Headers       map[string]string `json:"headers"`
	CompleteToken string            `json:"complete_token"`
	ExpiresAt     time.Time         `json:"expires_at"`
}
