package issue

import (
	"errors"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/triage"
)

var (
	ErrNotFound        = errors.New("issue not found")
	ErrNoStatusChange  = errors.New("issue status did not change")
	ErrInvalidMerge    = errors.New("issues cannot be merged")
	ErrFeedbackLinked  = errors.New("feedback is already linked to an issue")
	ErrProjectMismatch = errors.New("feedback projects do not match")
)

type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusResolved   Status = "resolved"
	StatusClosed     Status = "closed"
)

func (s Status) Valid() bool {
	return s == StatusOpen || s == StatusInProgress || s == StatusResolved || s == StatusClosed
}

type Issue struct {
	ID             string           `json:"id"`
	TenantID       string           `json:"tenant_id"`
	ProjectID      string           `json:"project_id"`
	PublicID       string           `json:"public_id"`
	Title          string           `json:"title"`
	Description    string           `json:"description"`
	Status         Status           `json:"status"`
	Priority       *triage.Priority `json:"priority"`
	Assignee       *triage.Assignee `json:"assignee"`
	Tags           []triage.Tag     `json:"tags"`
	MergedIntoID   *string          `json:"merged_into_id,omitempty"`
	MergedAt       *time.Time       `json:"merged_at,omitempty"`
	ReporterEmail  *string          `json:"reporter_email,omitempty"`
	NotifyReporter bool             `json:"notify_reporter"`
	ResolvedAt     *time.Time       `json:"resolved_at,omitempty"`
	FeedbackCount  int              `json:"feedback_count"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

type CreateFromFeedbackInput struct {
	FeedbackIDs []string
	Title       string
	Description string
	SetPriority bool
	Priority    *triage.Priority
	SetAssignee bool
	AssigneeID  *string
	SetTags     bool
	TagIDs      []string
}

type Update struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	MemberID  *string   `json:"member_id,omitempty"`
	OldStatus *Status   `json:"old_status,omitempty"`
	NewStatus Status    `json:"new_status"`
	Comment   *string   `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
