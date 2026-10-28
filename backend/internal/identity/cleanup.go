package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/retentionhealth"
	"github.com/fidibeki/app.fidibeki/backend/internal/storage"
)

const (
	cleanupBatchSize = 50
	cleanupLease     = 10 * time.Minute
)

type ObjectStorage interface {
	PutObject(context.Context, string, io.Reader, int64, string) error
	OpenObject(context.Context, string) (io.ReadCloser, error)
	RemoveObject(context.Context, string) error
}

func (store Store) ClaimCleanup(
	ctx context.Context,
	limit int,
) ([]DeletionCandidate, error) {
	if store.Database == nil {
		return nil, errors.New("identity cleanup database is unavailable")
	}
	if limit <= 0 {
		limit = cleanupBatchSize
	}
	rows, err := store.Database.Query(ctx, `
		WITH due AS (
			SELECT id
			FROM identity_asset_deletions
			WHERE next_attempt_at <= $1
			ORDER BY next_attempt_at, created_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE identity_asset_deletions AS deletion
		SET attempts = deletion.attempts + 1,
		    next_attempt_at = $1 + ($3 * interval '1 second')
		FROM due
		WHERE deletion.id = due.id
		RETURNING deletion.id, deletion.tenant_id, deletion.asset_kind,
		          deletion.member_id::text, deletion.version::text, deletion.attempts
	`, store.now(), limit, int(cleanupLease.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("claim identity asset cleanup: %w", err)
	}
	defer rows.Close()
	candidates := make([]DeletionCandidate, 0, limit)
	for rows.Next() {
		var candidate DeletionCandidate
		if err := rows.Scan(
			&candidate.ID, &candidate.TenantID, &candidate.Kind, &candidate.MemberID,
			&candidate.Version, &candidate.Attempts,
		); err != nil {
			return nil, fmt.Errorf("scan identity asset cleanup: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate identity asset cleanup: %w", err)
	}
	return candidates, nil
}

func (store Store) CleanupVersionIsCurrent(
	ctx context.Context,
	candidate DeletionCandidate,
) (bool, error) {
	if store.Database == nil {
		return false, errors.New("identity cleanup database is unavailable")
	}
	var current bool
	switch candidate.Kind {
	case ImageKindLogo:
		if err := store.Database.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM tenants
				WHERE id = $1 AND logo_version = $2
			)
		`, candidate.TenantID, candidate.Version).Scan(&current); err != nil {
			return false, fmt.Errorf("check current workspace logo: %w", err)
		}
	case ImageKindAvatar:
		if candidate.MemberID == nil {
			return false, ErrAssetNotFound
		}
		if err := store.Database.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM tenant_members
				WHERE tenant_id = $1 AND id = $2 AND avatar_version = $3
			)
		`, candidate.TenantID, *candidate.MemberID, candidate.Version).Scan(&current); err != nil {
			return false, fmt.Errorf("check current member avatar: %w", err)
		}
	default:
		return false, ErrAssetNotFound
	}
	return current, nil
}

func (store Store) CompleteCleanup(ctx context.Context, candidateID string) error {
	if _, err := store.Database.Exec(ctx, `
		DELETE FROM identity_asset_deletions WHERE id = $1
	`, candidateID); err != nil {
		return fmt.Errorf("complete identity asset cleanup: %w", err)
	}
	return nil
}

func (store Store) FailCleanup(
	ctx context.Context,
	candidate DeletionCandidate,
	failure error,
) error {
	delay := time.Duration(1<<min(candidate.Attempts, 10)) * time.Minute
	if delay > 24*time.Hour {
		delay = 24 * time.Hour
	}
	if _, err := store.Database.Exec(ctx, `
		UPDATE identity_asset_deletions
		SET last_error = $2, next_attempt_at = $3
		WHERE id = $1
	`, candidate.ID, truncateError(failure), store.now().Add(delay)); err != nil {
		return fmt.Errorf("defer identity asset cleanup: %w", err)
	}
	return nil
}

