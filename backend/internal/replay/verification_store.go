package replay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type ExpiredBatchUpload struct {
	TenantID   string
	BatchID    string
	SessionID  string
	StorageKey string
}

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
			WHERE (
			        safety_status = 'quarantined'
			        AND (
			            verification_enqueued_at IS NULL
			            OR verification_enqueued_at < $1::timestamptz - interval '15 minutes'
			        )
			      )
			   OR (
			        safety_status = 'safe'
			        AND storage_key LIKE '%.verified-%'
			        AND upload_expires_at + interval '1 minute' <= $1
			        AND (
			            verification_enqueued_at < $1::timestamptz - interval '1 minute'
			            OR (
			                verification_enqueued_at IS NULL
			                AND EXISTS (
			                    SELECT 1
			                    FROM sessions AS session
			                    WHERE session.id = session_batches.session_id
			                      AND session.merged_at IS NOT NULL
			                )
			            )
			        )
			      )
			   OR (
			        safety_status = 'rejected'
			        AND upload_expires_at + interval '1 minute' <= $1
			        AND verification_enqueued_at < $1::timestamptz - interval '1 minute'
			      )
			ORDER BY COALESCE(verification_enqueued_at, completed_at), id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE session_batches AS batch
		SET verification_generation = CASE
		        WHEN safety_status = 'quarantined' AND verification_enqueued_at IS NULL
		            THEN verification_generation
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
	var work BatchVerificationWork
	err := store.Database.QueryRow(ctx, `
		WITH claimed AS (
			UPDATE session_batches
			SET verification_attempts = verification_attempts + 1
			WHERE tenant_id = $1 AND id = $2
			  AND verification_generation = $3
			  AND safety_status IN ('quarantined', 'safe', 'rejected')
			  AND completed_at IS NOT NULL
			  AND retention_deleted_at IS NULL
			  AND storage_key IS NOT NULL
			RETURNING id, tenant_id, project_id, session_id, batch_index, storage_key,
			          events_count, size_bytes, content_type, complete_token_hash, completed_at,
			          safety_status, upload_expires_at, safety_checked_at, safety_error_reason,
			          verification_generation, verification_enqueued_at, verification_attempts
		)
		SELECT claimed.id, claimed.tenant_id, claimed.project_id, claimed.session_id,
		       claimed.batch_index, claimed.storage_key, claimed.events_count,
		       claimed.size_bytes, claimed.content_type, claimed.complete_token_hash,
		       claimed.completed_at, claimed.safety_status, claimed.upload_expires_at,
		       claimed.safety_checked_at, claimed.safety_error_reason,
		       claimed.verification_generation, claimed.verification_enqueued_at,
		       claimed.verification_attempts, COALESCE(session.page_url, ''),
		       session.rrweb_version
		FROM claimed
		JOIN sessions AS session ON session.id = claimed.session_id
	`, task.TenantID, task.BatchID, task.Generation).Scan(
		&work.ID, &work.TenantID, &work.ProjectID, &work.SessionID, &work.Index,
		&work.StorageKey, &work.EventsCount, &work.SizeBytes, &work.ContentType,
		&work.CompleteTokenHash, &work.CompletedAt, &work.SafetyStatus,
		&work.UploadExpiresAt, &work.SafetyCheckedAt, &work.SafetyErrorReason,
		&work.Generation, &work.VerificationEnqueuedAt, &work.VerificationTries,
		&work.PageURL, &work.RRWebVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchVerificationWork{}, ErrNoBatchVerificationWork
	}
	if err != nil {
		return BatchVerificationWork{}, fmt.Errorf("claim replay batch verification: %w", err)
	}
	return work, nil
}

func (store Store) MarkSafe(
	ctx context.Context,
	work BatchVerificationWork,
	verifiedStorageKey string,
	checkedAt time.Time,
) error {
	var updated int
	err := store.Database.QueryRow(ctx, `
		WITH updated_batch AS (
			UPDATE session_batches
			SET safety_status = 'safe',
			    storage_key = $5,
			    safety_checked_at = $4,
			    safety_error_reason = NULL
			WHERE tenant_id = $1 AND id = $2
			  AND verification_generation = $3
			  AND safety_status = 'quarantined'
			RETURNING id, session_id, events_count
		),
		updated_session AS (
			UPDATE sessions AS session
			SET events_count = session.events_count + updated.events_count,
			    batches_count = session.batches_count + 1
			FROM updated_batch AS updated
			WHERE session.id = updated.session_id
			RETURNING session.id
		)
		SELECT count(*) FROM updated_batch
	`, work.TenantID, work.ID, work.Generation, checkedAt.UTC(), verifiedStorageKey).Scan(&updated)
	if err != nil {
		return fmt.Errorf("mark replay batch safe: %w", err)
	}
	if updated != 1 {
		return ErrNoBatchVerificationWork
	}
	return store.FinalizeRequestedSession(ctx, work.SessionID)
}

