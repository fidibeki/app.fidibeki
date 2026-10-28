package tenantmember

import (
	"context"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
)

type MaintenanceStore struct {
	Database db.Querier
}

// ExpiredMagicLinks hard-deletes tenant_magic_links rows whose expires_at
// boundary has already passed. That boundary is the same one ConsumeMagicLink
// enforces (expires_at > now), so a swept row never had remaining function:
// it authenticates nothing, activates no invitation, and its token_hash is a
// dead credential. Deleting it removes stored credential material rather than
// documenting a new retention period. A compact, non-content audit event
// (link id, whether it was ever consumed) is written in the same statement so
// the deletion itself remains evidenced.
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
			FROM tenant_magic_links
			WHERE expires_at <= $1
			ORDER BY expires_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		), deleted AS (
			DELETE FROM tenant_magic_links AS link
			USING candidates
			WHERE link.id = candidates.id
			RETURNING link.tenant_id, link.id, link.member_id,
			          (link.consumed_at IS NOT NULL) AS was_consumed
		), audited AS (
			INSERT INTO tenant_access_events (
				tenant_id, kind, actor_member_id, subject_member_id, data
			)
			SELECT tenant_id, 'auth.magic_link_expired', NULL, member_id,
			       jsonb_build_object('magic_link_id', id::text, 'was_consumed', was_consumed)
			FROM deleted
		)
		SELECT count(*) FROM deleted
	`, now.UTC(), limit).Scan(&expired)
	if err != nil {
		return 0, fmt.Errorf("expire tenant magic links: %w", err)
	}
	return expired, nil
}

var _ MaintenanceOperations = MaintenanceStore{}
