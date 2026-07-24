package privacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func TestMemberAccessExportTracer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	tenantID, memberID := seedPrivacyWorkspace(
		t, ctx, database, "member-access", "Member export issue",
	)
	otherTenantID, otherTenantMemberID := seedPrivacyWorkspace(
		t, ctx, database, "member-access-other", "Foreign member data",
	)
	if _, err := database.Exec(ctx, `
		UPDATE tenant_members
		SET name = 'Export Subject', locale = 'pt-PT', timezone = 'Europe/Lisbon'
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, memberID); err != nil {
		t.Fatalf("update export member profile: %v", err)
	}
	var otherMemberID string
	if err := database.QueryRow(ctx, `
		INSERT INTO tenant_members (
			tenant_id, email, name, permissions, status, last_login_at
		) VALUES (
			$1, 'colleague@member-access.test', 'Private Colleague',
			ARRAY['tenant:read'], 'active', now()
		) RETURNING id
	`, tenantID).Scan(&otherMemberID); err != nil {
		t.Fatalf("create colleague: %v", err)
	}
	refreshHash := strings.Repeat("a", 64)
	magicLinkHash := strings.Repeat("b", 64)
	if _, err := database.Exec(ctx, `
		INSERT INTO tenant_member_sessions (
			tenant_id, member_id, refresh_hash, user_agent, ip
		) VALUES ($1, $2, $3, 'Member browser marker', '192.0.2.42')
	`, tenantID, memberID, refreshHash); err != nil {
		t.Fatalf("create member session: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO tenant_magic_links (
			tenant_id, member_id, email, token_hash, expires_at, consumed_at
		) VALUES (
			$1, $2, 'owner@member-access.test', $3,
			now() + interval '15 minutes', now()
		)
	`, tenantID, memberID, magicLinkHash); err != nil {
		t.Fatalf("create member magic link: %v", err)
	}

	var issueID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM issues WHERE tenant_id = $1 ORDER BY created_at, id LIMIT 1
	`, tenantID).Scan(&issueID); err != nil {
		t.Fatalf("load member issue: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO issue_updates (
			tenant_id, issue_id, member_id, old_status, new_status, comment
		) VALUES (
			$1, $2, $3, 'open', 'in_progress', 'Member-owned status explanation'
		)
	`, tenantID, issueID, memberID); err != nil {
		t.Fatalf("create member issue update: %v", err)
	}
	var subjectCommentID, colleagueCommentID string
	if err := database.QueryRow(ctx, `
		INSERT INTO issue_comments (tenant_id, issue_id, author_member_id, body)
		VALUES ($1, $2, $3, 'Member-authored privacy note')
		RETURNING id
	`, tenantID, issueID, memberID).Scan(&subjectCommentID); err != nil {
		t.Fatalf("create member comment: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO issue_comments (tenant_id, issue_id, author_member_id, body)
		VALUES ($1, $2, $3, 'Other member confidential body')
		RETURNING id
	`, tenantID, issueID, otherMemberID).Scan(&colleagueCommentID); err != nil {
		t.Fatalf("create colleague comment: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO issue_comment_mentions (tenant_id, comment_id, member_id)
		VALUES ($1, $2, $3)
	`, tenantID, colleagueCommentID, memberID); err != nil {
		t.Fatalf("create member mention: %v", err)
	}
	var subjectActivityID, colleagueActivityID string
	if err := database.QueryRow(ctx, `
		INSERT INTO issue_activities (
			tenant_id, issue_id, actor_member_id, kind, visibility, data
		) VALUES (
			$1, $2, $3, 'issue.comment_added', 'internal',
			jsonb_build_object('comment_id', $4::text)
		) RETURNING id
	`, tenantID, issueID, memberID, subjectCommentID).Scan(&subjectActivityID); err != nil {
		t.Fatalf("create member activity: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO issue_activities (
			tenant_id, issue_id, actor_member_id, kind, visibility, data
		) VALUES (
			$1, $2, $3, 'issue.comment_added', 'internal',
			jsonb_build_object(
				'comment_id', $4::text,
				'private_payload', 'Other member activity secret'
			)
		) RETURNING id
	`, tenantID, issueID, otherMemberID, colleagueCommentID).Scan(&colleagueActivityID); err != nil {
		t.Fatalf("create colleague activity: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO member_notifications (
			tenant_id, recipient_member_id, actor_member_id, issue_id,
			activity_id, kind
		) VALUES ($1, $2, $3, $4, $5, 'issue.mentioned')
	`, tenantID, memberID, otherMemberID, issueID, colleagueActivityID); err != nil {
		t.Fatalf("create member notification: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, subject_member_id,
			resource_type, resource_id, action, permission_path, outcome, data
		) VALUES (
			$1, 'privacy.subject_accessed', $2, $2,
			'privacy_export', $3, 'export_grant', 'tenant:read', 'success',
			'{"raw_payload_secret":"must-not-leak"}'
		)
	`, tenantID, memberID, issueID); err != nil {
		t.Fatalf("create member access event: %v", err)
	}

	store := Store{Database: database}
	privacyCase, token, shouldEmail, err := store.CreateCase(
		ctx, database, tenantID, memberID, "member-access-case",
		CreateCaseInput{
			SubjectType:     SubjectMember,
			RightType:       RightAccess,
			ControllerRole:  RoleController,
			SubjectMemberID: memberID,
			OwnerMemberID:   memberID,
			Scope:           map[string]any{"intake_channel": "backoffice"},
		},
		now,
	)
	if err != nil || token != "" || shouldEmail || privacyCase.VerifiedAt == nil {
		t.Fatalf("CreateCase(member access) = %#v token=%q email=%t error=%v",
			privacyCase, token, shouldEmail, err)
	}
	if _, _, err := store.StartCaseExecution(
		ctx, database, otherTenantID, privacyCase.ID, otherTenantMemberID,
		"foreign-execution", now.Add(time.Minute),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant StartCaseExecution() error = %v", err)
	}
	execution, created, err := store.StartCaseExecution(
		ctx, database, tenantID, privacyCase.ID, memberID,
		"member-execution", now.Add(time.Minute),
	)
	if err != nil || !created || execution.JobID == nil ||
		execution.JobStatus == nil || *execution.JobStatus != JobPending ||
		execution.RequestStatus != nil {
		t.Fatalf("StartCaseExecution(member access) = %#v created=%t error=%v",
			execution, created, err)
	}
	replayed, replayCreated, err := store.StartCaseExecution(
		ctx, database, tenantID, privacyCase.ID, memberID,
		"member-execution", now.Add(2*time.Minute),
	)
	if err != nil || replayCreated || replayed.JobID == nil ||
		*replayed.JobID != *execution.JobID {
		t.Fatalf("idempotent member execution = %#v created=%t error=%v",
			replayed, replayCreated, err)
	}

	job, err := store.GetJob(ctx, database, tenantID, *execution.JobID)
	if err != nil || job.Kind != JobMemberExport ||
		job.PrivacyCaseID == nil || *job.PrivacyCaseID != privacyCase.ID {
		t.Fatalf("member export job = %#v error=%v", job, err)
	}
	storage := &privacyStorageCapture{}
	exporter := Exporter{
		Database: database,
		Store:    store,
		Storage:  storage,
		Now:      func() time.Time { return now.Add(3 * time.Minute) },
	}
	payload, _ := json.Marshal(map[string]any{
		"job_id": job.ID, "tenant_id": tenantID, "generation": job.Generation,
	})
	if err := exporter.Handler()(ctx, asynq.NewTask(TaskExport, payload)); err != nil {
		t.Fatalf("member export handler: %v", err)
	}
	files := readPrivacyArchive(t, storage.body)
	for _, filename := range []string{
		"member.json", "sessions.jsonl", "magic-links.jsonl",
		"issue-updates.jsonl", "issue-comments.jsonl", "issue-activity.jsonl",
		"mentions.jsonl", "notifications.jsonl", "access-events.jsonl",
		"privacy-cases.jsonl", "privacy-case-events.jsonl", "manifest.json",
	} {
		if _, exists := files[filename]; !exists {
			t.Errorf("member archive is missing %q", filename)
		}
	}
	archiveText := string(bytes.Join(mapValues(files), nil))
	for _, required := range []string{
		"fidibeki.member-export.v1", "Export Subject",
		"owner@member-access.test", `"role": "owner"`, "Member browser marker",
		"Member-owned status explanation", "Member-authored privacy note",
		subjectActivityID, colleagueCommentID, colleagueActivityID,
		"privacy.subject_accessed", privacyCase.PublicID,
	} {
		if !strings.Contains(archiveText, required) {
			t.Errorf("member archive does not contain %q", required)
		}
	}
	for _, forbidden := range []string{
		refreshHash, magicLinkHash, "Other member confidential body",
		"Other member activity secret", "raw_payload_secret",
		"must-not-leak", "colleague@member-access.test",
		otherTenantID, otherTenantMemberID,
	} {
		if strings.Contains(archiveText, forbidden) {
			t.Errorf("member archive leaked %q", forbidden)
		}
	}

	completed, err := store.GetCase(ctx, database, tenantID, privacyCase.ID)
	if err != nil || completed.Execution == nil ||
		completed.Execution.JobStatus == nil ||
		*completed.Execution.JobStatus != JobCompleted ||
		completed.Execution.CompletedAt == nil {
		t.Fatalf("completed member case = %#v error=%v", completed, err)
	}
	var completedAuditCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*)
		FROM tenant_access_events
		WHERE tenant_id = $1
		  AND kind = 'privacy.member_export_completed'
		  AND subject_member_id = $2
		  AND data->>'privacy_case_id' = $3
		  AND data->>'privacy_job_id' = $4
	`, tenantID, memberID, privacyCase.ID, job.ID).Scan(&completedAuditCount); err != nil {
		t.Fatalf("read member export completion audit: %v", err)
	}
	if completedAuditCount != 1 {
		t.Fatalf("member export completion audit count = %d, want 1", completedAuditCount)
	}

	failedCase, _, _, err := store.CreateCase(
		ctx, database, tenantID, memberID, "member-access-failure-case",
		CreateCaseInput{
			SubjectType:     SubjectMember,
			RightType:       RightAccess,
			ControllerRole:  RoleController,
			SubjectMemberID: memberID,
			OwnerMemberID:   memberID,
		},
		now.Add(4*time.Minute),
	)
	if err != nil {
		t.Fatalf("create member failure case: %v", err)
	}
	failedExecution, _, err := store.StartCaseExecution(
		ctx, database, tenantID, failedCase.ID, memberID,
		"member-failure-execution", now.Add(5*time.Minute),
	)
	if err != nil || failedExecution.JobID == nil {
		t.Fatalf("start member failure execution = %#v error=%v", failedExecution, err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE privacy_jobs SET attempts = $3
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, *failedExecution.JobID, maximumJobAttempts-1); err != nil {
		t.Fatalf("prime member export final attempt: %v", err)
	}
	failedJob, err := store.GetJob(ctx, database, tenantID, *failedExecution.JobID)
	if err != nil {
		t.Fatalf("load primed member export: %v", err)
	}
	failurePayload, _ := json.Marshal(map[string]any{
		"job_id": failedJob.ID, "tenant_id": tenantID,
		"generation": failedJob.Generation,
	})
	failingExporter := Exporter{
		Database: database,
		Store:    store,
		Storage:  &privacyStorageCapture{err: errors.New("object store unavailable")},
		Now:      func() time.Time { return now.Add(6 * time.Minute) },
	}
	if err := failingExporter.Handler()(
		ctx, asynq.NewTask(TaskExport, failurePayload),
	); err != nil {
		t.Fatalf("member export terminal failure handler: %v", err)
	}
	failedJob, err = store.GetJob(ctx, database, tenantID, failedJob.ID)
	if err != nil || failedJob.Status != JobFailed {
		t.Fatalf("terminal member export = %#v error=%v", failedJob, err)
	}
	var failureAuditCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*)
		FROM tenant_access_events
		WHERE tenant_id = $1
		  AND kind = 'privacy.member_export_failed'
		  AND subject_member_id = $2
		  AND data->>'privacy_case_id' = $3
		  AND data->>'privacy_job_id' = $4
	`, tenantID, memberID, failedCase.ID, failedJob.ID).Scan(&failureAuditCount); err != nil {
		t.Fatalf("read member export failure audit: %v", err)
	}
	if failureAuditCount != 1 {
		t.Fatalf("member export failure audit count = %d, want 1", failureAuditCount)
	}
	retried, err := store.RetryJob(
		ctx, database, tenantID, memberID, failedJob.ID, now.Add(7*time.Minute),
	)
	if err != nil || retried.Status != JobPending || retried.Generation != failedJob.Generation+1 {
		t.Fatalf("RetryJob(member export) = %#v error=%v", retried, err)
	}
	var retryAuditCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*)
		FROM tenant_access_events
		WHERE tenant_id = $1
		  AND kind = 'privacy.member_export_retried'
		  AND actor_member_id = $2
		  AND subject_member_id = $2
		  AND data->>'privacy_case_id' = $3
		  AND data->>'privacy_job_id' = $4
	`, tenantID, memberID, failedCase.ID, failedJob.ID).Scan(&retryAuditCount); err != nil {
		t.Fatalf("read member export retry audit: %v", err)
	}
	if retryAuditCount != 1 {
		t.Fatalf("member export retry audit count = %d, want 1", retryAuditCount)
	}
}
