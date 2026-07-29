DELETE FROM outbox_events
WHERE resource_type = 'email_feedback_invitation'
   OR event::text = 'email_feedback.invitation_created';

ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_resource_type_check;
ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_resource_type_check
    CHECK (resource_type IN (
        'feedback', 'issue', 'tenant',
        'privacy_recipient_action', 'privacy_case_communication'
    ));

ALTER TABLE email_feedback_suppressions
    DROP CONSTRAINT email_feedback_suppressions_tenant_invitation_fkey;
DROP TABLE email_feedback_invitations;
DROP TABLE email_feedback_suppressions;

ALTER TABLE email_feedback_campaigns
    DROP CONSTRAINT email_feedback_campaigns_tenant_project_id_key;

DROP TYPE email_feedback_suppression_reason;
DROP TYPE email_feedback_invitation_status;

UPDATE tenant_members
SET permissions = array_remove(permissions, 'email_feedback:send'),
    authorization_version = authorization_version + 1
WHERE 'email_feedback:send' = ANY(permissions);

UPDATE api_keys
SET permissions = array_remove(permissions, 'email_feedback:send')
WHERE 'email_feedback:send' = ANY(permissions);

-- PostgreSQL enum labels cannot be removed in place. A full rollback later
-- drops event_type in migration 18; reapplying is safe because the label uses
-- IF NOT EXISTS.
