package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

func (exporter Exporter) anonymizeReporter(ctx context.Context, job Job) error {
	if exporter.Database == nil {
		return exporter.fail(ctx, job, errors.New("privacy database is unavailable"))
	}
	scope, err := loadReporterScope(ctx, exporter.Database, job, ReporterAnonymize)
	if err != nil {
		return exporter.fail(ctx, job, err)
	}
	tx, err := exporter.Database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return exporter.fail(ctx, job, fmt.Errorf("begin reporter anonymization: %w", err))
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	failTransaction := func(failure error) error {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		rollbackErr := tx.Rollback(rollbackCtx)
		cancel()
		return exporter.fail(ctx, job, errors.Join(failure, rollbackErr))
	}
	if err := ensureCaseErasureAllowed(ctx, tx, job); err != nil {
		return failTransaction(err)
	}
	scope, err = loadReporterScope(ctx, tx, job, ReporterAnonymize)
	if err != nil {
		return failTransaction(err)
	}
	objectKeys, err := reporterObjectKeys(
		ctx, tx, job.TenantID, scope.Email, scope.VerifiedAt,
	)
	if err != nil {
		return failTransaction(err)
	}
	for _, key := range objectKeys {
		if err := exporter.Storage.RemoveObject(ctx, key); err != nil {
			return failTransaction(fmt.Errorf("remove reporter object %q: %w", key, err))
		}
	}
	recordCount, err := reporterRecordCount(ctx, tx, job.TenantID, scope.Email, scope.VerifiedAt)
	if err != nil {
		return failTransaction(err)
	}
	if job.ReporterRequestID == nil {
		return failTransaction(ErrJobUnavailable)
	}
	// Capture the resource scope before the statements below null reporter_email, so a later
	// review/finalize pass can recheck this fixed set of records without the reporter's address.
	if err := captureReporterRequestScope(
		ctx, tx, job.TenantID, *job.ReporterRequestID, scope.Email, scope.VerifiedAt,
	); err != nil {
		return failTransaction(err)
	}
	if err := anonymizeReporterRows(ctx, tx, job, scope, recordCount, exporter.now()); err != nil {
		return failTransaction(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return failTransaction(fmt.Errorf("commit reporter anonymization: %w", err))
	}
	return nil
}

func reporterObjectKeys(
	ctx context.Context,
	query interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	tenantID string,
	email string,
	cutoff time.Time,
) ([]string, error) {
	rows, err := query.Query(ctx, `
		SELECT attachment.storage_key
		FROM attachments AS attachment
		JOIN feedbacks AS feedback
		  ON feedback.tenant_id = attachment.tenant_id AND feedback.id = attachment.feedback_id
		WHERE attachment.tenant_id = $1 AND lower(feedback.reporter_email) = $2
		  AND feedback.created_at <= $3 AND attachment.created_at <= $3
		  AND attachment.storage_key IS NOT NULL
		UNION
		SELECT session.storage_key
		FROM sessions AS session
		JOIN feedbacks AS feedback
		  ON feedback.tenant_id = session.tenant_id AND feedback.id = session.feedback_id
		WHERE session.tenant_id = $1 AND lower(feedback.reporter_email) = $2
		  AND feedback.created_at <= $3 AND session.created_at <= $3
		  AND session.storage_key IS NOT NULL
		UNION
		SELECT batch.storage_key
		FROM session_batches AS batch
		JOIN sessions AS session
		  ON session.tenant_id = batch.tenant_id AND session.id = batch.session_id
		JOIN feedbacks AS feedback
		  ON feedback.tenant_id = session.tenant_id AND feedback.id = session.feedback_id
		WHERE batch.tenant_id = $1 AND lower(feedback.reporter_email) = $2
		  AND feedback.created_at <= $3 AND batch.created_at <= $3
		  AND batch.storage_key IS NOT NULL
	`, tenantID, email, cutoff.UTC())
	if err != nil {
		return nil, fmt.Errorf("list reporter storage objects: %w", err)
	}
	defer rows.Close()
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan reporter storage object: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reporter storage objects: %w", err)
	}
	sort.Strings(keys)
	return keys, nil
}

