// Package issuetracker creates a copy of a Fidibeki issue in an external tracker (Jira or
// Linear) when a workspace has explicitly connected, configured, and enabled that provider.
// The Fidibeki issue always remains the source record: a provider failure is recorded against
// the attempt, never against the issue, and an idempotency key plus a partial unique index make
// a retried or double-submitted request converge on the same external issue.
package issuetracker

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound covers an unknown connection, issue, or external link.
	ErrNotFound = errors.New("issue tracker resource not found")
	// ErrNotConnected means the workspace never stored credentials for the provider.
	ErrNotConnected = errors.New("issue tracker provider is not connected")
	// ErrNotEnabled means the provider is connected but still opt-out.
	ErrNotEnabled = errors.New("issue tracker provider is not enabled")
	// ErrDestinationRequired means enabling was attempted without a validated destination.
	ErrDestinationRequired = errors.New("issue tracker destination is required")
	// ErrLinkExists means a pending or created external issue already exists.
	ErrLinkExists = errors.New("external issue already exists for this issue and provider")
	// ErrLinkInFlight means a concurrent request is still creating the external issue.
	ErrLinkInFlight = errors.New("external issue creation is already in flight")
	// ErrLinkNotRetryable means a retry was requested for an attempt that did not fail.
	ErrLinkNotRetryable = errors.New("external issue attempt cannot be retried")
)

// Provider is the external tracker a workspace can connect. Both are disabled by default.
type Provider string

const (
	ProviderJira   Provider = "jira"
	ProviderLinear Provider = "linear"
)

// Providers lists every supported provider in a stable order.
func Providers() []Provider {
	return []Provider{ProviderJira, ProviderLinear}
}

// Valid reports whether the value is a supported provider.
func (p Provider) Valid() bool {
	return p == ProviderJira || p == ProviderLinear
}

// LinkStatus is the lifecycle of one creation attempt.
type LinkStatus string

const (
	// LinkStatusPending is written before the provider is called so a concurrent request
	// collides on the unique index instead of creating a second external issue.
	LinkStatusPending LinkStatus = "pending"
	LinkStatusCreated LinkStatus = "created"
	LinkStatusFailed  LinkStatus = "failed"
)

// Activity kinds appended to the issue timeline for every creation attempt.
const (
	ActivityExternalIssueCreated = "issue.external_issue_created"
	ActivityExternalIssueFailed  = "issue.external_issue_failed"
)

// Access-audit constants for configuration changes, recorded in tenant_access_events so the
// Settings audit tab shows who connected, configured, enabled, or disconnected a provider.
const (
	AuditResourceType    = "issue_tracker"
	AuditKindConnected   = "issue_tracker.connected"
	AuditKindConfigured  = "issue_tracker.configured"
	AuditKindEnabled     = "issue_tracker.enabled"
	AuditKindDisabled    = "issue_tracker.disabled"
	AuditKindDisconnect  = "issue_tracker.disconnected"
	AuditActionConnect   = "connect"
	AuditActionConfigure = "configure"
	AuditActionEnable    = "enable"
	AuditActionDisable   = "disable"
	AuditActionDisconn   = "disconnect"
	AuditKindAutoCreate  = "issue_tracker.auto_create_changed"
	AuditActionAutomate  = "automate"
)

const (
	AuditKindAutoCreateSuppressed              = "issue_tracker.auto_create_suppressed"
	autoCreateSuppressionReasonTenantLifecycle = "tenant_lifecycle_inactive"
)

