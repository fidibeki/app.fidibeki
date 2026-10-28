package apikey

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
)

const tokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func GenerateToken() (raw, hash, prefix string, err error) {
	value := make([]byte, 32)
	ceiling := big.NewInt(int64(len(tokenAlphabet)))
	for index := range value {
		random, randomErr := rand.Int(rand.Reader, ceiling)
		if randomErr != nil {
			return "", "", "", fmt.Errorf("generate API key: %w", randomErr)
		}
		value[index] = tokenAlphabet[random.Int64()]
	}
	raw = "fk_live_" + string(value)
	return raw, auth.HashToken(raw), raw[:12] + "...", nil
}

func ValidateCreate(input CreateInput, now time.Time) (CreateValues, FieldErrors) {
	values := CreateValues{
		Name: strings.TrimSpace(input.Name), Permissions: input.Permissions,
		RateLimitPerMin: input.RateLimitPerMin, ExpiresAt: input.ExpiresAt,
	}
	fields := FieldErrors{}
	if len(values.Name) < 1 || len(values.Name) > 120 {
		fields["name"] = "must be between 1 and 120 characters"
	}
	if !auth.ValidTenantPermissions(values.Permissions) {
		fields["permissions"] = "must contain unique, valid tenant permissions"
	}
	if values.RateLimitPerMin == 0 {
		values.RateLimitPerMin = 1000
	}
	if values.RateLimitPerMin < 1 || values.RateLimitPerMin > 100_000 {
		fields["rate_limit_per_min"] = "must be between 1 and 100000"
	}
	if values.ExpiresAt != nil && !values.ExpiresAt.After(now) {
		fields["expires_at"] = "must be in the future"
	}
	return values, fields
}
