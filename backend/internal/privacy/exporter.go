package privacy

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ExportStorage interface {
	PutObject(context.Context, string, io.Reader, int64, string) error
	RemoveObject(context.Context, string) error
}

type Exporter struct {
	Database *pgxpool.Pool
	Store    Store
	Storage  ExportStorage
	Logger   *slog.Logger
	Now      func() time.Time
}

type exportFile struct {
	Name                  string
	Query                 string
	PortabilityCategories []PortabilityDataCategory
}

type manifestFile struct {
	Name    string `json:"name"`
	Records int    `json:"records"`
}

type archiveManifest struct {
	SchemaVersion          string                    `json:"schema_version"`
	Scope                  string                    `json:"scope"`
	WorkspaceID            string                    `json:"workspace_id,omitempty"`
	GeneratedAt            time.Time                 `json:"generated_at"`
	Files                  []manifestFile            `json:"files"`
	BinaryDataPolicy       string                    `json:"binary_data_policy"`
	SensitiveOmissions     []string                  `json:"sensitive_omissions"`
	DiscoveryNotes         []string                  `json:"discovery_notes,omitempty"`
	PortabilityDecision    *PortabilityDecision      `json:"portability_decision,omitempty"`
	ApprovedDataCategories []PortabilityDataCategory `json:"approved_data_categories,omitempty"`
}

