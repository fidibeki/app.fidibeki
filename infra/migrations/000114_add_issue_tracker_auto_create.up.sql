-- Opt-in automatic creation: when a Fidibeki issue is created, the outbox fans out one
-- issue-tracker task per provider that has this switched on. It can only be set on a provider
-- that is already enabled, so turning a provider off also stops automatic creation.
ALTER TABLE issue_tracker_connections
    ADD COLUMN auto_create boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT issue_tracker_connections_auto_create_requires_enabled CHECK (
        NOT auto_create OR is_enabled
    );

CREATE INDEX idx_issue_tracker_connections_auto_create
    ON issue_tracker_connections (tenant_id, provider) WHERE auto_create;
