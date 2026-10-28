package replay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type ConsentDiagnostic struct {
	WidgetRuleID           string       `json:"widget_rule_id"`
	ReplayMode             string       `json:"replay_mode"`
	ConsentMode            string       `json:"consent_mode"`
	NoticeVersion          string       `json:"notice_version"`
	State                  ConsentState `json:"state"`
	Locale                 string       `json:"locale,omitempty"`
	Source                 string       `json:"source,omitempty"`
	AcceptedAt             *time.Time   `json:"accepted_at,omitempty"`
	WithdrawnAt            *time.Time   `json:"withdrawn_at,omitempty"`
	ExpiresAt              *time.Time   `json:"expires_at,omitempty"`
	LatestSessionStartedAt *time.Time   `json:"latest_session_started_at,omitempty"`
}

func (store Store) GetConsentDiagnostic(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	widgetRuleID string,
	now time.Time,
) (ConsentDiagnostic, error) {
	var result ConsentDiagnostic
	if err := query.QueryRow(ctx, `
		SELECT rule.id, rule.replay_mode, rule.replay_consent->>'mode',
		       COALESCE(rule.replay_consent->>'notice_version', '')
		FROM widget_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id
		WHERE project.tenant_id = $1 AND project.id = $2 AND rule.id = $3
	`, tenantID, projectID, widgetRuleID).Scan(
		&result.WidgetRuleID, &result.ReplayMode, &result.ConsentMode, &result.NoticeVersion,
	); errors.Is(err, pgx.ErrNoRows) {
		return ConsentDiagnostic{}, ErrNotFound
	} else if err != nil {
		return ConsentDiagnostic{}, fmt.Errorf("load replay consent diagnostic rule: %w", err)
	}
	result.State = ConsentNotObserved
	if result.ReplayMode == "none" || result.ConsentMode == "none" || result.NoticeVersion == "" {
		return result, nil
	}

	consent, err := scanConsent(query.QueryRow(ctx, `
		SELECT id, tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
		       source, customer_consent_reference, state, accepted_at, withdrawn_at, expires_at,
		       created_at, updated_at
		FROM replay_consents
		WHERE tenant_id = $1 AND project_id = $2 AND widget_rule_id = $3
		  AND purpose = 'session_replay' AND notice_version = $4 AND source = $5
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, tenantID, projectID, widgetRuleID, result.NoticeVersion, result.ConsentMode))
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return ConsentDiagnostic{}, fmt.Errorf("load replay consent diagnostic evidence: %w", err)
	}
	result.State = consent.State
	result.Locale = consent.Locale
	result.Source = consent.Source
	result.AcceptedAt = consent.AcceptedAt
	result.WithdrawnAt = consent.WithdrawnAt
	result.ExpiresAt = consent.ExpiresAt
	if result.State == ConsentActive && result.ExpiresAt != nil &&
		!now.UTC().Before(*result.ExpiresAt) {
		result.State = ConsentExpired
	}
	if consent.AcceptedAt == nil {
		return result, nil
	}
	if err := query.QueryRow(ctx, `
		SELECT max(started_at)
		FROM sessions
		WHERE tenant_id = $1 AND project_id = $2 AND replay_consent_id = $3
	`, tenantID, projectID, consent.ID).Scan(&result.LatestSessionStartedAt); err != nil {
		return ConsentDiagnostic{}, fmt.Errorf("load replay consent diagnostic session: %w", err)
	}
	return result, nil
}
