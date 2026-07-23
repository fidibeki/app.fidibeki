package integration

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("integration not found")
	ErrNotReady = errors.New("integration content moderation is not complete")
)

type Kind string

const (
	KindSlack   Kind = "slack"
	KindTeams   Kind = "teams"
	KindWebhook Kind = "webhook"
)

const (
	EventFeedbackCreated = "feedback.created"
	EventIssueCreated    = "issue.created"
	EventIssueUpdated    = "issue.updated"
	EventIssueResolved   = "issue.resolved"
)

type Integration struct {
	ID                       string    `json:"id"`
	Kind                     Kind      `json:"kind"`
	Name                     string    `json:"name"`
	WebhookURL               string    `json:"webhook_url"`
	Channel                  *string   `json:"channel,omitempty"`
	WebhookSecretFingerprint *string   `json:"webhook_secret_fingerprint,omitempty"`
	CustomHeaderNames        []string  `json:"custom_header_names"`
	Events                   []string  `json:"events"`
	Active                   bool      `json:"is_active"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

type CreateInput struct {
	Kind          Kind              `json:"kind"`
	Name          string            `json:"name"`
	WebhookURL    string            `json:"webhook_url"`
	Channel       *string           `json:"channel"`
	CustomHeaders map[string]string `json:"custom_headers"`
	Events        []string          `json:"events"`
	Active        *bool             `json:"is_active"`
}

type CreateValues struct {
	ID            string
	Kind          Kind
	Name          string
	WebhookURL    string
	Channel       *string
	CustomHeaders map[string]string
	Events        []string
	Active        bool
	WebhookSecret string
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type Notification struct {
	OutboxEventID             string
	TenantID                  string
	TenantName                string
	TenantSlug                string
	IntegrationID             string
	Kind                      Kind
	WebhookURL                string
	Channel                   string
	CustomHeaders             map[string]string
	WebhookSecret             string
	Event                     string
	ResourceType              string
	ResourceID                string
	OccurredAt                time.Time
	PublicID                  string
	Title                     string
	Description               string
	Text                      string
	SuggestedTitle            string
	FeedbackStatus            string
	TextProcessStatus         string
	TextModerationFlags       []string
	AIProcessed               bool
	IssueSuggestedTitle       string
	IssueTitleModerated       bool
	IssueDescriptionModerated bool
	IssueModeratedSources     []string
	PageURL                   string
	Rating                    *int
	Category                  string
	Actionability             string
	Priority                  string
	ProjectID                 string
	ProjectName               string
	FeedbackID                string
	IssueID                   string
	IssuePublicID             string
	IssueStatus               string
	OldStatus                 string
	NewStatus                 string
	Changes                   map[string]Change
	Actor                     *NotificationActor
	Comment                   string
	AttachmentCount           int
	AttachmentKinds           []string
	AttachmentFeedbackID      string
	ResourceURL               string
	IssueURL                  string
	FeedbackURL               string
	AttachmentsURL            string
}

type Change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

type NotificationActor struct {
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

type DeliveryAttempt struct {
	ID        string
	Completed bool
}
