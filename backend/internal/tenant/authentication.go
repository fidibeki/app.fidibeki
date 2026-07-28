package tenant

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// LockForAuthentication reloads and locks the lifecycle row inside the caller's
// transaction. Holding the shared lock gives authentication and concurrent
// lifecycle updates a deterministic commit order.
func LockForAuthentication(
	ctx context.Context,
	query RowQuerier,
	tenantID string,
	now time.Time,
) error {
	var status string
	var trialEndsAt pgtype.Timestamptz
	var deletionRequestedAt pgtype.Timestamptz
	err := query.QueryRow(ctx, `
		SELECT tenant.status, tenant.trial_ends_at, tenant.deletion_requested_at
		FROM tenants AS tenant
		WHERE tenant.id = $1
		FOR SHARE OF tenant
	`, tenantID).Scan(&status, &trialEndsAt, &deletionRequestedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnknownTenant
	}
	if err != nil {
		return fmt.Errorf("lock tenant for authentication: %w", err)
	}

	resolved := Tenant{ID: tenantID, Status: Status(status)}
	if trialEndsAt.Valid {
		resolved.TrialEndsAt = &trialEndsAt.Time
	}
	if deletionRequestedAt.Valid {
		resolved.DeletionRequestedAt = &deletionRequestedAt.Time
	}
	if !resolved.IsActiveAt(now) {
		return ErrInactive
	}
	return nil
}
