package privacy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRetentionStorePreservesManifestsAndExpiresAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	store := Store{Database: database}

	type workspace struct {
		tenantID string
		memberID string
		plan     string
		ageDays  int
	}
	workspaces := make([]workspace, 0, 3)
	for index, definition := range []struct {
		plan    string
		ageDays int
	}{
		{plan: "starter", ageDays: 31},
		{plan: "growth", ageDays: 91},
		{plan: "scale", ageDays: 366},
	} {
		slug := fmt.Sprintf("retention-%s-%d", definition.plan, index)
		tenantID, memberID := seedPrivacyWorkspace(t, ctx, database, slug, slug+" feedback")
		if _, err := database.Exec(ctx, `UPDATE tenants SET plan = $2 WHERE id = $1`,
			tenantID, definition.plan); err != nil {
			t.Fatalf("set %s plan: %v", definition.plan, err)
		}
		var feedbackID string
		if err := database.QueryRow(ctx, `
			SELECT id FROM feedbacks WHERE tenant_id = $1 ORDER BY created_at LIMIT 1
		`, tenantID).Scan(&feedbackID); err != nil {
			t.Fatalf("load %s feedback: %v", definition.plan, err)
		}
		for suffix, completedAt := range map[string]time.Time{
			"expired":  now.AddDate(0, 0, -definition.ageDays),
			"boundary": now.AddDate(0, 0, -(definition.ageDays - 1)),
		} {
			if _, err := database.Exec(ctx, `
				INSERT INTO attachments (
					tenant_id, feedback_id, kind, client_attachment_id, storage_key,
					content_type, size_bytes, processing_status, completed_at,
					transcription, sentiment_score, moderation_flags, processed_at, created_at,
					safety_status, safety_checked_at, detected_content_type
				) VALUES (
					$1, $2, 'audio', gen_random_uuid(), $3,
					'audio/webm', 2048, 'completed', $4,
					'personal transcript', 0.4, ARRAY['personal'], $4, $4,
					'safe', $4, 'audio/webm'
				)
			`, tenantID, feedbackID, slug+"/"+suffix+".webm", completedAt); err != nil {
				t.Fatalf("create %s %s attachment: %v", definition.plan, suffix, err)
			}
		}
		workspaces = append(workspaces, workspace{
			tenantID: tenantID, memberID: memberID,
			plan: definition.plan, ageDays: definition.ageDays,
		})
	}

	retentionFeedbackID := privacyFeedbackID(t, ctx, database, workspaces[0].tenantID)
	expiredInvitation := seedPrivacyEmailInvitation(
		t, ctx, database, workspaces[0].tenantID, workspaces[0].memberID,
		retentionFeedbackID, "expired-invitation@example.test",
		"expired-invitation", "event-expired-invitation", now,
	)
	freshInvitation := seedPrivacyEmailInvitation(
		t, ctx, database, workspaces[0].tenantID, workspaces[0].memberID,
		retentionFeedbackID, "fresh-invitation@example.test",
		"fresh-invitation", "event-fresh-invitation", now,
	)
	if _, err := database.Exec(ctx, `
		UPDATE email_feedback_invitations
		SET created_at = CASE
		        WHEN id = $1::uuid THEN $3::timestamptz - interval '120 days'
		        ELSE created_at
		    END,
		    expires_at = CASE
		        WHEN id = $1::uuid THEN $3::timestamptz - interval '91 days'
		        ELSE expires_at
		    END,
		    cleanup_after = CASE
		        WHEN id = $1::uuid THEN $3::timestamptz - interval '1 day'
		        ELSE cleanup_after
		    END
		WHERE id IN ($1, $2)
	`, expiredInvitation.InvitationID, freshInvitation.InvitationID, now); err != nil {
		t.Fatalf("set email invitation retention boundaries: %v", err)
	}
	expiredInvitations, err := store.ExpireEmailFeedbackInvitations(ctx, now, 10)
	if err != nil || expiredInvitations != 1 {
		t.Fatalf("ExpireEmailFeedbackInvitations() = %d, error=%v",
			expiredInvitations, err)
	}
	var remainingInvitations int
	if err := database.QueryRow(ctx, `
		SELECT count(*)
		FROM email_feedback_invitations
		WHERE id IN ($1, $2)
	`, expiredInvitation.InvitationID, freshInvitation.InvitationID).
		Scan(&remainingInvitations); err != nil || remainingInvitations != 1 {
		t.Fatalf("remaining email invitations = %d, error=%v",
			remainingInvitations, err)
	}
	var suppressionSource *string
	var preservedSuppressionDigest string
	if err := database.QueryRow(ctx, `
		SELECT source_invitation_id::text, recipient_digest
		FROM email_feedback_suppressions
		WHERE tenant_id = $1 AND recipient_digest = $2
	`, workspaces[0].tenantID, expiredInvitation.RecipientDigest).Scan(
		&suppressionSource, &preservedSuppressionDigest,
	); err != nil || suppressionSource != nil ||
		preservedSuppressionDigest != expiredInvitation.RecipientDigest {
		t.Fatalf("preserved suppression source=%v digest=%q error=%v",
			suppressionSource, preservedSuppressionDigest, err)
	}

	attachments, err := store.ExpiredAttachments(ctx, now, 100)
	if err != nil {
		t.Fatalf("ExpiredAttachments() error = %v", err)
	}
	if len(attachments) != 3 {
		t.Fatalf("expired attachments = %#v", attachments)
	}
	for _, candidate := range attachments {
		if candidate.TenantID == "" || candidate.ID == "" ||
			!containsRetentionSuffix(candidate.ObjectKey, "/expired.webm") {
			t.Fatalf("expired attachment candidate = %#v", candidate)
		}
	}
	if err := store.MarkAttachmentRetentionDeleted(ctx, attachments[0], now); err != nil {
		t.Fatalf("MarkAttachmentRetentionDeleted() error = %v", err)
	}
	var attachmentExists bool
	var attachmentKey *string
	var retentionDeletedAt *time.Time
	var transcription *string
	if err := database.QueryRow(ctx, `
		SELECT true, storage_key, retention_deleted_at, transcription
		FROM attachments WHERE tenant_id = $1 AND id = $2
	`, attachments[0].TenantID, attachments[0].ID).Scan(
		&attachmentExists, &attachmentKey, &retentionDeletedAt, &transcription,
	); err != nil {
		t.Fatalf("read retained attachment manifest: %v", err)
	}
	if !attachmentExists || attachmentKey != nil || transcription != nil ||
		retentionDeletedAt == nil || !retentionDeletedAt.Equal(now) {
		t.Fatalf("retained attachment key=%v transcription=%v deleted_at=%v",
			attachmentKey, transcription, retentionDeletedAt)
	}

	archiveJob, _, err := store.CreateWorkspaceExport(
		ctx, database, workspaces[0].tenantID, workspaces[0].memberID, now.AddDate(0, 0, -10),
	)
	if err != nil {
		t.Fatalf("create expiring archive: %v", err)
	}
	archiveKey := "tenants/" + workspaces[0].tenantID + "/exports/expired.zip"
	if _, err := database.Exec(ctx, `
		UPDATE privacy_jobs
		SET status = 'completed', started_at = $2, heartbeat_at = $2,
		    completed_at = $2, expires_at = $3, object_key = $4,
		    object_size_bytes = 2048
		WHERE tenant_id = $1 AND id = $5
	`, workspaces[0].tenantID, now.AddDate(0, 0, -8), now.Add(-time.Hour),
		archiveKey, archiveJob.ID); err != nil {
		t.Fatalf("complete expiring archive: %v", err)
	}
	archives, err := store.ExpiredExportArchives(ctx, now, 10)
	if err != nil || len(archives) != 1 || archives[0].ObjectKey != archiveKey {
		t.Fatalf("ExpiredExportArchives() = %#v, error=%v", archives, err)
	}
	if err := store.MarkExportArchiveExpired(ctx, archives[0], now); err != nil {
		t.Fatalf("MarkExportArchiveExpired() error = %v", err)
	}
	expiredArchive, err := store.GetJob(
		ctx, database, workspaces[0].tenantID, archiveJob.ID,
	)
	if err != nil || expiredArchive.Status != JobExpired || expiredArchive.ObjectKey != nil {
		t.Fatalf("expired archive = %#v, error=%v", expiredArchive, err)
	}

	pending, _, _, err := store.CreateReporterRequest(
		ctx, database, workspaces[0].tenantID, workspaces[0].memberID,
		"pending@example.test", ReporterExport, now.AddDate(0, 0, -3),
	)
	if err != nil {
		t.Fatalf("create expired unverified request: %v", err)
	}
	fresh, _, _, err := store.CreateReporterRequest(
		ctx, database, workspaces[0].tenantID, workspaces[0].memberID,
		"fresh@example.test", ReporterExport, now,
	)
	if err != nil {
		t.Fatalf("create fresh request: %v", err)
	}
	failed, failedToken, _, err := store.CreateReporterRequest(
		ctx, database, workspaces[0].tenantID, workspaces[0].memberID,
		"failed@example.test", ReporterExport, now.AddDate(0, 0, -32),
	)
	if err != nil {
		t.Fatalf("create verified request: %v", err)
	}
	failedTokenHash, ok := parseReporterRequestToken(failedToken)
	if !ok {
		t.Fatal("parse failed request token = false")
	}
	if _, err := store.VerifyReporterRequest(
		ctx, database, failedTokenHash, now.AddDate(0, 0, -31),
	); err != nil {
		t.Fatalf("verify old request: %v", err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE privacy_jobs
		SET status = 'failed', error_message = 'terminal export failure'
		WHERE tenant_id = $1 AND reporter_request_id = $2
	`, workspaces[0].tenantID, failed.ID); err != nil {
		t.Fatalf("fail old request job: %v", err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE reporter_privacy_requests
		SET status = 'failed', error_message = 'terminal export failure'
		WHERE tenant_id = $1 AND id = $2
	`, workspaces[0].tenantID, failed.ID); err != nil {
		t.Fatalf("fail old request: %v", err)
	}
	expiredRequests, err := store.ExpireReporterRequests(ctx, now, 10)
	if err != nil || expiredRequests != 2 {
		t.Fatalf("ExpireReporterRequests() = %d, error=%v", expiredRequests, err)
	}
	for _, requestID := range []string{pending.ID, failed.ID} {
		var status ReporterRequestStatus
		var email *string
		if err := database.QueryRow(ctx, `
			SELECT status, reporter_email
			FROM reporter_privacy_requests WHERE tenant_id = $1 AND id = $2
		`, workspaces[0].tenantID, requestID).Scan(&status, &email); err != nil {
			t.Fatalf("read expired request %s: %v", requestID, err)
		}
		if status != ReporterExpired || email != nil {
			t.Fatalf("expired request %s status=%s email=%v", requestID, status, email)
		}
	}
	var freshStatus ReporterRequestStatus
	if err := database.QueryRow(ctx, `
		SELECT status FROM reporter_privacy_requests WHERE tenant_id = $1 AND id = $2
	`, workspaces[0].tenantID, fresh.ID).Scan(&freshStatus); err != nil ||
		freshStatus != ReporterPendingVerification {
		t.Fatalf("fresh request status=%s error=%v", freshStatus, err)
	}
	var auditCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind IN (
			'privacy.export_expired', 'privacy.reporter_request_expired'
		)
	`, workspaces[0].tenantID).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("retention audits=%d error=%v", auditCount, err)
	}
	if _, err := store.GetReporterRequestView(ctx, failedTokenHash, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired public request error=%v, want ErrNotFound", err)
	}
}

func containsRetentionSuffix(value string, suffix string) bool {
	return len(value) >= len(suffix) && value[len(value)-len(suffix):] == suffix
}
