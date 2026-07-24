package privacy

import (
	"errors"
	"time"
)

type ReporterAction string

const (
	ReporterExport    ReporterAction = "export"
	ReporterAnonymize ReporterAction = "anonymize"
)

type ReporterRequestStatus string

const (
	ReporterPendingVerification ReporterRequestStatus = "pending_verification"
	ReporterProcessing          ReporterRequestStatus = "processing"
	ReporterAwaitingReview      ReporterRequestStatus = "awaiting_manual_review"
	ReporterCompleted           ReporterRequestStatus = "completed"
	ReporterFailed              ReporterRequestStatus = "failed"
	ReporterExpired             ReporterRequestStatus = "expired"
)

type JobKind string

const (
	JobWorkspaceExport   JobKind = "workspace_export"
	JobReporterExport    JobKind = "reporter_export"
	JobMemberExport      JobKind = "member_export"
	JobReporterAnonymize JobKind = "reporter_anonymize"
)

type JobStatus string

const (
	JobPending        JobStatus = "pending"
	JobProcessing     JobStatus = "processing"
	JobAwaitingReview JobStatus = "awaiting_manual_review"
	JobCompleted      JobStatus = "completed"
	JobFailed         JobStatus = "failed"
	JobExpired        JobStatus = "expired"
)

// ReviewItemStatus tracks the disposition of one flagged free-form record. Pending is the only
// non-terminal value; every other value is permanent once recorded (see the immutability
// trigger on reporter_privacy_review_items).
type ReviewItemStatus string

const (
	ReviewPending           ReviewItemStatus = "pending"
	ReviewRedacted          ReviewItemStatus = "redacted"
	ReviewDeleted           ReviewItemStatus = "deleted"
	ReviewNotPersonalData   ReviewItemStatus = "not_personal_data"
	ReviewRetainedException ReviewItemStatus = "retained_exception"
)

var (
	ErrNotFound            = errors.New("privacy job not found")
	ErrHumanRequired       = errors.New("active human member required")
	ErrJobUnavailable      = errors.New("privacy job is unavailable")
	ErrInvalidRequest      = errors.New("privacy request is invalid")
	ErrReviewItemDecided   = errors.New("privacy review item is already decided")
	ErrReviewReasonMissing = errors.New("privacy review decision requires a reason")
)

type Job struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"-"`
	Kind              JobKind    `json:"kind"`
	Status            JobStatus  `json:"status"`
	ActorMemberID     *string    `json:"actor_member_id,omitempty"`
	ReporterRequestID *string    `json:"-"`
	PrivacyCaseID     *string    `json:"-"`
	Generation        int        `json:"-"`
	Attempts          int        `json:"attempts"`
	EnqueuedAt        *time.Time `json:"-"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	HeartbeatAt       *time.Time `json:"-"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	ObjectKey         *string    `json:"-"`
	ObjectSizeBytes   *int64     `json:"object_size_bytes,omitempty"`
	ErrorMessage      *string    `json:"error_message,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type PreparedJob struct {
	ID         string
	TenantID   string
	Kind       JobKind
	Generation int
}

type ReporterRequest struct {
	ID                  string                `json:"id"`
	TenantID            string                `json:"-"`
	Action              ReporterAction        `json:"action"`
	Status              ReporterRequestStatus `json:"status"`
	ReporterEmail       *string               `json:"reporter_email,omitempty"`
	ActorMemberID       *string               `json:"actor_member_id,omitempty"`
	VerificationExpires time.Time             `json:"verification_expires_at"`
	VerifiedAt          *time.Time            `json:"verified_at,omitempty"`
	AccessExpiresAt     *time.Time            `json:"access_expires_at,omitempty"`
	CompletedAt         *time.Time            `json:"completed_at,omitempty"`
	RecordCount         *int                  `json:"record_count,omitempty"`
	ManualReviewCount   *int                  `json:"manual_review_count,omitempty"`
	ErrorMessage        *string               `json:"error_message,omitempty"`
	JobID               *string               `json:"job_id,omitempty"`
	JobStatus           *JobStatus            `json:"job_status,omitempty"`
	JobExpiresAt        *time.Time            `json:"job_expires_at,omitempty"`
	CreatedAt           time.Time             `json:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
}

type ReporterRequestView struct {
	Action            ReporterAction        `json:"action"`
	Status            ReporterRequestStatus `json:"status"`
	CompletedAt       *time.Time            `json:"completed_at,omitempty"`
	RecordCount       *int                  `json:"record_count,omitempty"`
	ManualReviewCount *int                  `json:"manual_review_count,omitempty"`
	JobStatus         *JobStatus            `json:"job_status,omitempty"`
	DownloadAvailable bool                  `json:"download_available"`
	AccessExpiresAt   time.Time             `json:"access_expires_at"`
}

// ReviewItem is one resource flagged for human review. Fields lists the flagged column names
// only (never content), and DisclosedDestinations summarizes prior delivery without leaking
// recipient-identifying detail, so the queue itself is safe to render and audit.
type ReviewItem struct {
	ID                    string           `json:"id"`
	TenantID              string           `json:"-"`
	ReporterRequestID     string           `json:"-"`
	PrivacyJobID          *string          `json:"privacy_job_id,omitempty"`
	ResourceType          string           `json:"resource_type"`
	ResourceID            string           `json:"resource_id"`
	Fields                []string         `json:"fields"`
	Status                ReviewItemStatus `json:"status"`
	DecisionReason        *string          `json:"decision_reason,omitempty"`
	DecidedByMemberID     *string          `json:"decided_by_member_id,omitempty"`
	DecidedAt             *time.Time       `json:"decided_at,omitempty"`
	DisclosedDestinations map[string]int   `json:"disclosed_destinations,omitempty"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
}

// ReviewDecision is the input to deciding a single review item.
type ReviewDecision struct {
	Status ReviewItemStatus
	Reason string
}

func ParseReviewDecisionStatus(value string) (ReviewItemStatus, error) {
	switch ReviewItemStatus(value) {
	case ReviewRedacted, ReviewDeleted, ReviewNotPersonalData, ReviewRetainedException:
		return ReviewItemStatus(value), nil
	default:
		return "", ErrInvalidRequest
	}
}
