package privacy

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

type memberScope struct {
	CaseID   string
	MemberID string
	Cutoff   time.Time
}

var memberExportFiles = []exportFile{
	{Name: "sessions.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', session.id, 'issued_at', session.issued_at,
			'last_used_at', session.last_used_at, 'revoked_at', session.revoked_at,
			'user_agent', session.user_agent, 'ip', session.ip,
			'created_at', session.created_at
		)
		FROM tenant_member_sessions AS session
		WHERE session.tenant_id = $1 AND session.member_id = $2
		  AND session.created_at <= $3
		ORDER BY session.created_at, session.id
	`},
	{Name: "magic-links.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', magic_link.id, 'email', magic_link.email,
			'invited_permissions', magic_link.invited_permissions,
			'expires_at', magic_link.expires_at, 'consumed_at', magic_link.consumed_at,
			'created_at', magic_link.created_at
		)
		FROM tenant_magic_links AS magic_link
		WHERE magic_link.tenant_id = $1 AND magic_link.member_id = $2
		  AND magic_link.created_at <= $3
		ORDER BY magic_link.created_at, magic_link.id
	`},
	{Name: "issue-updates.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', issue_update.id, 'issue_id', issue_update.issue_id,
			'old_status', issue_update.old_status, 'new_status', issue_update.new_status,
			'comment', issue_update.comment, 'created_at', issue_update.created_at
		)
		FROM issue_updates AS issue_update
		WHERE issue_update.tenant_id = $1 AND issue_update.member_id = $2
		  AND issue_update.created_at <= $3
		ORDER BY issue_update.created_at, issue_update.id
	`},
	{Name: "issue-comments.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', comment.id, 'issue_id', comment.issue_id,
			'body', comment.body, 'created_at', comment.created_at
		)
		FROM issue_comments AS comment
		WHERE comment.tenant_id = $1 AND comment.author_member_id = $2
		  AND comment.created_at <= $3
		ORDER BY comment.created_at, comment.id
	`},
	{Name: "issue-activity.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', activity.id, 'issue_id', activity.issue_id,
			'kind', activity.kind, 'visibility', activity.visibility,
			'created_at', activity.created_at
		)
		FROM issue_activities AS activity
		WHERE activity.tenant_id = $1 AND activity.actor_member_id = $2
		  AND activity.created_at <= $3
		ORDER BY activity.created_at, activity.id
	`},
	{Name: "mentions.jsonl", Query: `
		SELECT jsonb_build_object(
			'comment_id', mention.comment_id, 'created_at', mention.created_at
		)
		FROM issue_comment_mentions AS mention
		WHERE mention.tenant_id = $1 AND mention.member_id = $2
		  AND mention.created_at <= $3
		ORDER BY mention.created_at, mention.comment_id
	`},
	{Name: "notifications.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', notification.id, 'issue_id', notification.issue_id,
			'activity_id', notification.activity_id, 'kind', notification.kind,
			'read_at', notification.read_at, 'created_at', notification.created_at
		)
		FROM member_notifications AS notification
		WHERE notification.tenant_id = $1 AND notification.recipient_member_id = $2
		  AND notification.created_at <= $3
		ORDER BY notification.created_at, notification.id
	`},
	{Name: "access-events.jsonl", Query: `
		SELECT jsonb_strip_nulls(jsonb_build_object(
			'id', access_event.id, 'kind', access_event.kind,
			'relation', CASE
				WHEN access_event.actor_member_id = $2 AND access_event.subject_member_id = $2
					THEN 'actor_and_subject'
				WHEN access_event.actor_member_id = $2 THEN 'actor'
				ELSE 'subject'
			END,
			'actor_type', CASE
				WHEN access_event.actor_member_id = $2 THEN access_event.actor_type
			END,
			'resource_type', access_event.resource_type,
			'resource_id', access_event.resource_id,
			'action', access_event.action,
			'permission_path', CASE
				WHEN access_event.actor_member_id = $2 THEN access_event.permission_path
			END,
			'outcome', access_event.outcome,
			'created_at', access_event.created_at
		))
		FROM tenant_access_events AS access_event
		WHERE access_event.tenant_id = $1
		  AND (
			access_event.actor_member_id = $2
			OR access_event.subject_member_id = $2
		  )
		  AND access_event.created_at <= $3
		ORDER BY access_event.created_at, access_event.id
	`},
	{Name: "privacy-cases.jsonl", Query: `
		SELECT jsonb_strip_nulls(jsonb_build_object(
			'id', privacy_case.id, 'public_id', privacy_case.public_id,
			'right_type', privacy_case.right_type,
			'controller_role', privacy_case.controller_role,
			'status', privacy_case.status,
			'verification_method', privacy_case.verification_method,
			'verified_at', privacy_case.verified_at,
			'received_at', privacy_case.received_at,
			'deadline_at', privacy_case.deadline_at,
			'completed_at', privacy_case.completed_at,
			'decision_summary', privacy_case.decision_summary,
			'exception_reason', privacy_case.exception_reason,
			'created_at', privacy_case.created_at,
			'updated_at', privacy_case.updated_at
		))
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.subject_member_id = $2
		  AND privacy_case.created_at <= $3
		ORDER BY privacy_case.created_at, privacy_case.id
	`},
	{Name: "privacy-case-events.jsonl", Query: `
		SELECT jsonb_strip_nulls(jsonb_build_object(
			'id', case_event.id, 'privacy_case_id', case_event.privacy_case_id,
			'sequence', case_event.sequence, 'event_type', case_event.event_type,
			'previous_status', case_event.previous_status,
			'new_status', case_event.new_status,
			'occurred_at', case_event.occurred_at
		))
		FROM privacy_case_events AS case_event
		JOIN privacy_cases AS privacy_case
		  ON privacy_case.tenant_id = case_event.tenant_id
		 AND privacy_case.id = case_event.privacy_case_id
		WHERE case_event.tenant_id = $1 AND privacy_case.subject_member_id = $2
		  AND case_event.occurred_at <= $3
		ORDER BY case_event.sequence, case_event.id
	`},
	{Name: "privacy-rectifications.jsonl", Query: `
		SELECT jsonb_build_object(
			'id', rectification.id,
			'privacy_case_id', rectification.privacy_case_id,
			'changes', rectification.changes,
			'previous_values', rectification.previous_values,
			'reason', rectification.reason,
			'applied_at', rectification.applied_at,
			'created_at', rectification.created_at
		)
		FROM privacy_case_rectifications AS rectification
		WHERE rectification.tenant_id = $1
		  AND rectification.subject_member_id = $2
		  AND rectification.created_at <= $3
		ORDER BY rectification.created_at, rectification.id
	`},
}

