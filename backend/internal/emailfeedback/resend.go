package emailfeedback

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
)

type ResendEventApplier struct {
	Database db.Querier
	Now      func() time.Time
}

func (applier ResendEventApplier) ApplyResendEvent(
	ctx context.Context,
	providerMessageID string,
	eventType string,
	webhookEventID string,
) error {
	if eventType != "email.bounced" && eventType != "email.complained" {
		return errors.New("unsupported Resend event type")
	}
	if applier.Database == nil {
		return errors.New("email feedback Resend database is required")
	}
	now := time.Now().UTC()
	if applier.Now != nil {
		now = applier.Now().UTC()
	}
	reason := "bounce"
	status := "bounced"
	if eventType == "email.complained" {
		reason = "complaint"
		status = "complained"
	}
	var matched bool
	err := applier.Database.QueryRow(ctx, `
		WITH source AS MATERIALIZED (
			SELECT tenant_id, id, recipient_digest
			FROM email_feedback_invitations
			WHERE provider_message_id = $1
			  AND ($4::text = '' OR webhook_event_id IS DISTINCT FROM $4)
			FOR UPDATE
		), suppression AS (
			INSERT INTO email_feedback_suppressions (
				tenant_id, recipient_digest, reason, source_invitation_id,
				first_suppressed_at, last_suppressed_at
			)
			SELECT tenant_id, recipient_digest,
			       $2::email_feedback_suppression_reason, id, $5, $5
			FROM source
			ON CONFLICT (tenant_id, recipient_digest) DO UPDATE
			SET reason = CASE
			        WHEN EXCLUDED.reason = 'complaint'
			            THEN EXCLUDED.reason
			        WHEN email_feedback_suppressions.reason IN ('complaint', 'opt_out')
			            THEN email_feedback_suppressions.reason
			        ELSE EXCLUDED.reason
			    END,
			    source_invitation_id = COALESCE(
			        email_feedback_suppressions.source_invitation_id,
			        EXCLUDED.source_invitation_id
			    ),
			    last_suppressed_at = GREATEST(
			        email_feedback_suppressions.last_suppressed_at,
			        EXCLUDED.last_suppressed_at
			    )
		), source_updated AS (
			UPDATE email_feedback_invitations AS invitation
			SET status = CASE
			        WHEN invitation.status IN ('complained', 'submitted')
			            THEN invitation.status
			        ELSE $3::email_feedback_invitation_status
			    END,
			    recipient_email = NULL,
			    token_hash = NULL,
			    bounced_at = CASE
			        WHEN $3 = 'bounced' THEN COALESCE(invitation.bounced_at, $5)
			        ELSE invitation.bounced_at
			    END,
			    complained_at = CASE
			        WHEN $3 = 'complained' THEN COALESCE(invitation.complained_at, $5)
			        ELSE invitation.complained_at
			    END,
			    webhook_event_id = COALESCE(NULLIF($4, ''), invitation.webhook_event_id),
			    last_error = NULL
			FROM source
			WHERE invitation.tenant_id = source.tenant_id
			  AND invitation.id = source.id
			RETURNING invitation.tenant_id, invitation.id
		), queued_updated AS (
			UPDATE email_feedback_invitations AS invitation
			SET status = 'suppressed',
			    recipient_email = NULL,
			    token_hash = NULL,
			    suppressed_at = COALESCE(invitation.suppressed_at, $5),
			    last_error = NULL
			FROM source
			WHERE invitation.tenant_id = source.tenant_id
			  AND invitation.recipient_digest = source.recipient_digest
			  AND invitation.id <> source.id
			  AND invitation.status = 'queued'
		)
		SELECT EXISTS (SELECT 1 FROM source_updated)
	`, providerMessageID, reason, status, webhookEventID, now).Scan(&matched)
	if err != nil {
		return fmt.Errorf("apply email feedback Resend event: %w", err)
	}
	return nil
}
