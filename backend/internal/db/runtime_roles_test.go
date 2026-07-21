package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
)

func TestConfigureRuntimeRolesValidatesCredentialURLsBeforeConnecting(t *testing.T) {
	t.Parallel()

	const (
		directURL = "postgresql://owner:owner-password@database.invalid/fidibeki"
		appURL    = "postgresql://fidibeki_app:app-password@database.invalid/fidibeki"
		adminURL  = "postgresql://fidibeki_admin:admin-password@database.invalid/fidibeki"
	)
	tests := []struct {
		name      string
		appURL    string
		adminURL  string
		wantError string
	}{
		{name: "app URL is not absolute", appURL: "not-a-url", adminURL: adminURL, wantError: "absolute URL"},
		{name: "app role is wrong", appURL: "postgresql://other:password@database.invalid/fidibeki", adminURL: adminURL, wantError: "must use the fidibeki_app role"},
		{name: "app password is missing", appURL: "postgresql://fidibeki_app@database.invalid/fidibeki", adminURL: adminURL, wantError: "non-empty password"},
		{name: "admin role is wrong", appURL: appURL, adminURL: "postgresql://other:password@database.invalid/fidibeki", wantError: "must use the fidibeki_admin role"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := db.ConfigureRuntimeRoles(context.Background(), directURL, test.appURL, test.adminURL)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ConfigureRuntimeRoles() error = %v, want %q", err, test.wantError)
			}
		})
	}
}
