package apikey

import (
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("API key not found")
	ErrTokenConflict = errors.New("API key token collision")
)

type APIKey struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	Name            string     `json:"name"`
	TokenPrefix     string     `json:"token_prefix"`
	Permissions     []string   `json:"permissions"`
	RateLimitPerMin int        `json:"rate_limit_per_min"`
	LastUsedAt      *time.Time `json:"last_used_at"`
	ExpiresAt       *time.Time `json:"expires_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type CreateInput struct {
	Name            string     `json:"name"`
	Permissions     []string   `json:"permissions"`
	RateLimitPerMin int        `json:"rate_limit_per_min"`
	ExpiresAt       *time.Time `json:"expires_at"`
}

type CreateValues struct {
	Name            string
	TokenHash       string
	TokenPrefix     string
	Permissions     []string
	RateLimitPerMin int
	ExpiresAt       *time.Time
}

type FieldErrors map[string]string
