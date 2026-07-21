package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Database *pgxpool.Pool
}

func (s Store) PrepareFeedback(
	ctx context.Context,
	feedbackID string,
	dailyLimit int,
	now time.Time,
) ([]PreparedTask, bool, error) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin AI scheduling: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tenantID, planValue, textStatus string
	var textGeneration int64
	var text *string
	var textEnqueuedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT feedback.tenant_id, tenant.plan, feedback.text,
		       feedback.text_process_status, feedback.ai_job_enqueued_at,
		       feedback.text_processing_generation
		FROM feedbacks AS feedback
		JOIN tenants AS tenant ON tenant.id = feedback.tenant_id
		WHERE feedback.id = $1
		FOR UPDATE OF feedback
	`, feedbackID).Scan(
		&tenantID, &planValue, &text, &textStatus, &textEnqueuedAt, &textGeneration,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("lock feedback for AI scheduling: %w", err)
	}

	definition, planExists := config.PlanFor(tenant.Plan(planValue))
	if !planExists || !definition.Features.AI {
		if _, err := tx.Exec(ctx, `
			UPDATE feedbacks
			SET text_process_status = 'skipped', ai_job_enqueued_at = NULL
			WHERE id = $1 AND text_process_status IN ('pending', 'processing')
		`, feedbackID); err != nil {
			return nil, false, fmt.Errorf("skip feedback AI: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE attachments
			SET processing_status = 'skipped', error_message = NULL
			WHERE feedback_id = $1 AND processing_status IN ('pending', 'processing')
		`, feedbackID); err != nil {
			return nil, false, fmt.Errorf("skip attachment AI: %w", err)
		}
		if err := updateCompletion(ctx, tx, feedbackID, now); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, false, fmt.Errorf("commit skipped AI work: %w", err)
		}
		return nil, false, nil
	}

	usageDate := midnightUTC(now)
	prepared := make([]PreparedTask, 0)
	budgetExhausted := false
	if text == nil || strings.TrimSpace(*text) == "" {
		if textStatus == "pending" || textStatus == "processing" {
			if _, err := tx.Exec(ctx, `
				UPDATE feedbacks SET text_process_status = 'skipped', ai_job_enqueued_at = NULL
				WHERE id = $1
			`, feedbackID); err != nil {
				return nil, false, fmt.Errorf("skip empty feedback text: %w", err)
			}
		}
	} else if textStatus == "pending" && textEnqueuedAt == nil {
		reserved, err := reserveBudget(ctx, tx, tenantID, usageDate, dailyLimit)
		if err != nil {
			return nil, false, err
		}
		if reserved {
			if err := tx.QueryRow(ctx, `
					UPDATE feedbacks
					SET text_process_status = 'processing', ai_job_enqueued_at = $2,
					    text_processing_generation = text_processing_generation + 1,
					    ai_processed_at = NULL
					WHERE id = $1 AND text_process_status = 'pending' AND ai_job_enqueued_at IS NULL
					RETURNING text_processing_generation
				`, feedbackID, now).Scan(&textGeneration); err != nil {
				return nil, false, fmt.Errorf("prepare feedback text AI: %w", err)
			}
			prepared = append(prepared, PreparedTask{
				Kind: TaskKindText, TenantID: tenantID, FeedbackID: feedbackID,
				ResourceID: feedbackID, Generation: textGeneration,
				EnqueuedAt: now, UsageDate: usageDate,
			})
		} else {
			budgetExhausted = true
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE attachments
		SET processing_status = 'skipped', error_message = NULL
		WHERE feedback_id = $1 AND kind = 'replay' AND processing_status IN ('pending', 'processing')
	`, feedbackID); err != nil {
		return nil, false, fmt.Errorf("skip replay AI: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id, processing_attempts
		FROM attachments
		WHERE feedback_id = $1
		  AND completed_at IS NOT NULL
		  AND kind <> 'replay'
		  AND processing_status = 'pending'
		  AND error_message IS NULL
		ORDER BY created_at, id
		FOR UPDATE
	`, feedbackID)
	if err != nil {
		return nil, false, fmt.Errorf("lock pending attachment AI: %w", err)
	}
	type pendingAttachment struct {
		id       string
		attempts int
	}
	attachments := make([]pendingAttachment, 0)
	for rows.Next() {
		var attachment pendingAttachment
		if err := rows.Scan(&attachment.id, &attachment.attempts); err != nil {
			rows.Close()
			return nil, false, fmt.Errorf("scan pending attachment AI: %w", err)
		}
		attachments = append(attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, false, fmt.Errorf("iterate pending attachment AI: %w", err)
	}
	rows.Close()

	for _, attachment := range attachments {
		reserved, err := reserveBudget(ctx, tx, tenantID, usageDate, dailyLimit)
		if err != nil {
			return nil, false, err
		}
		if !reserved {
			budgetExhausted = true
			break
		}
		attempt := attachment.attempts + 1
		if _, err := tx.Exec(ctx, `
			UPDATE attachments
			SET processing_status = 'processing', processing_attempts = $2, error_message = NULL
			WHERE id = $1 AND processing_status = 'pending' AND error_message IS NULL
		`, attachment.id, attempt); err != nil {
			return nil, false, fmt.Errorf("prepare attachment AI: %w", err)
		}
		prepared = append(prepared, PreparedTask{
			Kind: TaskKindAttachment, TenantID: tenantID, FeedbackID: feedbackID,
			ResourceID: attachment.id, Attempt: attempt, EnqueuedAt: now, UsageDate: usageDate,
		})
	}

	if err := updateCompletion(ctx, tx, feedbackID, now); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit prepared AI work: %w", err)
	}
	return prepared, budgetExhausted, nil
}

