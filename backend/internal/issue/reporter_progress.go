package issue

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/activity"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ReporterProgressEvent struct {
	FromStatus *Status   `json:"from_status,omitempty"`
	ToStatus   Status    `json:"to_status"`
	CreatedAt  time.Time `json:"created_at"`
}

type ReporterClarification struct {
	Prompt      string     `json:"prompt"`
	Responded   bool       `json:"responded"`
	RespondedAt *time.Time `json:"responded_at,omitempty"`
}

type ReporterProgress struct {
	PublicID      string                  `json:"public_id"`
	Title         string                  `json:"title"`
	Status        Status                  `json:"status"`
	CreatedAt     time.Time               `json:"created_at"`
	ResolvedAt    *time.Time              `json:"resolved_at,omitempty"`
	Events        []ReporterProgressEvent `json:"events"`
	Clarification *ReporterClarification  `json:"clarification,omitempty"`
}

func reporterTokenHash(raw string) [sha256.Size]byte {
	return sha256.Sum256([]byte(raw))
}

func newReporterToken() (string, [sha256.Size]byte, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf("generate reporter access token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(bytes)
	return raw, reporterTokenHash(raw), nil
}

func (s Store) RotateReporterAccess(
	ctx context.Context,
	tenantID string,
	issueID string,
	now time.Time,
) (string, bool, error) {
	var projectID string
	err := s.Database.QueryRow(ctx, `
		SELECT issue.project_id
		FROM issues AS issue
		JOIN tenants AS tenant ON tenant.id = issue.tenant_id
		JOIN projects AS project
		  ON project.tenant_id = issue.tenant_id AND project.id = issue.project_id
		WHERE issue.tenant_id = $1 AND issue.id = $2
		  AND `+reporterConsentActiveExpression("issue")+`
		  AND issue.reporter_access_revoked_at IS NULL
		  AND tenant.status IN ('active', 'trial')
		  AND tenant.reporter_access_enabled = true
		  AND COALESCE(project.reporter_access_enabled, true) = true
	`, tenantID, issueID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("authorize reporter access rotation: %w", err)
	}
	raw, hash, err := newReporterToken()
	if err != nil {
		return "", false, err
	}
	if _, err := s.Database.Exec(ctx, `
		INSERT INTO reporter_access_tokens (
			tenant_id, project_id, issue_id, token_hash, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $5)
	`, tenantID, projectID, issueID, hash[:], now.UTC()); err != nil {
		return "", false, fmt.Errorf("rotate reporter access token: %w", err)
	}
	return raw, true, nil
}

func (s Store) LoadReporterProgress(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
	now time.Time,
) (ReporterProgress, error) {
	var progress ReporterProgress
	var tenantID, issueID string
	var prompt, response *string
	var respondedAt *time.Time
	err := s.Database.QueryRow(ctx, `
		SELECT token.tenant_id, issue.id, issue.public_id, issue.title, issue.status,
		       issue.created_at, issue.resolved_at,
		       clarification.prompt, clarification.response, clarification.responded_at
		FROM reporter_access_tokens AS token
		JOIN issues AS issue
		  ON issue.tenant_id = token.tenant_id AND issue.id = token.issue_id
		JOIN tenants AS tenant ON tenant.id = issue.tenant_id
		JOIN projects AS project
		  ON project.tenant_id = issue.tenant_id AND project.id = issue.project_id
		LEFT JOIN LATERAL (
			SELECT request.prompt, request.response, request.responded_at
			FROM reporter_clarification_requests AS request
			WHERE request.tenant_id = issue.tenant_id AND request.issue_id = issue.id
			ORDER BY request.created_at DESC, request.id DESC
			LIMIT 1
		) AS clarification ON true
		WHERE token.token_hash = $1
		  AND issue.reporter_access_revoked_at IS NULL
		  AND tenant.status IN ('active', 'trial')
		  AND tenant.reporter_access_enabled = true
		  AND COALESCE(project.reporter_access_enabled, true) = true
	`, tokenHash[:]).Scan(
		&tenantID, &issueID, &progress.PublicID, &progress.Title, &progress.Status,
		&progress.CreatedAt, &progress.ResolvedAt, &prompt, &response, &respondedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReporterProgress{}, ErrNotFound
	}
	if err != nil {
		return ReporterProgress{}, fmt.Errorf("load reporter progress: %w", err)
	}
	rows, err := s.Database.Query(ctx, `
		SELECT old_status, new_status, created_at
		FROM issue_updates
		WHERE tenant_id = $1 AND issue_id = $2
		ORDER BY created_at, id
	`, tenantID, issueID)
	if err != nil {
		return ReporterProgress{}, fmt.Errorf("list reporter progress events: %w", err)
	}
	defer rows.Close()
	progress.Events = make([]ReporterProgressEvent, 0)
	for rows.Next() {
		var event ReporterProgressEvent
		if err := rows.Scan(&event.FromStatus, &event.ToStatus, &event.CreatedAt); err != nil {
			return ReporterProgress{}, fmt.Errorf("scan reporter progress event: %w", err)
		}
		progress.Events = append(progress.Events, event)
	}
	if err := rows.Err(); err != nil {
		return ReporterProgress{}, fmt.Errorf("iterate reporter progress events: %w", err)
	}
	if prompt != nil {
		progress.Clarification = &ReporterClarification{
			Prompt: *prompt, Responded: response != nil, RespondedAt: respondedAt,
		}
	}
	if _, err := s.Database.Exec(ctx, `
		UPDATE reporter_access_tokens SET last_used_at = $2 WHERE token_hash = $1
	`, tokenHash[:], now.UTC()); err != nil {
		return ReporterProgress{}, fmt.Errorf("touch reporter access token: %w", err)
	}
	return progress, nil
}

func (s Store) SubmitReporterResponse(
	ctx context.Context,
	query db.Querier,
	tokenHash [sha256.Size]byte,
	response string,
	now time.Time,
) error {
	var tenantID, issueID, requestID string
	err := query.QueryRow(ctx, `
		SELECT token.tenant_id, issue.id, request.id
		FROM reporter_access_tokens AS token
		JOIN issues AS issue
		  ON issue.tenant_id = token.tenant_id AND issue.id = token.issue_id
		JOIN tenants AS tenant ON tenant.id = issue.tenant_id
		JOIN projects AS project
		  ON project.tenant_id = issue.tenant_id AND project.id = issue.project_id
		JOIN reporter_clarification_requests AS request
		  ON request.tenant_id = issue.tenant_id AND request.issue_id = issue.id
		 AND request.responded_at IS NULL AND request.closed_at IS NULL
		WHERE token.token_hash = $1
		  AND issue.status = 'needs_info'
		  AND issue.reporter_access_revoked_at IS NULL
		  AND tenant.status IN ('active', 'trial')
		  AND tenant.reporter_access_enabled = true
		  AND COALESCE(project.reporter_access_enabled, true) = true
		FOR UPDATE OF issue, request
	`, tokenHash[:]).Scan(&tenantID, &issueID, &requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock reporter clarification: %w", err)
	}
	activityID := uuid.NewString()
	if _, err := activity.Record(ctx, query, activity.RecordInput{
		ID: activityID, TenantID: tenantID, IssueID: issueID,
		Kind: "reporter.responded", Visibility: activity.VisibilityReporter,
		Data: map[string]any{"request_id": requestID, "response": response}, CreatedAt: now,
	}); err != nil {
		return err
	}
	if _, err := query.Exec(ctx, `
		UPDATE reporter_clarification_requests
		SET response = $4, responded_activity_id = $5, responded_at = $6
		WHERE tenant_id = $1 AND issue_id = $2 AND id = $3
		  AND responded_at IS NULL AND closed_at IS NULL
	`, tenantID, issueID, requestID, response, activityID, now.UTC()); err != nil {
		return fmt.Errorf("store reporter clarification response: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key)
		VALUES ($1, 'issue.updated', 'issue', $2, $3)
	`, tenantID, issueID, activityID); err != nil {
		return fmt.Errorf("create reporter response outbox event: %w", err)
	}
	return nil
}

func (Store) SetReporterAccess(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	memberID string,
	enabled bool,
	now time.Time,
) (Issue, error) {
	var revokedAt any = now.UTC()
	if enabled {
		revokedAt = nil
	}
	tag, err := query.Exec(ctx, `
		UPDATE issues SET reporter_access_revoked_at = $3
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, issueID, revokedAt)
	if err != nil {
		return Issue{}, fmt.Errorf("update issue reporter access: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Issue{}, ErrNotFound
	}
	if !enabled {
		if _, err := query.Exec(ctx, `
			DELETE FROM reporter_access_tokens WHERE tenant_id = $1 AND issue_id = $2
		`, tenantID, issueID); err != nil {
			return Issue{}, fmt.Errorf("revoke issue reporter access token: %w", err)
		}
	}
	actorID := memberID
	if _, err := activity.Record(ctx, query, activity.RecordInput{
		TenantID: tenantID, IssueID: issueID, ActorMemberID: &actorID,
		Kind: "reporter.access_changed", Data: map[string]any{"enabled": enabled}, CreatedAt: now,
	}); err != nil {
		return Issue{}, err
	}
	return (Store{}).Get(ctx, query, tenantID, issueID)
}
