DROP INDEX IF EXISTS idx_issue_tracker_connections_auto_create;

ALTER TABLE issue_tracker_connections
    DROP CONSTRAINT IF EXISTS issue_tracker_connections_auto_create_requires_enabled,
    DROP COLUMN IF EXISTS auto_create;
