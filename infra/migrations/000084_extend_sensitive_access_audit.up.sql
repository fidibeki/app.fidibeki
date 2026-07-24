-- Extends the append-only tenant_access_events ledger (introduced for workspace
-- ownership events) so it can also carry sensitive-data access events for
-- attachment streaming, replay playback, and privacy export downloads without
-- ever storing the underlying evidence, credentials, or reporter emails.
ALTER TABLE tenant_access_events
    ADD COLUMN actor_type text NOT NULL DEFAULT 'system',
    ADD COLUMN actor_api_key_id uuid,
    ADD COLUMN resource_type text,
    ADD COLUMN resource_id uuid,
    ADD COLUMN action text,
    ADD COLUMN permission_path text,
    ADD COLUMN request_id text,
    ADD COLUMN outcome text NOT NULL DEFAULT 'success',
    ADD COLUMN dedup_bucket text;

-- Classify every existing row from its tenant-scoped identity columns. Actorless
-- retention/scheduler events remain explicit system activity instead of being
-- mislabeled as an unknown human.
UPDATE tenant_access_events
SET actor_type = CASE
    WHEN actor_platform_admin_id IS NOT NULL THEN 'platform_admin'
    WHEN actor_member_id IS NOT NULL THEN 'human'
    ELSE 'system'
END;

UPDATE tenant_access_events
SET outcome = 'denied'
WHERE kind = 'ownership.recovery_denied';

ALTER TABLE tenant_access_events
    ADD CONSTRAINT tenant_access_events_actor_type_check
        CHECK (actor_type IN ('human', 'api_key', 'platform_admin', 'system')),
    ADD CONSTRAINT tenant_access_events_outcome_check
        CHECK (outcome IN ('success', 'denied', 'not_found')),
    ADD CONSTRAINT tenant_access_events_resource_type_format
        CHECK (resource_type IS NULL OR resource_type ~ '^[a-z][a-z0-9_]*$'),
    ADD CONSTRAINT tenant_access_events_action_format
        CHECK (action IS NULL OR action ~ '^[a-z][a-z0-9_]*$'),
    ADD CONSTRAINT tenant_access_events_permission_path_format
        CHECK (permission_path IS NULL OR permission_path ~ '^[a-z][a-z0-9_]*:[a-z][a-z0-9_]*$'),
    ADD CONSTRAINT tenant_access_events_actor_consistency CHECK (
        (actor_type = 'human' AND actor_member_id IS NOT NULL
            AND actor_api_key_id IS NULL AND actor_platform_admin_id IS NULL)
        OR (actor_type = 'api_key' AND actor_member_id IS NULL
            AND actor_api_key_id IS NOT NULL AND actor_platform_admin_id IS NULL)
        OR (actor_type = 'platform_admin' AND actor_member_id IS NULL
            AND actor_api_key_id IS NULL AND actor_platform_admin_id IS NOT NULL)
        OR (actor_type = 'system' AND actor_member_id IS NULL
            AND actor_api_key_id IS NULL AND actor_platform_admin_id IS NULL)
    );

ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_tenant_id_id_key UNIQUE (tenant_id, id);

ALTER TABLE tenant_access_events
    ADD CONSTRAINT tenant_access_events_actor_api_key_fkey
    FOREIGN KEY (tenant_id, actor_api_key_id)
    REFERENCES api_keys(tenant_id, id) ON DELETE SET NULL (actor_api_key_id);

-- Older audit writers only set one of the identity columns. Keep those additive
-- call sites compatible while making the persisted actor classification exact.
CREATE FUNCTION tenant_access_events_classify_actor() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.actor_type := CASE
        WHEN NEW.actor_api_key_id IS NOT NULL THEN 'api_key'
        WHEN NEW.actor_platform_admin_id IS NOT NULL THEN 'platform_admin'
        WHEN NEW.actor_member_id IS NOT NULL THEN 'human'
        ELSE 'system'
    END;
    RETURN NEW;
END;
$$;

CREATE TRIGGER tenant_access_events_classify_actor
BEFORE INSERT OR UPDATE OF actor_member_id, actor_api_key_id, actor_platform_admin_id, actor_type
ON tenant_access_events
FOR EACH ROW EXECUTE FUNCTION tenant_access_events_classify_actor();

CREATE INDEX idx_tenant_access_events_resource
    ON tenant_access_events (tenant_id, resource_type, resource_id, created_at DESC);
CREATE INDEX idx_tenant_access_events_actor_type
    ON tenant_access_events (tenant_id, actor_type, created_at DESC);
CREATE INDEX idx_tenant_access_events_outcome
    ON tenant_access_events (tenant_id, outcome, created_at DESC)
    WHERE outcome <> 'success';

-- Bounds event volume: a repeated view/download of the same resource by the
-- same actor within one dedup bucket (see accessaudit.DefaultDedupWindow)
-- collapses into a single row instead of flooding the ledger, e.g. under
-- replay chunking or media range-request retries.
CREATE UNIQUE INDEX idx_tenant_access_events_dedup ON tenant_access_events (
    tenant_id, resource_type, resource_id, actor_type,
    COALESCE(actor_member_id::text, actor_api_key_id::text, actor_platform_admin_id::text, ''),
    action, outcome, dedup_bucket
) WHERE resource_id IS NOT NULL AND dedup_bucket IS NOT NULL;

-- Grant tenant-visible audit read access to existing workspace administrators
-- (identified by tenant:update, the same tier that already manages members,
-- billing, and privacy retention) so the new endpoint has bounded visibility
-- from day one rather than defaulting every member into it.
UPDATE tenant_members
SET permissions = array_append(permissions, 'audit:read')
WHERE 'tenant:update' = ANY(permissions)
  AND NOT ('audit:read' = ANY(permissions));
