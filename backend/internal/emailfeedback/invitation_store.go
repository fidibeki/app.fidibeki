package emailfeedback

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrInvitationConflict = errors.New("external event was reused with different input")
	ErrCampaignInactive   = errors.New("email feedback campaign is inactive")
	ErrLocaleUnavailable  = errors.New("email feedback campaign locale is unavailable")
	ErrInvitationToken    = errors.New("email feedback invitation token could not be created")
)

func (Store) AcceptInvitation(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issuerAPIKeyID string,
	values InvitationValues,
	tokens *InvitationTokenManager,
	now time.Time,
) (Invitation, bool, error) {
	existing, found, err := findInvitationByEvent(
		ctx, query, tenantID, values.CampaignID, values.ExternalEventID,
	)
	if err != nil {
		return Invitation{}, false, err
	}
	if found {
		return matchInvitationRetry(existing, values.RequestDigest)
	}

	campaign, err := resolveInvitationCampaign(
		ctx, query, tenantID, values.CampaignID, values.RequestedLocale,
	)
	if err != nil {
		return Invitation{}, false, err
	}
	for range 3 {
		invitationID := uuid.NewString()
		_, tokenHash, tokenErr := tokens.Token(invitationID)
		if tokenErr != nil {
			return Invitation{}, false, fmt.Errorf("%w: %v", ErrInvitationToken, tokenErr)
		}
		invitation, created, createErr := createInvitation(
			ctx, query, tenantID, issuerAPIKeyID, invitationID, tokenHash,
			values, campaign, now.UTC(),
		)
		if errors.Is(createErr, ErrInvitationToken) {
			continue
		}
		return invitation, created, createErr
	}
	return Invitation{}, false, ErrInvitationToken
}

func resolveInvitationCampaign(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	campaignID string,
	requestedLocale string,
) (invitationCampaign, error) {
	var campaign invitationCampaign
	var status CampaignStatus
	var controllerReady, definitionReady, localizationReady bool
	err := query.QueryRow(ctx, `
		SELECT campaign.project_id, campaign.widget_rule_id,
		       COALESCE(NULLIF($3, ''), campaign.default_locale),
		       campaign.invitation_ttl_days, campaign.status,
		       rule.definition_version, tenant.privacy_profile_version,
		       tenant.privacy_contact_email IS NOT NULL
		           AND tenant.privacy_rights_email IS NOT NULL
		           AND tenant.privacy_policy_url IS NOT NULL
		           AND btrim(tenant.name) <> '',
		       rule.enabled AND rule.definition_status = 'active',
		       rule.localizations ? COALESCE(NULLIF($3, ''), campaign.default_locale),
		       jsonb_build_object(
		           'name', campaign.name,
		           'subject', campaign.subject,
		           'introduction', campaign.introduction
		       ),
		       jsonb_build_object(
		           'response_blocks', rule.response_blocks,
		           'allow_comment', rule.allow_comment,
		           'collect_contact', rule.collect_contact,
		           'ai_processing', rule.ai_processing
		       ),
		       COALESCE(
		           rule.localizations -> COALESCE(NULLIF($3, ''), campaign.default_locale),
		           '{}'::jsonb
		       ),
		       jsonb_build_object(
		           'name', tenant.name,
		           'privacy_contact_email', tenant.privacy_contact_email,
		           'privacy_rights_email', tenant.privacy_rights_email,
		           'privacy_policy_url', tenant.privacy_policy_url
		       )
		FROM email_feedback_campaigns AS campaign
		JOIN projects AS project
		  ON project.tenant_id = campaign.tenant_id
		 AND project.id = campaign.project_id
		JOIN tenants AS tenant ON tenant.id = campaign.tenant_id
		JOIN widget_rules AS rule
		  ON rule.project_id = campaign.project_id
		 AND rule.id = campaign.widget_rule_id
		WHERE campaign.tenant_id = $1 AND campaign.id = $2
		FOR SHARE OF campaign, project, tenant, rule
	`, tenantID, campaignID, requestedLocale).Scan(
		&campaign.ProjectID, &campaign.WidgetRuleID, &campaign.Locale,
		&campaign.InvitationTTLDays, &status,
		&campaign.DefinitionVersion, &campaign.ControllerProfileVersion,
		&controllerReady, &definitionReady, &localizationReady,
		&campaign.CampaignSnapshot, &campaign.DefinitionSnapshot,
		&campaign.LocalizationSnapshot, &campaign.ControllerSnapshot,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return invitationCampaign{}, ErrNotFound
	}
	if err != nil {
		return invitationCampaign{}, fmt.Errorf("resolve email feedback campaign: %w", err)
	}
	if !localizationReady {
		return invitationCampaign{}, ErrLocaleUnavailable
	}
	if status != CampaignActive || !controllerReady || !definitionReady {
		return invitationCampaign{}, ErrCampaignInactive
	}
	return campaign, nil
}

