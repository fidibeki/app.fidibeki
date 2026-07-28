package platformadmin

import (
	"errors"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

const (
	MagicLinkTTL  = 10 * time.Minute
	RefreshTTL    = 30 * 24 * time.Hour
	DefaultTrial  = 14 * 24 * time.Hour
	DefaultIssuer = "fidibeki"
)

var (
	ErrAdminNotFound                = errors.New("platform admin not found")
	ErrMagicLinkInvalid             = errors.New("magic link is invalid or expired")
	ErrRefreshInvalid               = errors.New("refresh token is invalid or expired")
	ErrRefreshExpired               = errors.New("refresh token is expired")
	ErrRefreshReuse                 = errors.New("revoked refresh token was reused")
	ErrTenantNotFound               = errors.New("tenant not found")
	ErrSlugConflict                 = errors.New("tenant slug already exists")
	ErrTenantUpdateConflict         = errors.New("tenant changed before update")
	ErrOwnershipConfirmation        = errors.New("workspace ownership confirmation is invalid")
	ErrRecentAuthenticationRequired = errors.New("recent platform authentication is required")
	ErrOwnershipTargetInvalid       = errors.New("workspace ownership recovery target is invalid")
	ErrRecoveryVerificationInvalid  = errors.New("workspace recovery verification is invalid")
)

type Admin struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	Name        *string  `json:"name,omitempty"`
	Permissions []string `json:"permissions"`
}

type TenantRecord struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Slug         string        `json:"slug"`
	Plan         tenant.Plan   `json:"plan"`
	Status       tenant.Status `json:"status"`
	BillingEmail *string       `json:"billing_email,omitempty"`
	TrialEndsAt  *time.Time    `json:"trial_ends_at,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type CreateTenantParams struct {
	Name                 string
	Slug                 string
	Plan                 tenant.Plan
	Status               tenant.Status
	BillingEmail         *string
	TrialEndsAt          *time.Time
	OwnerEmail           string
	ActorPlatformAdminID string
}

type RecoverOwnershipParams struct {
	TenantID              string
	ActorPlatformAdminID  string
	TargetMemberID        string
	Confirmation          string
	VerificationReference string
	Reason                string
}

// BackofficeOverview contains aggregate operational counts only. It must stay
// free of administrator, member, session, and credential data because it is
// rendered on the first authenticated screen of the platform console.
type BackofficeOverview struct {
	WorkspaceCount          int64                 `json:"workspace_count"`
	ActiveWorkspaceCount    int64                 `json:"active_workspace_count"`
	TrialWorkspaceCount     int64                 `json:"trial_workspace_count"`
	SuspendedWorkspaceCount int64                 `json:"suspended_workspace_count"`
	CancelledWorkspaceCount int64                 `json:"cancelled_workspace_count"`
	TrialsEndingSoonCount   int64                 `json:"trials_ending_soon_count"`
	RecentWorkspaceCount    int64                 `json:"recent_workspace_count"`
	Plans                   []BackofficePlanCount `json:"plans"`
}

type BackofficePlanCount struct {
	Plan           tenant.Plan `json:"plan"`
	WorkspaceCount int64       `json:"workspace_count"`
}

// BackofficeTenantSummary intentionally excludes billing and owner email. The
// directory is a high-volume operational surface; contact information belongs
// to the separately authorized workspace-detail projection.
type BackofficeTenantSummary struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Slug        string        `json:"slug"`
	Plan        tenant.Plan   `json:"plan"`
	Status      tenant.Status `json:"status"`
	TrialEndsAt *time.Time    `json:"trial_ends_at,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type BackofficeTenantPage struct {
	Tenants    []BackofficeTenantSummary `json:"tenants"`
	NextCursor string                    `json:"next_cursor,omitempty"`
}

type BackofficeTenantListParams struct {
	Query  string
	Status tenant.Status
	Plan   tenant.Plan
	Limit  int
	Cursor string
}

type BackofficeTenantDetail struct {
	Tenant        TenantRecord            `json:"tenant"`
	MemberCount   int64                   `json:"member_count"`
	ProjectCount  int64                   `json:"project_count"`
	FeedbackCount int64                   `json:"feedback_count"`
	Owner         *BackofficeTenantMember `json:"owner,omitempty"`
}

// BackofficeTenantMember is the minimum identity projection needed to verify
// and recover workspace ownership. It intentionally omits permissions,
// sessions, authentication factors, and other credential material.
type BackofficeTenantMember struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Name        *string    `json:"name,omitempty"`
	Status      string     `json:"status"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

type BackofficeRecoveryCandidatePage struct {
	Members    []BackofficeTenantMember `json:"members"`
	NextCursor string                   `json:"next_cursor,omitempty"`
}

type BackofficeRecoveryCandidateListParams struct {
	Limit  int
	Cursor string
}

// BackofficeAccessEvent intentionally excludes the ledger's JSON data column:
// event data can be useful evidence but may contain personal information and is
// not required for platform-level operational triage.
type BackofficeAccessEvent struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	ActorType string    `json:"actor_type"`
	Action    *string   `json:"action,omitempty"`
	Outcome   *string   `json:"outcome,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type BackofficeAccessEventPage struct {
	Events     []BackofficeAccessEvent `json:"events"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

type BackofficeAccessEventListParams struct {
	Limit  int
	Cursor string
}