var workspaceExportFiles = []exportFile{
	{Name: "projects.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', project.id, 'name', project.name, 'public_key', project.public_key,
			'allowed_domains', project.allowed_domains, 'default_locale', project.default_locale,
			'reporter_access_enabled', project.reporter_access_enabled,
			'widget_appearance_overrides', project.widget_appearance_overrides,
			'created_at', project.created_at, 'updated_at', project.updated_at
		)
		FROM projects AS project
		WHERE project.tenant_id = $1
		ORDER BY project.created_at, project.id
	`},
	{Name: "widget-rules.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', rule.id, 'project_id', rule.project_id, 'name', rule.name,
			'definition_key', rule.definition_key, 'definition_version', rule.definition_version,
			'definition_status', rule.definition_status, 'presentation', rule.presentation,
			'objective', rule.objective, 'priority', rule.priority, 'enabled', rule.enabled,
			'matcher_type', rule.matcher_type, 'matcher_value', rule.matcher_value,
			'trigger_type', rule.trigger_type, 'trigger_delay_seconds', rule.trigger_delay_seconds,
			'trigger_scroll_percent', rule.trigger_scroll_percent,
			'capabilities', jsonb_build_object(
				'can_rate', rule.can_rate, 'can_write_text', rule.can_write_text,
				'can_record_audio', rule.can_record_audio, 'can_record_video', rule.can_record_video,
				'can_attach_element', rule.can_attach_element,
				'can_attach_screenshot', rule.can_attach_screenshot,
				'allow_comment', rule.allow_comment, 'collect_contact', rule.collect_contact
			),
			'greeting', rule.greeting, 'position', rule.position, 'theme', rule.theme,
			'accent_color', rule.accent_color, 'replay_mode', rule.replay_mode,
			'replay_sampling_rate', rule.replay_sampling_rate,
			'replay_max_duration_seconds', rule.replay_max_duration_seconds,
			'replay_max_events', rule.replay_max_events,
			'replay_mask_all_inputs', rule.replay_mask_all_inputs,
			'replay_mask_text_selector', rule.replay_mask_text_selector,
			'replay_record_canvas', rule.replay_record_canvas,
			'replay_canvas_fps', rule.replay_canvas_fps,
			'recurrence_mode', rule.recurrence_mode, 'recurrence_days', rule.recurrence_days,
			'response_blocks', rule.response_blocks, 'localizations', rule.localizations,
			'widget_appearance_overrides', rule.widget_appearance_overrides,
			'ai_processing', rule.ai_processing,
			'created_at', rule.created_at, 'updated_at', rule.updated_at
		)
		FROM widget_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id
		WHERE project.tenant_id = $1
		ORDER BY rule.created_at, rule.id
	`},
	{Name: "widget-target-conditions.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', condition.id, 'widget_rule_id', condition.widget_rule_id,
			'kind', condition.kind, 'matcher_type', condition.matcher_type,
			'matcher_value', condition.matcher_value, 'position', condition.position,
			'created_at', condition.created_at, 'updated_at', condition.updated_at
		)
		FROM widget_rule_target_conditions AS condition
		JOIN widget_rules AS rule ON rule.id = condition.widget_rule_id
		JOIN projects AS project ON project.id = rule.project_id
		WHERE project.tenant_id = $1
		ORDER BY condition.widget_rule_id, condition.kind, condition.position, condition.id
	`},
	{Name: "feedback.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', feedback.id, 'public_id', feedback.public_id,
			'project_id', feedback.project_id, 'widget_rule_id', feedback.widget_rule_id,
			'widget_definition_version', feedback.widget_definition_version,
			'issue_id', feedback.issue_id, 'rating', feedback.rating,
			'title', feedback.title, 'description', feedback.description,
			'text', feedback.text, 'text_cleaned', feedback.text_cleaned,
			'responses', feedback.responses, 'comment', feedback.comment,
			'priority', feedback.priority, 'assignee_id', feedback.assignee_id,
			'reporter_email', feedback.reporter_email,
			'notify_reporter', feedback.notify_reporter,
			'reporter_consent_granted_at', feedback.reporter_consent_granted_at,
			'reporter_consent_withdrawn_at', feedback.reporter_consent_withdrawn_at,
			'reporter_consent_purpose', feedback.reporter_consent_purpose,
			'reporter_consent_notice_version', feedback.reporter_consent_notice_version,
			'reporter_consent_locale', feedback.reporter_consent_locale,
			'reporter_consent_source', feedback.reporter_consent_source,
			'page_url', feedback.page_url, 'referrer', feedback.referrer,
			'user_agent', feedback.user_agent, 'os', feedback.os,
			'browser', feedback.browser, 'device', feedback.device,
			'viewport_width', feedback.viewport_width, 'viewport_height', feedback.viewport_height,
			'language', feedback.language, 'dom_snapshot_html', feedback.dom_snapshot_html,
			'dom_snapshot_styles', feedback.dom_snapshot_styles,
			'status', feedback.status, 'text_process_status', feedback.text_process_status,
			'text_processing_generation', feedback.text_processing_generation,
			'text_sentiment_score', feedback.text_sentiment_score,
			'text_moderation_flags', feedback.text_moderation_flags,
			'ai_suggested_title', feedback.ai_suggested_title,
			'ai_actionability', feedback.ai_actionability,
			'ai_category', feedback.ai_category, 'ai_priority', feedback.ai_priority,
			'ai_redaction_manifest', feedback.ai_redaction_manifest,
			'ai_processed_at', feedback.ai_processed_at,
			'created_at', feedback.created_at, 'updated_at', feedback.updated_at
		)
		FROM feedbacks AS feedback
		WHERE feedback.tenant_id = $1
		ORDER BY feedback.created_at, feedback.id
	`},
	{Name: "feedback-tags.jsonl", Query: `
		SELECT jsonb_build_object(
			'feedback_id', relation.feedback_id, 'tag_id', relation.tag_id,
			'created_at', relation.created_at
		)
		FROM feedback_tags AS relation
		WHERE relation.tenant_id = $1
		ORDER BY relation.feedback_id, relation.tag_id
	`},
	{Name: "issues.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', issue.id, 'public_id', issue.public_id, 'project_id', issue.project_id,
			'title', issue.title, 'description', issue.description, 'status', issue.status,
			'priority', issue.priority, 'assignee_id', issue.assignee_id,
			'merged_into_id', issue.merged_into_id, 'merged_at', issue.merged_at,
			'reporter_email', issue.reporter_email, 'notify_reporter', issue.notify_reporter,
			'reporter_consent_granted_at', issue.reporter_consent_granted_at,
			'reporter_consent_withdrawn_at', issue.reporter_consent_withdrawn_at,
			'reporter_consent_purpose', issue.reporter_consent_purpose,
			'reporter_consent_notice_version', issue.reporter_consent_notice_version,
			'reporter_consent_locale', issue.reporter_consent_locale,
			'reporter_consent_source', issue.reporter_consent_source,
			'reporter_access_revoked_at', issue.reporter_access_revoked_at,
			'resolved_at', issue.resolved_at, 'created_at', issue.created_at,
			'updated_at', issue.updated_at
		)
		FROM issues AS issue
		WHERE issue.tenant_id = $1
		ORDER BY issue.created_at, issue.id
	`},
	{Name: "issue-tags.jsonl", Query: `
		SELECT jsonb_build_object(
			'issue_id', relation.issue_id, 'tag_id', relation.tag_id,
			'created_at', relation.created_at
		)
		FROM issue_tags AS relation
		WHERE relation.tenant_id = $1
		ORDER BY relation.issue_id, relation.tag_id
	`},
	{Name: "issue-updates.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', update.id, 'issue_id', update.issue_id, 'member_id', update.member_id,
			'old_status', update.old_status, 'new_status', update.new_status,
			'comment', update.comment, 'created_at', update.created_at
		)
		FROM issue_updates AS update
		WHERE update.tenant_id = $1
		ORDER BY update.created_at, update.id
	`},
	{Name: "issue-activities.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', activity.id, 'issue_id', activity.issue_id,
			'actor_member_id', activity.actor_member_id, 'kind', activity.kind,
			'visibility', activity.visibility, 'data', activity.data,
			'created_at', activity.created_at
		)
		FROM issue_activities AS activity
		WHERE activity.tenant_id = $1
		ORDER BY activity.created_at, activity.id
	`},
	{Name: "issue-comments.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', comment.id, 'issue_id', comment.issue_id,
			'author_member_id', comment.author_member_id, 'body', comment.body,
			'created_at', comment.created_at
		)
		FROM issue_comments AS comment
		WHERE comment.tenant_id = $1
		ORDER BY comment.created_at, comment.id
	`},
	{Name: "tags.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', tag.id, 'name', tag.name, 'color', tag.color,
			'created_at', tag.created_at, 'updated_at', tag.updated_at
		)
		FROM tags AS tag
		WHERE tag.tenant_id = $1
		ORDER BY tag.created_at, tag.id
	`},
	{Name: "attachments.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', attachment.id, 'feedback_id', attachment.feedback_id,
			'kind', attachment.kind, 'content_type', attachment.content_type,
			'size_bytes', attachment.size_bytes, 'duration_seconds', attachment.duration_seconds,
			'processing_status', attachment.processing_status,
			'error_message', attachment.error_message,
			'transcription', attachment.transcription,
			'sentiment_score', attachment.sentiment_score,
			'moderation_flags', attachment.moderation_flags,
				'ai_redaction_manifest', attachment.ai_redaction_manifest,
				'processing_attempts', attachment.processing_attempts,
				'completed_at', attachment.completed_at, 'processed_at', attachment.processed_at,
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
		WHERE attachment.tenant_id = $1
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
		WHERE run.tenant_id = $1
		ORDER BY run.started_at, run.id
	`},
	{Name: "replays.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', session.id, 'project_id', session.project_id,
			'feedback_id', session.feedback_id, 'session_id', session.session_id,
			'page_url', session.page_url, 'started_at', session.started_at,
			'ended_at', session.ended_at, 'events_count', session.events_count,
			'batches_count', session.batches_count, 'rrweb_version', session.rrweb_version,
			'merged_at', session.merged_at, 'merge_attempts', session.merge_attempts,
			'merge_error', session.merge_error, 'created_at', session.created_at,
			'object_availability', CASE WHEN session.storage_key IS NULL THEN 'unavailable' ELSE 'available_not_included' END
		)
		FROM sessions AS session
		WHERE session.tenant_id = $1
		ORDER BY session.created_at, session.id
	`},
	{Name: "widget-analytics-daily.jsonl", Query: `
		SELECT jsonb_build_object(
			'project_id', daily.project_id, 'rule_id', daily.rule_id,
			'rule_name', daily.rule_name, 'page_url', daily.page_url,
			'event_date', daily.event_date, 'matched_count', daily.matched_count,
			'impression_count', daily.impression_count,
			'interaction_count', daily.interaction_count,
			'submitted_count', daily.submitted_count,
			'dismissed_count', daily.dismissed_count, 'updated_at', daily.updated_at
		)
		FROM widget_funnel_daily AS daily
		WHERE daily.tenant_id = $1
		ORDER BY daily.event_date, daily.project_id, daily.rule_id, daily.page_key
	`},
	{Name: "integrations.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', integration.id, 'kind', integration.kind, 'name', integration.name,
			'channel', integration.channel, 'events', integration.events,
			'is_active', integration.is_active,
			'created_at', integration.created_at, 'updated_at', integration.updated_at,
			'credential_values_omitted', true
		)
		FROM integrations AS integration
		WHERE integration.tenant_id = $1
		ORDER BY integration.created_at, integration.id
	`},
}

