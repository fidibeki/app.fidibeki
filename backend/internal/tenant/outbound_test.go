package tenant_test

import (
	"strings"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestOutboundDeliveryEligibleSQLUsesTheRequestedClockParameter(t *testing.T) {
	t.Parallel()
	predicate := tenant.OutboundDeliveryEligibleSQL(4)
	for _, required := range []string{
		"tenant.deletion_requested_at IS NULL",
		"tenant.status = 'active'",
		"tenant.status = 'trial'",
		"tenant.trial_ends_at > $4",
	} {
		if !strings.Contains(predicate, required) {
			t.Errorf("predicate %q does not contain %q", predicate, required)
		}
	}
}

func TestOutboundDeliveryEligibleSQLRejectsInvalidClockParameter(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("OutboundDeliveryEligibleSQL(0) did not panic")
		}
	}()
	tenant.OutboundDeliveryEligibleSQL(0)
}
