package privacy

import (
	"errors"
	"strings"
	"time"
)

type SubjectType string

const (
	SubjectReporter SubjectType = "reporter"
	SubjectMember   SubjectType = "member"
)

type RightType string

const (
	RightAccess                RightType = "access"
	RightRectification         RightType = "rectification"
	RightErasure               RightType = "erasure"
	RightRestriction           RightType = "restriction"
	RightObjection             RightType = "objection"
	RightPortabilityAssessment RightType = "portability_assessment"
	RightConsentWithdrawal     RightType = "consent_withdrawal"
)

type ControllerRole string

const (
	RoleController                   ControllerRole = "controller"
	RoleCustomerControllerAssistance ControllerRole = "customer_controller_assistance"
)

type CaseStatus string

const (
	CaseReceived              CaseStatus = "received"
	CasePendingVerification   CaseStatus = "pending_verification"
	CaseInProgress            CaseStatus = "in_progress"
	CaseAwaitingClarification CaseStatus = "awaiting_clarification"
	CasePaused                CaseStatus = "paused"
	CaseAwaitingLegalReview   CaseStatus = "awaiting_legal_review"
	CasePartiallyFulfilled    CaseStatus = "partially_fulfilled"
	CaseCompleted             CaseStatus = "completed"
	CaseRefused               CaseStatus = "refused"
	CaseCancelled             CaseStatus = "cancelled"
)

type ProcessingPurpose string

const (
	PurposeAI                   ProcessingPurpose = "ai"
	PurposeIntegration          ProcessingPurpose = "integration"
	PurposeReporterEmail        ProcessingPurpose = "reporter_email"
	PurposeReplay               ProcessingPurpose = "replay"
	PurposeAnalytics            ProcessingPurpose = "analytics"
	PurposeBackgroundProcessing ProcessingPurpose = "background_processing"
)

type PortabilityDecision string

const (
	PortabilityApproved          PortabilityDecision = "approved"
	PortabilityPartiallyApproved PortabilityDecision = "partially_approved"
	PortabilityNotApplicable     PortabilityDecision = "not_applicable"
	PortabilityRefused           PortabilityDecision = "refused"
)

type PortabilityDataCategory string

const (
	PortabilityFeedback     PortabilityDataCategory = "feedback"
	PortabilityIssues       PortabilityDataCategory = "issues"
	PortabilityAttachments  PortabilityDataCategory = "attachments"
	PortabilityReplay       PortabilityDataCategory = "replay"
	PortabilityAIProcessing PortabilityDataCategory = "ai_processing"
	PortabilityConsent      PortabilityDataCategory = "consent"
	PortabilityDelivery     PortabilityDataCategory = "delivery"
	PortabilityWidgetEvents PortabilityDataCategory = "widget_events"
)

var (
	ErrCaseTransition           = errors.New("privacy case transition is invalid")
	ErrCaseReasonRequired       = errors.New("privacy case decision reason is required")
	ErrCaseExecutionUnsupported = errors.New("privacy case execution is not supported")
	ErrLegalHoldActive          = errors.New("privacy case has an active legal hold")
	ErrIdempotencyConflict      = errors.New("idempotency key was reused with different input")
	ErrProcessingRestricted     = errors.New("processing is restricted for this data subject")
)

type PrivacyCase struct {
	ID                      string                  `json:"id"`
	TenantID                string                  `json:"-"`
	PublicID                string                  `json:"public_id"`
	SubjectType             SubjectType             `json:"subject_type"`
	RightType               RightType               `json:"right_type"`
	ControllerRole          ControllerRole          `json:"controller_role"`
	Status                  CaseStatus              `json:"status"`
	SubjectEmail            *string                 `json:"subject_email,omitempty"`
	SubjectMemberID         *string                 `json:"subject_member_id,omitempty"`
	RequestedByMemberID     string                  `json:"requested_by_member_id"`
	OwnerMemberID           *string                 `json:"owner_member_id,omitempty"`
	VerificationExpiresAt   *time.Time              `json:"verification_expires_at,omitempty"`
	VerificationMethod      *string                 `json:"verification_method,omitempty"`
	VerifiedAt              *time.Time              `json:"verified_at,omitempty"`
	ReceivedAt              time.Time               `json:"received_at"`
	DeadlineAt              time.Time               `json:"deadline_at"`
	PauseStartedAt          *time.Time              `json:"pause_started_at,omitempty"`
	PausedSeconds           int64                   `json:"paused_seconds"`
	CompletedAt             *time.Time              `json:"completed_at,omitempty"`
	Scope                   map[string]any          `json:"scope"`
	DecisionSummary         *string                 `json:"decision_summary,omitempty"`
	LegalReviewNotes        *string                 `json:"legal_review_notes,omitempty"`
	ExceptionReason         *string                 `json:"exception_reason,omitempty"`
	LegacyReporterRequestID *string                 `json:"legacy_reporter_request_id,omitempty"`
	Restriction             *Restriction            `json:"restriction,omitempty"`
	LegalHolds              []LegalHold             `json:"legal_holds"`
	Execution               *CaseExecution          `json:"execution,omitempty"`
	PortabilityAssessment   *PortabilityAssessment  `json:"portability_assessment,omitempty"`
	Rectifications          []CaseRectification     `json:"rectifications"`
	FeedbackRectifications  []FeedbackRectification `json:"feedback_rectifications"`
	CreatedAt               time.Time               `json:"created_at"`
	UpdatedAt               time.Time               `json:"updated_at"`
}