func (exporter Exporter) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload struct {
			JobID      string `json:"job_id"`
			TenantID   string `json:"tenant_id"`
			Generation int    `json:"generation"`
		}
		if err := json.Unmarshal(task.Payload(), &payload); err != nil ||
			payload.JobID == "" || payload.TenantID == "" || payload.Generation < 1 {
			return fmt.Errorf("decode privacy export task: %w", asynq.SkipRetry)
		}
		now := exporter.now()
		job, claimed, err := exporter.Store.ClaimJob(
			ctx, payload.JobID, payload.TenantID, payload.Generation, now,
		)
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		if exporter.Storage == nil {
			return exporter.fail(ctx, job, errors.New("privacy export storage is unavailable"))
		}
		if job.Kind == JobReporterAnonymize {
			return exporter.anonymizeReporter(ctx, job)
		}
		if job.Kind != JobWorkspaceExport && job.Kind != JobReporterExport {
			return exporter.fail(ctx, job, fmt.Errorf("unsupported privacy job kind %q", job.Kind))
		}
		path, size, recordCount, manualReviewCount, err := exporter.buildArchive(ctx, job)
		if err != nil {
			return exporter.fail(ctx, job, err)
		}
		defer func() {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				exporter.logger().WarnContext(ctx, "remove privacy export temp file", "error", err)
			}
		}()
		file, err := os.Open(path)
		if err != nil {
			return exporter.fail(ctx, job, fmt.Errorf("open privacy archive: %w", err))
		}
		objectKey := fmt.Sprintf("tenants/%s/exports/%s.zip", job.TenantID, job.ID)
		uploadErr := exporter.Storage.PutObject(ctx, objectKey, file, size, "application/zip")
		closeErr := file.Close()
		if uploadErr != nil || closeErr != nil {
			return exporter.fail(ctx, job, errors.Join(uploadErr, closeErr))
		}
		if job.Kind == JobReporterExport {
			err = exporter.Store.CompleteReporterExportJob(
				ctx, job, objectKey, size, recordCount, manualReviewCount, exporter.now(),
			)
		} else {
			err = exporter.Store.CompleteJob(ctx, job, objectKey, size, exporter.now())
		}
		if err != nil {
			return fmt.Errorf("persist privacy export completion: %w", err)
		}
		return nil
	}
}

