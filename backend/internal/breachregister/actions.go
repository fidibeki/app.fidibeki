package breachregister

import (
	"strings"
	"time"
)

type ActionKind string

const (
	ActionContainment  ActionKind = "containment"
	ActionPostIncident ActionKind = "post_incident"
)

type ActionStatus string

const (
	ActionPending       ActionStatus = "pending"
	ActionInProgress    ActionStatus = "in_progress"
	ActionCompleted     ActionStatus = "completed"
	ActionNotApplicable ActionStatus = "not_applicable"
)

type ContainmentArea string

const (
	ContainmentAPIKeys        ContainmentArea = "api_keys"
	ContainmentSessions       ContainmentArea = "sessions"
	ContainmentIntegrations   ContainmentArea = "integrations"
	ContainmentObjectStorage  ContainmentArea = "object_storage"
	ContainmentDatabaseAccess ContainmentArea = "database_access"
	ContainmentEmail          ContainmentArea = "email"
	ContainmentAI             ContainmentArea = "ai"
	ContainmentAnalytics      ContainmentArea = "analytics"
	ContainmentReplay         ContainmentArea = "replay"
)

type containmentControl struct {
	Area  ContainmentArea
	Title string
}

var containmentControls = []containmentControl{
	{Area: ContainmentAPIKeys, Title: "Review and rotate affected API keys"},
	{Area: ContainmentSessions, Title: "Review and revoke affected sessions"},
	{Area: ContainmentIntegrations, Title: "Review and isolate affected integrations"},
	{Area: ContainmentObjectStorage, Title: "Review and restrict affected object storage"},
	{Area: ContainmentDatabaseAccess, Title: "Review and restrict affected database access"},
	{Area: ContainmentEmail, Title: "Review and contain affected email delivery"},
	{Area: ContainmentAI, Title: "Review and contain affected AI processing"},
	{Area: ContainmentAnalytics, Title: "Review and contain affected analytics collection"},
	{Area: ContainmentReplay, Title: "Review and contain affected session replay"},
}

// ActionEvent is a complete immutable revision of one logical breach action.
// The latest revision is the current state; all revisions remain evidence.
type ActionEvent struct {
	ID                   string           `json:"id"`
	Sequence             int64            `json:"sequence"`
	BreachIncidentID     string           `json:"breach_incident_id"`
	ActionID             string           `json:"action_id"`
	Revision             int              `json:"revision"`
	ActionKind           ActionKind       `json:"action_kind"`
	ContainmentArea      *ContainmentArea `json:"containment_area,omitempty"`
	Title                string           `json:"title"`
	Status               ActionStatus     `json:"status"`
	OwnerPlatformAdminID *string          `json:"owner_platform_admin_id,omitempty"`
	DueAt                *time.Time       `json:"due_at,omitempty"`
	OutcomeSummary       *string          `json:"outcome_summary,omitempty"`
	CorrectiveReference  *string          `json:"corrective_reference,omitempty"`
	EvidenceReferences   []string         `json:"evidence_references"`
	ActorPlatformAdminID string           `json:"actor_platform_admin_id"`
	OccurredAt           time.Time        `json:"occurred_at"`
	CreatedAt            time.Time        `json:"created_at"`
}

type CreatePostIncidentActionInput struct {
	Title                string
	OwnerPlatformAdminID string
	DueAt                time.Time
	CorrectiveReference  *string
}

type AppendActionRevisionInput struct {
	ExpectedRevision     int
	Status               ActionStatus
	OwnerPlatformAdminID *string
	DueAt                *time.Time
	OutcomeSummary       *string
	CorrectiveReference  *string
	EvidenceReferences   []string
}

func ParseActionStatus(value string) (ActionStatus, error) {
	switch ActionStatus(value) {
	case ActionPending, ActionInProgress, ActionCompleted, ActionNotApplicable:
		return ActionStatus(value), nil
	}
	return "", ErrInvalidRequest
}

func normalizeCreatePostIncidentActionInput(
	input CreatePostIncidentActionInput,
) (CreatePostIncidentActionInput, error) {
	title := strings.TrimSpace(input.Title)
	ownerID := strings.TrimSpace(input.OwnerPlatformAdminID)
	if title == "" || len(title) > 300 || containsControlCharacter(title) ||
		!isUUID(ownerID) || input.DueAt.IsZero() {
		return CreatePostIncidentActionInput{}, ErrInvalidRequest
	}
	dueAt := input.DueAt.UTC()
	correctiveReference, err := normalizeOptionalBoundedText(input.CorrectiveReference, 300)
	if err != nil {
		return CreatePostIncidentActionInput{}, err
	}
	return CreatePostIncidentActionInput{
		Title: title, OwnerPlatformAdminID: ownerID, DueAt: dueAt,
		CorrectiveReference: correctiveReference,
	}, nil
}

func normalizeAppendActionRevisionInput(
	input AppendActionRevisionInput,
) (AppendActionRevisionInput, error) {
	if input.ExpectedRevision <= 0 {
		return AppendActionRevisionInput{}, ErrInvalidRequest
	}
	status, err := ParseActionStatus(string(input.Status))
	if err != nil {
		return AppendActionRevisionInput{}, err
	}
	ownerID, err := normalizeOptionalUUID(input.OwnerPlatformAdminID)
	if err != nil {
		return AppendActionRevisionInput{}, err
	}
	dueAt, err := normalizeOptionalTime(input.DueAt)
	if err != nil {
		return AppendActionRevisionInput{}, err
	}
	outcome, err := normalizeOptionalBoundedText(input.OutcomeSummary, 4000)
	if err != nil {
		return AppendActionRevisionInput{}, err
	}
	correctiveReference, err := normalizeOptionalBoundedText(input.CorrectiveReference, 300)
	if err != nil {
		return AppendActionRevisionInput{}, err
	}
	evidence, err := normalizeEvidenceReferences(input.EvidenceReferences)
	if err != nil {
		return AppendActionRevisionInput{}, err
	}
	switch status {
	case ActionInProgress:
		if ownerID == nil {
			return AppendActionRevisionInput{}, ErrInvalidRequest
		}
	case ActionCompleted:
		if ownerID == nil || outcome == nil || len(evidence) == 0 {
			return AppendActionRevisionInput{}, ErrInvalidRequest
		}
	case ActionNotApplicable:
		if ownerID == nil || outcome == nil {
			return AppendActionRevisionInput{}, ErrInvalidRequest
		}
	}
	return AppendActionRevisionInput{
		ExpectedRevision: input.ExpectedRevision, Status: status,
		OwnerPlatformAdminID: ownerID, DueAt: dueAt, OutcomeSummary: outcome,
		CorrectiveReference: correctiveReference, EvidenceReferences: evidence,
	}, nil
}

func normalizeOptionalUUID(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if !isUUID(trimmed) {
		return nil, ErrInvalidRequest
	}
	return &trimmed, nil
}

func normalizeOptionalTime(value *time.Time) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	if value.IsZero() {
		return nil, ErrInvalidRequest
	}
	normalized := value.UTC()
	return &normalized, nil
}

func normalizeOptionalBoundedText(value *string, max int) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" || len(trimmed) > max || containsControlCharacter(trimmed) {
		return nil, ErrInvalidRequest
	}
	return &trimmed, nil
}

func validActionTransition(previous, next ActionStatus) bool {
	if previous == ActionCompleted || previous == ActionNotApplicable {
		return next == ActionInProgress
	}
	return true
}
