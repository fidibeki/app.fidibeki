package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrAssetNotFound = errors.New("identity asset not found")

type Store struct {
	Database db.Querier
	Now      func() time.Time
}

type DeletionCandidate struct {
	ID       string
	TenantID string
	Kind     ImageKind
	MemberID *string
	Version  string
	Attempts int
}

func (store Store) now() time.Time {
	if store.Now != nil {
		return store.Now().UTC()
	}
	return time.Now().UTC()
}

func (store Store) StageCleanup(
	ctx context.Context,
	tenantID string,
	kind ImageKind,
	memberID *string,
	version string,
	delay time.Duration,
) error {
	if store.Database == nil {
		return errors.New("identity cleanup database is unavailable")
	}
	if delay < 0 {
		delay = 0
	}
	return enqueueCleanup(
		ctx, store.Database, tenantID, kind, memberID, version, store.now().Add(delay),
	)
}

func (store Store) ActiveMember(
	ctx context.Context,
	query db.Querier,
	tenantID, memberID string,
) (bool, error) {
	var active bool
	if err := query.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM tenant_members
			WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		)
	`, tenantID, memberID).Scan(&active); err != nil {
		return false, fmt.Errorf("check active identity member: %w", err)
	}
	return active, nil
}

func (store Store) ReplaceLogo(
	ctx context.Context,
	query db.Querier,
	tenantID, actorMemberID, version string,
) error {
	var previous *string
	if err := query.QueryRow(ctx, `
		SELECT logo_version::text FROM tenants WHERE id = $1 FOR UPDATE
	`, tenantID).Scan(&previous); err != nil {
		return fmt.Errorf("lock workspace logo: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenants SET logo_version = $2 WHERE id = $1
	`, tenantID, version); err != nil {
		return fmt.Errorf("replace workspace logo: %w", err)
	}
	if previous != nil {
		if err := enqueueCleanup(
			ctx, query, tenantID, ImageKindLogo, nil, *previous, store.now(),
		); err != nil {
			return err
		}
	}
	return recordAssetAudit(
		ctx, query, tenantID, "organization.logo_updated", actorMemberID, nil,
		previous, &version,
	)
}

func (store Store) RemoveLogo(
	ctx context.Context,
	query db.Querier,
	tenantID, actorMemberID string,
) (bool, error) {
	var previous *string
	if err := query.QueryRow(ctx, `
		SELECT logo_version::text FROM tenants WHERE id = $1 FOR UPDATE
	`, tenantID).Scan(&previous); err != nil {
		return false, fmt.Errorf("lock workspace logo for removal: %w", err)
	}
	if previous == nil {
		return false, nil
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenants SET logo_version = NULL WHERE id = $1
	`, tenantID); err != nil {
		return false, fmt.Errorf("remove workspace logo: %w", err)
	}
	if err := enqueueCleanup(
		ctx, query, tenantID, ImageKindLogo, nil, *previous, store.now(),
	); err != nil {
		return false, err
	}
	if err := recordAssetAudit(
		ctx, query, tenantID, "organization.logo_removed", actorMemberID, nil,
		previous, nil,
	); err != nil {
		return false, err
	}
	return true, nil
}

func (store Store) ReplaceAvatar(
	ctx context.Context,
	query db.Querier,
	tenantID, memberID, version string,
) error {
	var previous *string
	if err := query.QueryRow(ctx, `
		SELECT avatar_version::text
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		FOR UPDATE
	`, tenantID, memberID).Scan(&previous); errors.Is(err, pgx.ErrNoRows) {
		return ErrAssetNotFound
	} else if err != nil {
		return fmt.Errorf("lock member avatar: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenant_members SET avatar_version = $3
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, memberID, version); err != nil {
		return fmt.Errorf("replace member avatar: %w", err)
	}
	if previous != nil {
		if err := enqueueCleanup(
			ctx, query, tenantID, ImageKindAvatar, &memberID, *previous, store.now(),
		); err != nil {
			return err
		}
	}
	return recordAssetAudit(
		ctx, query, tenantID, "member.avatar_updated", memberID, &memberID,
		previous, &version,
	)
}

func (store Store) RemoveAvatar(
	ctx context.Context,
	query db.Querier,
	tenantID, memberID string,
) (bool, error) {
	var previous *string
	if err := query.QueryRow(ctx, `
		SELECT avatar_version::text
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		FOR UPDATE
	`, tenantID, memberID).Scan(&previous); errors.Is(err, pgx.ErrNoRows) {
		return false, ErrAssetNotFound
	} else if err != nil {
		return false, fmt.Errorf("lock member avatar for removal: %w", err)
	}
	if previous == nil {
		return false, nil
	}
	if _, err := query.Exec(ctx, `
		UPDATE tenant_members SET avatar_version = NULL
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, memberID); err != nil {
		return false, fmt.Errorf("remove member avatar: %w", err)
	}
	if err := enqueueCleanup(
		ctx, query, tenantID, ImageKindAvatar, &memberID, *previous, store.now(),
	); err != nil {
		return false, err
	}
	if err := recordAssetAudit(
		ctx, query, tenantID, "member.avatar_removed", memberID, &memberID,
		previous, nil,
	); err != nil {
		return false, err
	}
	return true, nil
}

