package emailfeedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

var ErrInvitationDeliveryNotFound = errors.New("email feedback invitation delivery does not exist")

func (Store) LoadInvitationDelivery(
	ctx context.Context,
	query db.Querier,
	task DeliveryTask,
) (InvitationDelivery, error) {
	var delivery InvitationDelivery
	var snapshots deliverySnapshots
	err := query.QueryRow(ctx, `
		SELECT event.id, invitation.tenant_id, tenant.slug, invitation.id,
		       invitation.status, invitation.recipient_email::text,
		       invitation.recipient_digest, invitation.token_hash,
		       invitation.locale, invitation.expires_at,
		       invitation.campaign_snapshot, invitation.controller_snapshot,
		       EXISTS (
		           SELECT 1
		           FROM email_feedback_suppressions AS suppression
		           WHERE suppression.tenant_id = invitation.tenant_id
		             AND suppression.recipient_digest = invitation.recipient_digest
		       )
		FROM outbox_events AS event
		JOIN email_feedback_invitations AS invitation
		  ON invitation.tenant_id = event.tenant_id
		 AND invitation.id = event.resource_id
		JOIN tenants AS tenant ON tenant.id = invitation.tenant_id
		WHERE event.id = $1
		  AND event.tenant_id = $2
		  AND event.resource_id = $3
		  AND event.event = $4
		  AND event.resource_type = 'email_feedback_invitation'
		FOR UPDATE OF invitation
	`, task.OutboxEventID, task.TenantID, task.ResourceID, EventInvitationCreated).Scan(
		&delivery.OutboxEventID, &delivery.TenantID, &delivery.TenantSlug,
		&delivery.InvitationID, &delivery.Status, &delivery.RecipientEmail,
		&delivery.RecipientDigest, &delivery.TokenHash, &delivery.Locale,
		&delivery.ExpiresAt, &snapshots.Campaign, &snapshots.Controller,
		&delivery.Suppressed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InvitationDelivery{}, ErrInvitationDeliveryNotFound
	}
	if err != nil {
		return InvitationDelivery{}, fmt.Errorf("load email feedback invitation delivery: %w", err)
	}
	if delivery.Status != InvitationQueued {
		return delivery, nil
	}
	if err := json.Unmarshal(snapshots.Campaign, &delivery.CampaignSnapshot); err != nil {
		return InvitationDelivery{}, fmt.Errorf("decode email feedback campaign snapshot: %w", err)
	}
	if err := json.Unmarshal(snapshots.Controller, &delivery.ControllerProfile); err != nil {
		return InvitationDelivery{}, fmt.Errorf("decode email feedback controller snapshot: %w", err)
	}
	return delivery, nil
}

func (Store) MarkInvitationSent(
	ctx context.Context,
	query db.Querier,
	delivery InvitationDelivery,
	providerMessageID string,
	now time.Time,
) error {
	tag, err := query.Exec(ctx, `
		UPDATE email_feedback_invitations
		SET status = 'sent',
		    provider_message_id = $3,
		    sent_at = $4,
		    recipient_email = NULL,
		    last_error = NULL
		WHERE tenant_id = $1 AND id = $2 AND status = 'queued'
	`, delivery.TenantID, delivery.InvitationID, providerMessageID, now.UTC())
	if err != nil {
		return fmt.Errorf("mark email feedback invitation sent: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("email feedback invitation changed during delivery")
	}
	return nil
}

func (Store) MarkInvitationSuppressed(
	ctx context.Context,
	query db.Querier,
	delivery InvitationDelivery,
	now time.Time,
) error {
	if _, err := query.Exec(ctx, `
		UPDATE email_feedback_invitations
		SET status = 'suppressed',
		    recipient_email = NULL,
		    token_hash = NULL,
		    suppressed_at = COALESCE(suppressed_at, $3),
		    last_error = NULL
		WHERE tenant_id = $1 AND id = $2 AND status = 'queued'
	`, delivery.TenantID, delivery.InvitationID, now.UTC()); err != nil {
		return fmt.Errorf("suppress email feedback invitation delivery: %w", err)
	}
	return nil
}

func (Store) MarkInvitationExpired(
	ctx context.Context,
	query db.Querier,
	delivery InvitationDelivery,
	now time.Time,
) error {
	if _, err := query.Exec(ctx, `
		UPDATE email_feedback_invitations
		SET status = 'expired',
		    recipient_email = NULL,
		    token_hash = NULL,
		    expired_at = COALESCE(expired_at, $3),
		    last_error = NULL
		WHERE tenant_id = $1 AND id = $2 AND status = 'queued'
	`, delivery.TenantID, delivery.InvitationID, now.UTC()); err != nil {
		return fmt.Errorf("expire email feedback invitation delivery: %w", err)
	}
	return nil
}

func (Store) RecordInvitationDeliveryFailure(
	ctx context.Context,
	query db.Querier,
	delivery InvitationDelivery,
	message string,
	terminal bool,
	now time.Time,
) error {
	message = truncateInvitationError(message)
	if terminal {
		if _, err := query.Exec(ctx, `
			UPDATE email_feedback_invitations
			SET status = 'failed',
			    recipient_email = NULL,
			    token_hash = NULL,
			    failed_at = COALESCE(failed_at, $4),
			    last_error = $3
			WHERE tenant_id = $1 AND id = $2 AND status = 'queued'
		`, delivery.TenantID, delivery.InvitationID, message, now.UTC()); err != nil {
			return fmt.Errorf("mark email feedback invitation delivery failed: %w", err)
		}
		return nil
	}
	if _, err := query.Exec(ctx, `
		UPDATE email_feedback_invitations
		SET last_error = $3
		WHERE tenant_id = $1 AND id = $2 AND status = 'queued'
	`, delivery.TenantID, delivery.InvitationID, message); err != nil {
		return fmt.Errorf("record email feedback invitation delivery error: %w", err)
	}
	return nil
}

func truncateInvitationError(message string) string {
	runes := []rune(message)
	if len(runes) > 2000 {
		runes = runes[:2000]
	}
	if len(runes) == 0 {
		return "email delivery failed"
	}
	return string(runes)
}