func (s Store) UndoPrepared(ctx context.Context, task PreparedTask) error {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin AI enqueue rollback: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var affected int64
	refundBudget := task.Kind != TaskKindFileDelete
	if task.Kind == TaskKindText {
		tag, err := tx.Exec(ctx, `
				UPDATE feedbacks
				SET text_process_status = 'pending', ai_job_enqueued_at = NULL
				WHERE id = $1 AND tenant_id = $2
				  AND text_process_status = 'processing' AND ai_job_enqueued_at = $3
				  AND text_processing_generation = $4
			`, task.ResourceID, task.TenantID, task.EnqueuedAt, task.Generation)
		if err != nil {
			return fmt.Errorf("undo text AI enqueue: %w", err)
		}
		affected = tag.RowsAffected()
	} else if task.Kind == TaskKindAttachment {
		tag, err := tx.Exec(ctx, `
			UPDATE attachments
			SET processing_status = 'pending', processing_attempts = processing_attempts - 1
			WHERE id = $1 AND tenant_id = $2
			  AND processing_status = 'processing' AND processing_attempts = $3
		`, task.ResourceID, task.TenantID, task.Attempt)
		if err != nil {
			return fmt.Errorf("undo attachment AI enqueue: %w", err)
		}
		affected = tag.RowsAffected()
	} else {
		tag, err := tx.Exec(ctx, `
			UPDATE ai_gemini_files
			SET delete_enqueued_at = NULL, delete_attempts = delete_attempts - 1
			WHERE id = $1 AND tenant_id = $2 AND delete_attempts = $3
			  AND deleted_at IS NULL AND failed_at IS NULL
		`, task.ResourceID, task.TenantID, task.Attempt)
		if err != nil {
			return fmt.Errorf("undo Gemini file deletion enqueue: %w", err)
		}
		affected = tag.RowsAffected()
	}
	if affected > 0 && refundBudget {
		if _, err := tx.Exec(ctx, `
			UPDATE ai_daily_usage SET requests = GREATEST(requests - 1, 0)
			WHERE tenant_id = $1 AND usage_date = $2
		`, task.TenantID, task.UsageDate); err != nil {
			return fmt.Errorf("refund AI daily budget: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit AI enqueue rollback: %w", err)
	}
	return nil
}

func (s Store) PendingFeedbackIDs(
	ctx context.Context,
	limit int,
	dailyLimit int,
	now time.Time,
) ([]string, error) {
	rows, err := s.Database.Query(ctx, `
		WITH eligible AS (
			SELECT feedback.id, feedback.tenant_id,
			       row_number() OVER (
				   PARTITION BY feedback.tenant_id ORDER BY feedback.created_at, feedback.id
			       ) AS tenant_position
			FROM feedbacks AS feedback
			JOIN tenants AS tenant ON tenant.id = feedback.tenant_id
			LEFT JOIN ai_daily_usage AS usage
			  ON usage.tenant_id = feedback.tenant_id AND usage.usage_date = $2
			WHERE tenant.plan IN ('growth', 'scale', 'enterprise')
			  AND COALESCE(usage.requests, 0) < $3
			  AND (
				(feedback.text_process_status = 'pending' AND feedback.ai_job_enqueued_at IS NULL)
				OR EXISTS (
					SELECT 1 FROM attachments AS attachment
					WHERE attachment.feedback_id = feedback.id
					  AND attachment.completed_at IS NOT NULL
					  AND attachment.kind <> 'replay'
					  AND attachment.processing_status = 'pending'
					  AND attachment.error_message IS NULL
				)
			  )
		)
		SELECT id FROM eligible
		ORDER BY tenant_position, tenant_id, id
		LIMIT $1
	`, limit, midnightUTC(now), dailyLimit)
	if err != nil {
		return nil, fmt.Errorf("list pending AI feedbacks: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pending AI feedback: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending AI feedback: %w", err)
	}
	return ids, nil
}

func (s Store) InFlightTasks(ctx context.Context, limit int) ([]PreparedTask, error) {
	rows, err := s.Database.Query(ctx, `
		SELECT kind, tenant_id, resource_id, attempt, generation, enqueued_at
		FROM (
			SELECT 'text'::text AS kind, tenant_id, id AS resource_id, 0 AS attempt,
			       text_processing_generation AS generation, ai_job_enqueued_at AS enqueued_at
			FROM feedbacks
			WHERE text_process_status = 'processing' AND ai_job_enqueued_at IS NOT NULL
			UNION ALL
			SELECT 'attachment', tenant_id, id, processing_attempts, 0, created_at
			FROM attachments
			WHERE processing_status = 'processing'
			UNION ALL
			SELECT 'file_delete', tenant_id, id, delete_attempts, 0, delete_enqueued_at
			FROM ai_gemini_files
			WHERE deleted_at IS NULL AND failed_at IS NULL AND delete_enqueued_at IS NOT NULL
		) AS in_flight
		ORDER BY enqueued_at, resource_id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list in-flight AI tasks: %w", err)
	}
	defer rows.Close()
	tasks := make([]PreparedTask, 0)
	for rows.Next() {
		var kind string
		var task PreparedTask
		if err := rows.Scan(
			&kind, &task.TenantID, &task.ResourceID, &task.Attempt,
			&task.Generation, &task.EnqueuedAt,
		); err != nil {
			return nil, fmt.Errorf("scan in-flight AI task: %w", err)
		}
		task.Kind = TaskKind(kind)
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate in-flight AI tasks: %w", err)
	}
	return tasks, nil
}

func (s Store) TrackGeminiFile(
	ctx context.Context,
	work AttachmentWork,
	fileName string,
	deleteAfter time.Time,
) (string, error) {
	var id string
	err := s.Database.QueryRow(ctx, `
		INSERT INTO ai_gemini_files (tenant_id, attachment_id, file_name, delete_after)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, work.TenantID, work.AttachmentID, fileName, deleteAfter).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("track Gemini file: %w", err)
	}
	return id, nil
}

