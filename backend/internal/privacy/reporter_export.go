package privacy

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

type reporterScope struct {
	RequestID  string
	Email      string
	VerifiedAt time.Time
}

var reporterExportFiles = []exportFile{
	{Name: "feedback.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', feedback.id, 'public_id', feedback.public_id,
			'project_id', feedback.project_id, 'issue_id', feedback.issue_id,
			'rating', feedback.rating, 'title', feedback.title,
			'description', feedback.description, 'text', feedback.text,
			'text_cleaned', feedback.text_cleaned, 'responses', feedback.responses,
			'ai_redaction_manifest', feedback.ai_redaction_manifest,
			'comment', feedback.comment, 'reporter_email', feedback.reporter_email,
			'reporter_consent_granted_at', feedback.reporter_consent_granted_at,
			'reporter_consent_withdrawn_at', feedback.reporter_consent_withdrawn_at,
			'reporter_consent_purpose', feedback.reporter_consent_purpose,
			'reporter_consent_notice_version', feedback.reporter_consent_notice_version,
			'reporter_consent_locale', feedback.reporter_consent_locale,
			'reporter_consent_source', feedback.reporter_consent_source,
			'page_url', feedback.page_url, 'referrer', feedback.referrer,
			'user_agent', feedback.user_agent, 'os', feedback.os,
			'browser', feedback.browser, 'device', feedback.device,
			'language', feedback.language, 'status', feedback.status,
			'created_at', feedback.created_at, 'updated_at', feedback.updated_at
		)
		FROM feedbacks AS feedback
		WHERE feedback.tenant_id = $1 AND lower(feedback.reporter_email) = $2
		  AND feedback.created_at <= $3
		ORDER BY feedback.created_at, feedback.id
	`},
	{Name: "issues.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', issue.id, 'public_id', issue.public_id, 'project_id', issue.project_id,
			'title', issue.title, 'description', issue.description, 'status', issue.status,
			'priority', issue.priority, 'reporter_email', issue.reporter_email,
			'reporter_consent_granted_at', issue.reporter_consent_granted_at,
			'reporter_consent_withdrawn_at', issue.reporter_consent_withdrawn_at,
			'reporter_consent_purpose', issue.reporter_consent_purpose,
			'reporter_consent_notice_version', issue.reporter_consent_notice_version,
			'reporter_consent_locale', issue.reporter_consent_locale,
			'reporter_consent_source', issue.reporter_consent_source,
			'resolved_at', issue.resolved_at, 'created_at', issue.created_at,
			'updated_at', issue.updated_at
		)
		FROM issues AS issue
		WHERE issue.tenant_id = $1 AND issue.created_at <= $3 AND (
			lower(issue.reporter_email) = $2 OR EXISTS (
				SELECT 1 FROM feedbacks AS feedback
				WHERE feedback.tenant_id = issue.tenant_id AND feedback.issue_id = issue.id
				  AND lower(feedback.reporter_email) = $2 AND feedback.created_at <= $3
			)
		)
		ORDER BY issue.created_at, issue.id
	`},
	{Name: "issue-updates.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', update.id, 'issue_id', update.issue_id, 'old_status', update.old_status,
			'new_status', update.new_status, 'comment', update.comment,
			'created_at', update.created_at
		)
		FROM issue_updates AS update
		WHERE update.tenant_id = $1 AND update.created_at <= $3 AND EXISTS (
			SELECT 1 FROM issues AS issue
			WHERE issue.tenant_id = update.tenant_id AND issue.id = update.issue_id
			  AND issue.created_at <= $3 AND (
				lower(issue.reporter_email) = $2 OR EXISTS (
					SELECT 1 FROM feedbacks AS feedback
					WHERE feedback.tenant_id = issue.tenant_id AND feedback.issue_id = issue.id
					  AND lower(feedback.reporter_email) = $2 AND feedback.created_at <= $3
				)
			  )
		)
		ORDER BY update.created_at, update.id
	`},
	{Name: "issue-activities.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', activity.id, 'issue_id', activity.issue_id, 'kind', activity.kind,
			'visibility', activity.visibility, 'data', activity.data,
			'created_at', activity.created_at
		)
		FROM issue_activities AS activity
		WHERE activity.tenant_id = $1 AND activity.created_at <= $3 AND EXISTS (
			SELECT 1 FROM issues AS issue
			WHERE issue.tenant_id = activity.tenant_id AND issue.id = activity.issue_id
			  AND issue.created_at <= $3 AND (
				lower(issue.reporter_email) = $2 OR EXISTS (
					SELECT 1 FROM feedbacks AS feedback
					WHERE feedback.tenant_id = issue.tenant_id AND feedback.issue_id = issue.id
					  AND lower(feedback.reporter_email) = $2 AND feedback.created_at <= $3
				)
			  )
		)
		ORDER BY activity.created_at, activity.id
	`},
	{Name: "issue-comments.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', comment.id, 'issue_id', comment.issue_id,
			'body', comment.body, 'created_at', comment.created_at
		)
		FROM issue_comments AS comment
		WHERE comment.tenant_id = $1 AND comment.created_at <= $3 AND EXISTS (
			SELECT 1 FROM issues AS issue
			WHERE issue.tenant_id = comment.tenant_id AND issue.id = comment.issue_id
			  AND issue.created_at <= $3 AND (
				lower(issue.reporter_email) = $2 OR EXISTS (
					SELECT 1 FROM feedbacks AS feedback
					WHERE feedback.tenant_id = issue.tenant_id AND feedback.issue_id = issue.id
					  AND lower(feedback.reporter_email) = $2 AND feedback.created_at <= $3
				)
			  )
		)
		ORDER BY comment.created_at, comment.id
	`},
	{Name: "clarifications.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', request.id, 'issue_id', request.issue_id, 'prompt', request.prompt,
			'response', request.response, 'responded_at', request.responded_at,
			'closed_at', request.closed_at, 'created_at', request.created_at
		)
		FROM reporter_clarification_requests AS request
		WHERE request.tenant_id = $1 AND request.created_at <= $3 AND EXISTS (
			SELECT 1 FROM issues AS issue
			WHERE issue.tenant_id = request.tenant_id AND issue.id = request.issue_id
			  AND issue.created_at <= $3 AND (
				lower(issue.reporter_email) = $2 OR EXISTS (
					SELECT 1 FROM feedbacks AS feedback
					WHERE feedback.tenant_id = issue.tenant_id AND feedback.issue_id = issue.id
					  AND lower(feedback.reporter_email) = $2 AND feedback.created_at <= $3
				)
			  )
		)
		ORDER BY request.created_at, request.id
	`},
	{Name: "attachments.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', attachment.id, 'feedback_id', attachment.feedback_id,
			'kind', attachment.kind, 'content_type', attachment.content_type,
			'size_bytes', attachment.size_bytes, 'duration_seconds', attachment.duration_seconds,
				'processing_status', attachment.processing_status,
				'transcription', attachment.transcription,
				'ai_redaction_manifest', attachment.ai_redaction_manifest,
				'completed_at', attachment.completed_at,
				'retention_deleted_at', attachment.retention_deleted_at,
				'safety_status', attachment.safety_status,
				'safety_checked_at', attachment.safety_checked_at,
				'safety_error_reason', attachment.safety_error_reason,
				'detected_content_type', attachment.detected_content_type,
				'created_at', attachment.created_at,
				'object_availability', CASE
					WHEN attachment.retention_deleted_at IS NOT NULL THEN 'retention_deleted'
					WHEN attachment.safety_status = 'safe' THEN 'available_not_included'
					ELSE attachment.safety_status::text END
		)
		FROM attachments AS attachment
		JOIN feedbacks AS feedback
		  ON feedback.tenant_id = attachment.tenant_id AND feedback.id = attachment.feedback_id
		WHERE attachment.tenant_id = $1 AND lower(feedback.reporter_email) = $2
		  AND feedback.created_at <= $3 AND attachment.created_at <= $3
		ORDER BY attachment.created_at, attachment.id
	`},
	{Name: "ai-processing-runs.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', run.id, 'project_id', run.project_id, 'feedback_id', run.feedback_id,
			'attachment_id', run.attachment_id, 'widget_rule_id', run.widget_rule_id,
			'generation', run.generation, 'attempt', run.attempt,
			'policy_version', run.policy_version, 'provider', run.provider,
			'model', run.model, 'purpose', run.purpose,
			'input_category', run.input_category, 'status', run.status,
			'redaction_manifest', run.redaction_manifest,
			'failure_reason', run.failure_reason, 'started_at', run.started_at,
			'completed_at', run.completed_at, 'created_at', run.created_at,
			'provider_file_deleted_at', (
				SELECT max(file.deleted_at) FROM ai_gemini_files AS file
				WHERE file.processing_run_id = run.id
			),
			'provider_file_delete_failed_at', (
				SELECT max(file.failed_at) FROM ai_gemini_files AS file
				WHERE file.processing_run_id = run.id
			),
			'provider_file_delete_attempts', (
				SELECT COALESCE(sum(file.delete_attempts), 0) FROM ai_gemini_files AS file
				WHERE file.processing_run_id = run.id
			)
		)
		FROM ai_processing_runs AS run
		JOIN feedbacks AS feedback
		  ON feedback.tenant_id = run.tenant_id AND feedback.id = run.feedback_id
		WHERE run.tenant_id = $1 AND lower(feedback.reporter_email) = $2
		  AND feedback.created_at <= $3 AND run.created_at <= $3
		ORDER BY run.started_at, run.id
	`},
	{Name: "replays.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', session.id, 'feedback_id', session.feedback_id,
			'page_url', session.page_url, 'started_at', session.started_at,
			'ended_at', session.ended_at, 'events_count', session.events_count,
			'batches_count', session.batches_count, 'rrweb_version', session.rrweb_version,
			'merged_at', session.merged_at, 'retention_deleted_at', session.retention_deleted_at,
			'created_at', session.created_at,
			'object_availability', CASE
				WHEN session.retention_deleted_at IS NOT NULL THEN 'retention_deleted'
				WHEN session.storage_key IS NULL THEN 'unavailable'
				ELSE 'available_not_included' END
		)
		FROM sessions AS session
		JOIN feedbacks AS feedback
		  ON feedback.tenant_id = session.tenant_id AND feedback.id = session.feedback_id
		WHERE session.tenant_id = $1 AND lower(feedback.reporter_email) = $2
		  AND feedback.created_at <= $3 AND session.created_at <= $3
		ORDER BY session.created_at, session.id
	`},
}

func (exporter Exporter) buildReporterArchive(
	ctx context.Context,
	job Job,
) (string, int64, int, int, error) {
	if exporter.Database == nil {
		return "", 0, 0, 0, errors.New("privacy export database is unavailable")
	}
	tx, err := exporter.Database.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("begin reporter export snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	scope, err := loadReporterScope(ctx, tx, job, ReporterExport)
	if err != nil {
		return "", 0, 0, 0, err
	}
	temporary, err := os.CreateTemp("", "fidibeki-reporter-export-*.zip")
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("create reporter export temp file: %w", err)
	}
	path := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	archive := zip.NewWriter(temporary)
	requestJSON, _ := json.MarshalIndent(map[string]any{
		"schema_version": "fidibeki.reporter-export.v1",
		"action":         ReporterExport, "verified_at": scope.VerifiedAt,
		"generated_at": exporter.now(),
	}, "", "  ")
	if err := writeArchiveFile(archive, "request.json", append(requestJSON, '\n')); err != nil {
		_ = archive.Close()
		return "", 0, 0, 0, err
	}
	manifest := archiveManifest{
		SchemaVersion: "fidibeki.reporter-export.v1", Scope: "reporter",
		GeneratedAt:      exporter.now(),
		Files:            []manifestFile{{Name: "request.json", Records: 1}},
		BinaryDataPolicy: "Attachment and replay binaries are not copied; their manifests describe current availability.",
		SensitiveOmissions: []string{
			"other reporters and unrelated workspace records", "authentication and verification tokens",
			"integration credentials", "billing identifiers", "internal queue payloads",
		},
	}
	recordCount := 0
	for _, definition := range reporterExportFiles {
		count, err := writeJSONL(
			ctx, tx, archive, definition, job.TenantID, scope.Email, scope.VerifiedAt,
		)
		if err != nil {
			_ = archive.Close()
			return "", 0, 0, 0, err
		}
		if definition.Name == "feedback.jsonl" || definition.Name == "issues.jsonl" {
			recordCount += count
		}
		manifest.Files = append(manifest.Files, manifestFile{Name: definition.Name, Records: count})
	}
	manualReviewCount, err := reporterManualReviewCount(
		ctx, tx, job.TenantID, scope.Email, scope.VerifiedAt,
	)
	if err != nil {
		_ = archive.Close()
		return "", 0, 0, 0, err
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = archive.Close()
		return "", 0, 0, 0, fmt.Errorf("encode reporter export manifest: %w", err)
	}
	if err := writeArchiveFile(archive, "manifest.json", append(manifestJSON, '\n')); err != nil {
		_ = archive.Close()
		return "", 0, 0, 0, err
	}
	if err := archive.Close(); err != nil {
		return "", 0, 0, 0, fmt.Errorf("close reporter export archive: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", 0, 0, 0, fmt.Errorf("sync reporter export archive: %w", err)
	}
	info, err := temporary.Stat()
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("stat reporter export archive: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, 0, 0, fmt.Errorf("commit reporter export snapshot: %w", err)
	}
	keep = true
	return path, info.Size(), recordCount, manualReviewCount, nil
}

func loadReporterScope(
	ctx context.Context,
	query interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	job Job,
	action ReporterAction,
) (reporterScope, error) {
	if job.ReporterRequestID == nil {
		return reporterScope{}, ErrJobUnavailable
	}
	var scope reporterScope
	err := query.QueryRow(ctx, `
		SELECT request.id, request.reporter_email, request.verified_at
		FROM reporter_privacy_requests AS request
		WHERE request.tenant_id = $1 AND request.id = $2
		  AND request.action = $3 AND request.status = 'processing'
		  AND request.reporter_email IS NOT NULL AND request.verified_at IS NOT NULL
	`, job.TenantID, *job.ReporterRequestID, action).Scan(
		&scope.RequestID, &scope.Email, &scope.VerifiedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return reporterScope{}, ErrJobUnavailable
	}
	if err != nil {
		return reporterScope{}, fmt.Errorf("load reporter privacy scope: %w", err)
	}
	return scope, nil
}

func (store Store) CompleteReporterExportJob(
	ctx context.Context,
	job Job,
	objectKey string,
	size int64,
	recordCount int,
	manualReviewCount int,
	now time.Time,
) error {
	if job.ReporterRequestID == nil {
		return ErrJobUnavailable
	}
	data, err := json.Marshal(map[string]any{
		"kind": job.Kind, "object_size_bytes": size,
		"record_count": recordCount, "manual_review_count": manualReviewCount,
	})
	if err != nil {
		return fmt.Errorf("encode reporter export completion audit: %w", err)
	}
	var completedID string
	err = store.Database.QueryRow(ctx, `
		WITH completed_job AS (
			UPDATE privacy_jobs
			SET status = 'completed', heartbeat_at = $5, completed_at = $5,
			    expires_at = $5::timestamptz + interval '7 days',
			    object_key = $6, object_size_bytes = $7, error_message = NULL,
			    context_privacy_version = 1
			WHERE id = $1 AND tenant_id = $2 AND reporter_request_id = $3
			  AND generation = $4 AND kind = 'reporter_export' AND status = 'processing'
			RETURNING id, reporter_request_id
		), completed_request AS (
			UPDATE reporter_privacy_requests AS request
			SET status = 'completed', completed_at = $5,
			    record_count = $8, manual_review_count = $9, error_message = NULL
			FROM completed_job
			WHERE request.tenant_id = $2 AND request.id = completed_job.reporter_request_id
			  AND request.status = 'processing' AND request.action = 'export'
			RETURNING request.id
		), audited AS (
			INSERT INTO tenant_access_events (
				tenant_id, kind, actor_member_id, subject_member_id, data
			)
			SELECT $2, 'privacy.reporter_export_completed', NULL, NULL,
			       $10::jsonb
			       || jsonb_build_object('privacy_job_id', completed_job.id::text)
			       || jsonb_build_object('reporter_request_id', completed_request.id::text)
			FROM completed_job CROSS JOIN completed_request
			RETURNING id
		)
		SELECT completed_job.id
		FROM completed_job CROSS JOIN completed_request
	`, job.ID, job.TenantID, *job.ReporterRequestID, job.Generation, now.UTC(),
		objectKey, size, recordCount, manualReviewCount, string(data)).Scan(&completedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrJobUnavailable
	}
	if err != nil {
		return fmt.Errorf("complete reporter export job: %w", err)
	}
	return nil
}

func reporterManualReviewCount(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	email string,
	cutoff time.Time,
) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `
		WITH related_feedback AS (
			SELECT id, issue_id FROM feedbacks
			WHERE tenant_id = $1 AND lower(reporter_email) = $2 AND created_at <= $3
		), related_issues AS (
			SELECT issue.id FROM issues AS issue
			WHERE issue.tenant_id = $1 AND issue.created_at <= $3 AND (
				lower(issue.reporter_email) = $2
				OR EXISTS (SELECT 1 FROM related_feedback WHERE issue_id = issue.id)
			)
		)
		SELECT
			(SELECT count(*) FROM feedbacks AS feedback
			 WHERE feedback.id IN (SELECT id FROM related_feedback)
			   AND (feedback.description IS NOT NULL OR feedback.comment IS NOT NULL
			        OR feedback.dom_snapshot_html IS NOT NULL))
			+ (SELECT count(*) FROM issues AS issue
			   WHERE issue.id IN (SELECT id FROM related_issues) AND issue.description IS NOT NULL)
			+ (SELECT count(*) FROM issue_comments AS comment
			   WHERE comment.tenant_id = $1 AND comment.issue_id IN (SELECT id FROM related_issues))
			+ (SELECT count(*) FROM reporter_clarification_requests AS request
			   WHERE request.tenant_id = $1 AND request.issue_id IN (SELECT id FROM related_issues))
	`, tenantID, email, cutoff.UTC()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count reporter manual review records: %w", err)
	}
	return count, nil
}
