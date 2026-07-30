DROP INDEX idx_email_feedback_invitations_provider_message;

ALTER TABLE email_feedback_invitations
    DROP COLUMN webhook_event_id;
