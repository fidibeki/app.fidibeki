CREATE TYPE privacy_job_kind AS ENUM (
    'workspace_export',
    'reporter_export',
    'reporter_anonymize'
);

CREATE TYPE privacy_job_status AS ENUM (
    'pending',
    'processing',
    'completed',
    'failed',
    'expired'
);

CREATE TABLE privacy_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    kind privacy_job_kind NOT NULL,
    status privacy_job_status NOT NULL DEFAULT 'pending',
    actor_member_id uuid,
    generation integer NOT NULL DEFAULT 1 CHECK (generation >= 1),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 12),
    enqueued_at timestamptz,
    started_at timestamptz,
    heartbeat_at timestamptz,
    completed_at timestamptz,
    expires_at timestamptz,
    object_key text,
    object_size_bytes bigint CHECK (object_size_bytes IS NULL OR object_size_bytes >= 0),
    error_message text CHECK (error_message IS NULL OR char_length(error_message) <= 2000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT privacy_jobs_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_jobs_tenant_actor_fkey
        FOREIGN KEY (tenant_id, actor_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (actor_member_id),
    CONSTRAINT privacy_jobs_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT privacy_jobs_state_consistent CHECK (
        (status = 'pending' AND started_at IS NULL AND completed_at IS NULL
            AND expires_at IS NULL AND object_key IS NULL AND object_size_bytes IS NULL)
        OR (status = 'processing' AND started_at IS NOT NULL AND heartbeat_at IS NOT NULL
            AND completed_at IS NULL AND expires_at IS NULL AND object_key IS NULL
            AND object_size_bytes IS NULL)
        OR (status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL
            AND expires_at IS NOT NULL AND object_key IS NOT NULL AND object_size_bytes IS NOT NULL)
        OR (status = 'failed' AND completed_at IS NULL AND expires_at IS NULL
            AND object_key IS NULL AND object_size_bytes IS NULL AND error_message IS NOT NULL)
        OR (status = 'expired' AND completed_at IS NOT NULL AND expires_at IS NOT NULL
            AND object_key IS NULL AND object_size_bytes IS NOT NULL)
    ),
    CONSTRAINT privacy_jobs_expiry_after_completion CHECK (
        expires_at IS NULL OR (completed_at IS NOT NULL AND expires_at > completed_at)
    )
);

CREATE UNIQUE INDEX idx_privacy_jobs_active_workspace_export
    ON privacy_jobs (tenant_id, kind)
    WHERE kind = 'workspace_export' AND status IN ('pending', 'processing');
CREATE INDEX idx_privacy_jobs_tenant_created
    ON privacy_jobs (tenant_id, created_at DESC, id DESC);
CREATE INDEX idx_privacy_jobs_scheduler
    ON privacy_jobs (status, enqueued_at, heartbeat_at, created_at, id)
    WHERE status IN ('pending', 'processing');
CREATE INDEX idx_privacy_jobs_expiry
    ON privacy_jobs (expires_at, id)
    WHERE status = 'completed';

CREATE TRIGGER privacy_jobs_set_updated_at
BEFORE UPDATE ON privacy_jobs
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE privacy_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_jobs FORCE ROW LEVEL SECURITY;

CREATE POLICY privacy_jobs_tenant_isolation ON privacy_jobs
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON TYPE privacy_job_kind, privacy_job_status TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE ON privacy_jobs TO fidibeki_app, fidibeki_admin;