func reporterRecordCount(
	ctx context.Context,
	query interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	tenantID string,
	email string,
	cutoff time.Time,
) (int, error) {
	var count int
	err := query.QueryRow(ctx, `
		WITH related_feedback AS (
			SELECT id, issue_id FROM feedbacks
			WHERE tenant_id = $1 AND lower(reporter_email) = $2 AND created_at <= $3
			), related_issues AS (
				SELECT issue.id FROM issues AS issue
				WHERE issue.tenant_id = $1 AND issue.created_at <= $3 AND (
					lower(issue.reporter_email) = $2
					OR EXISTS (SELECT 1 FROM related_feedback WHERE issue_id = issue.id)
				)
			), related_invitations AS (
				SELECT invitation.id
				FROM email_feedback_invitations AS invitation
				WHERE invitation.tenant_id = $1
				  AND invitation.recipient_digest = encode(digest($2, 'sha256'), 'hex')
				  AND invitation.created_at <= $3
			)
			SELECT (SELECT count(*) FROM related_feedback)
			     + (SELECT count(*) FROM related_issues)
			     + (SELECT count(*) FROM related_invitations)
		`, tenantID, email, cutoff.UTC()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count reporter records: %w", err)
	}
	return count, nil
}

func anonymizeReporterRows(
	ctx context.Context,
	tx pgx.Tx,
	job Job,
	scope reporterScope,
	recordCount int,
	now time.Time,
) error {
	if job.ReporterRequestID == nil {
		return ErrJobUnavailable
	}
	baseArguments := []any{job.TenantID, scope.Email, scope.VerifiedAt.UTC()}
	statements := []struct {
		query      string
		includeNow bool
	}{
		{
			query: `UPDATE email_feedback_invitations
				 SET recipient_email = NULL, token_hash = NULL, context_url = NULL,
				     provider_message_id = NULL, webhook_event_id = NULL,
				     last_error = NULL, feedback_id = NULL,
				     status = CASE
				         WHEN status = 'queued'
				         THEN 'suppressed'::email_feedback_invitation_status
				         ELSE status
				     END,
				     suppressed_at = CASE
				         WHEN status = 'queued' THEN COALESCE(suppressed_at, $4)
				         ELSE suppressed_at
				     END
				 WHERE tenant_id = $1
				   AND recipient_digest = encode(digest($2, 'sha256'), 'hex')
				   AND created_at <= $3`,
			includeNow: true,
		},
		{query: `DELETE FROM ai_processing_runs AS run USING feedbacks AS feedback
		 WHERE run.tenant_id = $1 AND feedback.tenant_id = run.tenant_id
		   AND feedback.id = run.feedback_id AND lower(feedback.reporter_email) = $2
		   AND feedback.created_at <= $3 AND run.created_at <= $3`},
		{query: `DELETE FROM session_batches AS batch USING sessions AS session, feedbacks AS feedback
		 WHERE batch.tenant_id = $1 AND session.tenant_id = batch.tenant_id
		   AND session.id = batch.session_id AND feedback.tenant_id = session.tenant_id
		   AND feedback.id = session.feedback_id AND lower(feedback.reporter_email) = $2
		   AND feedback.created_at <= $3 AND batch.created_at <= $3
		   AND batch.completed_at IS NULL`},
		{query: `UPDATE session_batches AS batch
		 SET storage_key = NULL, retention_deleted_at = $4
		 FROM sessions AS session, feedbacks AS feedback
		 WHERE batch.tenant_id = $1 AND session.tenant_id = batch.tenant_id
		   AND session.id = batch.session_id AND feedback.tenant_id = session.tenant_id
		   AND feedback.id = session.feedback_id AND lower(feedback.reporter_email) = $2
		   AND feedback.created_at <= $3 AND batch.created_at <= $3
		   AND batch.completed_at IS NOT NULL`, includeNow: true},
		{query: `UPDATE sessions AS session
		 SET storage_key = NULL, retention_deleted_at = $4
		 FROM feedbacks AS feedback
		 WHERE session.tenant_id = $1 AND feedback.tenant_id = session.tenant_id
		   AND feedback.id = session.feedback_id AND lower(feedback.reporter_email) = $2
		   AND feedback.created_at <= $3 AND session.created_at <= $3`, includeNow: true},
		{query: `DELETE FROM attachments AS attachment USING feedbacks AS feedback
		 WHERE attachment.tenant_id = $1 AND feedback.tenant_id = attachment.tenant_id
		   AND feedback.id = attachment.feedback_id AND lower(feedback.reporter_email) = $2
		   AND feedback.created_at <= $3 AND attachment.created_at <= $3
		   AND attachment.completed_at IS NULL`},
		{query: `UPDATE attachments AS attachment
		 SET storage_key = NULL, retention_deleted_at = $4, transcription = NULL,
		     sentiment_score = NULL, moderation_flags = '{}', ai_redaction_manifest = '{}'
		 FROM feedbacks AS feedback
		 WHERE attachment.tenant_id = $1 AND feedback.tenant_id = attachment.tenant_id
		   AND feedback.id = attachment.feedback_id AND lower(feedback.reporter_email) = $2
		   AND feedback.created_at <= $3 AND attachment.created_at <= $3
		   AND attachment.completed_at IS NOT NULL`, includeNow: true},
		{query: `DELETE FROM reporter_access_tokens AS token USING issues AS issue
		 WHERE token.tenant_id = $1 AND issue.tenant_id = token.tenant_id
		   AND issue.id = token.issue_id AND lower(issue.reporter_email) = $2
		   AND issue.created_at <= $3`},
		{query: `UPDATE feedbacks
		 SET reporter_email = NULL, notify_reporter = false,
		     reporter_consent_withdrawn_at = $4,
		     dom_snapshot_html = NULL, dom_snapshot_styles = NULL,
		     text_cleaned = NULL, text_sentiment_score = NULL,
		     text_moderation_flags = '{}', ai_suggested_title = NULL,
		     ai_actionability = NULL, ai_category = NULL, ai_priority = NULL,
		     ai_redaction_manifest = '{}', ai_processed_at = NULL
		 WHERE tenant_id = $1 AND lower(reporter_email) = $2 AND created_at <= $3`, includeNow: true},
		{query: `UPDATE issues
		 SET reporter_email = NULL, notify_reporter = false,
		     reporter_consent_withdrawn_at = $4,
		     reporter_access_revoked_at = COALESCE(reporter_access_revoked_at, $4)
		 WHERE tenant_id = $1 AND lower(reporter_email) = $2 AND created_at <= $3`, includeNow: true},
	}
	for _, statement := range statements {
		arguments := baseArguments
		if statement.includeNow {
			arguments = append(append([]any{}, baseArguments...), now.UTC())
		}
		if _, err := tx.Exec(ctx, statement.query, arguments...); err != nil {
			return fmt.Errorf("anonymize reporter records: %w", err)
		}
	}
	// Structured identifiers are gone at this point; rescan for the free-form content that
	// still needs a documented human decision. dom_snapshot_html/styles are excluded because the
	// statements above already remove them unconditionally, not because they are safe.
	pending, err := reconcileReviewItems(ctx, tx, job.TenantID, *job.ReporterRequestID, &job.ID, now)
	if err != nil {
		return err
	}
	if pending > 0 {
		return markReporterAwaitingReview(ctx, tx, job, recordCount, pending, now)
	}
	data, err := json.Marshal(map[string]any{
		"record_count": recordCount, "manual_review_count": 0,
	})
	if err != nil {
		return fmt.Errorf("encode reporter anonymization audit: %w", err)
	}
	var completedID string
	err = tx.QueryRow(ctx, `
		WITH completed_job AS (
			UPDATE privacy_jobs
			SET status = 'completed', heartbeat_at = $5, completed_at = $5,
			    expires_at = $5::timestamptz + interval '30 days',
			    object_key = NULL, object_size_bytes = 0, error_message = NULL
			WHERE id = $1 AND tenant_id = $2 AND reporter_request_id = $3
			  AND generation = $4 AND kind = 'reporter_anonymize' AND status = 'processing'
			RETURNING id, reporter_request_id
		), completed_request AS (
			UPDATE reporter_privacy_requests AS request
			SET status = 'completed', reporter_email = NULL, completed_at = $5,
			    record_count = $6, manual_review_count = $7, error_message = NULL
			FROM completed_job
			WHERE request.tenant_id = $2 AND request.id = completed_job.reporter_request_id
			  AND request.status = 'processing' AND request.action = 'anonymize'
			RETURNING request.id
		), audited AS (
			INSERT INTO tenant_access_events (
				tenant_id, kind, actor_member_id, subject_member_id, data
			)
			SELECT $2, 'privacy.reporter_anonymization_completed', NULL, NULL,
			       $8::jsonb
			       || jsonb_build_object('privacy_job_id', completed_job.id::text)
			       || jsonb_build_object('reporter_request_id', completed_request.id::text)
			FROM completed_job CROSS JOIN completed_request
			RETURNING id
		)
		SELECT completed_job.id FROM completed_job CROSS JOIN completed_request
	`, job.ID, job.TenantID, *job.ReporterRequestID, job.Generation, now.UTC(),
		recordCount, 0, string(data)).Scan(&completedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrJobUnavailable
	}
	if err != nil {
		return fmt.Errorf("complete reporter anonymization: %w", err)
	}
	return nil
}

