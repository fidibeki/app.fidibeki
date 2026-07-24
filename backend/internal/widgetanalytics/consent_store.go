package widgetanalytics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ConsentStore struct {
	Database db.Querier
}

func (ConsentStore) RecordConsent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	widgetRuleID string,
	noticeVersion string,
	locale string,
	source string,
	customerReference *string,
	tokenHash *string,
	state ConsentState,
	now time.Time,
	expiresAt *time.Time,
) (Consent, error) {
	var acceptedAt *time.Time
	if state == ConsentActive {
		accepted := now.UTC()
		acceptedAt = &accepted
	}
	consent, err := scanConsentRow(query.QueryRow(ctx, `
		INSERT INTO widget_analytics_consents (
			id, tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
			source, customer_consent_reference, token_hash, state, accepted_at, expires_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, 'widget_analytics', $5, $6, $7, $8, $9, $10, $11, $12, $13, $13
		)
		ON CONFLICT (token_hash) DO UPDATE SET updated_at = widget_analytics_consents.updated_at
		RETURNING id, tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
			source, customer_consent_reference, state, accepted_at, withdrawn_at, expires_at,
			created_at, updated_at
	`, uuid.NewString(), tenantID, projectID, widgetRuleID, noticeVersion, locale, source,
		customerReference, tokenHash, state, acceptedAt, expiresAt, now.UTC()))
	if err != nil {
		return Consent{}, fmt.Errorf("record widget analytics consent: %w", err)
	}
	if consent.TenantID != tenantID || consent.ProjectID != projectID ||
		consent.WidgetRuleID != widgetRuleID || consent.NoticeVersion != noticeVersion ||
		consent.Locale != locale || consent.Source != source || consent.State != state ||
		!sameOptionalConsentString(consent.CustomerConsentReference, customerReference) {
		return Consent{}, ErrConsentRequired
	}
	return consent, nil
}

func (ConsentStore) AuthorizeConsent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	widgetRuleID string,
	noticeVersion string,
	source string,
	tokenHash string,
	now time.Time,
) (Consent, error) {
	consent, err := scanConsentRow(query.QueryRow(ctx, `
		SELECT consent.id, consent.tenant_id, consent.project_id, consent.widget_rule_id,
			consent.purpose, consent.notice_version, consent.locale, consent.source,
			consent.customer_consent_reference, consent.state, consent.accepted_at,
			consent.withdrawn_at, consent.expires_at, consent.created_at, consent.updated_at
		FROM widget_analytics_consents AS consent
		JOIN widget_rules AS rule
		  ON rule.project_id = consent.project_id
		 AND rule.id = consent.widget_rule_id
		WHERE consent.tenant_id = $1
		  AND consent.project_id = $2
		  AND consent.widget_rule_id = $3
		  AND consent.purpose = 'widget_analytics'
		  AND consent.notice_version = $4
		  AND consent.source = $5
		  AND consent.token_hash = $6
		  AND rule.enabled
		  AND rule.analytics_consent->>'mode' <> 'none'
		  AND rule.analytics_consent->>'mode' = consent.source
		  AND rule.analytics_consent->>'notice_version' = consent.notice_version
		FOR UPDATE OF consent
	`, tenantID, projectID, widgetRuleID, noticeVersion, source, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return Consent{}, ErrConsentRequired
	}
	if err != nil {
		return Consent{}, fmt.Errorf("authorize widget analytics consent: %w", err)
	}
	if consent.State == ConsentActive && consent.ExpiresAt != nil &&
		!now.UTC().Before(*consent.ExpiresAt) {
		if _, updateErr := query.Exec(ctx, `
			UPDATE widget_analytics_consents
			SET state = 'expired', updated_at = $2
			WHERE id = $1 AND state = 'active'
		`, consent.ID, now.UTC()); updateErr != nil {
			return Consent{}, fmt.Errorf("expire widget analytics consent: %w", updateErr)
		}
		return Consent{}, errors.Join(ErrConsentRequired, ErrConsentExpired)
	}
	if consent.State != ConsentActive || consent.AcceptedAt == nil || consent.ExpiresAt == nil {
		return Consent{}, ErrConsentRequired
	}
	return consent, nil
}

func (ConsentStore) RevokeConsent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	tokenHash string,
	now time.Time,
) (Consent, error) {
	consent, err := scanConsentRow(query.QueryRow(ctx, `
		SELECT id, tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
			source, customer_consent_reference, state, accepted_at, withdrawn_at, expires_at,
			created_at, updated_at
		FROM widget_analytics_consents
		WHERE tenant_id = $1 AND project_id = $2 AND token_hash = $3
		FOR UPDATE
	`, tenantID, projectID, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return Consent{}, ErrConsentRequired
	}
	if err != nil {
		return Consent{}, fmt.Errorf("lock widget analytics consent for revocation: %w", err)
	}
	if consent.State == ConsentWithdrawn {
		return consent, nil
	}
	if consent.State != ConsentActive {
		return Consent{}, ErrConsentRequired
	}
	withdrawnAt := now.UTC()
	consent, err = scanConsentRow(query.QueryRow(ctx, `
		UPDATE widget_analytics_consents
		SET state = 'withdrawn', withdrawn_at = $2, updated_at = $2
		WHERE id = $1 AND state = 'active'
		RETURNING id, tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
			source, customer_consent_reference, state, accepted_at, withdrawn_at, expires_at,
			created_at, updated_at
	`, consent.ID, withdrawnAt))
	if err != nil {
		return Consent{}, fmt.Errorf("revoke widget analytics consent: %w", err)
	}
	return consent, nil
}

type consentRowScanner interface {
	Scan(...any) error
}

func scanConsentRow(row consentRowScanner) (Consent, error) {
	var consent Consent
	err := row.Scan(
		&consent.ID, &consent.TenantID, &consent.ProjectID, &consent.WidgetRuleID,
		&consent.Purpose, &consent.NoticeVersion, &consent.Locale, &consent.Source,
		&consent.CustomerConsentReference, &consent.State, &consent.AcceptedAt,
		&consent.WithdrawnAt, &consent.ExpiresAt, &consent.CreatedAt, &consent.UpdatedAt,
	)
	return consent, err
}

func sameOptionalConsentString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
