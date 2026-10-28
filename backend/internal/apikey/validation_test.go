package apikey

import (
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
)

func TestGenerateTokenUsesPublicPrefixAndStoresOnlyHash(t *testing.T) {
	t.Parallel()
	raw, hash, prefix, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	if !strings.HasPrefix(raw, "fk_live_") || len(raw) != 40 {
		t.Errorf("raw token = %q", raw)
	}
	if len(hash) != 64 || hash == raw {
		t.Errorf("hash = %q", hash)
	}
	if prefix != raw[:12]+"..." {
		t.Errorf("prefix = %q, want %q", prefix, raw[:12]+"...")
	}
}

func TestValidateCreateNormalizesAndRejectsUnsafeValues(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	values, fields := ValidateCreate(CreateInput{
		Name: "  Deploy reader  ",
		Permissions: []string{
			string(auth.PermissionProjectRead),
			string(auth.PermissionFeedbackRead),
		},
		RateLimitPerMin: 750,
		ExpiresAt:       &future,
	}, time.Now())
	if len(fields) != 0 {
		t.Fatalf("fields = %#v", fields)
	}
	if values.Name != "Deploy reader" || values.RateLimitPerMin != 750 || values.ExpiresAt == nil {
		t.Errorf("values = %#v", values)
	}

	_, fields = ValidateCreate(CreateInput{
		Name:            " ",
		Permissions:     []string{"tenant:update", "not:real"},
		RateLimitPerMin: 100_001,
		ExpiresAt:       pointer(time.Now().Add(-time.Minute)),
	}, time.Now())
	for _, field := range []string{"name", "permissions", "rate_limit_per_min", "expires_at"} {
		if fields[field] == "" {
			t.Errorf("missing error for %s: %#v", field, fields)
		}
	}
}

func pointer[T any](value T) *T { return &value }
