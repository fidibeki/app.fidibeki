package mobilecapture

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/project"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrAccessDenied         = errors.New("mobile capture access denied")
	ErrNotFound             = errors.New("mobile capture resource not found")
	ErrTokenConflict        = errors.New("mobile capture token conflict")
	ErrConfigurationChanged = errors.New("mobile capture configuration changed")
	ErrSubmissionConflict   = errors.New("mobile capture submission conflict")
)

type Store struct {
	ProjectStore project.Store
}

func GenerateToken() (raw, hash string, err error) {
	opaque, _, err := auth.NewOpaqueToken()
	if err != nil {
		return "", "", err
	}
	raw = TokenPrefix + opaque
	return raw, auth.HashToken(raw), nil
}

func (store Store) LoadConfiguration(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
) (project.PublicConfiguration, error) {
	var publicKey string
	if err := query.QueryRow(ctx, `
		SELECT public_key FROM projects
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, projectID).Scan(&publicKey); errors.Is(err, pgx.ErrNoRows) {
		return project.PublicConfiguration{}, ErrNotFound
	} else if err != nil {
		return project.PublicConfiguration{}, fmt.Errorf("resolve mobile project: %w", err)
	}
	configuration, err := store.ProjectStore.LoadPublicConfiguration(ctx, query, publicKey)
	if errors.Is(err, project.ErrNotFound) {
		return project.PublicConfiguration{}, ErrNotFound
	}
	if err != nil {
		return project.PublicConfiguration{}, fmt.Errorf("load mobile project configuration: %w", err)
	}
	return configuration, nil
}

func (Store) LockDefinitionForMint(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	definitionKey string,
) error {
	var ignored string
	err := query.QueryRow(ctx, `
		SELECT rule.id
		FROM widget_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id
		JOIN tenants AS tenant ON tenant.id = project.tenant_id
		WHERE tenant.id = $1 AND project.id = $2 AND rule.definition_key = $3
		FOR SHARE OF tenant, project, rule
	`, tenantID, projectID, definitionKey).Scan(&ignored)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock mobile definition for mint: %w", err)
	}
	return nil
}

func (Store) LockMintedConfiguration(
	ctx context.Context,
	query db.Querier,
	session CaptureSession,
) error {
	var definitionVersion, controllerVersion int
	var enabled bool
	var status string
	err := query.QueryRow(ctx, `
		SELECT rule.definition_version, tenant.privacy_profile_version,
		       rule.enabled, rule.definition_status::text
		FROM widget_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id
		JOIN tenants AS tenant ON tenant.id = project.tenant_id
		WHERE tenant.id = $1 AND project.id = $2 AND rule.id = $3
		FOR SHARE OF tenant, project, rule
	`, session.TenantID, session.ProjectID, session.WidgetRuleID).Scan(
		&definitionVersion, &controllerVersion, &enabled, &status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConfigurationChanged
	}
	if err != nil {
		return fmt.Errorf("lock minted mobile configuration: %w", err)
	}
	if definitionVersion != session.DefinitionVersion ||
		controllerVersion != session.ControllerProfileVersion ||
		!enabled || status != string(project.DefinitionActive) {
		return ErrConfigurationChanged
	}
	return nil
}

func (Store) CreateSession(
	ctx context.Context,
	query db.Querier,
	session CaptureSession,
) (CaptureSession, error) {
	err := query.QueryRow(ctx, `
		INSERT INTO mobile_capture_sessions (
			tenant_id, project_id, widget_rule_id, issuer_api_key_id, token_hash,
			platform, application_id, screen, context_url, display_locale,
			definition_version, controller_profile_version,
			submit_expires_at, cleanup_after
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
		RETURNING id, created_at
	`, session.TenantID, session.ProjectID, session.WidgetRuleID, session.IssuerAPIKeyID,
		session.TokenHash, session.Platform, session.ApplicationID, session.Screen,
		session.ContextURL, session.DisplayLocale, session.DefinitionVersion,
		session.ControllerProfileVersion, session.SubmitExpiresAt, session.CleanupAfter,
	).Scan(&session.ID, &session.CreatedAt)
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return CaptureSession{}, ErrTokenConflict
	}
	if err != nil {
		return CaptureSession{}, fmt.Errorf("create mobile capture session: %w", err)
	}
	return session, nil
}

// ResolveToken is deliberately the only privileged mobile-capture query. It
// returns just enough tenancy metadata to enter an app-role FORCE-RLS
// transaction; every authorization and content read happens after that.
func (Store) ResolveToken(
	ctx context.Context,
	query db.Querier,
	rawToken string,
) (ResolvedToken, error) {
	if !validCaptureToken(rawToken) {
		return ResolvedToken{}, ErrAccessDenied
	}
	var resolved ResolvedToken
	err := query.QueryRow(ctx, `
		SELECT tenant_id, id
		FROM mobile_capture_sessions
		WHERE token_hash = $1
	`, auth.HashToken(rawToken)).Scan(&resolved.TenantID, &resolved.SessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedToken{}, ErrAccessDenied
	}
	if err != nil {
		return ResolvedToken{}, fmt.Errorf("resolve mobile capture token: %w", err)
	}
	return resolved, nil
}

func (Store) LockSession(
	ctx context.Context,
	query db.Querier,
	resolved ResolvedToken,
	rawToken string,
) (CaptureSession, error) {
	var session CaptureSession
	tokenHash := auth.HashToken(rawToken)
	err := query.QueryRow(ctx, `
		SELECT project_id, issuer_api_key_id
		FROM mobile_capture_sessions
		WHERE tenant_id = $1 AND id = $2 AND token_hash = $3
	`, resolved.TenantID, resolved.SessionID, tokenHash).Scan(
		&session.ProjectID, &session.IssuerAPIKeyID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CaptureSession{}, ErrAccessDenied
	}
	if err != nil {
		return CaptureSession{}, fmt.Errorf("resolve mobile capture lock boundary: %w", err)
	}

	// Lock the rows whose deletion or lifecycle update revokes the bearer before
	// locking the capture row. This matches the parent-to-child order used by
	// tenant, project, and API-key deletion cascades.
	err = query.QueryRow(ctx, `
		SELECT status::text, trial_ends_at, deletion_requested_at
		FROM tenants
		WHERE id = $1
		FOR SHARE
	`, resolved.TenantID).Scan(
		&session.TenantStatus, &session.TenantTrialEndsAt,
		&session.TenantDeletionRequestedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CaptureSession{}, ErrAccessDenied
	}
	if err != nil {
		return CaptureSession{}, fmt.Errorf("lock mobile capture tenant lifecycle: %w", err)
	}
	var lockedProjectID string
	err = query.QueryRow(ctx, `
		SELECT id
		FROM projects
		WHERE tenant_id = $1 AND id = $2
		FOR SHARE
	`, resolved.TenantID, session.ProjectID).Scan(&lockedProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CaptureSession{}, ErrAccessDenied
	}
	if err != nil {
		return CaptureSession{}, fmt.Errorf("lock mobile capture project lifecycle: %w", err)
	}
	err = query.QueryRow(ctx, `
		SELECT expires_at
		FROM api_keys
		WHERE tenant_id = $1 AND id = $2
		FOR SHARE
	`, resolved.TenantID, session.IssuerAPIKeyID).Scan(&session.IssuerExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CaptureSession{}, ErrAccessDenied
	}
	if err != nil {
		return CaptureSession{}, fmt.Errorf("lock mobile capture issuer lifecycle: %w", err)
	}

	err = query.QueryRow(ctx, `
		SELECT capture.id, capture.tenant_id, capture.project_id, capture.widget_rule_id,
		       capture.issuer_api_key_id, capture.token_hash, capture.platform::text,
		       capture.application_id, capture.screen, capture.context_url,
		       capture.display_locale, capture.definition_version,
		       capture.controller_profile_version, capture.feedback_id,
		       capture.client_feedback_id, capture.request_digest, capture.accepted_at,
		       capture.submit_expires_at, capture.retry_expires_at,
		       capture.cleanup_after, capture.created_at
		FROM mobile_capture_sessions AS capture
		WHERE capture.tenant_id = $1
		  AND capture.id = $2
		  AND capture.token_hash = $3
		  AND capture.project_id = $4
		  AND capture.issuer_api_key_id = $5
		FOR UPDATE OF capture
	`, resolved.TenantID, resolved.SessionID, tokenHash,
		session.ProjectID, session.IssuerAPIKeyID).Scan(
		&session.ID, &session.TenantID, &session.ProjectID, &session.WidgetRuleID,
		&session.IssuerAPIKeyID, &session.TokenHash, &session.Platform,
		&session.ApplicationID, &session.Screen, &session.ContextURL,
		&session.DisplayLocale, &session.DefinitionVersion,
		&session.ControllerProfileVersion, &session.FeedbackID,
		&session.ClientFeedbackID, &session.RequestDigest, &session.AcceptedAt,
		&session.SubmitExpiresAt, &session.RetryExpiresAt,
		&session.CleanupAfter, &session.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CaptureSession{}, ErrAccessDenied
	}
	if err != nil {
		return CaptureSession{}, fmt.Errorf("lock mobile capture session: %w", err)
	}
	return session, nil
}

func (Store) BindSession(
	ctx context.Context,
	query db.Querier,
	sessionID string,
	feedbackID string,
	clientFeedbackID string,
	requestDigest string,
	acceptedAt time.Time,
	latestUploadExpiry time.Time,
) error {
	retryExpiresAt := acceptedAt.UTC().Add(RetryTTL)
	lastDeadline := retryExpiresAt
	if latestUploadExpiry.After(lastDeadline) {
		lastDeadline = latestUploadExpiry.UTC()
	}
	tag, err := query.Exec(ctx, `
		UPDATE mobile_capture_sessions
		SET feedback_id = $2, client_feedback_id = $3, request_digest = $4,
		    accepted_at = $5, retry_expires_at = $6, cleanup_after = $7
		WHERE id = $1 AND feedback_id IS NULL
	`, sessionID, feedbackID, clientFeedbackID, requestDigest, acceptedAt.UTC(),
		retryExpiresAt, lastDeadline.Add(CleanupGrace))
	if err != nil {
		return fmt.Errorf("bind mobile capture session: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrSubmissionConflict
	}
	return nil
}

func (Store) DeleteExpired(ctx context.Context, query db.Querier, now time.Time, limit int) (int64, error) {
	if limit < 1 {
		limit = 100
	}
	result, err := query.Exec(ctx, `
		DELETE FROM mobile_capture_sessions
		WHERE id IN (
			SELECT id FROM mobile_capture_sessions
			WHERE cleanup_after <= $1
			ORDER BY cleanup_after, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
	`, now.UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired mobile capture sessions: %w", err)
	}
	return result.RowsAffected(), nil
}

func validCaptureToken(raw string) bool {
	if len(raw) != len(TokenPrefix)+43 || len(raw) > 128 ||
		!strings.HasPrefix(raw, TokenPrefix) {
		return false
	}
	for _, character := range raw[len(TokenPrefix):] {
		if character >= '0' && character <= '9' ||
			character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' ||
			character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
