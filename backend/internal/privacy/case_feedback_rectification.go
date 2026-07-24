package privacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxFeedbackRectificationDescriptionLength = 10000

func (Store) ListRectifiableFeedbacks(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
) ([]RectifiableFeedback, error) {
	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
	`, tenantID, caseID))
	if err != nil {
		return nil, err
	}
	if privacyCase.RightType != RightRectification ||
		privacyCase.SubjectType != SubjectReporter ||
		privacyCase.VerifiedAt == nil ||
		privacyCase.Status == CasePendingVerification {
		return nil, ErrCaseExecutionUnsupported
	}

	rows, err := query.Query(ctx, `
		SELECT feedback.id, feedback.public_id, feedback.title,
		       COALESCE(feedback.description, feedback.text),
		       feedback.page_url, feedback.text_process_status, feedback.created_at
		FROM feedbacks AS feedback
		JOIN privacy_cases AS privacy_case
		  ON privacy_case.tenant_id = feedback.tenant_id
		 AND privacy_case.id = $2
		WHERE feedback.tenant_id = $1
		  AND feedback.reporter_email IS NOT NULL
		  AND digest(lower(btrim(feedback.reporter_email)), 'sha256')
		      = privacy_case.subject_email_digest
		  AND feedback.created_at <= privacy_case.received_at
		  AND COALESCE(feedback.description, feedback.text) IS NOT NULL
		ORDER BY feedback.created_at DESC, feedback.id DESC
		LIMIT 100
	`, tenantID, caseID)
	if err != nil {
		return nil, fmt.Errorf("list rectifiable reporter feedback: %w", err)
	}
	defer rows.Close()

	feedbacks := make([]RectifiableFeedback, 0)
	for rows.Next() {
		var feedback RectifiableFeedback
		if err := rows.Scan(
			&feedback.ID, &feedback.PublicID, &feedback.Title, &feedback.Description,
			&feedback.PageURL, &feedback.TextProcessStatus, &feedback.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan rectifiable reporter feedback: %w", err)
		}
		feedbacks = append(feedbacks, feedback)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rectifiable reporter feedback: %w", err)
	}
	return feedbacks, nil
}

func (Store) ApplyFeedbackRectification(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actorMemberID string,
	idempotencyKey string,
	input RecordFeedbackRectificationInput,
	now time.Time,
) (FeedbackRectification, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return FeedbackRectification{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	input.FeedbackID = strings.TrimSpace(input.FeedbackID)
	input.Reason = strings.TrimSpace(input.Reason)
	if idempotencyKey == "" || len(idempotencyKey) > 200 ||
		input.Reason == "" || utf8.RuneCountInString(input.Reason) > 2000 ||
		strings.TrimSpace(input.Description) == "" ||
		utf8.RuneCountInString(input.Description) > maxFeedbackRectificationDescriptionLength {
		return FeedbackRectification{}, false, ErrInvalidRequest
	}
	if _, err := uuid.Parse(input.FeedbackID); err != nil {
		return FeedbackRectification{}, false, ErrInvalidRequest
	}
	fingerprint, err := feedbackRectificationFingerprint(input)
	if err != nil {
		return FeedbackRectification{}, false, err
	}

	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
		FOR UPDATE
	`, tenantID, caseID))
	if err != nil {
		return FeedbackRectification{}, false, err
	}
	if privacyCase.RightType != RightRectification ||
		privacyCase.SubjectType != SubjectReporter ||
		privacyCase.VerifiedAt == nil ||
		privacyCase.Status == CasePendingVerification ||
		caseStatusTerminal(privacyCase.Status) {
		return FeedbackRectification{}, false, ErrCaseExecutionUnsupported
	}

	existing, storedFingerprint, err := scanFeedbackRectification(query.QueryRow(ctx, `
		SELECT `+feedbackRectificationSelectColumns("rectification")+`,
		       rectification.request_fingerprint
		FROM privacy_case_feedback_rectifications AS rectification
		WHERE rectification.tenant_id = $1
		  AND rectification.privacy_case_id = $2
		  AND rectification.idempotency_key = $3
	`, tenantID, caseID, idempotencyKey), true)
	if err == nil {
		if !bytes.Equal(storedFingerprint, fingerprint[:]) {
			return FeedbackRectification{}, false, ErrIdempotencyConflict
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return FeedbackRectification{}, false, err
	}

	var previousDescription *string
	if err := query.QueryRow(ctx, `
		SELECT COALESCE(feedback.description, feedback.text)
		FROM feedbacks AS feedback
		JOIN privacy_cases AS privacy_case
		  ON privacy_case.tenant_id = feedback.tenant_id
		 AND privacy_case.id = $2
		WHERE feedback.tenant_id = $1 AND feedback.id = $3
		  AND feedback.reporter_email IS NOT NULL
		  AND digest(lower(btrim(feedback.reporter_email)), 'sha256')
		      = privacy_case.subject_email_digest
		  AND feedback.created_at <= privacy_case.received_at
		FOR UPDATE OF feedback
	`, tenantID, caseID, input.FeedbackID).Scan(&previousDescription); errors.Is(err, pgx.ErrNoRows) {
		return FeedbackRectification{}, false, ErrNotFound
	} else if err != nil {
		return FeedbackRectification{}, false, fmt.Errorf("lock reporter feedback rectification: %w", err)
	}
	if previousDescription != nil && *previousDescription == input.Description {
		return FeedbackRectification{}, false, ErrInvalidRequest
	}

	var textProcessStatus string
	if err := query.QueryRow(ctx, `
		UPDATE feedbacks AS feedback
		SET description = $3,
		    text = $3,
		    text_cleaned = NULL,
		    text_sentiment_score = NULL,
		    text_moderation_flags = '{}',
		    text_process_status = CASE
		        WHEN EXISTS (
		            SELECT 1
		            FROM tenants AS tenant
		            JOIN widget_rules AS rule
		              ON rule.id = feedback.widget_rule_id
		             AND rule.project_id = feedback.project_id
		            WHERE tenant.id = feedback.tenant_id
		              AND tenant.plan IN ('growth', 'scale', 'enterprise')
		              AND COALESCE((rule.ai_processing->>'enabled')::boolean, false)
		              AND COALESCE(
		                  rule.ai_processing->'input_categories' ? 'feedback_text',
		                  false
		              )
		        )
		        AND NOT fidibeki_privacy_feedback_restricted(
		            feedback.tenant_id, feedback.id, 'ai'
		        )
		        THEN 'pending'::processing_status
		        ELSE 'skipped'::processing_status
		    END,
		    text_processing_generation = text_processing_generation + 1,
		    ai_job_enqueued_at = NULL,
		    ai_suggested_title = NULL,
		    ai_actionability = NULL,
		    ai_category = NULL,
		    ai_priority = NULL,
		    ai_redaction_manifest = '{}',
		    ai_processed_at = NULL,
		    ai_budget_deferred_until = NULL,
		    ai_budget_deferred_reason = NULL
		WHERE feedback.tenant_id = $1 AND feedback.id = $2
		RETURNING feedback.text_process_status
	`, tenantID, input.FeedbackID, input.Description).Scan(&textProcessStatus); err != nil {
		return FeedbackRectification{}, false, fmt.Errorf("apply reporter feedback rectification: %w", err)
	}

	rectification, _, err := scanFeedbackRectification(query.QueryRow(ctx, `
		INSERT INTO privacy_case_feedback_rectifications (
			id, tenant_id, privacy_case_id, feedback_id, previous_description,
			description, reason, derived_data_invalidated, applied_by_member_id,
			applied_at, idempotency_key, request_fingerprint, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, true, $8, $9, $10, $11, $9
		)
		RETURNING `+feedbackRectificationSelectColumns("privacy_case_feedback_rectifications")+`
	`, uuid.NewString(), tenantID, caseID, input.FeedbackID, previousDescription,
		input.Description, input.Reason, actorMemberID, now.UTC(), idempotencyKey,
		fingerprint[:]), false)
	if err != nil {
		return FeedbackRectification{}, false, fmt.Errorf("record feedback rectification: %w", err)
	}

	eventData := map[string]any{
		"rectification_id":         rectification.ID,
		"feedback_id":              input.FeedbackID,
		"fields":                   []string{"description"},
		"derived_data_invalidated": true,
		"text_process_status":      textProcessStatus,
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "feedback-rectification:"+idempotencyKey,
		"privacy_case.feedback_rectification_applied", &actorMemberID, nil, nil,
		eventData, now,
	); err != nil {
		return FeedbackRectification{}, false, err
	}
	if err := recordFeedbackRectificationAudit(
		ctx, query, tenantID, actorMemberID, caseID, input.FeedbackID, eventData,
	); err != nil {
		return FeedbackRectification{}, false, err
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (
			tenant_id, event, resource_type, resource_id, data
		) VALUES (
			$1, 'feedback.rectified', 'feedback', $2,
			jsonb_build_object(
				'privacy_case_id', $3::text,
				'rectification_id', $4::text
			)
		)
	`, tenantID, input.FeedbackID, caseID, rectification.ID); err != nil {
		return FeedbackRectification{}, false, fmt.Errorf("queue rectified feedback processing: %w", err)
	}
	if err := expireStaleReporterExports(ctx, query, tenantID, now); err != nil {
		return FeedbackRectification{}, false, err
	}
	if _, err := query.Exec(ctx, `
		UPDATE privacy_cases SET updated_at = $3
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, caseID, now.UTC()); err != nil {
		return FeedbackRectification{}, false, fmt.Errorf("touch rectification case: %w", err)
	}
	return rectification, true, nil
}

