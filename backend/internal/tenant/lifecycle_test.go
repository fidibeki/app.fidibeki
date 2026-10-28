package tenant_test

import (
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

func TestTenantIsActiveAtUsesOneLifecyclePolicy(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 14, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	expired := now.Add(-time.Nanosecond)
	deletionRequested := now.Add(-24 * time.Hour)

	for _, test := range []struct {
		name   string
		tenant tenant.Tenant
		want   bool
	}{
		{name: "active", tenant: tenant.Tenant{Status: tenant.StatusActive}, want: true},
		{
			name:   "active ignores stale trial metadata",
			tenant: tenant.Tenant{Status: tenant.StatusActive, TrialEndsAt: &expired},
			want:   true,
		},
		{
			name:   "unexpired trial",
			tenant: tenant.Tenant{Status: tenant.StatusTrial, TrialEndsAt: &future},
			want:   true,
		},
		{
			name:   "trial expires at exact boundary",
			tenant: tenant.Tenant{Status: tenant.StatusTrial, TrialEndsAt: &now},
		},
		{
			name:   "expired trial",
			tenant: tenant.Tenant{Status: tenant.StatusTrial, TrialEndsAt: &expired},
		},
		{name: "trial without end", tenant: tenant.Tenant{Status: tenant.StatusTrial}},
		{name: "suspended", tenant: tenant.Tenant{Status: tenant.StatusSuspended}},
		{name: "cancelled", tenant: tenant.Tenant{Status: tenant.StatusCancelled}},
		{
			name: "suspended deletion grace",
			tenant: tenant.Tenant{
				Status: tenant.StatusSuspended, DeletionRequestedAt: &deletionRequested,
			},
			want: true,
		},
		{
			name: "cancelled deletion grace",
			tenant: tenant.Tenant{
				Status: tenant.StatusCancelled, DeletionRequestedAt: &deletionRequested,
			},
			want: true,
		},
		{
			name: "expired trial deletion grace",
			tenant: tenant.Tenant{
				Status: tenant.StatusTrial, TrialEndsAt: &expired,
				DeletionRequestedAt: &deletionRequested,
			},
			want: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.tenant.IsActiveAt(now); got != test.want {
				t.Fatalf("IsActiveAt() = %t, want %t", got, test.want)
			}
		})
	}
}