func (Store) CurrentAvatarVersion(
	ctx context.Context,
	query db.Querier,
	tenantID, memberID string,
) (string, error) {
	var version string
	if err := query.QueryRow(ctx, `
		SELECT avatar_version::text
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2 AND status <> 'removed'
		  AND avatar_version IS NOT NULL
	`, tenantID, memberID).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAssetNotFound
	} else if err != nil {
		return "", fmt.Errorf("get member avatar: %w", err)
	}
	return version, nil
}

func (store Store) CurrentLogoVersion(
	ctx context.Context,
	tenantID string,
) (string, error) {
	if store.Database == nil {
		return "", errors.New("identity database is unavailable")
	}
	var version string
	if err := store.Database.QueryRow(ctx, `
		SELECT logo_version::text
		FROM tenants
		WHERE id = $1 AND logo_version IS NOT NULL
	`, tenantID).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAssetNotFound
	} else if err != nil {
		return "", fmt.Errorf("get workspace logo: %w", err)
	}
	return version, nil
}

func enqueueCleanup(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	kind ImageKind,
	memberID *string,
	version string,
	nextAttempt time.Time,
) error {
	if _, err := query.Exec(ctx, `
		INSERT INTO identity_asset_deletions (
			tenant_id, asset_kind, member_id, version, next_attempt_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, asset_kind, member_id, version)
		DO UPDATE SET next_attempt_at = LEAST(
		    identity_asset_deletions.next_attempt_at, EXCLUDED.next_attempt_at
		)
	`, tenantID, kind, memberID, version, nextAttempt.UTC()); err != nil {
		return fmt.Errorf("enqueue identity asset cleanup: %w", err)
	}
	return nil
}

func recordAssetAudit(
	ctx context.Context,
	query db.Querier,
	tenantID, kind, actorMemberID string,
	subjectMemberID *string,
	before, after *string,
) error {
	data, err := json.Marshal(map[string]any{
		"previous_version": before,
		"version":          after,
	})
	if err != nil {
		return fmt.Errorf("encode identity audit: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, subject_member_id, data
		) VALUES ($1, $2, $3, $4, $5)
	`, tenantID, kind, actorMemberID, subjectMemberID, string(data)); err != nil {
		return fmt.Errorf("record identity audit: %w", err)
	}
	return nil
}

func ObjectKey(
	tenantID string,
	kind ImageKind,
	memberID *string,
	version string,
	size int,
) (string, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return "", ErrAssetNotFound
	}
	if _, err := uuid.Parse(version); err != nil {
		return "", ErrAssetNotFound
	}
	if size != 64 && size != 256 {
		return "", ErrAssetNotFound
	}
	switch kind {
	case ImageKindLogo:
		if memberID != nil {
			return "", ErrAssetNotFound
		}
		return fmt.Sprintf(
			"tenants/%s/identity/logos/%s/%d.png", tenantID, version, size,
		), nil
	case ImageKindAvatar:
		if memberID == nil {
			return "", ErrAssetNotFound
		}
		if _, err := uuid.Parse(*memberID); err != nil {
			return "", ErrAssetNotFound
		}
		return fmt.Sprintf(
			"tenants/%s/identity/members/%s/%s/%d.png",
			tenantID, *memberID, version, size,
		), nil
	default:
		return "", ErrAssetNotFound
	}
}

func truncateError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 500 {
		return message[:500]
	}
	return message
}
