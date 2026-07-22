package feedback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound          = errors.New("feedback resource not found")
	ErrPublicIDCollision = errors.New("feedback public ID collision")
	ErrInvalidReplay     = errors.New("replay session cannot be linked to feedback")
	ErrLinkConflict      = errors.New("feedback is linked to another issue")
	ErrProjectMismatch   = errors.New("feedback and issue projects do not match")
	ErrMergedIssue       = errors.New("merged issue cannot receive feedback")
)

type PreparedAttachment struct {
	Attachment
	CompleteTokenHash string
	ProcessingStatus  string
}

type Store struct{}

func (Store) LockReplaySession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	sessionID string,
) error {
	var feedbackID *string
	err := query.QueryRow(ctx, `
		SELECT feedback_id FROM sessions
		WHERE tenant_id = $1 AND project_id = $2 AND id = $3
		FOR UPDATE
	`, tenantID, projectID, sessionID).Scan(&feedbackID)
	if errors.Is(err, pgx.ErrNoRows) || feedbackID != nil {
		return ErrInvalidReplay
	}
	if err != nil {
		return fmt.Errorf("lock feedback replay session: %w", err)
	}
	return nil
}

func (Store) LockSubmission(
	ctx context.Context,
	query db.Querier,
	projectID string,
	clientFeedbackID string,
) error {
	if _, err := query.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, projectID+":"+clientFeedbackID,
	); err != nil {
		return fmt.Errorf("lock feedback idempotency key: %w", err)
	}
	return nil
}

func (Store) FindSubmission(
	ctx context.Context,
	query db.Querier,
	projectID string,
	clientFeedbackID string,
) (Submission, bool, error) {
	var submission Submission
	err := query.QueryRow(ctx, `
		SELECT feedback.id, feedback.public_id, COALESCE(issue.public_id, ''), feedback.notify_reporter
		FROM feedbacks AS feedback
		LEFT JOIN issues AS issue ON issue.id = feedback.issue_id
		WHERE feedback.project_id = $1 AND feedback.client_feedback_id = $2
	`, projectID, clientFeedbackID).Scan(
		&submission.ID, &submission.PublicID, &submission.IssuePublicID, &submission.NotifyReporter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Submission{}, false, nil
	}
	if err != nil {
		return Submission{}, false, fmt.Errorf("find idempotent feedback: %w", err)
	}
	attachments, err := listAttachments(ctx, query, submission.ID)
	if err != nil {
		return Submission{}, false, err
	}
	submission.Attachments = attachments
	return submission, true, nil
}

