package replay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

func (store Store) MarkVerificationEnqueued(
	ctx context.Context,
	task BatchVerificationTask,
	at time.Time,
) error {
	tag, err := store.Database.Exec(ctx, `
		UPDATE session_batches
		SET verification_enqueued_at = $4
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status = 'quarantined'
	`, task.TenantID, task.BatchID, task.Generation, at.UTC())
	if err != nil {
		return fmt.Errorf("mark replay batch verification enqueued: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNoBatchVerificationWork
	}
	return nil
}

func (store Store) UndoVerificationEnqueued(
	ctx context.Context,
	task BatchVerificationTask,
) error {
	_, err := store.Database.Exec(ctx, `
		UPDATE session_batches
		SET verification_enqueued_at = NULL
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status = 'quarantined'
	`, task.TenantID, task.BatchID, task.Generation)
	if err != nil {
		return fmt.Errorf("undo replay batch verification enqueue: %w", err)
	}
	return nil
}

func (store Store) PrepareBatchVerifications(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]BatchVerificationTask, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("replay batch verification limit must be between 1 and 1000")
	}
	rows, err := store.Database.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM session_batches
			WHERE safety_status = 'quarantined'
			  AND (
			      verification_enqueued_at IS NULL
			      OR verification_enqueued_at < $1::timestamptz - interval '15 minutes'
			  )
			ORDER BY COALESCE(verification_enqueued_at, completed_at), id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE session_batches AS batch
		SET verification_generation = CASE
		        WHEN verification_enqueued_at IS NULL THEN verification_generation
		        ELSE verification_generation + 1
		    END,
		    verification_enqueued_at = $1::timestamptz
		FROM candidates
		WHERE batch.id = candidates.id
		RETURNING batch.tenant_id, batch.id, batch.verification_generation
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("prepare replay batch verifications: %w", err)
	}
	defer rows.Close()
	tasks := make([]BatchVerificationTask, 0)
	for rows.Next() {
		var task BatchVerificationTask
		if err := rows.Scan(&task.TenantID, &task.BatchID, &task.Generation); err != nil {
			return nil, fmt.Errorf("scan replay batch verification task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replay batch verification tasks: %w", err)
	}
	return tasks, nil
}

func (store Store) Claim(
	ctx context.Context,
	task BatchVerificationTask,
) (BatchVerificationWork, error) {
	batch, err := scanBatchRow(store.Database.QueryRow(ctx, `
		UPDATE session_batches
		SET verification_attempts = verification_attempts + 1
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status IN ('quarantined', 'rejected')
		  AND completed_at IS NOT NULL
		  AND retention_deleted_at IS NULL
		  AND storage_key IS NOT NULL
		RETURNING id, tenant_id, project_id, session_id, batch_index, storage_key,
		          events_count, size_bytes, content_type, complete_token_hash, completed_at,
		          safety_status, upload_expires_at, safety_checked_at, safety_error_reason,
		          verification_generation, verification_enqueued_at, verification_attempts
	`, task.TenantID, task.BatchID, task.Generation))
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchVerificationWork{}, ErrNoBatchVerificationWork
	}
	if err != nil {
		return BatchVerificationWork{}, fmt.Errorf("claim replay batch verification: %w", err)
	}
	return BatchVerificationWork{Batch: batch}, nil
}

func (store Store) MarkSafe(
	ctx context.Context,
	work BatchVerificationWork,
	checkedAt time.Time,
) error {
	var updated int
	err := store.Database.QueryRow(ctx, `
		WITH updated_batch AS (
			UPDATE session_batches
			SET safety_status = 'safe',
			    safety_checked_at = $4,
			    safety_error_reason = NULL,
			    verification_enqueued_at = NULL
			WHERE tenant_id = $1 AND id = $2
			  AND verification_generation = $3
			  AND safety_status = 'quarantined'
			RETURNING id, session_id, events_count
		),
		safe_totals AS (
			SELECT updated.session_id,
			       COALESCE(sum(batch.events_count), 0)::integer + updated.events_count
			           AS events_count,
			       count(batch.id)::integer + 1 AS batches_count
			FROM updated_batch AS updated
			LEFT JOIN session_batches AS batch
			  ON batch.session_id = updated.session_id
			 AND batch.safety_status = 'safe'
			 AND batch.id <> updated.id
			GROUP BY updated.session_id, updated.events_count
		),
		updated_session AS (
			UPDATE sessions AS session
			SET events_count = totals.events_count,
			    batches_count = totals.batches_count
			FROM safe_totals AS totals
			WHERE session.id = totals.session_id
			RETURNING session.id
		)
		SELECT count(*) FROM updated_batch
	`, work.TenantID, work.ID, work.Generation, checkedAt.UTC()).Scan(&updated)
	if err != nil {
		return fmt.Errorf("mark replay batch safe: %w", err)
	}
	if updated != 1 {
		return ErrNoBatchVerificationWork
	}
	return nil
}

func (store Store) MarkRejected(
	ctx context.Context,
	work BatchVerificationWork,
	reason string,
	checkedAt time.Time,
) error {
	tag, err := store.Database.Exec(ctx, `
		UPDATE session_batches
		SET safety_status = 'rejected',
		    safety_checked_at = $4,
		    safety_error_reason = $5
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status = 'quarantined'
	`, work.TenantID, work.ID, work.Generation, checkedAt.UTC(), reason)
	if err != nil {
		return fmt.Errorf("reject replay batch verification: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNoBatchVerificationWork
	}
	return nil
}

func (store Store) CompleteRejectedCleanup(
	ctx context.Context,
	work BatchVerificationWork,
) error {
	tag, err := store.Database.Exec(ctx, `
		UPDATE session_batches
		SET verification_enqueued_at = NULL
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status = 'rejected'
	`, work.TenantID, work.ID, work.Generation)
	if err != nil {
		return fmt.Errorf("complete rejected replay batch cleanup: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNoBatchVerificationWork
	}
	return nil
}

func (store Store) ExpirePendingBatches(
	ctx context.Context,
	now time.Time,
	limit int,
) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, errors.New("replay batch expiry limit must be between 1 and 1000")
	}
	tag, err := store.Database.Exec(ctx, `
		WITH expired AS (
			SELECT id
			FROM session_batches
			WHERE safety_status = 'pending'
			  AND upload_expires_at <= $1
			ORDER BY upload_expires_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE session_batches AS batch
		SET complete_token_hash = NULL,
		    safety_status = 'expired',
		    safety_checked_at = $1,
		    safety_error_reason = 'upload_expired',
		    verification_enqueued_at = NULL
		FROM expired
		WHERE batch.id = expired.id
	`, now.UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("expire pending replay batch uploads: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (store Store) ExpireBatch(
	ctx context.Context,
	query db.Querier,
	batchID string,
	expiredAt time.Time,
) error {
	var id string
	err := query.QueryRow(ctx, `
		UPDATE session_batches
		SET complete_token_hash = NULL,
		    safety_status = 'expired',
		    safety_checked_at = $2,
		    safety_error_reason = 'upload_expired',
		    verification_enqueued_at = NULL
		WHERE id = $1 AND safety_status = 'pending'
		  AND upload_expires_at <= $2
		RETURNING id
	`, batchID, expiredAt.UTC()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("expire replay batch upload: %w", err)
	}
	return nil
}
