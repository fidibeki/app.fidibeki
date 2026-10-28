package emailfeedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

func (Store) LoadInvitationCollection(
	ctx context.Context,
	query db.Querier,
	resolved PublicInvitationResolution,
	tokenHash string,
	lock bool,
) (InvitationCollection, error) {
	statement := `
		SELECT invitation.id, invitation.tenant_id, tenant.slug,
		       invitation.project_id, invitation.widget_rule_id,
		       invitation.locale, invitation.context_url, invitation.status,
		       invitation.feedback_id, invitation.definition_version,
		       invitation.controller_profile_version, invitation.expires_at,
		       tenant.plan, tenant.status, tenant.trial_ends_at,
		       tenant.deletion_requested_at,
		       invitation.campaign_snapshot, invitation.definition_snapshot,
		       invitation.localization_snapshot, invitation.controller_snapshot,
		       EXISTS (
		           SELECT 1
		           FROM email_feedback_suppressions AS suppression
		           WHERE suppression.tenant_id = invitation.tenant_id
		             AND suppression.recipient_digest = invitation.recipient_digest
		       )
		FROM email_feedback_invitations AS invitation
		JOIN tenants AS tenant ON tenant.id = invitation.tenant_id
		WHERE invitation.tenant_id = $1
		  AND invitation.id = $2
		  AND invitation.token_hash = $3
	`
	if lock {
		statement += " FOR UPDATE OF invitation"
	}
	var collection InvitationCollection
	var campaignSnapshot, definitionSnapshot, localizationSnapshot, controllerSnapshot json.RawMessage
	err := query.QueryRow(
		ctx, statement, resolved.TenantID, resolved.ID, tokenHash,
	).Scan(
		&collection.ID, &collection.TenantID, &collection.TenantSlug,
		&collection.ProjectID, &collection.WidgetRuleID,
		&collection.Locale, &collection.ContextURL, &collection.Status,
		&collection.FeedbackID, &collection.DefinitionVersion,
		&collection.ControllerProfileVersion, &collection.ExpiresAt,
		&collection.Plan, &collection.TenantStatus, &collection.TrialEndsAt,
		&collection.DeletionRequestedAt,
		&campaignSnapshot, &definitionSnapshot, &localizationSnapshot,
		&controllerSnapshot, &collection.Suppressed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InvitationCollection{}, ErrInvitationPublicToken
	}
	if err != nil {
		return InvitationCollection{}, fmt.Errorf("load email feedback collection: %w", err)
	}
	for name, snapshot := range map[string]struct {
		raw         json.RawMessage
		destination any
	}{
		"campaign":     {campaignSnapshot, &collection.Campaign},
		"definition":   {definitionSnapshot, &collection.Definition},
		"localization": {localizationSnapshot, &collection.Localization},
		"controller":   {controllerSnapshot, &collection.Controller},
	} {
		if err := json.Unmarshal(snapshot.raw, snapshot.destination); err != nil {
			return InvitationCollection{}, fmt.Errorf(
				"decode email feedback %s snapshot: %w", name, err,
			)
		}
	}
	return collection, nil
}

func (Store) MarkInvitationSubmitted(
	ctx context.Context,
	query db.Querier,
	collection InvitationCollection,
	feedbackID string,
	now time.Time,
) error {
	tag, err := query.Exec(ctx, `
		UPDATE email_feedback_invitations
		SET status = 'submitted',
		    feedback_id = $3,
		    submitted_at = $4,
		    recipient_email = NULL,
		    last_error = NULL
		WHERE tenant_id = $1
		  AND id = $2
		  AND status = 'sent'
		  AND feedback_id IS NULL
	`, collection.TenantID, collection.ID, feedbackID, now.UTC())
	if err != nil {
		return fmt.Errorf("mark email feedback invitation submitted: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrInvitationPublicToken
	}
	return nil
}
