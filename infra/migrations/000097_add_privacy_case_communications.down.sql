DELETE FROM outbox_events
WHERE resource_type = 'privacy_case_communication'
   OR event::text = 'privacy.case_communication.requested';

DROP TABLE IF EXISTS privacy_case_communication_attempts;
DROP TABLE IF EXISTS privacy_case_communications;
DROP TYPE IF EXISTS privacy_case_communication_delivery_status;
DROP TYPE IF EXISTS privacy_case_communication_status;
DROP TYPE IF EXISTS privacy_case_communication_kind;

ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_resource_type_check;
ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_resource_type_check
    CHECK (resource_type IN ('feedback', 'issue', 'tenant', 'privacy_recipient_action'));

-- PostgreSQL enum labels cannot be removed in place. A full rollback later drops event_type in
-- migration 18; reapplying this migration is safe because the addition uses IF NOT EXISTS.