func (s Store) MarkGeminiFileReady(
	ctx context.Context,
	id string,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE ai_gemini_files SET delete_after = $2
		WHERE id = $1 AND deleted_at IS NULL AND failed_at IS NULL
	`, id, now); err != nil {
		return fmt.Errorf("mark Gemini file ready for deletion: %w", err)
	}
	return nil
}

func (s Store) RecordGeminiFileDeleteFailure(
	ctx context.Context,
	id string,
	failure error,
	permanentFailure bool,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE ai_gemini_files
		SET last_error = $2, failed_at = CASE WHEN $3 THEN $4::timestamptz ELSE failed_at END
		WHERE id = $1 AND deleted_at IS NULL AND failed_at IS NULL
	`, id, truncateError(failure), permanentFailure, now); err != nil {
		return fmt.Errorf("record Gemini file deletion failure: %w", err)
	}
	return nil
}

func (s Store) CompleteTrackedGeminiFileDeletion(
	ctx context.Context,
	id string,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE ai_gemini_files
		SET deleted_at = $2, delete_enqueued_at = NULL, last_error = NULL
		WHERE id = $1 AND deleted_at IS NULL AND failed_at IS NULL
	`, id, now); err != nil {
		return fmt.Errorf("complete tracked Gemini file deletion: %w", err)
	}
	return nil
}

func (s Store) PrepareGeminiFileDeletions(
	ctx context.Context,
	limit int,
	now time.Time,
) ([]PreparedTask, error) {
	rows, err := s.Database.Query(ctx, `
		WITH due AS (
			SELECT id
			FROM ai_gemini_files
			WHERE deleted_at IS NULL AND failed_at IS NULL
			  AND delete_enqueued_at IS NULL AND delete_after <= $2
			ORDER BY delete_after, id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE ai_gemini_files AS file
		SET delete_attempts = file.delete_attempts + 1, delete_enqueued_at = $2
		FROM due
		WHERE file.id = due.id
		RETURNING file.id, file.tenant_id, file.delete_attempts
	`, limit, now)
	if err != nil {
		return nil, fmt.Errorf("prepare Gemini file deletions: %w", err)
	}
	defer rows.Close()
	tasks := make([]PreparedTask, 0)
	for rows.Next() {
		var task PreparedTask
		task.Kind, task.EnqueuedAt = TaskKindFileDelete, now
		if err := rows.Scan(&task.ResourceID, &task.TenantID, &task.Attempt); err != nil {
			return nil, fmt.Errorf("scan Gemini file deletion: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Gemini file deletions: %w", err)
	}
	return tasks, nil
}

func (s Store) ClaimGeminiFile(
	ctx context.Context,
	tenantID, id string,
	attempt int,
) (GeminiFileWork, error) {
	work := GeminiFileWork{ID: id, TenantID: tenantID, Attempt: attempt}
	err := s.Database.QueryRow(ctx, `
		SELECT file_name
		FROM ai_gemini_files
		WHERE id = $1 AND tenant_id = $2 AND delete_attempts = $3
		  AND delete_enqueued_at IS NOT NULL AND deleted_at IS NULL AND failed_at IS NULL
	`, id, tenantID, attempt).Scan(&work.FileName)
	if errors.Is(err, pgx.ErrNoRows) {
		return GeminiFileWork{}, ErrNoWork
	}
	if err != nil {
		return GeminiFileWork{}, fmt.Errorf("claim Gemini file deletion: %w", err)
	}
	return work, nil
}

func (s Store) CompleteGeminiFileDeletion(
	ctx context.Context,
	work GeminiFileWork,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE ai_gemini_files
		SET deleted_at = $4, delete_enqueued_at = NULL, last_error = NULL
		WHERE id = $1 AND tenant_id = $2 AND delete_attempts = $3
		  AND deleted_at IS NULL AND failed_at IS NULL
	`, work.ID, work.TenantID, work.Attempt, now); err != nil {
		return fmt.Errorf("complete Gemini file deletion: %w", err)
	}
	return nil
}

