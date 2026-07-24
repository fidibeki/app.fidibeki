package privacy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReporterFeedbackRectificationInvalidatesDerivedData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 19, 0, 0, 0, time.UTC)
	tenantID, actorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "feedback-rectification", "Original reporter description",
	)
	var feedbackID, issueID, originalTitle string
	if err := database.QueryRow(ctx, `
		SELECT id, issue_id, title FROM feedbacks WHERE tenant_id = $1
	`, tenantID).Scan(&feedbackID, &issueID, &originalTitle); err != nil {
		t.Fatalf("read feedback rectification fixture: %v", err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE tenants SET plan = 'growth' WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("enable feedback rectification plan: %v", err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE widget_rules
		SET ai_processing = jsonb_build_object(
			'enabled', true,
			'policy_version', '2026-07-24',
			'provider', 'google_gemini',
			'purposes', jsonb_build_array(
				'cleanup', 'moderation', 'sentiment', 'triage_recommendations'
			),
			'input_categories', jsonb_build_array('feedback_text'),
			'privacy_policy_url', 'https://feedback-rectification.test/privacy',
			'sensitive_data_mode', 'prohibit_and_quarantine',
			'acknowledged', true,
			'acknowledged_at', '2026-07-24T18:00:00Z',
			'acknowledged_by_member_id', $2::text
		)
		WHERE project_id = (SELECT project_id FROM feedbacks WHERE id = $1)
	`, feedbackID, actorMemberID); err != nil {
		t.Fatalf("enable feedback rectification AI: %v", err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE feedbacks
		SET created_at = $3::timestamptz - interval '1 hour',
		    text_cleaned = 'Cleaned old description',
		    text_sentiment_score = 0.25,
		    text_moderation_flags = ARRAY['old_flag'],
		    ai_suggested_title = 'Old suggestion',
		    ai_actionability = 'actionable',
		    ai_category = 'bug',
		    ai_priority = 'high',
		    ai_redaction_manifest = '{"email":"masked"}',
		    ai_processed_at = $3,
		    text_processing_generation = 4
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, feedbackID, now); err != nil {
		t.Fatalf("prepare feedback rectification fixture: %v", err)
	}
	var staleExportID string
	if err := database.QueryRow(ctx, `
		INSERT INTO privacy_jobs (
			tenant_id, kind, status, actor_member_id, started_at, heartbeat_at,
			completed_at, expires_at, object_key, object_size_bytes
		) VALUES (
			$1, 'workspace_export', 'completed', $2,
			$3::timestamptz - interval '2 hours',
			$3::timestamptz - interval '2 hours',
			$3::timestamptz - interval '1 hour',
			$3::timestamptz + interval '7 days',
			'tenants/stale/exports/stale.zip', 128
		)
		RETURNING id
	`, tenantID, actorMemberID, now).Scan(&staleExportID); err != nil {
		t.Fatalf("create stale rectification export: %v", err)
	}

	store := Store{Database: database}
	privacyCase, token, _, err := store.CreateCase(
		ctx, database, tenantID, actorMemberID, "feedback-rectification-case",
		CreateCaseInput{
			SubjectType: SubjectReporter, RightType: RightRectification,
			ControllerRole: RoleCustomerControllerAssistance,
			SubjectEmail:   "reporter@example.test", OwnerMemberID: actorMemberID,
		},
		now,
	)
	if err != nil {
		t.Fatalf("create reporter rectification case: %v", err)
	}
	tokenHash, ok := parseReporterRequestToken(token)
	if !ok {
		t.Fatalf("parse reporter rectification verification token: %q", token)
	}
	if _, err := store.VerifyCase(ctx, database, tokenHash, now.Add(time.Minute)); err != nil {
		t.Fatalf("verify reporter rectification case: %v", err)
	}

	candidates, err := store.ListRectifiableFeedbacks(
		ctx, database, tenantID, privacyCase.ID,
	)
	if err != nil || len(candidates) != 1 || candidates[0].ID != feedbackID ||
		candidates[0].Description == nil ||
		*candidates[0].Description != "Original reporter description" {
		t.Fatalf("rectifiable feedbacks = %#v, error=%v", candidates, err)
	}

	input := RecordFeedbackRectificationInput{
		FeedbackID:  feedbackID,
		Description: "Corrected reporter description with the complete context.",
		Reason:      "The verified reporter corrected the original statement.",
	}
	rectification, created, err := store.ApplyFeedbackRectification(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"apply-feedback-rectification", input, now.Add(2*time.Minute),
	)
	if err != nil || !created {
		t.Fatalf("ApplyFeedbackRectification() = %#v created=%t error=%v",
			rectification, created, err)
	}
	if rectification.FeedbackID != feedbackID ||
		rectification.PreviousDescription == nil ||
		*rectification.PreviousDescription != "Original reporter description" ||
		rectification.Description != input.Description ||
		!rectification.DerivedDataInvalidated {
		t.Fatalf("feedback rectification = %#v", rectification)
	}

	var description, text, title, textStatus string
	var generation int64
	var textCleaned, suggestedTitle, actionability, category, priority *string
	var sentiment *float64
	var moderationFlags []string
	var redactionManifest []byte
	var processedAt *time.Time
	if err := database.QueryRow(ctx, `
		SELECT description, text, title, text_process_status,
		       text_processing_generation, text_cleaned, text_sentiment_score,
		       text_moderation_flags, ai_suggested_title, ai_actionability,
		       ai_category, ai_priority, ai_redaction_manifest, ai_processed_at
		FROM feedbacks WHERE tenant_id = $1 AND id = $2
	`, tenantID, feedbackID).Scan(
		&description, &text, &title, &textStatus, &generation, &textCleaned,
		&sentiment, &moderationFlags, &suggestedTitle, &actionability,
		&category, &priority, &redactionManifest, &processedAt,
	); err != nil {
		t.Fatalf("read rectified feedback: %v", err)
	}
	if description != input.Description || text != input.Description ||
		title != originalTitle || textStatus != "pending" || generation != 5 ||
		textCleaned != nil || sentiment != nil || len(moderationFlags) != 0 ||
		suggestedTitle != nil || actionability != nil || category != nil ||
		priority != nil || string(redactionManifest) != "{}" || processedAt != nil {
		t.Fatalf(
			"rectified feedback description=%q text=%q title=%q status=%q generation=%d "+
				"cleaned=%v sentiment=%v flags=%v suggestion=%v actionability=%v "+
				"category=%v priority=%v manifest=%s processed=%v",
			description, text, title, textStatus, generation, textCleaned, sentiment,
			moderationFlags, suggestedTitle, actionability, category, priority,
			redactionManifest, processedAt,
		)
	}
	var issueDescription string
	if err := database.QueryRow(ctx, `
		SELECT description FROM issues WHERE tenant_id = $1 AND id = $2
	`, tenantID, issueID).Scan(&issueDescription); err != nil {
		t.Fatalf("read linked issue after feedback rectification: %v", err)
	}
	if issueDescription != "Issue description" {
		t.Fatalf("linked issue description = %q, want unchanged", issueDescription)
	}
	var staleExportExpiresAt time.Time
	if err := database.QueryRow(ctx, `
		SELECT expires_at FROM privacy_jobs WHERE tenant_id = $1 AND id = $2
	`, tenantID, staleExportID).Scan(&staleExportExpiresAt); err != nil {
		t.Fatalf("read stale rectification export: %v", err)
	}
	if !staleExportExpiresAt.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("stale rectification export expires_at = %s, want %s",
			staleExportExpiresAt, now.Add(2*time.Minute))
	}

	replayed, created, err := store.ApplyFeedbackRectification(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"apply-feedback-rectification", input, now.Add(3*time.Minute),
	)
	if err != nil || created || replayed.ID != rectification.ID {
		t.Fatalf("idempotent feedback rectification = %#v created=%t error=%v",
			replayed, created, err)
	}
	if _, _, err := store.ApplyFeedbackRectification(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"apply-feedback-rectification",
		RecordFeedbackRectificationInput{
			FeedbackID: feedbackID, Description: "Conflicting correction",
			Reason: input.Reason,
		},
		now.Add(4*time.Minute),
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting feedback rectification error = %v", err)
	}

	loaded, err := store.GetCase(ctx, database, tenantID, privacyCase.ID)
	if err != nil || len(loaded.FeedbackRectifications) != 1 ||
		loaded.FeedbackRectifications[0].ID != rectification.ID {
		t.Fatalf("feedback rectification history = %#v error=%v",
			loaded.FeedbackRectifications, err)
	}
	var eventCount, auditCount, outboxCount int
	var auditData string
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM privacy_case_events
		WHERE tenant_id = $1 AND privacy_case_id = $2
		  AND event_type = 'privacy_case.feedback_rectification_applied'
	`, tenantID, privacyCase.ID).Scan(&eventCount); err != nil {
		t.Fatalf("count feedback rectification events: %v", err)
	}
	if err := database.QueryRow(ctx, `
		SELECT count(*), max(data::text)
		FROM tenant_access_events
		WHERE tenant_id = $1
		  AND kind = 'privacy.reporter_feedback_rectification_applied'
		  AND actor_member_id = $2 AND resource_id = $3
	`, tenantID, actorMemberID, feedbackID).Scan(&auditCount, &auditData); err != nil {
		t.Fatalf("read feedback rectification audit: %v", err)
	}
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE tenant_id = $1 AND event = 'feedback.rectified'
		  AND resource_type = 'feedback' AND resource_id = $2
	`, tenantID, feedbackID).Scan(&outboxCount); err != nil {
		t.Fatalf("count feedback rectification outbox events: %v", err)
	}
	if eventCount != 1 || auditCount != 1 || outboxCount != 1 ||
		strings.Contains(auditData, "Original reporter description") ||
		strings.Contains(auditData, "Corrected reporter description") {
		t.Fatalf("rectification evidence event=%d audit=%d outbox=%d data=%s",
			eventCount, auditCount, outboxCount, auditData)
	}
	if _, err := database.Exec(ctx, `
		UPDATE privacy_case_feedback_rectifications SET reason = 'rewritten'
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, rectification.ID); err == nil {
		t.Fatal("feedback rectification evidence was mutable")
	}
}