func feedbackRectificationFingerprint(
	input RecordFeedbackRectificationInput,
) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode feedback rectification fingerprint: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func recordFeedbackRectificationAudit(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	caseID string,
	feedbackID string,
	data map[string]any,
) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode feedback rectification audit: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, resource_type, resource_id,
			action, permission_path, outcome, data
		) VALUES (
			$1, 'privacy.reporter_feedback_rectification_applied', $2,
			'feedback', $3, 'rectify', 'tenant:update', 'success',
			$4::jsonb || jsonb_build_object('privacy_case_id', $5::text)
		)
	`, tenantID, actorMemberID, feedbackID, string(encoded), caseID); err != nil {
		return fmt.Errorf("record feedback rectification audit: %w", err)
	}
	return nil
}

func feedbackRectificationSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.privacy_case_id, ` +
		alias + `.feedback_id, ` + alias + `.previous_description, ` +
		alias + `.description, ` + alias + `.reason, ` +
		alias + `.derived_data_invalidated, ` + alias + `.applied_by_member_id, ` +
		alias + `.applied_at, ` + alias + `.created_at`
}

func scanFeedbackRectification(
	row rowScanner,
	withFingerprint bool,
) (FeedbackRectification, []byte, error) {
	var rectification FeedbackRectification
	var fingerprint []byte
	destinations := []any{
		&rectification.ID, &rectification.PrivacyCaseID, &rectification.FeedbackID,
		&rectification.PreviousDescription, &rectification.Description,
		&rectification.Reason, &rectification.DerivedDataInvalidated,
		&rectification.AppliedByMemberID, &rectification.AppliedAt,
		&rectification.CreatedAt,
	}
	if withFingerprint {
		destinations = append(destinations, &fingerprint)
	}
	if err := row.Scan(destinations...); errors.Is(err, pgx.ErrNoRows) {
		return FeedbackRectification{}, nil, ErrNotFound
	} else if err != nil {
		return FeedbackRectification{}, nil, fmt.Errorf("scan feedback rectification: %w", err)
	}
	return rectification, fingerprint, nil
}
