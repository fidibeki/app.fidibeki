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
