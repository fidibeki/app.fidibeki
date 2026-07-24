package platformadmin

import (
	"context"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
)

type MaintenanceStore struct {
	Database db.Querier
}

// ExpiredMagicLinks hard-deletes platform_admin_magic_links rows whose
// expires_at boundary has already passed. That boundary is the same one
// ConsumeMagicLink enforces (expires_at > now), so a swept row never had
// remaining function: it authenticates no platform admin and its token_hash
// is a dead credential. Deleting it removes stored credential material
// rather than documenting a new retention period. A minimized, non-content
// audit event (whether it was ever consumed) is written to the global
// platform_security_events ledger in the same statement so the deletion
// itself remains evidenced; an insertion failure rolls back the deletion.
func (store MaintenanceStore) ExpiredMagicLinks(
	ctx context.Context,
	now time.Time,
	limit int,
) (int64, error) {
	if limit <= 0 {
		limit = magicLinkRetentionBatchSize
	}
	var expired int64
	err := store.Database.QueryRow(ctx, `
		WITH candidates AS (
			SELECT id
			FROM platform_admin_magic_links
			WHERE expires_at <= $1
			ORDER BY expires_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		), deleted AS (
			DELETE FROM platform_admin_magic_links AS link
			USING candidates
			WHERE link.id = candidates.id
			RETURNING link.id, (link.consumed_at IS NOT NULL) AS was_consumed
		), audited AS (
			INSERT INTO platform_security_events (kind, resource_type, resource_id, data)
			SELECT 'auth.platform_magic_link_expired', 'platform_admin_magic_link', id,
			       jsonb_build_object('was_consumed', was_consumed)
			FROM deleted
		)
		SELECT count(*) FROM deleted
	`, now.UTC(), limit).Scan(&expired)
	if err != nil {
		return 0, fmt.Errorf("expire platform admin magic links: %w", err)
	}
	return expired, nil
}

var _ MaintenanceOperations = MaintenanceStore{}