func (exporter Exporter) buildArchive(
	ctx context.Context,
	job Job,
) (string, int64, int, int, error) {
	if job.Kind == JobReporterExport {
		return exporter.buildReporterArchive(ctx, job)
	}
	path, size, err := exporter.buildWorkspaceArchive(ctx, job)
	return path, size, 0, 0, err
}

func (exporter Exporter) buildWorkspaceArchive(ctx context.Context, job Job) (string, int64, error) {
	if exporter.Database == nil {
		return "", 0, errors.New("privacy export database is unavailable")
	}
	tx, err := exporter.Database.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return "", 0, fmt.Errorf("begin privacy export snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	temporary, err := os.CreateTemp("", "fidibeki-export-*.zip")
	if err != nil {
		return "", 0, fmt.Errorf("create privacy export temp file: %w", err)
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
	workspace, err := workspaceJSON(ctx, tx, job.TenantID)
	if err != nil {
		_ = archive.Close()
		return "", 0, err
	}
	if err := writeArchiveFile(archive, "workspace.json", append(workspace, '\n')); err != nil {
		_ = archive.Close()
		return "", 0, err
	}
	manifest := archiveManifest{
		SchemaVersion: "fidibeki.workspace-export.v1", Scope: "workspace",
		WorkspaceID: job.TenantID, GeneratedAt: exporter.now(),
		Files:            []manifestFile{{Name: "workspace.json", Records: 1}},
		BinaryDataPolicy: "Attachment and replay binaries are not copied; their manifests describe current availability.",
		SensitiveOmissions: []string{
			"API keys and token hashes", "authentication sessions and magic links",
			"encrypted integration credentials", "billing provider identifiers", "internal queue payloads",
		},
	}
	for _, definition := range workspaceExportFiles {
		count, err := writeJSONL(ctx, tx, archive, definition, job.TenantID)
		if err != nil {
			_ = archive.Close()
			return "", 0, err
		}
		manifest.Files = append(manifest.Files, manifestFile{Name: definition.Name, Records: count})
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = archive.Close()
		return "", 0, fmt.Errorf("encode privacy export manifest: %w", err)
	}
	if err := writeArchiveFile(archive, "manifest.json", append(manifestJSON, '\n')); err != nil {
		_ = archive.Close()
		return "", 0, err
	}
	if err := archive.Close(); err != nil {
		return "", 0, fmt.Errorf("close privacy export archive: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", 0, fmt.Errorf("sync privacy export archive: %w", err)
	}
	info, err := temporary.Stat()
	if err != nil {
		return "", 0, fmt.Errorf("stat privacy export archive: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, fmt.Errorf("commit privacy export snapshot: %w", err)
	}
	keep = true
	return path, info.Size(), nil
}

func workspaceJSON(ctx context.Context, tx pgx.Tx, tenantID string) ([]byte, error) {
	var encoded []byte
	err := tx.QueryRow(ctx, `
		SELECT jsonb_build_object(
			'id', tenant.id, 'name', tenant.name, 'slug', tenant.slug,
			'plan', tenant.plan, 'status', tenant.status,
			'trial_ends_at', tenant.trial_ends_at,
			'deletion_requested_at', tenant.deletion_requested_at,
			'reporter_access_enabled', tenant.reporter_access_enabled,
			'widget_appearance_defaults', tenant.widget_appearance_defaults,
			'created_at', tenant.created_at, 'updated_at', tenant.updated_at
		)
		FROM tenants AS tenant WHERE tenant.id = $1
	`, tenantID).Scan(&encoded)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("export workspace identity: %w", err)
	}
	var compact bytes.Buffer
	if err := json.Indent(&compact, encoded, "", "  "); err != nil {
		return nil, fmt.Errorf("format workspace export identity: %w", err)
	}
	return compact.Bytes(), nil
}

func writeJSONL(
	ctx context.Context,
	tx pgx.Tx,
	archive *zip.Writer,
	definition exportFile,
	arguments ...any,
) (int, error) {
	entry, err := archive.CreateHeader(&zip.FileHeader{
		Name: definition.Name, Method: zip.Deflate,
	})
	if err != nil {
		return 0, fmt.Errorf("create privacy export file %s: %w", definition.Name, err)
	}
	rows, err := tx.Query(ctx, definition.Query, arguments...)
	if err != nil {
		return 0, fmt.Errorf("query privacy export file %s: %w", definition.Name, err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var encoded []byte
		if err := rows.Scan(&encoded); err != nil {
			return count, fmt.Errorf("scan privacy export file %s: %w", definition.Name, err)
		}
		if _, err := entry.Write(append(encoded, '\n')); err != nil {
			return count, fmt.Errorf("write privacy export file %s: %w", definition.Name, err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("iterate privacy export file %s: %w", definition.Name, err)
	}
	return count, nil
}

func writeArchiveFile(archive *zip.Writer, name string, body []byte) error {
	header := &zip.FileHeader{Name: filepath.ToSlash(name), Method: zip.Deflate}
	entry, err := archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create privacy archive file %s: %w", name, err)
	}
	if _, err := entry.Write(body); err != nil {
		return fmt.Errorf("write privacy archive file %s: %w", name, err)
	}
	return nil
}

func (exporter Exporter) fail(ctx context.Context, job Job, failure error) error {
	message := "privacy export failed"
	if failure != nil {
		message = failure.Error()
	}
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := exporter.Store.MarkJobFailed(markCtx, job, message, exporter.now()); err != nil {
		return errors.Join(failure, err)
	}
	exporter.logger().ErrorContext(ctx, "privacy export attempt failed",
		"job_id", job.ID, "tenant_id", job.TenantID, "attempt", job.Attempts, "error", failure)
	return nil
}

func (exporter Exporter) now() time.Time {
	if exporter.Now != nil {
		return exporter.Now().UTC()
	}
	return time.Now().UTC()
}

func (exporter Exporter) logger() *slog.Logger {
	if exporter.Logger != nil {
		return exporter.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