type Restriction struct {
	ID                    string              `json:"id"`
	PrivacyCaseID         string              `json:"privacy_case_id"`
	Purposes              []ProcessingPurpose `json:"purposes"`
	Active                bool                `json:"active"`
	Reason                string              `json:"reason"`
	ActivatedAt           time.Time           `json:"activated_at"`
	DeactivatedAt         *time.Time          `json:"deactivated_at,omitempty"`
	DeactivatedByMemberID *string             `json:"deactivated_by_member_id,omitempty"`
}

type CaseEvent struct {
	ID             string         `json:"id"`
	Sequence       int64          `json:"sequence"`
	PrivacyCaseID  string         `json:"privacy_case_id"`
	EventType      string         `json:"event_type"`
	ActorMemberID  *string        `json:"actor_member_id,omitempty"`
	PreviousStatus *CaseStatus    `json:"previous_status,omitempty"`
	NewStatus      *CaseStatus    `json:"new_status,omitempty"`
	Data           map[string]any `json:"data"`
	OccurredAt     time.Time      `json:"occurred_at"`
}

type LegalHold struct {
	ID                 string     `json:"id"`
	PrivacyCaseID      string     `json:"privacy_case_id"`
	Reason             string     `json:"reason"`
	LegalReference     *string    `json:"legal_reference,omitempty"`
	Active             bool       `json:"active"`
	PlacedByMemberID   string     `json:"placed_by_member_id"`
	PlacedAt           time.Time  `json:"placed_at"`
	ReleasedByMemberID *string    `json:"released_by_member_id,omitempty"`
	ReleasedAt         *time.Time `json:"released_at,omitempty"`
	ReleaseReason      *string    `json:"release_reason,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type CaseExecution struct {
	RightType         RightType              `json:"right_type"`
	Primitive         ReporterAction         `json:"-"`
	RequestStatus     *ReporterRequestStatus `json:"request_status,omitempty"`
	JobID             *string                `json:"job_id,omitempty"`
	JobStatus         *JobStatus             `json:"job_status,omitempty"`
	JobExpiresAt      *time.Time             `json:"job_expires_at,omitempty"`
	RecordCount       *int                   `json:"record_count,omitempty"`
	ManualReviewCount *int                   `json:"manual_review_count,omitempty"`
	ErrorMessage      *string                `json:"error_message,omitempty"`
	StartedAt         time.Time              `json:"started_at"`
	CompletedAt       *time.Time             `json:"completed_at,omitempty"`
}

type PortabilityAssessment struct {
	ID                 string                    `json:"id"`
	PrivacyCaseID      string                    `json:"privacy_case_id"`
	Decision           PortabilityDecision       `json:"decision"`
	DataCategories     []PortabilityDataCategory `json:"data_categories"`
	ExportFormat       *string                   `json:"export_format,omitempty"`
	Reason             string                    `json:"reason"`
	AssessedByMemberID string                    `json:"assessed_by_member_id"`
	AssessedAt         time.Time                 `json:"assessed_at"`
	CreatedAt          time.Time                 `json:"created_at"`
}

type RecordPortabilityAssessmentInput struct {
	Decision       PortabilityDecision
	DataCategories []PortabilityDataCategory
	ExportFormat   string
	Reason         string
}

type CaseRectification struct {
	ID                string             `json:"id"`
	PrivacyCaseID     string             `json:"privacy_case_id"`
	SubjectMemberID   string             `json:"subject_member_id"`
	Changes           map[string]*string `json:"changes"`
	PreviousValues    map[string]*string `json:"previous_values"`
	Reason            string             `json:"reason"`
	AppliedByMemberID string             `json:"applied_by_member_id"`
	AppliedAt         time.Time          `json:"applied_at"`
	CreatedAt         time.Time          `json:"created_at"`
}

type RecordCaseRectificationInput struct {
	Changes map[string]*string
	Reason  string
}

type RectifiableFeedback struct {
	ID                string    `json:"id"`
	PublicID          string    `json:"public_id"`
	Title             string    `json:"title"`
	Description       *string   `json:"description,omitempty"`
	PageURL           string    `json:"page_url"`
	TextProcessStatus string    `json:"text_process_status"`
	CreatedAt         time.Time `json:"created_at"`
}

type FeedbackRectification struct {
	ID                     string    `json:"id"`
	PrivacyCaseID          string    `json:"privacy_case_id"`
	FeedbackID             string    `json:"feedback_id"`
	PreviousDescription    *string   `json:"previous_description,omitempty"`
	Description            string    `json:"description"`
	Reason                 string    `json:"reason"`
	DerivedDataInvalidated bool      `json:"derived_data_invalidated"`
	AppliedByMemberID      string    `json:"applied_by_member_id"`
	AppliedAt              time.Time `json:"applied_at"`
	CreatedAt              time.Time `json:"created_at"`
}

type RecordFeedbackRectificationInput struct {
	FeedbackID  string
	Description string
	Reason      string
}

type CreateCaseInput struct {
	SubjectType     SubjectType
	RightType       RightType
	ControllerRole  ControllerRole
	SubjectEmail    string
	SubjectMemberID string
	OwnerMemberID   string
	Scope           map[string]any
}

type TransitionCaseInput struct {
	Status           CaseStatus
	DecisionSummary  string
	LegalReviewNotes string
	ExceptionReason  string
}

type CaseDashboard struct {
	Overdue             int `json:"overdue"`
	DueSoon             int `json:"due_soon"`
	AwaitingManual      int `json:"awaiting_manual"`
	AwaitingLegalReview int `json:"awaiting_legal_review"`
	FailedExternal      int `json:"failed_external"`
	Completed           int `json:"completed"`
}

type ReporterCaseView struct {
	PublicID   string     `json:"public_id"`
	RightType  RightType  `json:"right_type"`
	Status     CaseStatus `json:"status"`
	ReceivedAt time.Time  `json:"received_at"`
	DeadlineAt time.Time  `json:"deadline_at"`
}

func ParseSubjectType(value string) (SubjectType, error) {
	switch subjectType := SubjectType(strings.TrimSpace(value)); subjectType {
	case SubjectReporter, SubjectMember:
		return subjectType, nil
	default:
		return "", ErrInvalidRequest
	}
}

func ParseRightType(value string) (RightType, error) {
	switch rightType := RightType(strings.TrimSpace(value)); rightType {
	case RightAccess, RightRectification, RightErasure, RightRestriction, RightObjection,
		RightPortabilityAssessment, RightConsentWithdrawal:
		return rightType, nil
	default:
		return "", ErrInvalidRequest
	}
}

func ParseControllerRole(value string) (ControllerRole, error) {
	switch role := ControllerRole(strings.TrimSpace(value)); role {
	case RoleController, RoleCustomerControllerAssistance:
		return role, nil
	default:
		return "", ErrInvalidRequest
	}
}

func ParseCaseStatus(value string) (CaseStatus, error) {
	switch status := CaseStatus(strings.TrimSpace(value)); status {
	case CaseInProgress, CaseAwaitingClarification, CasePaused, CaseAwaitingLegalReview,
		CasePartiallyFulfilled, CaseCompleted, CaseRefused, CaseCancelled:
		return status, nil
	default:
		return "", ErrInvalidRequest
	}
}

func ParsePortabilityDecision(value string) (PortabilityDecision, error) {
	switch decision := PortabilityDecision(strings.TrimSpace(value)); decision {
	case PortabilityApproved, PortabilityPartiallyApproved,
		PortabilityNotApplicable, PortabilityRefused:
		return decision, nil
	default:
		return "", ErrInvalidRequest
	}
}

func ParsePortabilityDataCategory(value string) (PortabilityDataCategory, error) {
	switch category := PortabilityDataCategory(strings.TrimSpace(value)); category {
	case PortabilityFeedback, PortabilityIssues, PortabilityAttachments, PortabilityReplay,
		PortabilityAIProcessing, PortabilityConsent, PortabilityDelivery,
		PortabilityWidgetEvents:
		return category, nil
	default:
		return "", ErrInvalidRequest
	}
}