func (exporter Exporter) buildMemberArchive(
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
		return "", 0, 0, 0, fmt.Errorf("begin member export snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	scope, err := loadMemberScope(ctx, tx, job)
	if err != nil {
		return "", 0, 0, 0, err
	}
	temporary, err := os.CreateTemp("", "fidibeki-member-export-*.zip")
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("create member export temp file: %w", err)
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
	member, err := memberJSON(ctx, tx, job.TenantID, scope.MemberID, scope.Cutoff)
	if err != nil {
		_ = archive.Close()
		return "", 0, 0, 0, err
	}
	if err := writeArchiveFile(archive, "member.json", append(member, '\n')); err != nil {
		_ = archive.Close()
		return "", 0, 0, 0, err
	}
	manifest := archiveManifest{
		SchemaVersion:    "fidibeki.member-export.v1",
		Scope:            "member",
		WorkspaceID:      job.TenantID,
		GeneratedAt:      exporter.now(),
		Files:            []manifestFile{{Name: "member.json", Records: 1}},
		BinaryDataPolicy: "The member avatar binary is not copied; member.json reports only its version identifier.",
		SensitiveOmissions: []string{
			"private platform support conversations; available to the participant through their own support transcript download",
			"session refresh hashes and magic-link token hashes",
			"API keys, integration credentials, signed URLs, and internal queue payloads",
			"other members' identities and content authored by other people",
			"raw access-event payloads, request correlation identifiers, and avatar binary data",
			"internal legal-review notes",
		},
		DiscoveryNotes: []string{
			"Activity payloads are reduced to subject-authored or subject-addressed operational metadata.",
			"The archive uses a repeatable-read processing snapshot and excludes collection rows created after the execution request time.",
		},
	}
	recordCount := 1
	for _, definition := range memberExportFiles {
		count, err := writeJSONL(
			ctx, tx, archive, definition, job.TenantID, scope.MemberID, scope.Cutoff,
		)
		if err != nil {
			_ = archive.Close()
			return "", 0, 0, 0, err
		}
		recordCount += count
		manifest.Files = append(
			manifest.Files, manifestFile{Name: definition.Name, Records: count},
		)
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = archive.Close()
		return "", 0, 0, 0, fmt.Errorf("encode member export manifest: %w", err)
	}
	if err := writeArchiveFile(archive, "manifest.json", append(manifestJSON, '\n')); err != nil {
		_ = archive.Close()
		return "", 0, 0, 0, err
	}
	if err := archive.Close(); err != nil {
		return "", 0, 0, 0, fmt.Errorf("close member export archive: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", 0, 0, 0, fmt.Errorf("sync member export archive: %w", err)
	}
	info, err := temporary.Stat()
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("stat member export archive: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, 0, 0, fmt.Errorf("commit member export snapshot: %w", err)
	}
	keep = true
	return path, info.Size(), recordCount, 0, nil
}

func loadMemberScope(
	ctx context.Context,
	query interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	job Job,
) (memberScope, error) {
	if job.PrivacyCaseID == nil {
		return memberScope{}, ErrJobUnavailable
	}
	var scope memberScope
	err := query.QueryRow(ctx, `
		SELECT privacy_case.id, privacy_case.subject_member_id, job.created_at
		FROM privacy_jobs AS job
		JOIN privacy_cases AS privacy_case
		  ON privacy_case.tenant_id = job.tenant_id
		 AND privacy_case.id = job.privacy_case_id
		JOIN tenant_members AS member
		  ON member.tenant_id = privacy_case.tenant_id
		 AND member.id = privacy_case.subject_member_id
		WHERE job.tenant_id = $1 AND job.id = $2
		  AND job.kind = 'member_export' AND job.status = 'processing'
		  AND privacy_case.subject_type = 'member'
		  AND privacy_case.right_type = 'access'
		  AND privacy_case.verified_at IS NOT NULL
		  AND privacy_case.status NOT IN (
			'partially_fulfilled', 'completed', 'refused', 'cancelled'
		  )
	`, job.TenantID, job.ID).Scan(&scope.CaseID, &scope.MemberID, &scope.Cutoff)
	if errors.Is(err, pgx.ErrNoRows) {
		return memberScope{}, ErrJobUnavailable
	}
	if err != nil {
		return memberScope{}, fmt.Errorf("load member privacy scope: %w", err)
	}
	return scope, nil
}

func memberJSON(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	memberID string,
	cutoff time.Time,
) ([]byte, error) {
	var encoded []byte
	err := tx.QueryRow(ctx, `
		SELECT jsonb_build_object(
			'id', member.id, 'email', member.email, 'name', member.name,
			'permissions', member.permissions, 'status', member.status,
			'role', CASE WHEN tenant.owner_member_id = member.id
			             THEN 'owner' ELSE 'member' END,
			'authorization_version', member.authorization_version,
			'email_bounced', member.email_bounced,
			'email_complained', member.email_complained,
			'last_login_at', member.last_login_at,
			'avatar_version', member.avatar_version,
			'locale', member.locale, 'timezone', member.timezone,
			'created_at', member.created_at, 'updated_at', member.updated_at
		)
		FROM tenant_members AS member
		JOIN tenants AS tenant ON tenant.id = member.tenant_id
		WHERE member.tenant_id = $1 AND member.id = $2
		  AND member.created_at <= $3
	`, tenantID, memberID, cutoff).Scan(&encoded)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrJobUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("export member identity: %w", err)
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, encoded, "", "  "); err != nil {
		return nil, fmt.Errorf("format member export identity: %w", err)
	}
	return formatted.Bytes(), nil
}
