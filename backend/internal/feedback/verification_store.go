package feedback

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

var ErrNoVerificationWork = errors.New("no attachment verification work")

type VerificationStore struct {
	Database db.Querier
}

type VerificationWork struct {
	VerificationTask
	FeedbackID   string
	ProjectID    string
	Kind         AttachmentKind
	StorageKey   string
	ContentType  string
	SizeBytes    int64
	Attempt      int
	SafetyStatus AttachmentSafetyStatus
}

func (store VerificationStore) MarkEnqueued(
	ctx context.Context,
	task VerificationTask,
	enqueuedAt time.Time,
) error {
	tag, err := store.Database.Exec(ctx, `
		UPDATE attachments
		SET verification_enqueued_at = $4
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status = 'quarantined'
	`, task.TenantID, task.AttachmentID, task.Generation, enqueuedAt.UTC())
	if err != nil {
		return fmt.Errorf("mark attachment verification enqueued: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNoVerificationWork
	}
	return nil
}

func (store VerificationStore) UndoEnqueued(
	ctx context.Context,
	task VerificationTask,
) error {
	_, err := store.Database.Exec(ctx, `
		UPDATE attachments
		SET verification_enqueued_at = NULL
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status = 'quarantined'
	`, task.TenantID, task.AttachmentID, task.Generation)
	if err != nil {
		return fmt.Errorf("undo attachment verification enqueue: %w", err)
	}
	return nil
}

func (store VerificationStore) Prepare(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]VerificationTask, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("attachment verification prepare limit must be between 1 and 1000")
	}
	rows, err := store.Database.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM attachments
			WHERE safety_status = 'quarantined'
			  AND (
			      verification_enqueued_at IS NULL
			      OR verification_enqueued_at < $1 - interval '15 minutes'
			  )
			ORDER BY COALESCE(verification_enqueued_at, completed_at), id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE attachments AS attachment
		SET verification_generation = CASE
		        WHEN verification_enqueued_at IS NULL THEN verification_generation
		        ELSE verification_generation + 1
		    END,
		    verification_enqueued_at = $1
		FROM candidates
		WHERE attachment.id = candidates.id
		RETURNING attachment.tenant_id, attachment.id, attachment.verification_generation
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("prepare attachment verifications: %w", err)
	}
	defer rows.Close()
	tasks := make([]VerificationTask, 0)
	for rows.Next() {
		var task VerificationTask
		if err := rows.Scan(&task.TenantID, &task.AttachmentID, &task.Generation); err != nil {
			return nil, fmt.Errorf("scan attachment verification task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment verification tasks: %w", err)
	}
	return tasks, nil
}

func (store VerificationStore) Claim(
	ctx context.Context,
	task VerificationTask,
) (VerificationWork, error) {
	var work VerificationWork
	work.VerificationTask = task
	err := store.Database.QueryRow(ctx, `
		UPDATE attachments
		SET verification_attempts = verification_attempts + 1
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status IN ('quarantined', 'rejected')
		  AND completed_at IS NOT NULL
		  AND retention_deleted_at IS NULL
		  AND storage_key IS NOT NULL
		RETURNING feedback_id,
		          (SELECT project_id FROM feedbacks WHERE id = attachments.feedback_id),
		          kind, storage_key, content_type, size_bytes, verification_attempts,
		          safety_status
	`, task.TenantID, task.AttachmentID, task.Generation).Scan(
		&work.FeedbackID, &work.ProjectID, &work.Kind, &work.StorageKey, &work.ContentType,
		&work.SizeBytes, &work.Attempt, &work.SafetyStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return VerificationWork{}, ErrNoVerificationWork
	}
	if err != nil {
		return VerificationWork{}, fmt.Errorf("claim attachment verification: %w", err)
	}
	return work, nil
}

func (store VerificationStore) MarkSafe(
	ctx context.Context,
	work VerificationWork,
	detectedContentType string,
	checkedAt time.Time,
) error {
	var updated int
	err := store.Database.QueryRow(ctx, `
		WITH updated_attachment AS (
			UPDATE attachments
			SET safety_status = 'safe',
			    safety_checked_at = $4,
			    safety_error_reason = NULL,
			    detected_content_type = $5,
			    verification_enqueued_at = NULL
			WHERE tenant_id = $1 AND id = $2
			  AND verification_generation = $3
			  AND safety_status = 'quarantined'
			RETURNING feedback_id
		),
		completed_feedback AS (
			UPDATE feedbacks AS feedback
			SET ai_processed_at = COALESCE(feedback.ai_processed_at, $4)
			WHERE feedback.id IN (SELECT feedback_id FROM updated_attachment)
			  AND feedback.text_process_status IN ('completed', 'failed', 'skipped')
			  AND NOT EXISTS (
				SELECT 1 FROM attachments AS attachment
				WHERE attachment.feedback_id = feedback.id
				  AND attachment.processing_status NOT IN ('completed', 'failed', 'skipped')
			  )
			RETURNING feedback.id
		)
		SELECT count(*) FROM updated_attachment
	`, work.TenantID, work.AttachmentID, work.Generation, checkedAt.UTC(), detectedContentType).
		Scan(&updated)
	if err != nil {
		return fmt.Errorf("mark attachment safe: %w", err)
	}
	if updated != 1 {
		return ErrNoVerificationWork
	}
	return nil
}

