CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    feedback_id uuid,
    session_id uuid NOT NULL,
    page_url text CHECK (page_url IS NULL OR char_length(page_url) <= 4096),
    started_at timestamptz NOT NULL,
    ended_at timestamptz,
    events_count integer NOT NULL DEFAULT 0 CHECK (events_count BETWEEN 0 AND 50000),
    batches_count integer NOT NULL DEFAULT 0 CHECK (batches_count BETWEEN 0 AND 1000),
    storage_key text,
    rrweb_version text NOT NULL CHECK (char_length(rrweb_version) BETWEEN 1 AND 40),
    merged_at timestamptz,
    merge_requested_at timestamptz,
    merge_enqueued_at timestamptz,
    merge_attempts integer NOT NULL DEFAULT 0 CHECK (merge_attempts >= 0),
    merge_error text,
    max_duration_seconds integer NOT NULL CHECK (max_duration_seconds BETWEEN 1 AND 1800),
    max_events integer NOT NULL CHECK (max_events BETWEEN 1 AND 50000),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sessions_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT sessions_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT sessions_tenant_feedback_fkey FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES feedbacks(tenant_id, id) ON DELETE SET NULL (feedback_id),
    CONSTRAINT sessions_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT sessions_project_id_id_key UNIQUE (project_id, id),
    CONSTRAINT sessions_project_session_id_key UNIQUE (project_id, session_id),
    CONSTRAINT sessions_merge_consistent CHECK (
        (merged_at IS NULL AND storage_key IS NULL) OR
        (merged_at IS NOT NULL AND storage_key IS NOT NULL AND merge_requested_at IS NOT NULL)
    ),
    CONSTRAINT sessions_timing_consistent CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE UNIQUE INDEX idx_sessions_feedback_id ON sessions (tenant_id, feedback_id)
    WHERE feedback_id IS NOT NULL;
CREATE INDEX idx_sessions_tenant_id ON sessions (tenant_id);
CREATE INDEX idx_sessions_project_id ON sessions (project_id);
CREATE INDEX idx_sessions_session_id ON sessions (session_id);
CREATE INDEX idx_sessions_merge_pending ON sessions (merge_requested_at, id)
    WHERE merge_requested_at IS NOT NULL AND merged_at IS NULL;
CREATE INDEX idx_sessions_retention ON sessions (COALESCE(ended_at, created_at));

CREATE TABLE session_batches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    session_id uuid NOT NULL,
    batch_index integer NOT NULL CHECK (batch_index BETWEEN 0 AND 999),
    storage_key text NOT NULL,
    events_count integer NOT NULL CHECK (events_count BETWEEN 1 AND 50),
    size_bytes bigint NOT NULL CHECK (size_bytes BETWEEN 1 AND 2097152),
    content_type text NOT NULL DEFAULT 'application/json'
        CHECK (content_type = 'application/json'),
    complete_token_hash text,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT session_batches_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT session_batches_tenant_session_fkey FOREIGN KEY (tenant_id, session_id)
        REFERENCES sessions(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT session_batches_project_session_fkey FOREIGN KEY (project_id, session_id)
        REFERENCES sessions(project_id, id) ON DELETE CASCADE,
    CONSTRAINT session_batches_session_index_key UNIQUE (session_id, batch_index),
    CONSTRAINT session_batches_storage_key_key UNIQUE (storage_key),
    CONSTRAINT session_batches_completion_consistent CHECK (
        (completed_at IS NULL AND complete_token_hash IS NOT NULL) OR
        (completed_at IS NOT NULL AND complete_token_hash IS NULL)
    )
);

CREATE INDEX idx_session_batches_tenant_id ON session_batches (tenant_id);
CREATE INDEX idx_session_batches_session_id ON session_batches (session_id, batch_index);

ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE session_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE session_batches FORCE ROW LEVEL SECURITY;

CREATE POLICY sessions_tenant_isolation ON sessions
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE POLICY session_batches_tenant_isolation ON session_batches
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON sessions TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON session_batches TO fidibeki_app, fidibeki_admin;
