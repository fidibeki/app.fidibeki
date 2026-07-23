package middleware

import (
	"strings"
	"testing"
)

func TestValidAPIKeyToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
		valid bool
	}{
		{name: "valid", token: "fk_live_" + strings.Repeat("A", 32), valid: true},
		{name: "wrong prefix", token: "fk_test_" + strings.Repeat("A", 32)},
		{name: "too short", token: "fk_live_short"},
		{name: "invalid character", token: "fk_live_" + strings.Repeat("A", 31) + "-"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := validAPIKeyToken(test.token); got != test.valid {
				t.Fatalf("validAPIKeyToken(%q) = %t, want %t", test.token, got, test.valid)
			}
		})
	}
}