func (s Store) FailGeminiFileDeletion(
	ctx context.Context,
	work GeminiFileWork,
	failure error,
	requeue bool,
	permanentFailure bool,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE ai_gemini_files
		SET last_error = $4,
		    delete_enqueued_at = CASE WHEN $5 OR $6 THEN NULL ELSE delete_enqueued_at END,
		    failed_at = CASE WHEN $6 THEN $7::timestamptz ELSE failed_at END
		WHERE id = $1 AND tenant_id = $2 AND delete_attempts = $3
		  AND deleted_at IS NULL AND failed_at IS NULL
	`, work.ID, work.TenantID, work.Attempt, truncateError(failure), requeue,
		permanentFailure, now); err != nil {
		return fmt.Errorf("record Gemini file deletion failure: %w", err)
	}
	return nil
}

func (s Store) FinalizeExhaustedTask(
	ctx context.Context,
	kind TaskKind,
	payload taskPayload,
	failure error,
	now time.Time,
) error {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin exhausted AI task finalization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var feedbackID string
	if kind == TaskKindText {
		err = tx.QueryRow(ctx, `
			UPDATE feedbacks
			SET text_process_status = 'failed', ai_job_enqueued_at = NULL
			WHERE id = $1 AND tenant_id = $2 AND text_processing_generation = $3
			  AND text_process_status IN ('pending', 'processing')
			RETURNING id
		`, payload.ResourceID, payload.TenantID, payload.Generation).Scan(&feedbackID)
	} else {
		err = tx.QueryRow(ctx, `
			UPDATE attachments
			SET processing_status = 'failed', error_message = $4, processed_at = $5
			WHERE id = $1 AND tenant_id = $2 AND processing_attempts = $3
			  AND processing_status IN ('pending', 'processing')
			RETURNING feedback_id
		`, payload.ResourceID, payload.TenantID, payload.Attempt,
			truncateError(failure), now).Scan(&feedbackID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("finalize exhausted AI task: %w", err)
	}
	if err := updateCompletion(ctx, tx, feedbackID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit exhausted AI task finalization: %w", err)
	}
	return nil
}

func (s Store) ClaimText(
	ctx context.Context,
	tenantID, feedbackID string,
	generation int64,
) (TextWork, error) {
	var work TextWork
	work.TenantID, work.FeedbackID, work.Generation = tenantID, feedbackID, generation
	err := s.Database.QueryRow(ctx, `
		UPDATE feedbacks
		SET text_process_status = 'processing'
		WHERE id = $1 AND tenant_id = $2
		  AND text IS NOT NULL AND btrim(text) <> ''
		  AND text_process_status IN ('pending', 'processing')
		  AND text_processing_generation = $3
		RETURNING text
	`, feedbackID, tenantID, generation).Scan(&work.Text)
	if errors.Is(err, pgx.ErrNoRows) {
		return TextWork{}, ErrNoWork
	}
	if err != nil {
		return TextWork{}, fmt.Errorf("claim text AI: %w", err)
	}
	return work, nil
}

func (s Store) ClaimAttachment(
	ctx context.Context,
	tenantID, attachmentID string,
	attempt int,
) (AttachmentWork, error) {
	var work AttachmentWork
	work.TenantID, work.AttachmentID, work.Attempt = tenantID, attachmentID, attempt
	err := s.Database.QueryRow(ctx, `
		UPDATE attachments
		SET processing_status = 'processing'
		WHERE id = $1 AND tenant_id = $2 AND processing_attempts = $3
		  AND completed_at IS NOT NULL AND kind <> 'replay'
		  AND processing_status IN ('pending', 'processing')
		RETURNING feedback_id, kind, storage_key, content_type
	`, attachmentID, tenantID, attempt).Scan(
		&work.FeedbackID, &work.Kind, &work.StorageKey, &work.ContentType,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AttachmentWork{}, ErrNoWork
	}
	if err != nil {
		return AttachmentWork{}, fmt.Errorf("claim attachment AI: %w", err)
	}
	return work, nil
}

func (s Store) CompleteText(ctx context.Context, work TextWork, result Result, now time.Time) error {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin text AI completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE feedbacks
		SET text_cleaned = $3, text_sentiment_score = $4,
		    text_moderation_flags = $5, text_process_status = 'completed',
		    ai_job_enqueued_at = NULL
		WHERE id = $1 AND tenant_id = $2 AND text_process_status = 'processing'
		  AND text_processing_generation = $6
	`, work.FeedbackID, work.TenantID, nullableText(result.CleanedText),
		result.SentimentScore, moderationFlags(result.ModerationFlags), work.Generation)
	if err != nil {
		return fmt.Errorf("complete text AI: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoWork
	}
	if err := updateCompletion(ctx, tx, work.FeedbackID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit text AI completion: %w", err)
	}
	return nil
}