func TestReporterFeedbackRectificationRejectsUnverifiedAndUnownedFeedback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 20, 0, 0, 0, time.UTC)
	tenantID, actorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "rectification-scope", "Reporter-owned feedback",
	)
	var feedbackID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM feedbacks WHERE tenant_id = $1
	`, tenantID).Scan(&feedbackID); err != nil {
		t.Fatalf("read rectification scope fixture: %v", err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE feedbacks SET created_at = $3::timestamptz - interval '1 hour'
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, feedbackID, now); err != nil {
		t.Fatalf("prepare rectification scope fixture: %v", err)
	}
	store := Store{Database: database}
	privacyCase, token, _, err := store.CreateCase(
		ctx, database, tenantID, actorMemberID, "feedback-rectification-scope-case",
		CreateCaseInput{
			SubjectType: SubjectReporter, RightType: RightRectification,
			ControllerRole: RoleCustomerControllerAssistance,
			SubjectEmail:   "other@example.test", OwnerMemberID: actorMemberID,
		},
		now,
	)
	if err != nil {
		t.Fatalf("create rectification scope case: %v", err)
	}
	input := RecordFeedbackRectificationInput{
		FeedbackID: feedbackID, Description: "Attempted correction",
		Reason: "Must not apply before verification or outside reporter scope.",
	}
	if _, _, err := store.ApplyFeedbackRectification(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"unverified-rectification", input, now.Add(time.Minute),
	); !errors.Is(err, ErrCaseExecutionUnsupported) {
		t.Fatalf("unverified feedback rectification error = %v", err)
	}
	tokenHash, ok := parseReporterRequestToken(token)
	if !ok {
		t.Fatalf("parse rectification scope token: %q", token)
	}
	if _, err := store.VerifyCase(ctx, database, tokenHash, now.Add(time.Minute)); err != nil {
		t.Fatalf("verify rectification scope case: %v", err)
	}
	if _, _, err := store.ApplyFeedbackRectification(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"unowned-rectification", input, now.Add(2*time.Minute),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unowned feedback rectification error = %v", err)
	}
	candidates, err := store.ListRectifiableFeedbacks(
		ctx, database, tenantID, privacyCase.ID,
	)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("unowned rectifiable feedbacks = %#v, error=%v", candidates, err)
	}
}
