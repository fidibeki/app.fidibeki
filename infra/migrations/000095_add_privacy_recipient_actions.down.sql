DELETE FROM outbox_events
WHERE resource_type = 'privacy_recipient_action'
   OR event::text = 'privacy.recipient_action.requested';

DROP TABLE IF EXISTS privacy_recipient_action_attempts;
DROP TABLE IF EXISTS privacy_recipient_actions;
DROP TYPE IF EXISTS privacy_recipient_delivery_status;
DROP TYPE IF EXISTS privacy_recipient_action_status;
DROP TYPE IF EXISTS privacy_recipient_action_kind;

ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_resource_type_check;
ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_resource_type_check
    CHECK (resource_type IN ('feedback', 'issue', 'tenant'));

-- PostgreSQL enum labels cannot be removed in place. A full rollback later drops event_type in
-- migration 18; reapplying this migration is safe because the addition uses IF NOT EXISTS.
