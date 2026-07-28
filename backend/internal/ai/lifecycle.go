package ai

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

const tenantLifecycleInactiveReason = "tenant_lifecycle_inactive"

func (s Store) CancelIneligibleWork(
	ctx context.Context,
	limit int,
	now time.Time,
) (int, error) {
	if limit <= 0 {
		return 0, errors.New("inactive AI cancellation limit must be positive")
	}
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin inactive AI cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT feedback.id, feedback.tenant_id
		FROM feedbacks AS feedback
		JOIN tenants AS tenant ON tenant.id = feedback.tenant_id
		WHERE NOT fidibeki_ai_tenant_processing_eligible(
		          tenant.status,
		          tenant.trial_ends_at,
		          tenant.deletion_requested_at,
		          $2
		      )
		  AND (
		      feedback.text_process_status IN ('pending', 'processing')
		      OR EXISTS (
		          SELECT 1
		          FROM attachments AS attachment
		          WHERE attachment.feedback_id = feedback.id
		            AND attachment.processing_status IN ('pending', 'processing')
		      )
		  )
		ORDER BY feedback.created_at, feedback.id
		LIMIT $1
		FOR UPDATE OF feedback SKIP LOCKED
		FOR SHARE OF tenant SKIP LOCKED
	`, limit, now)
	if err != nil {
		return 0, fmt.Errorf("lock inactive tenant AI work: %w", err)
	}
	type candidate struct {
		feedbackID string
		tenantID   string
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.feedbackID, &item.tenantID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan inactive tenant AI work: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate inactive tenant AI work: %w", err)
	}
	rows.Close()
	for _, item := range candidates {
		if err := cancelFeedbackForTenantLifecycle(
			ctx, tx, item.tenantID, item.feedbackID, now,
		); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit inactive AI cancellation: %w", err)
	}
	return len(candidates), nil
}

func (s Store) AuthorizeTextForProvider(
	ctx context.Context,
	work TextWork,
	now time.Time,
) (bool, error) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin text AI provider authorization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	eligible, err := tenantAIProcessingEligible(ctx, tx, work.TenantID)
	if err != nil {
		return false, err
	}
	if !eligible {
		if err := cancelTextForTenantLifecycle(
			ctx, tx, work.TenantID, work.FeedbackID, work.Generation, now,
		); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit text AI provider authorization: %w", err)
	}
	return eligible, nil
}

func (s Store) AuthorizeAttachmentForProvider(
	ctx context.Context,
	work AttachmentWork,
	now time.Time,
) (bool, error) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin attachment AI provider authorization: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	eligible, err := tenantAIProcessingEligible(ctx, tx, work.TenantID)
	if err != nil {
		return false, err
	}
	if !eligible {
		if err := cancelAttachmentForTenantLifecycle(
			ctx, tx, work.TenantID, work.AttachmentID, work.Attempt, now,
		); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit attachment AI provider authorization: %w", err)
	}
	return eligible, nil
}

func tenantAIProcessingEligible(
	ctx context.Context,
	query db.Querier,
	tenantID string,
) (bool, error) {
	var eligible bool
	err := query.QueryRow(ctx, `
		SELECT fidibeki_ai_tenant_processing_eligible(
		    tenant.status,
		    tenant.trial_ends_at,
		    tenant.deletion_requested_at,
		    CURRENT_TIMESTAMP
		)
		FROM tenants AS tenant
		WHERE tenant.id = $1
		FOR SHARE OF tenant
	`, tenantID).Scan(&eligible)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("authorize tenant AI processing: %w", err)
	}
	return eligible, nil
}

func cancelFeedbackForTenantLifecycle(
	ctx context.Context,
	query db.Querier,
	tenantID, feedbackID string,
	now time.Time,
) error {
	reservations, err := feedbackBudgetReservations(
		ctx, query, tenantID, feedbackID,
	)
	if err != nil {
		return err
	}
	if _, err := query.Exec(ctx, `
		UPDATE feedbacks
		SET text_process_status = 'skipped',
		    ai_job_enqueued_at = NULL,
		    ai_budget_deferred_until = NULL,
		    ai_budget_deferred_reason = NULL
		WHERE id = $1 AND tenant_id = $2
		  AND text_process_status IN ('pending', 'processing')
	`, feedbackID, tenantID); err != nil {
		return fmt.Errorf("cancel feedback AI for tenant lifecycle: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE attachments
		SET processing_status = 'skipped',
		    error_message = NULL,
		    ai_budget_deferred_until = NULL
		WHERE feedback_id = $1 AND tenant_id = $2
		  AND processing_status IN ('pending', 'processing')
	`, feedbackID, tenantID); err != nil {
		return fmt.Errorf("cancel feedback attachment AI for tenant lifecycle: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE ai_processing_runs
		SET status = 'discarded',
		    failure_reason = $3,
		    completed_at = $4
		WHERE tenant_id = $1 AND feedback_id = $2 AND status = 'processing'
	`, tenantID, feedbackID, tenantLifecycleInactiveReason, now); err != nil {
		return fmt.Errorf("discard feedback AI for tenant lifecycle: %w", err)
	}
	for _, reservation := range reservations {
		if err := refundBudget(ctx, query, reservation); err != nil {
			return err
		}
	}
	if err := updateCompletion(ctx, query, feedbackID, now); err != nil {
		return err
	}
	return nil
}

func cancelTextForTenantLifecycle(
	ctx context.Context,
	query db.Querier,
	tenantID, feedbackID string,
	generation int64,
	now time.Time,
) error {
	tag, err := query.Exec(ctx, `
		UPDATE feedbacks
		SET text_process_status = 'skipped',
		    ai_job_enqueued_at = NULL,
		    ai_budget_deferred_until = NULL,
		    ai_budget_deferred_reason = NULL
		WHERE id = $1 AND tenant_id = $2
		  AND text_processing_generation = $3
		  AND text_process_status IN ('pending', 'processing')
	`, feedbackID, tenantID, generation)
	if err != nil {
		return fmt.Errorf("cancel text AI for tenant lifecycle: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if err := refundReservedWork(
		ctx, query, tenantID, TaskKindText, feedbackID, generation,
	); err != nil {
		return err
	}
	if _, err := query.Exec(ctx, `
		UPDATE ai_processing_runs
		SET status = 'discarded',
		    failure_reason = $4,
		    completed_at = $5
		WHERE tenant_id = $1 AND feedback_id = $2 AND generation = $3
		  AND attachment_id IS NULL AND status = 'processing'
	`, tenantID, feedbackID, generation, tenantLifecycleInactiveReason, now); err != nil {
		return fmt.Errorf("discard text AI for tenant lifecycle: %w", err)
	}
	if err := updateCompletion(ctx, query, feedbackID, now); err != nil {
		return err
	}
	return nil
}

func cancelAttachmentForTenantLifecycle(
	ctx context.Context,
	query db.Querier,
	tenantID, attachmentID string,
	attempt int,
	now time.Time,
) error {
	var feedbackID string
	err := query.QueryRow(ctx, `
		UPDATE attachments
		SET processing_status = 'skipped',
		    error_message = NULL,
		    ai_budget_deferred_until = NULL
		WHERE id = $1 AND tenant_id = $2
		  AND processing_attempts = $3
		  AND processing_status IN ('pending', 'processing')
		RETURNING feedback_id
	`, attachmentID, tenantID, attempt).Scan(&feedbackID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cancel attachment AI for tenant lifecycle: %w", err)
	}
	if err := refundReservedWork(
		ctx, query, tenantID, TaskKindAttachment, attachmentID, int64(attempt),
	); err != nil {
		return err
	}
	if _, err := query.Exec(ctx, `
		UPDATE ai_processing_runs
		SET status = 'discarded',
		    failure_reason = $4,
		    completed_at = $5
		WHERE tenant_id = $1 AND attachment_id = $2 AND attempt = $3
		  AND status = 'processing'
	`, tenantID, attachmentID, attempt, tenantLifecycleInactiveReason, now); err != nil {
		return fmt.Errorf("discard attachment AI for tenant lifecycle: %w", err)
	}
	if err := updateCompletion(ctx, query, feedbackID, now); err != nil {
		return err
	}
	return nil
}

func feedbackBudgetReservations(
	ctx context.Context,
	query db.Querier,
	tenantID, feedbackID string,
) ([]budgetReservation, error) {
	rows, err := query.Query(ctx, `
		SELECT reservation.project_id, reservation.work_kind,
		       reservation.resource_id, reservation.generation, reservation.usage_date
		FROM ai_work_reservations AS reservation
		WHERE reservation.tenant_id = $1
		  AND (
		      (
		          reservation.work_kind = 'text'
		          AND reservation.resource_id = $2
		      )
		      OR (
		          reservation.work_kind = 'attachment'
		          AND EXISTS (
		              SELECT 1
		              FROM attachments AS attachment
		              WHERE attachment.id = reservation.resource_id
		                AND attachment.feedback_id = $2
		                AND attachment.tenant_id = $1
		          )
		      )
		  )
		ORDER BY reservation.work_kind, reservation.resource_id, reservation.generation
		FOR UPDATE OF reservation
	`, tenantID, feedbackID)
	if err != nil {
		return nil, fmt.Errorf("lock feedback AI budget reservations: %w", err)
	}
	defer rows.Close()
	reservations := make([]budgetReservation, 0)
	for rows.Next() {
		var reservation budgetReservation
		reservation.TenantID = tenantID
		if err := rows.Scan(
			&reservation.ProjectID, &reservation.Kind, &reservation.ResourceID,
			&reservation.Generation, &reservation.UsageDate,
		); err != nil {
			return nil, fmt.Errorf("scan feedback AI budget reservation: %w", err)
		}
		reservations = append(reservations, reservation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feedback AI budget reservations: %w", err)
	}
	return reservations, nil
}

func refundReservedWork(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	kind TaskKind,
	resourceID string,
	generation int64,
) error {
	var projectID string
	var usageDate time.Time
	err := query.QueryRow(ctx, `
		SELECT project_id, usage_date
		FROM ai_work_reservations
		WHERE tenant_id = $1 AND work_kind = $2
		  AND resource_id = $3 AND generation = $4
		FOR UPDATE
	`, tenantID, kind, resourceID, generation).Scan(&projectID, &usageDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock cancelled AI budget reservation: %w", err)
	}
	return refundBudget(ctx, query, budgetReservation{
		TenantID: tenantID, ProjectID: projectID, Kind: kind,
		ResourceID: resourceID, Generation: generation, UsageDate: usageDate,
	})
}
