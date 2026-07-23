CREATE TABLE ai_project_daily_usage (
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    usage_date date NOT NULL,
    requests integer NOT NULL DEFAULT 0 CHECK (requests >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, usage_date),
    CONSTRAINT ai_project_daily_usage_tenant_project_fkey
        FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_ai_project_daily_usage_tenant_date
    ON ai_project_daily_usage (tenant_id, usage_date);

CREATE TRIGGER ai_project_daily_usage_set_updated_at
BEFORE UPDATE ON ai_project_daily_usage
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE ai_work_reservations (
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    work_kind text NOT NULL CHECK (work_kind IN ('text', 'attachment')),
    resource_id uuid NOT NULL,
    generation bigint NOT NULL CHECK (generation > 0),
    usage_date date NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, work_kind, resource_id, generation),
    CONSTRAINT ai_work_reservations_tenant_project_fkey
        FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_ai_work_reservations_usage_date
    ON ai_work_reservations (usage_date, tenant_id, project_id);

ALTER TABLE feedbacks
    ADD COLUMN ai_budget_deferred_until timestamptz,
    ADD COLUMN ai_budget_deferred_reason text,
    ADD CONSTRAINT feedbacks_ai_budget_deferred_reason_length CHECK (
        ai_budget_deferred_reason IS NULL
        OR char_length(ai_budget_deferred_reason) BETWEEN 1 AND 200
    ),
    ADD CONSTRAINT feedbacks_ai_budget_deferred_consistent CHECK (
        (ai_budget_deferred_until IS NULL AND ai_budget_deferred_reason IS NULL)
        OR (
            ai_budget_deferred_until IS NOT NULL
            AND ai_budget_deferred_reason IS NOT NULL
            AND text_process_status = 'pending'
            AND ai_job_enqueued_at IS NULL
        )
    );

ALTER TABLE attachments
    ADD COLUMN ai_budget_deferred_until timestamptz,
    ADD CONSTRAINT attachments_ai_budget_deferred_consistent CHECK (
        ai_budget_deferred_until IS NULL
        OR (
            processing_status = 'pending'
            AND error_message IS NOT NULL
        )
    );

CREATE INDEX idx_feedbacks_ai_budget_deferred
    ON feedbacks (ai_budget_deferred_until, id)
    WHERE ai_budget_deferred_until IS NOT NULL;

CREATE INDEX idx_attachments_ai_budget_deferred
    ON attachments (ai_budget_deferred_until, id)
    WHERE ai_budget_deferred_until IS NOT NULL;

ALTER TABLE ai_project_daily_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_project_daily_usage FORCE ROW LEVEL SECURITY;
ALTER TABLE ai_work_reservations ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_work_reservations FORCE ROW LEVEL SECURITY;

CREATE POLICY ai_project_daily_usage_tenant_isolation ON ai_project_daily_usage
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE POLICY ai_work_reservations_tenant_isolation ON ai_work_reservations
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON ai_project_daily_usage
    TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON ai_work_reservations
    TO fidibeki_app, fidibeki_admin;