// markReporterAwaitingReview transitions the job and request into the non-terminal
// awaiting_manual_review state: automated deletion succeeded, but completed_at stays unset and
// nothing is communicated to the reporter as finished while flagged free-form content remains
// undecided.
func markReporterAwaitingReview(
	ctx context.Context,
	tx pgx.Tx,
	job Job,
	recordCount int,
	pendingCount int,
	now time.Time,
) error {
	data, err := json.Marshal(map[string]any{
		"record_count": recordCount, "manual_review_count": pendingCount,
	})
	if err != nil {
		return fmt.Errorf("encode reporter awaiting-review audit: %w", err)
	}
	var awaitingID string
	err = tx.QueryRow(ctx, `
		WITH awaiting_job AS (
			UPDATE privacy_jobs
			SET status = 'awaiting_manual_review', heartbeat_at = $5, error_message = NULL
			WHERE id = $1 AND tenant_id = $2 AND reporter_request_id = $3
			  AND generation = $4 AND kind = 'reporter_anonymize' AND status = 'processing'
			RETURNING id, reporter_request_id
		), awaiting_request AS (
			UPDATE reporter_privacy_requests AS request
			SET status = 'awaiting_manual_review', reporter_email = NULL,
			    record_count = $6, manual_review_count = $7, error_message = NULL
			FROM awaiting_job
			WHERE request.tenant_id = $2 AND request.id = awaiting_job.reporter_request_id
			  AND request.status = 'processing' AND request.action = 'anonymize'
			RETURNING request.id
		), audited AS (
			INSERT INTO tenant_access_events (
				tenant_id, kind, actor_member_id, subject_member_id, data
			)
			SELECT $2, 'privacy.reporter_awaiting_manual_review', NULL, NULL,
			       $8::jsonb
			       || jsonb_build_object('privacy_job_id', awaiting_job.id::text)
			       || jsonb_build_object('reporter_request_id', awaiting_request.id::text)
			FROM awaiting_job CROSS JOIN awaiting_request
			RETURNING id
		)
		SELECT awaiting_job.id FROM awaiting_job CROSS JOIN awaiting_request
	`, job.ID, job.TenantID, *job.ReporterRequestID, job.Generation, now.UTC(),
		recordCount, pendingCount, string(data)).Scan(&awaitingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrJobUnavailable
	}
	if err != nil {
		return fmt.Errorf("mark reporter anonymization awaiting review: %w", err)
	}
	return nil
}