func (s Store) CompleteAttachment(ctx context.Context, work AttachmentWork, result Result, now time.Time) error {
	transcription := result.Transcription
	if transcription == "" {
		transcription = result.CleanedText
	}
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin attachment AI completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE attachments
		SET transcription = $4, sentiment_score = $5, moderation_flags = $6,
		    processing_status = 'completed', error_message = NULL, processed_at = $7
		WHERE id = $1 AND tenant_id = $2 AND processing_attempts = $3
		  AND processing_status = 'processing'
	`, work.AttachmentID, work.TenantID, work.Attempt, nullableText(transcription),
		result.SentimentScore, moderationFlags(result.ModerationFlags), now)
	if err != nil {
		return fmt.Errorf("complete attachment AI: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoWork
	}
	if err := updateCompletion(ctx, tx, work.FeedbackID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit attachment AI completion: %w", err)
	}
	return nil
}

func (s Store) FailText(ctx context.Context, work TextWork, _ error, permanentFailure bool, now time.Time) error {
	status := "pending"
	if permanentFailure {
		status = "failed"
	}
	query := db.Querier(s.Database)
	var tx pgx.Tx
	if permanentFailure {
		var err error
		tx, err = s.Database.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin terminal text AI failure: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		query = tx
	}
	if _, updateErr := query.Exec(ctx, `
		UPDATE feedbacks
		SET text_process_status = $3,
		    ai_job_enqueued_at = CASE WHEN $4 THEN NULL ELSE ai_job_enqueued_at END
		WHERE id = $1 AND tenant_id = $2 AND text_process_status = 'processing'
		  AND text_processing_generation = $5
	`, work.FeedbackID, work.TenantID, status, permanentFailure, work.Generation); updateErr != nil {
		return fmt.Errorf("record text AI failure: %w", updateErr)
	}
	if permanentFailure {
		if err := updateCompletion(ctx, query, work.FeedbackID, now); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit terminal text AI failure: %w", err)
		}
	}
	return nil
}

func (s Store) FailAttachment(
	ctx context.Context,
	work AttachmentWork,
	failure error,
	permanentFailure bool,
	now time.Time,
) error {
	status := "pending"
	if permanentFailure {
		status = "failed"
	}
	query := db.Querier(s.Database)
	var tx pgx.Tx
	if permanentFailure {
		var err error
		tx, err = s.Database.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin terminal attachment AI failure: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		query = tx
	}
	if _, err := query.Exec(ctx, `
		UPDATE attachments
		SET processing_status = $4::processing_status, error_message = $5,
		    processed_at = CASE WHEN $7 THEN $6::timestamptz ELSE NULL::timestamptz END
		WHERE id = $1 AND tenant_id = $2 AND processing_attempts = $3
		  AND processing_status = 'processing'
	`, work.AttachmentID, work.TenantID, work.Attempt, status, truncateError(failure), now,
		permanentFailure); err != nil {
		return fmt.Errorf("record attachment AI failure: %w", err)
	}
	if permanentFailure {
		if err := updateCompletion(ctx, query, work.FeedbackID, now); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit terminal attachment AI failure: %w", err)
		}
	}
	return nil
}

func (s Store) UpdateCompletion(ctx context.Context, feedbackID string, now time.Time) error {
	return updateCompletion(ctx, s.Database, feedbackID, now)
}

func (Store) RetryAttachment(
	ctx context.Context,
	query db.Querier,
	tenantID, attachmentID string,
) (string, error) {
	var currentStatus string
	if err := query.QueryRow(ctx, `
		SELECT processing_status FROM attachments
		WHERE id = $1 AND tenant_id = $2 AND completed_at IS NOT NULL AND kind <> 'replay'
	`, attachmentID, tenantID).Scan(&currentStatus); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	} else if err != nil {
		return "", fmt.Errorf("read attachment AI status: %w", err)
	}
	if currentStatus == "processing" {
		return "", ErrAlreadyRunning
	}
	var feedbackID string
	err := query.QueryRow(ctx, `
		UPDATE attachments
		SET processing_status = 'pending', error_message = NULL,
		    transcription = NULL, sentiment_score = NULL, moderation_flags = '{}', processed_at = NULL
		WHERE id = $1 AND tenant_id = $2 AND completed_at IS NOT NULL
		  AND kind <> 'replay' AND processing_status = $3
		RETURNING feedback_id
	`, attachmentID, tenantID, currentStatus).Scan(&feedbackID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("reset attachment AI: %w", err)
	}
	if _, err := query.Exec(ctx, `UPDATE feedbacks SET ai_processed_at = NULL WHERE id = $1`, feedbackID); err != nil {
		return "", fmt.Errorf("reset feedback AI completion: %w", err)
	}
	return feedbackID, nil
}

func (Store) ReprocessFeedback(
	ctx context.Context,
	query db.Querier,
	tenantID, feedbackID string,
) error {
	var found string
	err := query.QueryRow(ctx, `
		UPDATE feedbacks AS feedback
		SET text_process_status = CASE
		        WHEN feedback.text IS NULL OR btrim(feedback.text) = '' THEN 'skipped'::processing_status
		        ELSE 'pending'::processing_status
		    END,
		    text_cleaned = NULL, text_sentiment_score = NULL, text_moderation_flags = '{}',
		    text_processing_generation = text_processing_generation + 1,
		    ai_job_enqueued_at = NULL, ai_processed_at = NULL
		WHERE feedback.id = $1 AND feedback.tenant_id = $2
		  AND feedback.text_process_status IN ('completed', 'failed', 'skipped')
		  AND NOT EXISTS (
			SELECT 1 FROM attachments AS attachment
			WHERE attachment.feedback_id = feedback.id
			  AND attachment.processing_status = 'processing'
		  )
		RETURNING feedback.id
	`, feedbackID, tenantID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		var textStatus string
		var activeAttachments bool
		readErr := query.QueryRow(ctx, `
			SELECT feedback.text_process_status,
			       EXISTS (
				   SELECT 1 FROM attachments AS attachment
				   WHERE attachment.feedback_id = feedback.id
				     AND attachment.processing_status = 'processing'
			       )
			FROM feedbacks AS feedback
			WHERE feedback.id = $1 AND feedback.tenant_id = $2
		`, feedbackID, tenantID).Scan(&textStatus, &activeAttachments)
		if errors.Is(readErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if readErr != nil {
			return fmt.Errorf("read feedback AI status: %w", readErr)
		}
		if textStatus == "pending" && !activeAttachments {
			return nil
		}
		return ErrAlreadyRunning
	}
	if err != nil {
		return fmt.Errorf("reset feedback AI: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE attachments
		SET processing_status = CASE
		        WHEN kind = 'replay' THEN 'skipped'::processing_status
		        ELSE 'pending'::processing_status
		    END,
		    error_message = NULL, transcription = NULL, sentiment_score = NULL,
		    moderation_flags = '{}', processed_at = NULL
		WHERE feedback_id = $1 AND tenant_id = $2
		  AND completed_at IS NOT NULL AND processing_status <> 'processing'
	`, feedbackID, tenantID); err != nil {
		return fmt.Errorf("reset feedback attachments AI: %w", err)
	}
	return nil
}