type CleanupMaintainer struct {
	Store     Store
	Storage   ObjectStorage
	Health    retentionhealth.Recorder
	Logger    *slog.Logger
	Now       func() time.Time
	BatchSize int
}

func (maintainer CleanupMaintainer) Maintain(ctx context.Context) error {
	if maintainer.Storage == nil {
		return errors.New("identity cleanup storage is unavailable")
	}
	if maintainer.Health == nil {
		return errors.New("identity cleanup health recorder is unavailable")
	}
	now := maintainer.now()
	completed, sweepErr := maintainer.sweep(ctx)
	result := retentionhealth.ResultSucceeded
	if sweepErr != nil {
		result = retentionhealth.ResultFailed
	}
	recordErr := maintainer.Health.RecordRun(ctx, retentionhealth.Run{
		Operation:   retentionhealth.OperationIdentityAssetDeletions,
		AttemptedAt: now, Result: result, Processed: completed,
	})
	if recordErr != nil {
		recordErr = fmt.Errorf("record identity cleanup retention health: %w", recordErr)
	}
	return errors.Join(sweepErr, recordErr)
}

func (maintainer CleanupMaintainer) sweep(ctx context.Context) (int64, error) {
	candidates, err := maintainer.Store.ClaimCleanup(ctx, maintainer.BatchSize)
	if err != nil {
		return 0, err
	}
	var completed int64
	var failures []error
	for _, candidate := range candidates {
		current, currentErr := maintainer.Store.CleanupVersionIsCurrent(ctx, candidate)
		if currentErr != nil {
			failures = append(failures, maintainer.deferFailure(ctx, candidate, currentErr))
			continue
		}
		if current {
			if err := maintainer.Store.CompleteCleanup(ctx, candidate.ID); err != nil {
				failures = append(failures, err)
				continue
			}
			completed++
			continue
		}
		removeErr := removeCandidateObjects(ctx, maintainer.Storage, candidate)
		if removeErr != nil {
			failures = append(failures, maintainer.deferFailure(ctx, candidate, removeErr))
			continue
		}
		if err := maintainer.Store.CompleteCleanup(ctx, candidate.ID); err != nil {
			failures = append(failures, err)
			continue
		}
		completed++
	}
	return completed, errors.Join(failures...)
}

func (maintainer CleanupMaintainer) now() time.Time {
	if maintainer.Now != nil {
		return maintainer.Now().UTC()
	}
	return time.Now().UTC()
}

func (maintainer CleanupMaintainer) deferFailure(
	ctx context.Context,
	candidate DeletionCandidate,
	failure error,
) error {
	if maintainer.Logger != nil {
		maintainer.Logger.WarnContext(
			ctx, "identity asset cleanup deferred", "error", failure,
			"tenant_id", candidate.TenantID, "asset_kind", candidate.Kind,
			"attempt", candidate.Attempts,
		)
	}
	if err := maintainer.Store.FailCleanup(ctx, candidate, failure); err != nil {
		return errors.Join(failure, err)
	}
	return failure
}

func removeCandidateObjects(
	ctx context.Context,
	objectStorage ObjectStorage,
	candidate DeletionCandidate,
) error {
	for _, size := range []int{64, 256} {
		key, err := ObjectKey(
			candidate.TenantID, candidate.Kind, candidate.MemberID, candidate.Version, size,
		)
		if err != nil {
			return err
		}
		if err := objectStorage.RemoveObject(ctx, key); err != nil &&
			!errors.Is(err, storage.ErrObjectNotFound) {
			return fmt.Errorf("remove identity object %q: %w", key, err)
		}
	}
	return nil
}

func (maintainer CleanupMaintainer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Minute
	}
	if err := maintainer.Maintain(ctx); err != nil && maintainer.Logger != nil {
		maintainer.Logger.ErrorContext(ctx, "identity cleanup maintenance failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := maintainer.Maintain(ctx); err != nil && maintainer.Logger != nil {
				maintainer.Logger.ErrorContext(ctx, "identity cleanup maintenance failed", "error", err)
			}
		}
	}
}