func (Store) CountMonthly(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	now time.Time,
) (int, error) {
	start := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	var count int
	if err := query.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks
		WHERE tenant_id = $1 AND created_at >= $2 AND created_at < $3
	`, tenantID, start, end).Scan(&count); err != nil {
		return 0, fmt.Errorf("count monthly feedback: %w", err)
	}
	return count, nil
}

func (Store) CreateSubmission(
	ctx context.Context,
	query db.Querier,
	configuration project.PublicConfiguration,
	values SubmissionValues,
	feedbackID string,
	publicIDCandidates []string,
	attachments []PreparedAttachment,
) (Submission, error) {
	title := feedbackTitle(values.Text)
	publicID := ""
	for _, candidate := range publicIDCandidates {
		err := query.QueryRow(ctx, `
			INSERT INTO feedbacks (
				id, tenant_id, project_id, widget_rule_id, widget_definition_version, public_id,
				client_feedback_id, rating, title, description, text, responses, comment,
				reporter_email, notify_reporter,
				page_url, referrer, user_agent, os, browser, device,
				viewport_width, viewport_height, language, dom_snapshot_html, dom_snapshot_styles,
				text_process_status, session_id
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $11, $12, $13, $14,
				$15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27
			)
			ON CONFLICT (tenant_id, public_id) DO NOTHING
			RETURNING public_id
		`, feedbackID, configuration.TenantID, configuration.ProjectID, values.WidgetRuleID,
			values.DefinitionVersion, candidate, values.ClientFeedbackID, values.Rating, title, values.Text,
			values.Responses, values.Comment, values.ReporterEmail, values.NotifyReporter, values.PageURL, values.Referrer,
			values.UserAgent, values.OS, values.Browser, values.Device, values.ViewportWidth,
			values.ViewportHeight, values.Language, values.DOMSnapshotHTML, values.DOMSnapshotStyles,
			textProcessingStatus(values.Text, configuration.Plan), values.ReplaySessionID,
		).Scan(&publicID)
		if err == nil {
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Submission{}, fmt.Errorf("create feedback: %w", err)
		}
	}
	if publicID == "" {
		return Submission{}, ErrPublicIDCollision
	}
	if values.ReplaySessionID != nil {
		tag, err := query.Exec(ctx, `
			UPDATE sessions SET feedback_id = $2
			WHERE id = $1 AND tenant_id = $3 AND project_id = $4 AND feedback_id IS NULL
		`, *values.ReplaySessionID, feedbackID, configuration.TenantID, configuration.ProjectID)
		if err != nil {
			return Submission{}, fmt.Errorf("link replay session to feedback: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return Submission{}, ErrInvalidReplay
		}
	}

	createdAttachments := make([]Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		if _, err := query.Exec(ctx, `
			INSERT INTO attachments (
				id, tenant_id, feedback_id, kind, client_attachment_id, storage_key,
				content_type, size_bytes, duration_seconds, processing_status, complete_token_hash
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, attachment.ID, configuration.TenantID, feedbackID, attachment.Kind,
			attachment.ClientAttachmentID, attachment.StorageKey, attachment.ContentType,
			attachment.SizeBytes, attachment.DurationSeconds, attachment.ProcessingStatus,
			attachment.CompleteTokenHash); err != nil {
			return Submission{}, fmt.Errorf("create feedback attachment: %w", err)
		}
		createdAttachments = append(createdAttachments, attachment.Attachment)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id)
		VALUES ($1, 'feedback.created', 'feedback', $2)
	`, configuration.TenantID, feedbackID); err != nil {
		return Submission{}, fmt.Errorf("create feedback outbox event: %w", err)
	}
	return Submission{
		ID: feedbackID, PublicID: publicID,
		NotifyReporter: values.NotifyReporter, Attachments: createdAttachments,
	}, nil
}

func (Store) RotateCompletionToken(
	ctx context.Context,
	query db.Querier,
	attachmentID string,
	tokenHash string,
	refreshedAt time.Time,
) error {
	var feedbackID string
	var pendingAI bool
	err := query.QueryRow(ctx, `
		UPDATE attachments
		SET complete_token_hash = $2,
		    upload_refreshed_at = $3,
		    processing_status = CASE
		        WHEN processing_status = 'failed' THEN 'pending'::processing_status
		        ELSE processing_status
		    END,
		    error_message = CASE WHEN processing_status = 'failed' THEN NULL ELSE error_message END,
		    processed_at = CASE WHEN processing_status = 'failed' THEN NULL ELSE processed_at END
		WHERE id = $1 AND completed_at IS NULL
		RETURNING feedback_id, processing_status = 'pending'
	`, attachmentID, tokenHash, refreshedAt.UTC()).Scan(&feedbackID, &pendingAI)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("rotate attachment completion token: %w", err)
	}
	if pendingAI {
		if _, err := query.Exec(ctx, `UPDATE feedbacks SET ai_processed_at = NULL WHERE id = $1`, feedbackID); err != nil {
			return fmt.Errorf("reset feedback AI after attachment retry: %w", err)
		}
	}
	return nil
}

func (Store) LockAttachment(
	ctx context.Context,
	query db.Querier,
	projectID string,
	attachmentID string,
) (Attachment, error) {
	var attachment Attachment
	var completeTokenHash *string
	var completedAt *time.Time
	err := query.QueryRow(ctx, `
		SELECT attachment.id, attachment.feedback_id, attachment.client_attachment_id, attachment.kind,
		       attachment.storage_key, attachment.content_type, attachment.size_bytes,
		       attachment.duration_seconds, attachment.complete_token_hash, attachment.completed_at
		FROM attachments AS attachment
		JOIN feedbacks AS feedback ON feedback.id = attachment.feedback_id
		WHERE feedback.project_id = $1 AND attachment.id = $2
		FOR UPDATE OF attachment
	`, projectID, attachmentID).Scan(
		&attachment.ID, &attachment.FeedbackID, &attachment.ClientAttachmentID, &attachment.Kind,
		&attachment.StorageKey, &attachment.ContentType, &attachment.SizeBytes,
		&attachment.DurationSeconds, &completeTokenHash, &completedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, ErrNotFound
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("lock attachment: %w", err)
	}
	attachment.CompleteTokenHash = completeTokenHash
	attachment.CompletedAt = completedAt
	return attachment, nil
}

func (Store) CompleteAttachment(
	ctx context.Context,
	query db.Querier,
	attachmentID string,
	completedAt time.Time,
) error {
	var feedbackID string
	var pendingAI bool
	err := query.QueryRow(ctx, `
		UPDATE attachments
		SET complete_token_hash = NULL,
		    completed_at = $2,
		    processing_status = CASE
		        WHEN processing_status = 'failed' THEN 'pending'::processing_status
		        ELSE processing_status
		    END,
		    error_message = CASE WHEN processing_status = 'failed' THEN NULL ELSE error_message END,
		    processed_at = CASE WHEN processing_status = 'failed' THEN NULL ELSE processed_at END
		WHERE id = $1 AND completed_at IS NULL AND complete_token_hash IS NOT NULL
		RETURNING feedback_id, processing_status = 'pending'
	`, attachmentID, completedAt.UTC()).Scan(&feedbackID, &pendingAI)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("complete attachment: %w", err)
	}
	if pendingAI {
		if _, err := query.Exec(ctx, `UPDATE feedbacks SET ai_processed_at = NULL WHERE id = $1`, feedbackID); err != nil {
			return fmt.Errorf("reset feedback AI after attachment completion: %w", err)
		}
	}
	return nil
}

func listAttachments(ctx context.Context, query db.Querier, feedbackID string) ([]Attachment, error) {
	rows, err := query.Query(ctx, `
		SELECT id, client_attachment_id, kind, storage_key, content_type, size_bytes,
		       duration_seconds, complete_token_hash, completed_at
		FROM attachments WHERE feedback_id = $1 ORDER BY created_at, id
	`, feedbackID)
	if err != nil {
		return nil, fmt.Errorf("list feedback attachments: %w", err)
	}
	defer rows.Close()
	attachments := make([]Attachment, 0)
	for rows.Next() {
		var attachment Attachment
		if err := rows.Scan(
			&attachment.ID, &attachment.ClientAttachmentID, &attachment.Kind,
			&attachment.StorageKey, &attachment.ContentType, &attachment.SizeBytes,
			&attachment.DurationSeconds, &attachment.CompleteTokenHash, &attachment.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan feedback attachment: %w", err)
		}
		attachments = append(attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feedback attachments: %w", err)
	}
	return attachments, nil
}

func feedbackTitle(text *string) string {
	if text == nil || strings.TrimSpace(*text) == "" {
		return "Feedback without written detail"
	}
	title := strings.TrimSpace(*text)
	for index, character := range title {
		if character == '\n' || character == '\r' {
			title = strings.TrimSpace(title[:index])
			break
		}
		if character == '.' || character == '!' || character == '?' {
			title = strings.TrimSpace(title[:index+len(string(character))])
			break
		}
	}
	if utf8.RuneCountInString(title) <= 80 {
		return title
	}
	return string([]rune(title)[:80])
}

func textProcessingStatus(text *string, plan tenant.Plan) string {
	if text == nil || strings.TrimSpace(*text) == "" {
		return "skipped"
	}
	definition, exists := config.PlanFor(plan)
	if !exists || !definition.Features.AI {
		return "skipped"
	}
	return "pending"
}

func NewID() string {
	return uuid.NewString()
}