func createInvitation(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issuerAPIKeyID string,
	invitationID string,
	tokenHash string,
	values InvitationValues,
	campaign invitationCampaign,
	now time.Time,
) (Invitation, bool, error) {
	var suppressed bool
	if err := query.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM email_feedback_suppressions
			WHERE tenant_id = $1 AND recipient_digest = $2
		)
	`, tenantID, values.RecipientDigest).Scan(&suppressed); err != nil {
		return Invitation{}, false, fmt.Errorf("check email feedback suppression: %w", err)
	}
	status := InvitationQueued
	var recipientEmail any = values.RecipientEmail
	var persistedTokenHash any = tokenHash
	var suppressedAt any
	if suppressed {
		status = InvitationSuppressed
		recipientEmail = nil
		persistedTokenHash = nil
		suppressedAt = now
	}
	expiresAt := now.Add(time.Duration(campaign.InvitationTTLDays) * 24 * time.Hour)
	tag, err := query.Exec(ctx, `
		INSERT INTO email_feedback_invitations (
			id, tenant_id, project_id, campaign_id, widget_rule_id,
			issuer_api_key_id, external_event_id, request_digest,
			recipient_email, recipient_digest, locale, context_url, token_hash,
			definition_version, controller_profile_version,
			campaign_snapshot, definition_snapshot, localization_snapshot,
			controller_snapshot, status, suppressed_at, expires_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11, $12, $13,
			$14, $15,
			$16, $17, $18,
			$19, $20, $21, $22,
			$23, $23
		)
		ON CONFLICT (tenant_id, campaign_id, external_event_id) DO NOTHING
	`, invitationID, tenantID, campaign.ProjectID, values.CampaignID,
		campaign.WidgetRuleID, issuerAPIKeyID, values.ExternalEventID,
		values.RequestDigest, recipientEmail, values.RecipientDigest, campaign.Locale,
		values.ContextURL, persistedTokenHash, campaign.DefinitionVersion,
		campaign.ControllerProfileVersion, campaign.CampaignSnapshot,
		campaign.DefinitionSnapshot, campaign.LocalizationSnapshot,
		campaign.ControllerSnapshot, status, suppressedAt, expiresAt, now)
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return Invitation{}, false, ErrInvitationToken
	}
	if err != nil {
		return Invitation{}, false, fmt.Errorf("create email feedback invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		existing, found, findErr := findInvitationByEvent(
			ctx, query, tenantID, values.CampaignID, values.ExternalEventID,
		)
		if findErr != nil {
			return Invitation{}, false, findErr
		}
		if !found {
			return Invitation{}, false, errors.New("email feedback invitation conflict disappeared")
		}
		return matchInvitationRetry(existing, values.RequestDigest)
	}
	if status == InvitationQueued {
		if _, err := query.Exec(ctx, `
			INSERT INTO outbox_events (
				tenant_id, event, resource_type, resource_id, event_key
			) VALUES (
				$1, $2, 'email_feedback_invitation', $3, $4
			)
		`, tenantID, EventInvitationCreated, invitationID, invitationID); err != nil {
			return Invitation{}, false, fmt.Errorf("record email feedback invitation event: %w", err)
		}
	}
	return Invitation{
		ID: invitationID, TenantID: tenantID, ProjectID: campaign.ProjectID,
		CampaignID: values.CampaignID, WidgetRuleID: campaign.WidgetRuleID,
		ExternalEventID: values.ExternalEventID, Status: status, Locale: campaign.Locale,
		ContextURL: values.ContextURL, ExpiresAt: expiresAt, CreatedAt: now,
		RequestDigest: values.RequestDigest,
	}, true, nil
}

func findInvitationByEvent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	campaignID string,
	externalEventID string,
) (Invitation, bool, error) {
	invitation, err := scanInvitation(query.QueryRow(ctx, `
		SELECT id, tenant_id, project_id, campaign_id, widget_rule_id,
		       external_event_id, status, locale, context_url,
		       expires_at, created_at, request_digest
		FROM email_feedback_invitations
		WHERE tenant_id = $1 AND campaign_id = $2 AND external_event_id = $3
	`, tenantID, campaignID, externalEventID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, false, nil
	}
	if err != nil {
		return Invitation{}, false, fmt.Errorf("find email feedback invitation: %w", err)
	}
	return invitation, true, nil
}

func matchInvitationRetry(
	existing Invitation,
	requestDigest string,
) (Invitation, bool, error) {
	if subtle.ConstantTimeCompare(
		[]byte(existing.RequestDigest), []byte(requestDigest),
	) != 1 {
		return Invitation{}, false, ErrInvitationConflict
	}
	return existing, false, nil
}

func scanInvitation(row campaignScanner) (Invitation, error) {
	var invitation Invitation
	err := row.Scan(
		&invitation.ID, &invitation.TenantID, &invitation.ProjectID,
		&invitation.CampaignID, &invitation.WidgetRuleID,
		&invitation.ExternalEventID, &invitation.Status, &invitation.Locale,
		&invitation.ContextURL, &invitation.ExpiresAt, &invitation.CreatedAt,
		&invitation.RequestDigest,
	)
	return invitation, err
}
