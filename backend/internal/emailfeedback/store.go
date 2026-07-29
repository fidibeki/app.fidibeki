package emailfeedback

import (
	"context"
	"errors"
	"fmt"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound      = errors.New("email feedback campaign not found")
	ErrKeyConflict   = errors.New("email feedback campaign key conflict")
	ErrNotReady      = errors.New("email feedback campaign is not ready")
	ErrInvalidStatus = errors.New("invalid email feedback campaign status transition")
	ErrArchived      = errors.New("email feedback campaign is archived")
)

type Store struct{}

func (Store) ListCampaigns(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
) ([]Campaign, error) {
	rows, err := query.Query(ctx, `
		SELECT campaign.id, campaign.tenant_id, campaign.project_id,
		       campaign.widget_rule_id, rule.name, campaign.campaign_key,
		       campaign.name, campaign.status, campaign.default_locale,
		       campaign.subject, campaign.introduction, campaign.invitation_ttl_days,
		       campaign.created_at, campaign.updated_at,
		       tenant.privacy_contact_email IS NOT NULL
		           AND tenant.privacy_rights_email IS NOT NULL
		           AND tenant.privacy_policy_url IS NOT NULL
		           AND btrim(tenant.name) <> '',
		       rule.enabled AND rule.definition_status = 'active',
		       rule.localizations ? campaign.default_locale
		FROM email_feedback_campaigns AS campaign
		JOIN tenants AS tenant ON tenant.id = campaign.tenant_id
		JOIN widget_rules AS rule
		  ON rule.project_id = campaign.project_id
		 AND rule.id = campaign.widget_rule_id
		WHERE campaign.tenant_id = $1
		  AND ($2 = '' OR campaign.project_id = NULLIF($2, '')::uuid)
		ORDER BY campaign.created_at, campaign.id
	`, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list email feedback campaigns: %w", err)
	}
	defer rows.Close()
	campaigns := make([]Campaign, 0)
	for rows.Next() {
		campaign, err := scanCampaign(rows)
		if err != nil {
			return nil, fmt.Errorf("scan email feedback campaign: %w", err)
		}
		campaigns = append(campaigns, campaign)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate email feedback campaigns: %w", err)
	}
	return campaigns, nil
}

func (Store) GetCampaign(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	campaignID string,
) (Campaign, error) {
	campaign, err := scanCampaign(query.QueryRow(ctx, `
		SELECT campaign.id, campaign.tenant_id, campaign.project_id,
		       campaign.widget_rule_id, rule.name, campaign.campaign_key,
		       campaign.name, campaign.status, campaign.default_locale,
		       campaign.subject, campaign.introduction, campaign.invitation_ttl_days,
		       campaign.created_at, campaign.updated_at,
		       tenant.privacy_contact_email IS NOT NULL
		           AND tenant.privacy_rights_email IS NOT NULL
		           AND tenant.privacy_policy_url IS NOT NULL
		           AND btrim(tenant.name) <> '',
		       rule.enabled AND rule.definition_status = 'active',
		       rule.localizations ? campaign.default_locale
		FROM email_feedback_campaigns AS campaign
		JOIN tenants AS tenant ON tenant.id = campaign.tenant_id
		JOIN widget_rules AS rule
		  ON rule.project_id = campaign.project_id
		 AND rule.id = campaign.widget_rule_id
		WHERE campaign.tenant_id = $1 AND campaign.id = $2
	`, tenantID, campaignID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Campaign{}, ErrNotFound
	}
	if err != nil {
		return Campaign{}, fmt.Errorf("get email feedback campaign: %w", err)
	}
	return campaign, nil
}

func (Store) CreateCampaign(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	values CampaignValues,
) (Campaign, error) {
	readiness, err := lockCampaignDependencies(
		ctx, query, tenantID, values.ProjectID, values.WidgetRuleID, values.DefaultLocale,
	)
	if err != nil {
		return Campaign{}, err
	}
	if values.Status == CampaignActive && !readiness.Ready {
		return Campaign{}, ErrNotReady
	}
	var campaignID string
	err = query.QueryRow(ctx, `
		INSERT INTO email_feedback_campaigns (
			tenant_id, project_id, widget_rule_id, campaign_key, name, status,
			default_locale, subject, introduction, invitation_ttl_days,
			created_by, updated_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11
		)
		RETURNING id
	`, tenantID, values.ProjectID, values.WidgetRuleID, values.CampaignKey,
		values.Name, values.Status, values.DefaultLocale, values.Subject,
		values.Introduction, values.InvitationTTLDays, actorMemberID,
	).Scan(&campaignID)
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return Campaign{}, ErrKeyConflict
	}
	if err != nil {
		return Campaign{}, fmt.Errorf("create email feedback campaign: %w", err)
	}
	return Store{}.GetCampaign(ctx, query, tenantID, campaignID)
}

