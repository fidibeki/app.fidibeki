package issue

import (
	"errors"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/activity"
	"github.com/fidibeki/app.fidibeki/backend/internal/triage"
)

var (
	ErrNotFound                  = errors.New("issue not found")
	ErrNoStatusChange            = errors.New("issue status did not change")
	ErrInvalidMerge              = errors.New("issues cannot be merged")
	ErrFeedbackLinked            = errors.New("feedback is already linked to an issue")
	ErrProjectMismatch           = errors.New("feedback projects do not match")
	ErrReporterAccessUnavailable = errors.New("reporter progress access is unavailable")
)

type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusNeedsInfo  Status = "needs_info"
	StatusResolved   Status = "resolved"
	StatusClosed     Status = "closed"
)

func (s Status) Valid() bool {
	return s == StatusOpen || s == StatusInProgress || s == StatusNeedsInfo ||
		s == StatusResolved || s == StatusClosed
}

type Issue struct {
	ID                         string           `json:"id"`
	TenantID                   string           `json:"tenant_id"`
	ProjectID                  string           `json:"project_id"`
	PublicID                   string           `json:"public_id"`
	Title                      string           `json:"title"`
	Description                string           `json:"description"`
	Status                     Status           `json:"status"`
	Priority                   *triage.Priority `json:"priority"`
	Assignee                   *triage.Assignee `json:"assignee"`
	Tags                       []triage.Tag     `json:"tags"`
	MergedIntoID               *string          `json:"merged_into_id,omitempty"`
	MergedAt                   *time.Time       `json:"merged_at,omitempty"`
	ReporterEmail              *string          `json:"reporter_email,omitempty"`
	NotifyReporter             bool             `json:"notify_reporter"`
	ReporterConsentGrantedAt   *time.Time       `json:"reporter_consent_granted_at,omitempty"`
	ReporterConsentWithdrawnAt *time.Time       `json:"reporter_consent_withdrawn_at,omitempty"`
	ResolvedAt                 *time.Time       `json:"resolved_at,omitempty"`
	ReporterAccessRevokedAt    *time.Time       `json:"reporter_access_revoked_at,omitempty"`
	FeedbackCount              int              `json:"feedback_count"`
	ContentModerated           bool             `json:"-"`
	CreatedAt                  time.Time        `json:"created_at"`
	UpdatedAt                  time.Time        `json:"updated_at"`
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

type Mutation struct {
	Status          *Status
	Comment         *string
	ReporterMessage *string
	Title           *string
	Description     *string
	SetPriority     bool
	Priority        *triage.Priority
	SetAssignee     bool
	AssigneeID      *string
	AddTagIDs       []string
	RemoveTagIDs    []string
}

func (mutation Mutation) HasMetadata() bool {
	return mutation.Title != nil || mutation.Description != nil || mutation.SetPriority ||
		mutation.SetAssignee || len(mutation.AddTagIDs) > 0 || len(mutation.RemoveTagIDs) > 0
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

type Comment struct {
	ID        string           `json:"id"`
	IssueID   string           `json:"issue_id"`
	Author    *activity.Actor  `json:"author,omitempty"`
	Body      string           `json:"body"`
	Mentions  []activity.Actor `json:"mentions"`
	CreatedAt time.Time        `json:"created_at"`
}

type MemberNotification struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	IssueID    string          `json:"issue_id"`
	PublicID   string          `json:"public_id"`
	IssueTitle string          `json:"issue_title"`
	Actor      *activity.Actor `json:"actor,omitempty"`
	Body       string          `json:"body"`
	ReadAt     *time.Time      `json:"read_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}