func reserveBudget(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	usageDate time.Time,
	limit int,
) (bool, error) {
	var requests int
	err := query.QueryRow(ctx, `
		INSERT INTO ai_daily_usage (tenant_id, usage_date, requests)
		VALUES ($1, $2, 1)
		ON CONFLICT (tenant_id, usage_date) DO UPDATE
		SET requests = ai_daily_usage.requests + 1
		WHERE ai_daily_usage.requests < $3
		RETURNING requests
	`, tenantID, usageDate, limit).Scan(&requests)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserve tenant AI daily budget: %w", err)
	}
	return true, nil
}

func updateCompletion(ctx context.Context, query db.Querier, feedbackID string, now time.Time) error {
	if _, err := query.Exec(ctx, `
		UPDATE feedbacks AS feedback
		SET ai_processed_at = COALESCE(feedback.ai_processed_at, $2)
		WHERE feedback.id = $1
		  AND feedback.text_process_status IN ('completed', 'failed', 'skipped')
		  AND NOT EXISTS (
			SELECT 1 FROM attachments AS attachment
			WHERE attachment.feedback_id = feedback.id
			  AND attachment.processing_status NOT IN ('completed', 'failed', 'skipped')
		  )
	`, feedbackID, now); err != nil {
		return fmt.Errorf("update feedback AI completion: %w", err)
	}
	return nil
}

func midnightUTC(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func nullableText(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func moderationFlags(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
