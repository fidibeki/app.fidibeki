package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/abuse"
	"github.com/fidibeki/app.fidibeki/backend/internal/config"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Database          *pgxpool.Pool
	ProjectDailyLimit int
	Model             string
	Diagnostics       DiagnosticRecorder
	Logger            *slog.Logger
}

type DiagnosticRecorder interface {
	Record(context.Context, abuse.DiagnosticEvent) error
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

	var tenantID, projectID, planValue, textStatus string
	var textGeneration int64
	var text *string
	var textEnqueuedAt *time.Time
	var textDeferredUntil *time.Time
	var tenantEligible bool
	var aiEnabled, textAIEnabled, screenshotAIEnabled, audioAIEnabled, videoAIEnabled bool
	var processingRestricted bool
	err = tx.QueryRow(ctx, `
		SELECT feedback.tenant_id, feedback.project_id, tenant.plan,
		       fidibeki_ai_tenant_processing_eligible(
		           tenant.status,
		           tenant.trial_ends_at,
		           tenant.deletion_requested_at,
		           $2
		       ),
		       feedback.text,
		       feedback.text_process_status, feedback.ai_job_enqueued_at,
		       feedback.text_processing_generation, feedback.ai_budget_deferred_until,
		       COALESCE((rule.ai_processing->>'enabled')::boolean, false),
		       COALESCE(rule.ai_processing->'input_categories' ? 'feedback_text', false),
		       COALESCE(rule.ai_processing->'input_categories' ? 'screenshot', false),
		       COALESCE(rule.ai_processing->'input_categories' ? 'audio', false),
		       COALESCE(rule.ai_processing->'input_categories' ? 'video', false),
		       fidibeki_privacy_feedback_restricted(feedback.tenant_id, feedback.id, 'ai')
		FROM feedbacks AS feedback
		JOIN tenants AS tenant ON tenant.id = feedback.tenant_id
		LEFT JOIN widget_rules AS rule
		  ON rule.id = feedback.widget_rule_id AND rule.project_id = feedback.project_id
		WHERE feedback.id = $1
		FOR UPDATE OF feedback
		FOR SHARE OF tenant
	`, feedbackID, now).Scan(
		&tenantID, &projectID, &planValue, &tenantEligible, &text, &textStatus, &textEnqueuedAt,
		&textGeneration, &textDeferredUntil, &aiEnabled, &textAIEnabled,
		&screenshotAIEnabled, &audioAIEnabled, &videoAIEnabled, &processingRestricted,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("lock feedback for AI scheduling: %w", err)
	}

	definition, planExists := config.PlanFor(tenant.Plan(planValue))
	if !tenantEligible {
		if err := cancelFeedbackForTenantLifecycle(
			ctx, tx, tenantID, feedbackID, now,
		); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, false, fmt.Errorf("commit inactive tenant AI cancellation: %w", err)
		}
		return nil, false, nil
	}
	if !planExists || !definition.Features.AI || !aiEnabled || processingRestricted {
		if _, err := tx.Exec(ctx, `
				UPDATE feedbacks
				SET text_process_status = 'skipped', ai_job_enqueued_at = NULL,
				    ai_budget_deferred_until = NULL, ai_budget_deferred_reason = NULL
				WHERE id = $1 AND text_process_status IN ('pending', 'processing')
		`, feedbackID); err != nil {
			return nil, false, fmt.Errorf("skip feedback AI: %w", err)
		}
		if _, err := tx.Exec(ctx, `
				UPDATE attachments
				SET processing_status = 'skipped', error_message = NULL,
				    ai_budget_deferred_until = NULL
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
	deferredUntil := usageDate.AddDate(0, 0, 1)
	prepared := make([]PreparedTask, 0)
	budgetExhausted := false
	if !textAIEnabled || text == nil || strings.TrimSpace(*text) == "" {
		if textStatus == "pending" || textStatus == "processing" {
			if _, err := tx.Exec(ctx, `
				UPDATE feedbacks
				SET text_process_status = 'skipped', ai_job_enqueued_at = NULL,
				    ai_budget_deferred_until = NULL, ai_budget_deferred_reason = NULL
				WHERE id = $1
			`, feedbackID); err != nil {
				return nil, false, fmt.Errorf("skip empty feedback text: %w", err)
			}
		}
	} else if textStatus == "pending" && textEnqueuedAt == nil &&
		(textDeferredUntil == nil || !textDeferredUntil.After(now)) {
		nextGeneration := textGeneration + 1
		reserved, err := reserveBudget(ctx, tx, budgetReservation{
			TenantID: tenantID, ProjectID: projectID, Kind: TaskKindText,
			ResourceID: feedbackID, Generation: nextGeneration, UsageDate: usageDate,
		}, dailyLimit, s.projectDailyLimit(dailyLimit))
		if err != nil {
			return nil, false, err
		}
		if reserved {
			if err := tx.QueryRow(ctx, `
					UPDATE feedbacks
					SET text_process_status = 'processing', ai_job_enqueued_at = $2,
					    text_processing_generation = text_processing_generation + 1,
					    ai_processed_at = NULL, ai_budget_deferred_until = NULL,
					    ai_budget_deferred_reason = NULL
					WHERE id = $1 AND text_process_status = 'pending' AND ai_job_enqueued_at IS NULL
					RETURNING text_processing_generation
				`, feedbackID, now).Scan(&textGeneration); err != nil {
				return nil, false, fmt.Errorf("prepare feedback text AI: %w", err)
			}
			prepared = append(prepared, PreparedTask{
				Kind: TaskKindText, TenantID: tenantID, ProjectID: projectID,
				FeedbackID: feedbackID,
				ResourceID: feedbackID, Generation: textGeneration,
				EnqueuedAt: now, UsageDate: usageDate,
			})
		} else {
			budgetExhausted = true
			if _, err := tx.Exec(ctx, `
					UPDATE feedbacks
					SET ai_budget_deferred_until = $2, ai_budget_deferred_reason = $3
					WHERE id = $1 AND text_process_status = 'pending'
					  AND ai_job_enqueued_at IS NULL
				`, feedbackID, deferredUntil, budgetDeferredReason); err != nil {
				return nil, false, fmt.Errorf("defer feedback text AI: %w", err)
			}
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE attachments
		SET processing_status = 'skipped', error_message = NULL,
		    ai_budget_deferred_until = NULL
		WHERE feedback_id = $1 AND processing_status IN ('pending', 'processing')
		  AND NOT (
		      (kind = 'screenshot' AND $2)
		      OR (kind = 'audio' AND $3)
		      OR (kind = 'video' AND $4)
		  )
	`, feedbackID, screenshotAIEnabled, audioAIEnabled, videoAIEnabled); err != nil {
		return nil, false, fmt.Errorf("skip unauthorized attachment AI: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id, processing_attempts
		FROM attachments
		WHERE feedback_id = $1
		  AND completed_at IS NOT NULL
		  AND safety_status = 'safe'
		  AND retention_deleted_at IS NULL
		  AND kind <> 'replay'
		  AND processing_status = 'pending'
		  AND (
		      error_message IS NULL
		      OR (
		          ai_budget_deferred_until IS NOT NULL
		          AND ai_budget_deferred_until <= $2
		      )
		  )
		ORDER BY created_at, id
		FOR UPDATE
	`, feedbackID, now)
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
		if budgetExhausted {
			if err := deferAttachmentAI(
				ctx, tx, attachment.id, deferredUntil,
			); err != nil {
				return nil, false, err
			}
			continue
		}
		attempt := attachment.attempts + 1
		reserved, err := reserveBudget(ctx, tx, budgetReservation{
			TenantID: tenantID, ProjectID: projectID, Kind: TaskKindAttachment,
			ResourceID: attachment.id, Generation: int64(attempt), UsageDate: usageDate,
		}, dailyLimit, s.projectDailyLimit(dailyLimit))
		if err != nil {
			return nil, false, err
		}
		if !reserved {
			budgetExhausted = true
			if err := deferAttachmentAI(
				ctx, tx, attachment.id, deferredUntil,
			); err != nil {
				return nil, false, err
			}
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE attachments
			SET processing_status = 'processing', processing_attempts = $2,
			    error_message = NULL, ai_budget_deferred_until = NULL
			WHERE id = $1 AND processing_status = 'pending'
			  AND (
			      error_message IS NULL
			      OR (
			          ai_budget_deferred_until IS NOT NULL
			          AND ai_budget_deferred_until <= $3
			      )
			  )
			  AND safety_status = 'safe' AND retention_deleted_at IS NULL
		`, attachment.id, attempt, now); err != nil {
			return nil, false, fmt.Errorf("prepare attachment AI: %w", err)
		}
		prepared = append(prepared, PreparedTask{
			Kind: TaskKindAttachment, TenantID: tenantID, ProjectID: projectID,
			FeedbackID: feedbackID,
			ResourceID: attachment.id, Attempt: attempt, EnqueuedAt: now, UsageDate: usageDate,
		})
	}

	if err := updateCompletion(ctx, tx, feedbackID, now); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit prepared AI work: %w", err)
	}
	if budgetExhausted {
		s.recordDiagnostic(ctx, abuse.DiagnosticEvent{
			TenantID: tenantID, ProjectID: projectID, Category: abuse.CategoryAIBudget,
			Reason: budgetDeferredReason, OccurredAt: now,
		})
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
	shouldRefundBudget := task.Kind != TaskKindFileDelete
	switch task.Kind {
	case TaskKindText:
		tag, err := tx.Exec(ctx, `
				UPDATE feedbacks
				SET text_process_status = 'pending', ai_job_enqueued_at = NULL,
				    ai_budget_deferred_until = NULL, ai_budget_deferred_reason = NULL
				WHERE id = $1 AND tenant_id = $2
				  AND text_process_status = 'processing' AND ai_job_enqueued_at = $3
				  AND text_processing_generation = $4
			`, task.ResourceID, task.TenantID, task.EnqueuedAt, task.Generation)
		if err != nil {
			return fmt.Errorf("undo text AI enqueue: %w", err)
		}
		affected = tag.RowsAffected()
	case TaskKindAttachment:
		tag, err := tx.Exec(ctx, `
			UPDATE attachments
			SET processing_status = 'pending', processing_attempts = processing_attempts - 1,
			    ai_budget_deferred_until = NULL
			WHERE id = $1 AND tenant_id = $2
			  AND processing_status = 'processing' AND processing_attempts = $3
		`, task.ResourceID, task.TenantID, task.Attempt)
		if err != nil {
			return fmt.Errorf("undo attachment AI enqueue: %w", err)
		}
		affected = tag.RowsAffected()
	default:
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
	if affected > 0 && shouldRefundBudget {
		if task.ProjectID == "" {
			if _, err := tx.Exec(ctx, `
				UPDATE ai_daily_usage SET requests = GREATEST(requests - 1, 0)
				WHERE tenant_id = $1 AND usage_date = $2
			`, task.TenantID, task.UsageDate); err != nil {
				return fmt.Errorf("refund legacy AI daily budget: %w", err)
			}
		} else if err := refundBudget(
			ctx, tx, budgetReservationForTask(task),
		); err != nil {
			return err
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
				JOIN widget_rules AS rule
				  ON rule.id = feedback.widget_rule_id AND rule.project_id = feedback.project_id
			LEFT JOIN ai_daily_usage AS usage
			  ON usage.tenant_id = feedback.tenant_id AND usage.usage_date = $2
			LEFT JOIN ai_project_daily_usage AS project_usage
			  ON project_usage.project_id = feedback.project_id
			 AND project_usage.usage_date = $2
				WHERE tenant.plan IN ('growth', 'scale', 'enterprise')
				  AND fidibeki_ai_tenant_processing_eligible(
				      tenant.status,
				      tenant.trial_ends_at,
				      tenant.deletion_requested_at,
				      $4
				  )
				  AND COALESCE((rule.ai_processing->>'enabled')::boolean, false)
			  AND NOT fidibeki_privacy_feedback_restricted(
			      feedback.tenant_id, feedback.id, 'ai'
			  )
			  AND COALESCE(usage.requests, 0) < $3
			  AND COALESCE(project_usage.requests, 0) < $5
			  AND (
				(
					    feedback.text_process_status = 'pending'
					    AND COALESCE(rule.ai_processing->'input_categories' ? 'feedback_text', false)
				    AND feedback.ai_job_enqueued_at IS NULL
				    AND (
				        feedback.ai_budget_deferred_until IS NULL
				        OR feedback.ai_budget_deferred_until <= $4
				    )
				)
				OR EXISTS (
					SELECT 1 FROM attachments AS attachment
					WHERE attachment.feedback_id = feedback.id
					  AND attachment.completed_at IS NOT NULL
					  AND attachment.safety_status = 'safe'
					  AND attachment.retention_deleted_at IS NULL
					  AND attachment.kind <> 'replay'
					  AND attachment.processing_status = 'pending'
					  AND (
					      attachment.error_message IS NULL
					      OR (
					          attachment.ai_budget_deferred_until IS NOT NULL
					          AND attachment.ai_budget_deferred_until <= $4
					      )
					  )
				)
			  )
		)
		SELECT id FROM eligible
		ORDER BY tenant_position, tenant_id, id
		LIMIT $1
	`, limit, midnightUTC(now), dailyLimit, now, s.projectDailyLimit(dailyLimit))
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
		SELECT kind, tenant_id, project_id, resource_id, attempt, generation, enqueued_at
		FROM (
			SELECT 'text'::text AS kind, feedback.tenant_id, feedback.project_id,
			       feedback.id AS resource_id, 0 AS attempt,
			       feedback.text_processing_generation AS generation,
			       feedback.ai_job_enqueued_at AS enqueued_at
			FROM feedbacks AS feedback
			JOIN tenants AS tenant ON tenant.id = feedback.tenant_id
			WHERE feedback.text_process_status = 'processing'
			  AND feedback.ai_job_enqueued_at IS NOT NULL
			  AND fidibeki_ai_tenant_processing_eligible(
			      tenant.status,
			      tenant.trial_ends_at,
			      tenant.deletion_requested_at,
			      CURRENT_TIMESTAMP
			  )
			  AND NOT fidibeki_privacy_feedback_restricted(
			      feedback.tenant_id, feedback.id, 'ai'
			  )
			UNION ALL
			SELECT 'attachment', attachment.tenant_id, feedback.project_id,
			       attachment.id, attachment.processing_attempts, 0, attachment.created_at
			FROM attachments AS attachment
			JOIN feedbacks AS feedback ON feedback.id = attachment.feedback_id
			JOIN tenants AS tenant ON tenant.id = attachment.tenant_id
			WHERE attachment.processing_status = 'processing'
			  AND attachment.safety_status = 'safe'
			  AND attachment.retention_deleted_at IS NULL
			  AND fidibeki_ai_tenant_processing_eligible(
			      tenant.status,
			      tenant.trial_ends_at,
			      tenant.deletion_requested_at,
			      CURRENT_TIMESTAMP
			  )
			  AND NOT fidibeki_privacy_feedback_restricted(
			      attachment.tenant_id, feedback.id, 'ai'
			  )
			UNION ALL
			SELECT 'file_delete', file.tenant_id, feedback.project_id,
			       file.id, file.delete_attempts, 0, file.delete_enqueued_at
			FROM ai_gemini_files AS file
			JOIN attachments AS attachment ON attachment.id = file.attachment_id
			JOIN feedbacks AS feedback ON feedback.id = attachment.feedback_id
			WHERE file.deleted_at IS NULL AND file.failed_at IS NULL
			  AND file.delete_enqueued_at IS NOT NULL
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
			&kind, &task.TenantID, &task.ProjectID, &task.ResourceID, &task.Attempt,
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

func (s Store) ExpireIncompleteAttachments(
	ctx context.Context,
	_ time.Time,
	now time.Time,
	limit int,
) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin incomplete attachment expiration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		WITH expired AS (
			SELECT id
			FROM attachments
			WHERE safety_status = 'pending'
			  AND upload_expires_at <= $1
			ORDER BY upload_expires_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE attachments AS attachment
		SET complete_token_hash = NULL,
		    safety_status = 'expired',
		    safety_checked_at = $1,
		    safety_error_reason = 'upload_expired',
		    verification_enqueued_at = NULL,
		    processing_status = 'failed',
		    error_message = 'attachment upload expired before completion',
		    processed_at = $1
		FROM expired
		WHERE attachment.id = expired.id
		RETURNING attachment.feedback_id
	`, now.UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("expire incomplete attachments: %w", err)
	}
	feedbackIDs := make(map[string]struct{})
	expiredCount := 0
	for rows.Next() {
		var feedbackID string
		if err := rows.Scan(&feedbackID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan expired attachment feedback: %w", err)
		}
		feedbackIDs[feedbackID] = struct{}{}
		expiredCount++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate expired attachment feedbacks: %w", err)
	}
	rows.Close()
	orderedFeedbackIDs := make([]string, 0, len(feedbackIDs))
	for feedbackID := range feedbackIDs {
		orderedFeedbackIDs = append(orderedFeedbackIDs, feedbackID)
	}
	sort.Strings(orderedFeedbackIDs)
	for _, feedbackID := range orderedFeedbackIDs {
		var lockedID string
		if err := tx.QueryRow(ctx, `
			/* lock expired attachment feedbacks */
			SELECT id FROM feedbacks WHERE id = $1 FOR UPDATE
		`, feedbackID).Scan(&lockedID); err != nil {
			return 0, fmt.Errorf("lock expired attachment feedback %s: %w", feedbackID, err)
		}
	}
	for _, feedbackID := range orderedFeedbackIDs {
		if err := updateCompletion(ctx, tx, feedbackID, now.UTC()); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit incomplete attachment expiration: %w", err)
	}
	return expiredCount, nil
}

func (s Store) TrackGeminiFile(
	ctx context.Context,
	work AttachmentWork,
	fileName string,
	deleteAfter time.Time,
) (string, error) {
	var id string
	err := s.Database.QueryRow(ctx, `
		INSERT INTO ai_gemini_files (
			tenant_id, attachment_id, processing_run_id, file_name, delete_after
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, work.TenantID, work.AttachmentID, nullableText(work.RunID), fileName, deleteAfter).Scan(&id)
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
			  AND safety_status = 'safe'
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
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return TextWork{}, fmt.Errorf("begin text AI claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	eligible, err := tenantAIProcessingEligible(ctx, tx, tenantID)
	if err != nil {
		return TextWork{}, err
	}
	if !eligible {
		if err := cancelTextForTenantLifecycle(
			ctx, tx, tenantID, feedbackID, generation, time.Now().UTC(),
		); err != nil {
			return TextWork{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return TextWork{}, fmt.Errorf("commit text AI lifecycle cancellation: %w", err)
		}
		return TextWork{}, ErrNoWork
	}
	var work TextWork
	work.TenantID, work.FeedbackID, work.Generation = tenantID, feedbackID, generation
	err = tx.QueryRow(ctx, `
		UPDATE feedbacks AS feedback
		SET text_process_status = 'processing'
		FROM widget_rules AS rule
		WHERE feedback.id = $1 AND feedback.tenant_id = $2
		  AND feedback.widget_rule_id = rule.id AND feedback.project_id = rule.project_id
		  AND COALESCE((rule.ai_processing->>'enabled')::boolean, false)
		  AND COALESCE(rule.ai_processing->'input_categories' ? 'feedback_text', false)
		  AND feedback.text IS NOT NULL AND btrim(feedback.text) <> ''
		  AND feedback.text_process_status IN ('pending', 'processing')
		  AND feedback.text_processing_generation = $3
		  AND NOT fidibeki_privacy_feedback_restricted(feedback.tenant_id, feedback.id, 'ai')
		RETURNING feedback.text, feedback.project_id, rule.id,
		          rule.ai_processing->>'policy_version'
	`, feedbackID, tenantID, generation).Scan(
		&work.Text, &work.ProjectID, &work.RuleID, &work.PolicyVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TextWork{}, ErrNoWork
	}
	if err != nil {
		return TextWork{}, fmt.Errorf("claim text AI: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO ai_processing_runs (
			tenant_id, project_id, feedback_id, widget_rule_id, generation,
			policy_version, provider, model, purpose, input_category, status
		) VALUES (
			$1, $2, $3, $4, $5, $6, 'google_gemini', $7,
			'advisory_feedback_analysis', 'feedback_text', 'processing'
		)
		ON CONFLICT (tenant_id, feedback_id, generation) WHERE attachment_id IS NULL
		DO UPDATE SET status = 'processing', completed_at = NULL, failure_reason = NULL
		RETURNING id
	`, work.TenantID, work.ProjectID, work.FeedbackID, work.RuleID, work.Generation,
		work.PolicyVersion, s.model()).Scan(&work.RunID); err != nil {
		return TextWork{}, fmt.Errorf("start text AI processing evidence: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TextWork{}, fmt.Errorf("commit text AI claim: %w", err)
	}
	return work, nil
}

func (s Store) ClaimAttachment(
	ctx context.Context,
	tenantID, attachmentID string,
	attempt int,
) (AttachmentWork, error) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return AttachmentWork{}, fmt.Errorf("begin attachment AI claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	eligible, err := tenantAIProcessingEligible(ctx, tx, tenantID)
	if err != nil {
		return AttachmentWork{}, err
	}
	if !eligible {
		if err := cancelAttachmentForTenantLifecycle(
			ctx, tx, tenantID, attachmentID, attempt, time.Now().UTC(),
		); err != nil {
			return AttachmentWork{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return AttachmentWork{}, fmt.Errorf("commit attachment AI lifecycle cancellation: %w", err)
		}
		return AttachmentWork{}, ErrNoWork
	}
	var work AttachmentWork
	work.TenantID, work.AttachmentID, work.Attempt = tenantID, attachmentID, attempt
	err = tx.QueryRow(ctx, `
		UPDATE attachments AS attachment
		SET processing_status = 'processing'
		FROM feedbacks AS feedback
		JOIN widget_rules AS rule
		  ON rule.id = feedback.widget_rule_id AND rule.project_id = feedback.project_id
			WHERE attachment.id = $1 AND attachment.tenant_id = $2
			  AND attachment.processing_attempts = $3
			  AND attachment.completed_at IS NOT NULL AND attachment.kind <> 'replay'
			  AND attachment.safety_status = 'safe'
			  AND attachment.retention_deleted_at IS NULL
			  AND attachment.processing_status IN ('pending', 'processing')
			  AND feedback.id = attachment.feedback_id
			  AND NOT fidibeki_privacy_feedback_restricted(
			      attachment.tenant_id, feedback.id, 'ai'
			  )
			  AND COALESCE((rule.ai_processing->>'enabled')::boolean, false)
			  AND COALESCE(
			      (rule.ai_processing->'input_categories') ?
			      (CASE attachment.kind
			          WHEN 'screenshot' THEN 'screenshot'
			          WHEN 'audio' THEN 'audio'
			          WHEN 'video' THEN 'video'
			          ELSE ''
			      END),
			      false
			  )
		RETURNING feedback.id, feedback.project_id, attachment.kind,
		          attachment.storage_key, attachment.content_type, rule.id,
		          rule.ai_processing->>'policy_version'
	`, attachmentID, tenantID, attempt).Scan(
		&work.FeedbackID, &work.ProjectID, &work.Kind, &work.StorageKey, &work.ContentType,
		&work.RuleID, &work.PolicyVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AttachmentWork{}, ErrNoWork
	}
	if err != nil {
		return AttachmentWork{}, fmt.Errorf("claim attachment AI: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO ai_processing_runs (
			tenant_id, project_id, feedback_id, attachment_id, widget_rule_id, attempt,
			policy_version, provider, model, purpose, input_category, status
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, 'google_gemini', $8,
			'advisory_feedback_analysis', $9, 'processing'
		)
		ON CONFLICT (tenant_id, attachment_id, attempt) WHERE attachment_id IS NOT NULL
		DO UPDATE SET status = 'processing', completed_at = NULL, failure_reason = NULL
		RETURNING id
	`, work.TenantID, work.ProjectID, work.FeedbackID, work.AttachmentID, work.RuleID,
		work.Attempt, work.PolicyVersion, s.model(), work.Kind).Scan(&work.RunID); err != nil {
		return AttachmentWork{}, fmt.Errorf("start attachment AI processing evidence: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AttachmentWork{}, fmt.Errorf("commit attachment AI claim: %w", err)
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
		UPDATE feedbacks AS feedback
		SET text_cleaned = $3, text_sentiment_score = $4,
		    text_moderation_flags = $5, text_process_status = 'completed',
		    ai_job_enqueued_at = NULL, ai_suggested_title = $7,
		    ai_actionability = $8, ai_category = $9, ai_priority = $10,
		    ai_redaction_manifest = $11::jsonb
		FROM widget_rules AS rule
		WHERE feedback.id = $1 AND feedback.tenant_id = $2
		  AND feedback.widget_rule_id = rule.id AND feedback.project_id = rule.project_id
		  AND COALESCE((rule.ai_processing->>'enabled')::boolean, false)
		  AND COALESCE(rule.ai_processing->'input_categories' ? 'feedback_text', false)
		  AND feedback.text_process_status = 'processing'
		  AND feedback.text_processing_generation = $6
	`, work.FeedbackID, work.TenantID, nullableText(result.CleanedText),
		result.SentimentScore, moderationFlags(result.ModerationFlags), work.Generation,
		nullableText(result.SuggestedTitle), nullableText(result.Actionability),
		nullableText(result.Category), nullableText(result.Priority), redactionManifest(result))
	if err != nil {
		return fmt.Errorf("complete text AI: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if err := discardProcessingRun(ctx, tx, work.RunID, "policy_disabled", now); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit discarded text AI result: %w", err)
		}
		return ErrNoWork
	}
	if err := completeProcessingRun(ctx, tx, work.RunID, result, now); err != nil {
		return err
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
		    processing_status = 'completed', error_message = NULL, processed_at = $7,
		    ai_redaction_manifest = $8::jsonb
		FROM feedbacks AS feedback
		JOIN widget_rules AS rule
		  ON rule.id = feedback.widget_rule_id AND rule.project_id = feedback.project_id
			WHERE attachments.id = $1 AND attachments.tenant_id = $2
			  AND attachments.feedback_id = feedback.id
			  AND attachments.processing_attempts = $3
			  AND attachments.processing_status = 'processing'
			  AND attachments.safety_status = 'safe'
			  AND COALESCE((rule.ai_processing->>'enabled')::boolean, false)
			  AND COALESCE(
			      (rule.ai_processing->'input_categories') ?
			      (CASE attachments.kind
			          WHEN 'screenshot' THEN 'screenshot'
			          WHEN 'audio' THEN 'audio'
			          WHEN 'video' THEN 'video'
			          ELSE ''
			      END),
			      false
			  )
	`, work.AttachmentID, work.TenantID, work.Attempt, nullableText(transcription),
		result.SentimentScore, moderationFlags(result.ModerationFlags), now, redactionManifest(result))
	if err != nil {
		return fmt.Errorf("complete attachment AI: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if err := discardProcessingRun(ctx, tx, work.RunID, "policy_disabled", now); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit discarded attachment AI result: %w", err)
		}
		return ErrNoWork
	}
	if err := completeProcessingRun(ctx, tx, work.RunID, result, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE feedbacks
		SET ai_suggested_title = COALESCE(ai_suggested_title, $3),
		    ai_actionability = COALESCE(ai_actionability, $4),
		    ai_category = COALESCE(ai_category, $5),
		    ai_priority = COALESCE(ai_priority, $6)
		WHERE id = $1 AND tenant_id = $2
	`, work.FeedbackID, work.TenantID, nullableText(result.SuggestedTitle),
		nullableText(result.Actionability), nullableText(result.Category),
		nullableText(result.Priority)); err != nil {
		return fmt.Errorf("store attachment AI recommendations: %w", err)
	}
	if err := updateCompletion(ctx, tx, work.FeedbackID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit attachment AI completion: %w", err)
	}
	return nil
}

func (s Store) FailText(ctx context.Context, work TextWork, failure error, permanentFailure bool, now time.Time) error {
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
		if err := failProcessingRun(ctx, query, work.RunID, failure, now); err != nil {
			return err
		}
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
			  AND processing_status = 'processing' AND safety_status = 'safe'
	`, work.AttachmentID, work.TenantID, work.Attempt, status, truncateError(failure), now,
		permanentFailure); err != nil {
		return fmt.Errorf("record attachment AI failure: %w", err)
	}
	if permanentFailure {
		if err := failProcessingRun(ctx, query, work.RunID, failure, now); err != nil {
			return err
		}
		if err := updateCompletion(ctx, query, work.FeedbackID, now); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit terminal attachment AI failure: %w", err)
		}
	}
	return nil
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
			  AND safety_status = 'safe' AND retention_deleted_at IS NULL
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
		    transcription = NULL, sentiment_score = NULL, moderation_flags = '{}',
		    processed_at = NULL, ai_budget_deferred_until = NULL
			WHERE id = $1 AND tenant_id = $2 AND completed_at IS NOT NULL
			  AND kind <> 'replay' AND processing_status = $3
			  AND safety_status = 'safe' AND retention_deleted_at IS NULL
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
			    ai_suggested_title = NULL, ai_actionability = NULL,
			    ai_category = NULL, ai_priority = NULL,
			    text_processing_generation = text_processing_generation + 1,
			    ai_job_enqueued_at = NULL, ai_processed_at = NULL,
			    ai_budget_deferred_until = NULL, ai_budget_deferred_reason = NULL
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
			if _, err := query.Exec(ctx, `
				UPDATE feedbacks
				SET ai_budget_deferred_until = NULL, ai_budget_deferred_reason = NULL
				WHERE id = $1 AND tenant_id = $2 AND text_process_status = 'pending'
			`, feedbackID, tenantID); err != nil {
				return fmt.Errorf("clear deferred feedback AI: %w", err)
			}
			if _, err := query.Exec(ctx, `
				UPDATE attachments
				SET ai_budget_deferred_until = NULL, error_message = NULL
				WHERE feedback_id = $1 AND tenant_id = $2
				  AND processing_status = 'pending'
				  AND ai_budget_deferred_until IS NOT NULL
			`, feedbackID, tenantID); err != nil {
				return fmt.Errorf("clear deferred attachment AI: %w", err)
			}
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
		    error_message = NULL, ai_budget_deferred_until = NULL,
		    transcription = NULL, sentiment_score = NULL,
		    moderation_flags = '{}', processed_at = NULL
			WHERE feedback_id = $1 AND tenant_id = $2
			  AND completed_at IS NOT NULL AND processing_status <> 'processing'
			  AND safety_status = 'safe' AND retention_deleted_at IS NULL
	`, feedbackID, tenantID); err != nil {
		return fmt.Errorf("reset feedback attachments AI: %w", err)
	}
	return nil
}

type budgetReservation struct {
	TenantID   string
	ProjectID  string
	Kind       TaskKind
	ResourceID string
	Generation int64
	UsageDate  time.Time
}

func reserveBudget(
	ctx context.Context,
	query db.Querier,
	reservation budgetReservation,
	tenantLimit int,
	projectLimit int,
) (bool, error) {
	if tenantLimit <= 0 || projectLimit <= 0 {
		return false, errors.New("AI daily budget limits must be positive")
	}
	tag, err := query.Exec(ctx, `
		INSERT INTO ai_work_reservations (
			tenant_id, project_id, work_kind, resource_id, generation, usage_date
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, work_kind, resource_id, generation) DO NOTHING
	`, reservation.TenantID, reservation.ProjectID, reservation.Kind,
		reservation.ResourceID, reservation.Generation, reservation.UsageDate)
	if err != nil {
		return false, fmt.Errorf("create AI work reservation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return true, nil
	}

	var requests int
	err = query.QueryRow(ctx, `
		INSERT INTO ai_daily_usage (tenant_id, usage_date, requests)
		VALUES ($1, $2, 1)
		ON CONFLICT (tenant_id, usage_date) DO UPDATE
		SET requests = ai_daily_usage.requests + 1
		WHERE ai_daily_usage.requests < $3
		RETURNING requests
	`, reservation.TenantID, reservation.UsageDate, tenantLimit).Scan(&requests)
	if errors.Is(err, pgx.ErrNoRows) {
		if cleanupErr := deleteBudgetReservation(ctx, query, reservation); cleanupErr != nil {
			return false, cleanupErr
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserve tenant AI daily budget: %w", err)
	}

	err = query.QueryRow(ctx, `
		INSERT INTO ai_project_daily_usage (
			tenant_id, project_id, usage_date, requests
		) VALUES ($1, $2, $3, 1)
		ON CONFLICT (project_id, usage_date) DO UPDATE
		SET requests = ai_project_daily_usage.requests + 1
		WHERE ai_project_daily_usage.requests < $4
		RETURNING requests
	`, reservation.TenantID, reservation.ProjectID, reservation.UsageDate,
		projectLimit).Scan(&requests)
	if errors.Is(err, pgx.ErrNoRows) {
		if cleanupErr := decrementTenantBudget(
			ctx, query, reservation.TenantID, reservation.UsageDate,
		); cleanupErr != nil {
			return false, cleanupErr
		}
		if cleanupErr := deleteBudgetReservation(ctx, query, reservation); cleanupErr != nil {
			return false, cleanupErr
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserve project AI daily budget: %w", err)
	}
	return true, nil
}

func refundBudget(
	ctx context.Context,
	query db.Querier,
	reservation budgetReservation,
) error {
	tag, err := query.Exec(ctx, `
		DELETE FROM ai_work_reservations
		WHERE tenant_id = $1 AND project_id = $2 AND work_kind = $3
		  AND resource_id = $4 AND generation = $5 AND usage_date = $6
	`, reservation.TenantID, reservation.ProjectID, reservation.Kind,
		reservation.ResourceID, reservation.Generation, reservation.UsageDate)
	if err != nil {
		return fmt.Errorf("remove AI work reservation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if err := decrementTenantBudget(
		ctx, query, reservation.TenantID, reservation.UsageDate,
	); err != nil {
		return err
	}
	if _, err := query.Exec(ctx, `
		UPDATE ai_project_daily_usage
		SET requests = GREATEST(requests - 1, 0)
		WHERE tenant_id = $1 AND project_id = $2 AND usage_date = $3
	`, reservation.TenantID, reservation.ProjectID, reservation.UsageDate); err != nil {
		return fmt.Errorf("refund project AI daily budget: %w", err)
	}
	return nil
}

func deleteBudgetReservation(
	ctx context.Context,
	query db.Querier,
	reservation budgetReservation,
) error {
	if _, err := query.Exec(ctx, `
		DELETE FROM ai_work_reservations
		WHERE tenant_id = $1 AND work_kind = $2 AND resource_id = $3 AND generation = $4
	`, reservation.TenantID, reservation.Kind, reservation.ResourceID,
		reservation.Generation); err != nil {
		return fmt.Errorf("remove denied AI work reservation: %w", err)
	}
	return nil
}

func decrementTenantBudget(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	usageDate time.Time,
) error {
	if _, err := query.Exec(ctx, `
		UPDATE ai_daily_usage SET requests = GREATEST(requests - 1, 0)
		WHERE tenant_id = $1 AND usage_date = $2
	`, tenantID, usageDate); err != nil {
		return fmt.Errorf("refund tenant AI daily budget: %w", err)
	}
	return nil
}

func budgetReservationForTask(task PreparedTask) budgetReservation {
	generation := task.Generation
	if task.Kind == TaskKindAttachment {
		generation = int64(task.Attempt)
	}
	return budgetReservation{
		TenantID: task.TenantID, ProjectID: task.ProjectID, Kind: task.Kind,
		ResourceID: task.ResourceID, Generation: generation, UsageDate: task.UsageDate,
	}
}

func deferAttachmentAI(
	ctx context.Context,
	query db.Querier,
	attachmentID string,
	deferredUntil time.Time,
) error {
	if _, err := query.Exec(ctx, `
		UPDATE attachments
		SET error_message = $3, ai_budget_deferred_until = $2
		WHERE id = $1 AND processing_status = 'pending'
	`, attachmentID, deferredUntil, budgetDeferredReason); err != nil {
		return fmt.Errorf("defer attachment AI: %w", err)
	}
	return nil
}

func (s Store) projectDailyLimit(tenantLimit int) int {
	if s.ProjectDailyLimit > 0 {
		return s.ProjectDailyLimit
	}
	return tenantLimit
}

func (s Store) recordDiagnostic(ctx context.Context, event abuse.DiagnosticEvent) {
	if s.Diagnostics == nil {
		return
	}
	if err := s.Diagnostics.Record(ctx, event); err != nil {
		logger := s.Logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.WarnContext(ctx, "record AI budget diagnostic", "error", err)
	}
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

func redactionManifest(result Result) string {
	manifest := result.Redactions
	if manifest == nil {
		manifest = map[string]int{}
	}
	encoded, err := json.Marshal(map[string]any{
		"policy_version": RedactionPolicyVersion,
		"categories":     manifest,
	})
	if err != nil {
		return `{"policy_version":"` + RedactionPolicyVersion + `","categories":{}}`
	}
	return string(encoded)
}

func (s Store) model() string {
	if strings.TrimSpace(s.Model) == "" {
		return "unreported"
	}
	return strings.TrimSpace(s.Model)
}

func completeProcessingRun(
	ctx context.Context,
	query db.Querier,
	runID string,
	result Result,
	now time.Time,
) error {
	if runID == "" {
		return nil
	}
	if _, err := query.Exec(ctx, `
		UPDATE ai_processing_runs
		SET status = 'completed', redaction_manifest = $2::jsonb,
		    failure_reason = NULL, completed_at = $3
		WHERE id = $1 AND status = 'processing'
	`, runID, redactionManifest(result), now); err != nil {
		return fmt.Errorf("complete AI processing evidence: %w", err)
	}
	return nil
}

func discardProcessingRun(
	ctx context.Context,
	query db.Querier,
	runID string,
	reason string,
	now time.Time,
) error {
	if runID == "" {
		return nil
	}
	if _, err := query.Exec(ctx, `
		UPDATE ai_processing_runs
		SET status = 'discarded', failure_reason = $2, completed_at = $3
		WHERE id = $1 AND status = 'processing'
	`, runID, reason, now); err != nil {
		return fmt.Errorf("discard AI processing evidence: %w", err)
	}
	return nil
}

func failProcessingRun(
	ctx context.Context,
	query db.Querier,
	runID string,
	failure error,
	now time.Time,
) error {
	if runID == "" {
		return nil
	}
	status := "failed"
	reason := providerDiagnosticReason(failure)
	if errors.Is(failure, ErrUnsafeAIResult) {
		status = "quarantined"
		reason = "redaction_policy_failed"
	}
	if _, err := query.Exec(ctx, `
		UPDATE ai_processing_runs
		SET status = $2, failure_reason = $3, completed_at = $4
		WHERE id = $1 AND status = 'processing'
	`, runID, status, reason, now); err != nil {
		return fmt.Errorf("fail AI processing evidence: %w", err)
	}
	return nil
}

func moderationFlags(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