func (store Store) CompleteSafeCleanup(
	ctx context.Context,
	work BatchVerificationWork,
	cleanedAt time.Time,
) error {
	tag, err := store.Database.Exec(ctx, `
		UPDATE session_batches
		SET verification_enqueued_at = NULL
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status = 'safe'
		  AND verification_enqueued_at IS NOT NULL
		  AND upload_expires_at + interval '1 minute' <= $4
	`, work.TenantID, work.ID, work.Generation, cleanedAt.UTC())
	if err != nil {
		return fmt.Errorf("complete verified replay staging cleanup: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNoBatchVerificationWork
	}
	return store.DeleteBatch(ctx, work.ID)
}

func (store Store) FinalizeRequestedSession(ctx context.Context, sessionID string) error {
	if _, err := store.Database.Exec(ctx, `
		UPDATE sessions AS session
		SET ended_at = LEAST(
		        GREATEST(session.started_at, session.finalize_requested_at),
		        session.started_at + session.max_duration_seconds * interval '1 second'
		    ),
		    merge_requested_at = session.finalize_requested_at,
		    merge_enqueued_at = NULL,
		    merge_error = NULL,
		    failure_code = NULL
		WHERE session.id = $1
		  AND session.finalize_requested_at IS NOT NULL
		  AND session.ended_at IS NULL
		  AND EXISTS (
		      SELECT 1 FROM session_batches AS batch
		      WHERE batch.session_id = session.id
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM session_batches AS batch
		      WHERE batch.session_id = session.id
		        AND batch.safety_status <> 'safe'
		  )
	`, sessionID); err != nil {
		return fmt.Errorf("finalize verified replay session: %w", err)
	}
	return nil
}

func (store Store) FinalizeReadySessions(ctx context.Context, limit int) error {
	if limit < 1 || limit > 1000 {
		return errors.New("replay finalization limit must be between 1 and 1000")
	}
	if _, err := store.Database.Exec(ctx, `
		WITH candidates AS (
			SELECT session.id
			FROM sessions AS session
			WHERE session.finalize_requested_at IS NOT NULL
			  AND session.ended_at IS NULL
			  AND EXISTS (
			      SELECT 1 FROM session_batches AS batch
			      WHERE batch.session_id = session.id
			  )
			  AND NOT EXISTS (
			      SELECT 1 FROM session_batches AS batch
			      WHERE batch.session_id = session.id
			        AND batch.safety_status <> 'safe'
			  )
			ORDER BY session.finalize_requested_at, session.id
			LIMIT $1
			FOR UPDATE OF session SKIP LOCKED
		)
		UPDATE sessions AS session
		SET ended_at = LEAST(
		        GREATEST(session.started_at, session.finalize_requested_at),
		        session.started_at + session.max_duration_seconds * interval '1 second'
		    ),
		    merge_requested_at = session.finalize_requested_at,
		    merge_enqueued_at = NULL,
		    merge_error = NULL,
		    failure_code = NULL
		FROM candidates
		WHERE session.id = candidates.id
	`, limit); err != nil {
		return fmt.Errorf("finalize ready replay sessions: %w", err)
	}
	return nil
}

func (store Store) MarkRejected(
	ctx context.Context,
	work BatchVerificationWork,
	failureCode FailureCode,
	reason string,
	checkedAt time.Time,
) error {
	var updated int
	err := store.Database.QueryRow(ctx, `
		WITH updated_batch AS (
			UPDATE session_batches
			SET safety_status = 'rejected',
			    safety_checked_at = $4,
			    safety_error_reason = $5
			WHERE tenant_id = $1 AND id = $2
			  AND verification_generation = $3
			  AND safety_status = 'quarantined'
			RETURNING session_id
		),
		updated_session AS (
			UPDATE sessions AS session
			SET ended_at = CASE
			        WHEN session.finalize_requested_at IS NOT NULL AND session.ended_at IS NULL
			            THEN LEAST(
			                GREATEST(session.started_at, session.finalize_requested_at),
			                session.started_at + session.max_duration_seconds * interval '1 second'
			            )
			        ELSE session.ended_at
			    END,
			    merge_requested_at = CASE
			        WHEN session.finalize_requested_at IS NOT NULL AND session.ended_at IS NULL
			            THEN NULL
			        ELSE session.merge_requested_at
			    END,
			    merge_enqueued_at = CASE
			        WHEN session.finalize_requested_at IS NOT NULL AND session.ended_at IS NULL
			            THEN NULL
			        ELSE session.merge_enqueued_at
			    END,
			    merge_error = CASE
			        WHEN session.finalize_requested_at IS NOT NULL AND session.ended_at IS NULL
			            THEN $7
			        ELSE session.merge_error
			    END,
			    merge_attempts = CASE
			        WHEN session.finalize_requested_at IS NOT NULL AND session.ended_at IS NULL
			            THEN $8
			        ELSE session.merge_attempts
			    END,
			    failure_code = CASE
			        WHEN session.failure_code = 'contract_violation' THEN session.failure_code
			        WHEN $6::text = 'contract_violation' THEN $6::text
			        WHEN session.failure_code = 'unsafe_payload' THEN session.failure_code
			        WHEN $6::text = 'unsafe_payload' THEN $6::text
			        ELSE COALESCE(session.failure_code, $6::text)
			    END
			FROM updated_batch AS batch
			WHERE session.id = batch.session_id
		)
		SELECT count(*) FROM updated_batch
	`, work.TenantID, work.ID, work.Generation, checkedAt.UTC(), reason,
		string(failureCode), unavailableReplayMessage, terminalMergeAttempts).Scan(&updated)
	if err != nil {
		return fmt.Errorf("reject replay batch verification: %w", err)
	}
	if updated != 1 {
		return ErrNoBatchVerificationWork
	}
	return nil
}

func (store Store) CompleteRejectedCleanup(
	ctx context.Context,
	work BatchVerificationWork,
	cleanedAt time.Time,
) error {
	tag, err := store.Database.Exec(ctx, `
		UPDATE session_batches
		SET verification_enqueued_at = NULL
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status = 'rejected'
		  AND verification_enqueued_at IS NOT NULL
		  AND upload_expires_at + interval '1 minute' <= $4
	`, work.TenantID, work.ID, work.Generation, cleanedAt.UTC())
	if err != nil {
		return fmt.Errorf("complete rejected replay batch cleanup: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNoBatchVerificationWork
	}
	return nil
}

func (store Store) ClaimExpiredBatches(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]ExpiredBatchUpload, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("replay batch expiry limit must be between 1 and 1000")
	}
	rows, err := store.Database.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM session_batches
			WHERE (
			        safety_status = 'pending'
			        AND upload_expires_at + interval '1 minute' <= $1
			      )
			   OR (
			        safety_status = 'cleaning'
			        AND upload_expires_at + interval '1 minute' <= $1
			        AND (
			            verification_enqueued_at IS NULL
			            OR verification_enqueued_at < $1::timestamptz - interval '1 minute'
			        )
			      )
			ORDER BY upload_expires_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE session_batches AS batch
		SET complete_token_hash = NULL,
		    safety_status = 'cleaning',
		    verification_enqueued_at = $1
		FROM candidates
		WHERE batch.id = candidates.id
		RETURNING batch.tenant_id, batch.id, batch.session_id, batch.storage_key
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("claim expired replay batch uploads: %w", err)
	}
	defer rows.Close()
	uploads := make([]ExpiredBatchUpload, 0)
	for rows.Next() {
		var upload ExpiredBatchUpload
		if err := rows.Scan(
			&upload.TenantID, &upload.BatchID, &upload.SessionID, &upload.StorageKey,
		); err != nil {
			return nil, fmt.Errorf("scan expired replay batch upload: %w", err)
		}
		uploads = append(uploads, upload)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired replay batch uploads: %w", err)
	}
	return uploads, nil
}

