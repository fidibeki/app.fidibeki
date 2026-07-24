package widgetanalytics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

const (
	analyticsConsentExpiringSoonWindow   = 7 * 24 * time.Hour
	analyticsConsentRecentActivityWindow = 30 * 24 * time.Hour
)

type ConsentDiagnostic struct {
	WidgetRuleID      string     `json:"widget_rule_id"`
	ConsentMode       string     `json:"consent_mode"`
	NoticeVersion     string     `json:"notice_version"`
	ActiveCount       int        `json:"active_count"`
	ExpiringSoonCount int        `json:"expiring_soon_count"`
	DeniedCount       int        `json:"denied_count"`
	WithdrawnCount    int        `json:"withdrawn_count"`
	ExpiredCount      int        `json:"expired_count"`
	RecentCount       int        `json:"recent_count"`
	NextExpiresAt     *time.Time `json:"next_expires_at,omitempty"`
	LastActivityAt    *time.Time `json:"last_activity_at,omitempty"`
}

func (Store) GetConsentDiagnostic(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	widgetRuleID string,
	now time.Time,
) (ConsentDiagnostic, error) {
	var result ConsentDiagnostic
	var mode string
	if err := query.QueryRow(ctx, `
		SELECT rule.id, COALESCE(rule.analytics_consent->>'mode', 'none'),
		       COALESCE(rule.analytics_consent->>'notice_version', '')
		FROM widget_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id
		WHERE project.tenant_id = $1 AND project.id = $2 AND rule.id = $3
	`, tenantID, projectID, widgetRuleID).Scan(
		&result.WidgetRuleID, &mode, &result.NoticeVersion,
	); errors.Is(err, pgx.ErrNoRows) {
		return ConsentDiagnostic{}, ErrNotFound
	} else if err != nil {
		return ConsentDiagnostic{}, fmt.Errorf("load widget analytics consent diagnostic rule: %w", err)
	}
	result.ConsentMode = mode
	if mode == "none" || result.NoticeVersion == "" {
		return result, nil
	}

	now = now.UTC()
	expiringSoonBefore := now.Add(analyticsConsentExpiringSoonWindow)
	recentSince := now.Add(-analyticsConsentRecentActivityWindow)
	if err := query.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state = 'active' AND expires_at > $5),
			count(*) FILTER (WHERE state = 'active' AND expires_at > $5 AND expires_at <= $6),
			count(*) FILTER (WHERE state = 'denied'),
			count(*) FILTER (WHERE state = 'withdrawn'),
			count(*) FILTER (WHERE state = 'expired' OR (state = 'active' AND expires_at <= $5)),
			count(*) FILTER (WHERE updated_at >= $7),
			min(expires_at) FILTER (WHERE state = 'active' AND expires_at > $5),
			max(updated_at)
		FROM widget_analytics_consents
		WHERE tenant_id = $1 AND project_id = $2 AND widget_rule_id = $3
		  AND purpose = 'widget_analytics' AND notice_version = $4 AND source = $8
	`, tenantID, projectID, widgetRuleID, result.NoticeVersion, now, expiringSoonBefore, recentSince, mode).Scan(
		&result.ActiveCount, &result.ExpiringSoonCount, &result.DeniedCount, &result.WithdrawnCount,
		&result.ExpiredCount, &result.RecentCount, &result.NextExpiresAt, &result.LastActivityAt,
	); err != nil {
		return ConsentDiagnostic{}, fmt.Errorf("load widget analytics consent diagnostic evidence: %w", err)
	}
	return result, nil
}
