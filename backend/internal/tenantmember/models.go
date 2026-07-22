package tenantmember

import (
	"errors"
	"time"
)

var (
	ErrMemberNotFound              = errors.New("tenant member not found")
	ErrMemberExists                = errors.New("tenant member already exists")
	ErrMemberDomainNotVerified     = errors.New("email domain is not verified for this workspace")
	ErrMemberDomainAlreadyVerified = errors.New("member domain is already verified")
	ErrDomainVerificationInvalid   = errors.New("domain verification is invalid or expired")
	ErrMagicLinkInvalid            = errors.New("magic link is invalid or expired")
	ErrRefreshInvalid              = errors.New("refresh token is invalid or expired")
	ErrRefreshExpired              = errors.New("refresh token is expired")
	ErrRefreshReuse                = errors.New("revoked refresh token was reused")
)

type Member struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenant_id"`
	Email       string   `json:"email"`
	Name        *string  `json:"name,omitempty"`
	Permissions []string `json:"permissions"`
}

type MemberDomain struct {
	ID                    string     `json:"id"`
	TenantID              string     `json:"tenant_id"`
	Domain                string     `json:"domain"`
	VerificationEmail     string     `json:"verification_email"`
	VerificationExpiresAt time.Time  `json:"verification_expires_at"`
	VerifiedAt            *time.Time `json:"verified_at,omitempty"`
	Status                string     `json:"status"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type SessionTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Member       Member
}