func (Store) UpdateCampaign(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	campaignID string,
	actorMemberID string,
	values CampaignValues,
) (Campaign, error) {
	var current CampaignStatus
	var projectID string
	err := query.QueryRow(ctx, `
		SELECT status, project_id
		FROM email_feedback_campaigns
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, campaignID).Scan(&current, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Campaign{}, ErrNotFound
	}
	if err != nil {
		return Campaign{}, fmt.Errorf("lock email feedback campaign: %w", err)
	}
	if current == CampaignArchived {
		return Campaign{}, ErrArchived
	}
	if projectID != values.ProjectID {
		return Campaign{}, ErrNotFound
	}
	if !ValidTransition(current, values.Status) {
		return Campaign{}, ErrInvalidStatus
	}
	readiness, err := lockCampaignDependencies(
		ctx, query, tenantID, values.ProjectID, values.WidgetRuleID, values.DefaultLocale,
	)
	if err != nil {
		return Campaign{}, err
	}
	if values.Status == CampaignActive && !readiness.Ready {
		return Campaign{}, ErrNotReady
	}
	tag, err := query.Exec(ctx, `
		UPDATE email_feedback_campaigns
		SET widget_rule_id = $3, campaign_key = $4, name = $5, status = $6,
		    default_locale = $7, subject = $8, introduction = $9,
		    invitation_ttl_days = $10, updated_by = $11
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, campaignID, values.WidgetRuleID, values.CampaignKey,
		values.Name, values.Status, values.DefaultLocale, values.Subject,
		values.Introduction, values.InvitationTTLDays, actorMemberID)
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return Campaign{}, ErrKeyConflict
	}
	if err != nil {
		return Campaign{}, fmt.Errorf("update email feedback campaign: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return Campaign{}, ErrNotFound
	}
	return Store{}.GetCampaign(ctx, query, tenantID, campaignID)
}

func lockCampaignDependencies(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	widgetRuleID string,
	locale string,
) (CampaignReadiness, error) {
	var controllerReady, definitionReady, localizationReady bool
	err := query.QueryRow(ctx, `
		SELECT tenant.privacy_contact_email IS NOT NULL
		           AND tenant.privacy_rights_email IS NOT NULL
		           AND tenant.privacy_policy_url IS NOT NULL
		           AND btrim(tenant.name) <> '',
		       rule.enabled AND rule.definition_status = 'active',
		       rule.localizations ? $4
		FROM projects AS project
		JOIN tenants AS tenant ON tenant.id = project.tenant_id
		JOIN widget_rules AS rule
		  ON rule.project_id = project.id
		 AND rule.id = $3
		WHERE tenant.id = $1 AND project.id = $2
		FOR SHARE OF tenant, project, rule
	`, tenantID, projectID, widgetRuleID, locale).Scan(
		&controllerReady, &definitionReady, &localizationReady,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CampaignReadiness{}, ErrNotFound
	}
	if err != nil {
		return CampaignReadiness{}, fmt.Errorf("lock email feedback campaign dependencies: %w", err)
	}
	return campaignReadiness(controllerReady, definitionReady, localizationReady), nil
}

func campaignReadiness(
	controllerReady bool,
	definitionReady bool,
	localizationReady bool,
) CampaignReadiness {
	reasons := make([]string, 0, 3)
	if !controllerReady {
		reasons = append(reasons, "controller_profile_incomplete")
	}
	if !definitionReady {
		reasons = append(reasons, "widget_definition_inactive")
	}
	if !localizationReady {
		reasons = append(reasons, "localization_missing")
	}
	return CampaignReadiness{Ready: len(reasons) == 0, Reasons: reasons}
}

type campaignScanner interface {
	Scan(...any) error
}

func scanCampaign(row campaignScanner) (Campaign, error) {
	var campaign Campaign
	var controllerReady, definitionReady, localizationReady bool
	err := row.Scan(
		&campaign.ID, &campaign.TenantID, &campaign.ProjectID,
		&campaign.WidgetRuleID, &campaign.WidgetRuleName, &campaign.CampaignKey,
		&campaign.Name, &campaign.Status, &campaign.DefaultLocale,
		&campaign.Subject, &campaign.Introduction, &campaign.InvitationTTLDays,
		&campaign.CreatedAt, &campaign.UpdatedAt,
		&controllerReady, &definitionReady, &localizationReady,
	)
	campaign.Readiness = campaignReadiness(
		controllerReady, definitionReady, localizationReady,
	)
	return campaign, err
}
