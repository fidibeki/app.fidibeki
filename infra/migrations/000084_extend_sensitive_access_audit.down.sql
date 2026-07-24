UPDATE tenant_members
SET permissions = array_remove(permissions, 'audit:read')
WHERE 'audit:read' = ANY(permissions);

DROP TRIGGER IF EXISTS tenant_access_events_classify_actor ON tenant_access_events;
DROP FUNCTION IF EXISTS tenant_access_events_classify_actor();

DROP INDEX idx_tenant_access_events_dedup;
DROP INDEX idx_tenant_access_events_outcome;
DROP INDEX idx_tenant_access_events_actor_type;
DROP INDEX idx_tenant_access_events_resource;

ALTER TABLE tenant_access_events
    DROP CONSTRAINT tenant_access_events_actor_consistency,
    DROP CONSTRAINT tenant_access_events_actor_api_key_fkey,
    DROP CONSTRAINT tenant_access_events_permission_path_format,
    DROP CONSTRAINT tenant_access_events_action_format,
    DROP CONSTRAINT tenant_access_events_resource_type_format,
    DROP CONSTRAINT tenant_access_events_outcome_check,
    DROP CONSTRAINT tenant_access_events_actor_type_check;

ALTER TABLE tenant_access_events
    DROP COLUMN dedup_bucket,
    DROP COLUMN outcome,
    DROP COLUMN request_id,
    DROP COLUMN permission_path,
    DROP COLUMN action,
    DROP COLUMN resource_id,
    DROP COLUMN resource_type,
    DROP COLUMN actor_api_key_id,
    DROP COLUMN actor_type;

ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_tenant_id_id_key;
