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

var ErrInvitationPublicToken = errors.New("email feedback invitation token is unavailable")

type PublicInvitationResolution struct {
	TenantID   string
	TenantSlug string
	ID         string
}

type InvitationOptOut struct {
	ID               string
	TenantID         string
	RecipientDigest  string
	Locale           string
	Status           InvitationStatus
	ControllerName   string
	PrivacyPolicyURL string
	Suppressed       bool
}

func (Store) ResolvePublicInvitation(
	ctx context.Context,
	query db.Querier,
	tokenHash string,
) (PublicInvitationResolution, error) {
	var resolved PublicInvitationResolution
	err := query.QueryRow(ctx, `
		SELECT invitation.tenant_id, tenant.slug, invitation.id
		FROM email_feedback_invitations AS invitation
		JOIN tenants AS tenant ON tenant.id = invitation.tenant_id
		WHERE invitation.token_hash = $1
	`, tokenHash).Scan(&resolved.TenantID, &resolved.TenantSlug, &resolved.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicInvitationResolution{}, ErrInvitationPublicToken
	}
	if err != nil {
		return PublicInvitationResolution{}, fmt.Errorf("resolve public email feedback invitation: %w", err)
	}
	return resolved, nil
}

func (Store) LoadInvitationOptOut(
	ctx context.Context,
	query db.Querier,
	resolved PublicInvitationResolution,
	tokenHash string,
	lock bool,
) (InvitationOptOut, error) {
	statement := `
		SELECT invitation.id, invitation.tenant_id, invitation.recipient_digest,
		       invitation.locale, invitation.status, invitation.controller_snapshot,
		       EXISTS (
		           SELECT 1
		           FROM email_feedback_suppressions AS suppression
		           WHERE suppression.tenant_id = invitation.tenant_id
		             AND suppression.recipient_digest = invitation.recipient_digest
		       )
		FROM email_feedback_invitations AS invitation
		WHERE invitation.tenant_id = $1
		  AND invitation.id = $2
		  AND invitation.token_hash = $3
	`
	if lock {
		statement += " FOR UPDATE"
	}
	var optOut InvitationOptOut
	var controllerSnapshot json.RawMessage
	err := query.QueryRow(
		ctx, statement, resolved.TenantID, resolved.ID, tokenHash,
	).Scan(
		&optOut.ID, &optOut.TenantID, &optOut.RecipientDigest,
		&optOut.Locale, &optOut.Status, &controllerSnapshot, &optOut.Suppressed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InvitationOptOut{}, ErrInvitationPublicToken
	}
	if err != nil {
		return InvitationOptOut{}, fmt.Errorf("load email feedback opt-out: %w", err)
	}
	var controller invitationControllerSnapshot
	if err := json.Unmarshal(controllerSnapshot, &controller); err != nil {
		return InvitationOptOut{}, fmt.Errorf("decode email feedback opt-out controller: %w", err)
	}
	optOut.ControllerName = controller.Name
	optOut.PrivacyPolicyURL = controller.PrivacyPolicyURL
	return optOut, nil
}

func (Store) SuppressInvitationRecipient(
	ctx context.Context,
	query db.Querier,
	optOut InvitationOptOut,
	now time.Time,
) error {
	if _, err := query.Exec(ctx, `
		INSERT INTO email_feedback_suppressions (
			tenant_id, recipient_digest, reason, source_invitation_id,
			first_suppressed_at, last_suppressed_at
		) VALUES ($1, $2, 'opt_out', $3, $4, $4)
		ON CONFLICT (tenant_id, recipient_digest) DO UPDATE
		SET reason = CASE
		        WHEN email_feedback_suppressions.reason = 'complaint'
		            THEN email_feedback_suppressions.reason
		        ELSE 'opt_out'::email_feedback_suppression_reason
		    END,
		    source_invitation_id = COALESCE(
		        email_feedback_suppressions.source_invitation_id,
		        EXCLUDED.source_invitation_id
		    ),
		    last_suppressed_at = GREATEST(
		        email_feedback_suppressions.last_suppressed_at,
		        EXCLUDED.last_suppressed_at
		    )
	`, optOut.TenantID, optOut.RecipientDigest, optOut.ID, now.UTC()); err != nil {
		return fmt.Errorf("record email feedback opt-out: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE email_feedback_invitations
		SET status = CASE
		        WHEN id = $3
		             AND status IN ('bounced', 'complained', 'submitted')
		            THEN status
		        ELSE 'suppressed'::email_feedback_invitation_status
		    END,
		    recipient_email = NULL,
		    token_hash = CASE WHEN id = $3 THEN token_hash ELSE NULL END,
		    suppressed_at = COALESCE(suppressed_at, $4),
		    last_error = NULL
		WHERE tenant_id = $1
		  AND recipient_digest = $2
		  AND (id = $3 OR status = 'queued')
	`, optOut.TenantID, optOut.RecipientDigest, optOut.ID, now.UTC()); err != nil {
		return fmt.Errorf("suppress email feedback invitations: %w", err)
	}
	return nil
}
