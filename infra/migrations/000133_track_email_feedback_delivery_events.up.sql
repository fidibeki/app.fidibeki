ALTER TABLE email_feedback_invitations
    ADD COLUMN webhook_event_id text CHECK (
        webhook_event_id IS NULL
        OR char_length(webhook_event_id) BETWEEN 1 AND 500
    );

CREATE UNIQUE INDEX idx_email_feedback_invitations_provider_message
    ON email_feedback_invitations (provider_message_id)
    WHERE provider_message_id IS NOT NULL;