func (store Store) CompleteBatchExpiry(
	ctx context.Context,
	upload ExpiredBatchUpload,
	expiredAt time.Time,
) error {
	var updated int
	err := store.Database.QueryRow(ctx, `
		WITH updated_batch AS (
			UPDATE session_batches
			SET safety_status = 'expired',
			    safety_checked_at = $3,
			    safety_error_reason = 'upload_expired',
			    verification_enqueued_at = NULL
			WHERE tenant_id = $1 AND id = $2
			  AND safety_status = 'cleaning'
			RETURNING session_id
		),
		updated_session AS (
			UPDATE sessions AS session
			SET failure_code = CASE
			        WHEN session.failure_code IN ('contract_violation', 'unsafe_payload')
			            THEN session.failure_code
			        ELSE 'upload_expired'
			    END
			FROM updated_batch AS batch
			WHERE session.id = batch.session_id
		)
		SELECT count(*) FROM updated_batch
	`, upload.TenantID, upload.BatchID, expiredAt.UTC()).Scan(&updated)
	if err != nil {
		return fmt.Errorf("complete expired replay batch cleanup: %w", err)
	}
	if updated != 1 {
		return ErrNoBatchVerificationWork
	}
	return nil
}

func (store Store) ExpireBatch(
	ctx context.Context,
	query db.Querier,
	batchID string,
	expiredAt time.Time,
) error {
	tag, err := query.Exec(ctx, `
		UPDATE session_batches
		SET complete_token_hash = NULL,
		    safety_status = 'cleaning',
		    safety_checked_at = NULL,
		    safety_error_reason = NULL,
		    verification_enqueued_at = NULL
		WHERE id = $1 AND safety_status = 'pending'
		  AND upload_expires_at <= $2
	`, batchID, expiredAt.UTC())
	if err != nil {
		return fmt.Errorf("claim replay batch upload expiry: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}
