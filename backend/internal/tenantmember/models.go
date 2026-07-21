package tenantmember

import (
	"errors"
	"time"
)

var (
	ErrMemberNotFound   = errors.New("tenant member not found")
	ErrMemberExists     = errors.New("tenant member already exists")
	ErrMagicLinkInvalid = errors.New("magic link is invalid or expired")
	ErrRefreshInvalid   = errors.New("refresh token is invalid or expired")
	ErrRefreshReuse     = errors.New("revoked refresh token was reused")
)

type Member struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenant_id"`
	Email       string   `json:"email"`
	Name        *string  `json:"name,omitempty"`
	Permissions []string `json:"permissions"`
}

type SessionTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Member       Member
}
