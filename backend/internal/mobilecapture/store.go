package mobilecapture

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
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
	var status, presentation string
	err := query.QueryRow(ctx, `
		SELECT rule.definition_version, tenant.privacy_profile_version,
		       rule.enabled, rule.definition_status::text, rule.presentation::text
		FROM widget_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id
		JOIN tenants AS tenant ON tenant.id = project.tenant_id
		WHERE tenant.id = $1 AND project.id = $2 AND rule.id = $3
		FOR SHARE OF tenant, project, rule
	`, session.TenantID, session.ProjectID, session.WidgetRuleID).Scan(
		&definitionVersion, &controllerVersion, &enabled, &status, &presentation,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConfigurationChanged
	}
	if err != nil {
		return fmt.Errorf("lock minted mobile configuration: %w", err)
	}
	if definitionVersion != session.DefinitionVersion ||
		controllerVersion != session.ControllerProfileVersion ||
		!enabled || status != string(project.DefinitionActive) ||
		presentation != string(project.PresentationLauncher) {
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
	now time.Time,
) (CaptureSession, error) {
	var session CaptureSession
	err := query.QueryRow(ctx, `
		SELECT capture.id, capture.tenant_id, capture.project_id, capture.widget_rule_id,
		       capture.issuer_api_key_id, capture.token_hash, capture.platform::text,
		       capture.application_id, capture.screen, capture.context_url,
		       capture.display_locale, capture.definition_version,
		       capture.controller_profile_version, capture.feedback_id,
		       capture.client_feedback_id, capture.request_digest, capture.accepted_at,
		       capture.submit_expires_at, capture.retry_expires_at,
		       capture.cleanup_after, capture.created_at
		FROM mobile_capture_sessions AS capture
		JOIN api_keys AS issuer
		  ON issuer.id = capture.issuer_api_key_id
		 AND issuer.tenant_id = capture.tenant_id
		 AND (issuer.expires_at IS NULL OR issuer.expires_at > $4)
		JOIN tenants AS tenant ON tenant.id = capture.tenant_id
		JOIN projects AS project
		  ON project.id = capture.project_id AND project.tenant_id = capture.tenant_id
		WHERE capture.tenant_id = $1
		  AND capture.id = $2
		  AND capture.token_hash = $3
		  AND tenant.deletion_requested_at IS NULL
		  AND (
		      tenant.status = 'active'
		      OR (
		          tenant.status = 'trial'
		          AND tenant.trial_ends_at IS NOT NULL
		          AND tenant.trial_ends_at > $4
		      )
		  )
		FOR UPDATE OF capture
	`, resolved.TenantID, resolved.SessionID, auth.HashToken(rawToken), now.UTC()).Scan(
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