// Connection is a workspace's stored provider configuration. It never carries the credential:
// only the fingerprint is exposed, so the browser and the logs can distinguish two credentials
// without being able to read either.
type Connection struct {
	ID                    string     `json:"id"`
	Provider              Provider   `json:"provider"`
	SiteURL               string     `json:"site_url"`
	AccountLabel          string     `json:"account_label"`
	CredentialFingerprint string     `json:"credential_fingerprint"`
	DestinationID         string     `json:"destination_id"`
	DestinationKey        string     `json:"destination_key"`
	DestinationName       string     `json:"destination_name"`
	IssueTypeID           string     `json:"issue_type_id"`
	IssueTypeName         string     `json:"issue_type_name"`
	SubDestinationID      string     `json:"sub_destination_id"`
	SubDestinationName    string     `json:"sub_destination_name"`
	Enabled               bool       `json:"is_enabled"`
	AutoCreate            bool       `json:"auto_create"`
	ValidatedAt           *time.Time `json:"validated_at,omitempty"`
	LastError             string     `json:"last_error"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

// Configured reports whether the connection can create an external issue.
func (c Connection) Configured() bool {
	if c.DestinationID == "" || c.ValidatedAt == nil {
		return false
	}
	return c.Provider != ProviderJira || c.IssueTypeID != ""
}

// ExternalLink is one creation attempt and, once it succeeds, the direct link to the copy.
type ExternalLink struct {
	ID                 string     `json:"id"`
	IssueID            string     `json:"issue_id"`
	Provider           Provider   `json:"provider"`
	Status             LinkStatus `json:"status"`
	RequestTitle       string     `json:"request_title"`
	RequestDescription string     `json:"request_description"`
	RequestPriority    string     `json:"request_priority"`
	DestinationID      string     `json:"destination_id"`
	DestinationName    string     `json:"destination_name"`
	IssueTypeID        string     `json:"issue_type_id"`
	ExternalID         string     `json:"external_id"`
	ExternalKey        string     `json:"external_key"`
	ExternalURL        string     `json:"external_url"`
	LastError          string     `json:"last_error"`
	AttemptCount       int        `json:"attempt_count"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// ConnectInput stores credentials for a provider. Credentials are write-only.
type ConnectInput struct {
	SiteURL  string `json:"site_url"`
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
}

// ConfigureInput updates the destination and the opt-in switch. Nil fields are left untouched
// so a caller can enable without restating the destination.
type ConfigureInput struct {
	DestinationID    *string `json:"destination_id"`
	SubDestinationID *string `json:"sub_destination_id"`
	IssueTypeID      *string `json:"issue_type_id"`
	Enabled          *bool   `json:"is_enabled"`
	AutoCreate       *bool   `json:"auto_create"`
}

// CreateInput is the user-confirmed external issue, reviewed before it is sent.
type CreateInput struct {
	Provider       Provider `json:"provider"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Priority       string   `json:"priority"`
	IdempotencyKey string   `json:"idempotency_key"`
}

// FieldError mirrors the validation envelope used by the rest of the tenant API.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Credentials is the decrypted secret material handed to a provider client. It never leaves
// the process and is never serialized.
type Credentials struct {
	SiteURL  string
	Email    string
	APIToken string
}

// Destination is one place an external issue can be created: a Jira project or a Linear team.
type Destination struct {
	ID         string           `json:"id"`
	Key        string           `json:"key"`
	Name       string           `json:"name"`
	IssueTypes []DestinationRef `json:"issue_types,omitempty"`
	Projects   []DestinationRef `json:"projects,omitempty"`
}

// DestinationRef is a Jira issue type or a Linear project.
type DestinationRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Account is the identity the stored credential authenticates as.
type Account struct {
	Label string
}

// CreateRequest is the provider-agnostic creation payload.
type CreateRequest struct {
	Title          string
	Description    string
	Priority       string
	DestinationID  string
	IssueTypeID    string
	SubDestination string
	IdempotencyKey string
}

// CreateResult identifies the created external issue.
type CreateResult struct {
	ID  string
	Key string
	URL string
}

// Client is one external tracker's contract. Every method must translate provider failures
// into a *ProviderError carrying a message that is safe to show a workspace administrator.
type Client interface {
	Validate(ctx context.Context, credentials Credentials) (Account, error)
	Destinations(ctx context.Context, credentials Credentials) ([]Destination, error)
	CreateIssue(ctx context.Context, credentials Credentials, request CreateRequest) (CreateResult, error)
}