func (store VerificationStore) MarkRejected(
	ctx context.Context,
	work VerificationWork,
	reason string,
	detectedContentType *string,
	checkedAt time.Time,
) error {
	var updated int
	err := store.Database.QueryRow(ctx, `
		WITH updated_attachment AS (
			UPDATE attachments
			SET safety_status = 'rejected',
			    safety_checked_at = $4,
			    safety_error_reason = $5,
			    detected_content_type = $6,
			    processing_status = 'failed',
			    error_message = 'attachment safety verification failed',
			    processed_at = $4
			WHERE tenant_id = $1 AND id = $2
			  AND verification_generation = $3
			  AND safety_status = 'quarantined'
			RETURNING id, feedback_id
		),
		completed_feedback AS (
			UPDATE feedbacks AS feedback
			SET ai_processed_at = COALESCE(feedback.ai_processed_at, $4)
			WHERE feedback.id IN (SELECT feedback_id FROM updated_attachment)
			  AND feedback.text_process_status IN ('completed', 'failed', 'skipped')
			  AND NOT EXISTS (
				SELECT 1 FROM attachments AS attachment
				WHERE attachment.feedback_id = feedback.id
				  AND attachment.processing_status NOT IN ('completed', 'failed', 'skipped')
				  AND attachment.id NOT IN (SELECT id FROM updated_attachment)
			  )
			RETURNING feedback.id
		)
		SELECT count(*) FROM updated_attachment
	`, work.TenantID, work.AttachmentID, work.Generation, checkedAt.UTC(), reason,
		detectedContentType).Scan(&updated)
	if err != nil {
		return fmt.Errorf("reject attachment verification: %w", err)
	}
	if updated != 1 {
		return ErrNoVerificationWork
	}
	return nil
}

func (store VerificationStore) CompleteRejectedCleanup(
	ctx context.Context,
	work VerificationWork,
) error {
	tag, err := store.Database.Exec(ctx, `
		UPDATE attachments
		SET verification_enqueued_at = NULL
		WHERE tenant_id = $1 AND id = $2
		  AND verification_generation = $3
		  AND safety_status = 'rejected'
	`, work.TenantID, work.AttachmentID, work.Generation)
	if err != nil {
		return fmt.Errorf("complete rejected attachment cleanup: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNoVerificationWork
	}
	return nil
}

func (store VerificationStore) ExpirePending(
	ctx context.Context,
	now time.Time,
	limit int,
) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, errors.New("attachment expiry limit must be between 1 and 1000")
	}
	var expired int64
	err := store.Database.QueryRow(ctx, `
		WITH expired AS (
			SELECT id
			FROM attachments
			WHERE safety_status = 'pending'
			  AND upload_expires_at <= $1
			ORDER BY upload_expires_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		),
		updated_attachments AS (
			UPDATE attachments AS attachment
			SET complete_token_hash = NULL,
			    safety_status = 'expired',
			    safety_checked_at = $1,
			    safety_error_reason = 'upload_expired',
			    processing_status = 'failed',
			    error_message = 'attachment upload expired before completion',
			    processed_at = $1
			FROM expired
			WHERE attachment.id = expired.id
			RETURNING attachment.id, attachment.feedback_id
		),
		completed_feedbacks AS (
			UPDATE feedbacks AS feedback
			SET ai_processed_at = COALESCE(feedback.ai_processed_at, $1)
			WHERE feedback.id IN (SELECT feedback_id FROM updated_attachments)
			  AND feedback.text_process_status IN ('completed', 'failed', 'skipped')
			  AND NOT EXISTS (
				SELECT 1 FROM attachments AS attachment
				WHERE attachment.feedback_id = feedback.id
				  AND attachment.processing_status NOT IN ('completed', 'failed', 'skipped')
				  AND attachment.id NOT IN (SELECT id FROM updated_attachments)
			  )
			RETURNING feedback.id
		)
		SELECT count(*) FROM updated_attachments
	`, now.UTC(), limit).Scan(&expired)
	if err != nil {
		return 0, fmt.Errorf("expire pending attachment uploads: %w", err)
	}
	return expired, nil
}
