// Package seatlimit serializes workspace member creation against the current
// plan capacity.
package seatlimit

import (
	"context"
	"errors"
	"fmt"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

// ErrAtCapacity reports that creating one more non-removed member would exceed
// the workspace plan.
var ErrAtCapacity = errors.New("workspace member limit reached")

// EnsureAvailable locks the workspace before deriving capacity from durable
// membership data. Callers must insert the member in the same transaction so
// concurrent acceptances serialize behind this lock.
func EnsureAvailable(ctx context.Context, query db.Querier, tenantID string) error {
	var plan tenant.Plan
	if err := query.QueryRow(ctx, `
		SELECT plan
		FROM tenants
		WHERE id = $1
		FOR UPDATE
	`, tenantID).Scan(&plan); err != nil {
		return fmt.Errorf("lock workspace for member capacity: %w", err)
	}

	definition, exists := config.PlanFor(plan)
	if !exists {
		return fmt.Errorf("resolve member capacity for plan %q", plan)
	}
	if definition.Limits.Users < 0 {
		return nil
	}

	var members int
	if err := query.QueryRow(ctx, `
		SELECT count(*)
		FROM tenant_members
		WHERE tenant_id = $1 AND status <> 'removed'
	`, tenantID).Scan(&members); err != nil {
		return fmt.Errorf("count workspace members for capacity: %w", err)
	}
	if members >= definition.Limits.Users {
		return ErrAtCapacity
	}
	return nil
}
