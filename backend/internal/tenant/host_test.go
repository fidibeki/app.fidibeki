package tenant_test

import (
	"errors"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestSlugFromHostAcceptsExactlyOneValidatedSubdomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		base     string
		wantSlug string
		wantErr  error
	}{
		{name: "local host with port", host: "acme.localhost:8080", base: "localhost", wantSlug: "acme"},
		{name: "production host", host: "beta.fidibeki.app", base: "fidibeki.app", wantSlug: "beta"},
		{name: "case normalized", host: "Acme.FIDIBEKI.APP", base: "fidibeki.app", wantSlug: "acme"},
		{name: "base host", host: "fidibeki.app", base: "fidibeki.app", wantErr: tenant.ErrNotTenantHost},
		{name: "nested subdomain", host: "one.two.fidibeki.app", base: "fidibeki.app", wantErr: tenant.ErrNotTenantHost},
		{name: "suffix spoof", host: "acme.fidibeki.app.evil.test", base: "fidibeki.app", wantErr: tenant.ErrNotTenantHost},
		{name: "reserved platform host", host: "api.fidibeki.app", base: "fidibeki.app", wantErr: tenant.ErrInvalidSlug},
		{name: "invalid slug", host: "-bad.fidibeki.app", base: "fidibeki.app", wantErr: tenant.ErrInvalidSlug},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tenant.SlugFromHost(tt.host, tt.base)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SlugFromHost() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.wantSlug {
				t.Errorf("SlugFromHost() = %q, want %q", got, tt.wantSlug)
			}
		})
	}
}
