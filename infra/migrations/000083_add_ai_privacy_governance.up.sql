ALTER TABLE widget_rules
    ADD COLUMN ai_processing jsonb NOT NULL DEFAULT '{"enabled":false}'::jsonb,
    ADD CONSTRAINT widget_rules_ai_processing_object CHECK (
        jsonb_typeof(ai_processing) = 'object'
        AND jsonb_typeof(ai_processing->'enabled') = 'boolean'
    ),
    ADD CONSTRAINT widget_rules_ai_processing_contract CHECK (
        (ai_processing->>'enabled')::boolean = false
        OR (
            ai_processing->>'policy_version' = '2026-07-24'
            AND ai_processing->>'provider' = 'google_gemini'
            AND ai_processing->>'sensitive_data_mode' = 'prohibit_and_quarantine'
            AND jsonb_typeof(ai_processing->'purposes') = 'array'
            AND jsonb_array_length(ai_processing->'purposes') > 0
            AND jsonb_typeof(ai_processing->'input_categories') = 'array'
            AND jsonb_array_length(ai_processing->'input_categories') > 0
            AND ai_processing->>'privacy_policy_url' ~ '^https://'
            AND ai_processing->>'acknowledged' = 'true'
            AND (ai_processing->>'acknowledged_at') IS NOT NULL
            AND (ai_processing->>'acknowledged_by_member_id') IS NOT NULL
        )
    );

ALTER TABLE feedbacks
    ADD COLUMN ai_redaction_manifest jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT feedbacks_ai_redaction_manifest_object CHECK (
        jsonb_typeof(ai_redaction_manifest) = 'object'
    );

ALTER TABLE attachments
    ADD COLUMN ai_redaction_manifest jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD CONSTRAINT attachments_ai_redaction_manifest_object CHECK (
        jsonb_typeof(ai_redaction_manifest) = 'object'
    );

CREATE TABLE ai_processing_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    feedback_id uuid NOT NULL,
    attachment_id uuid,
    widget_rule_id uuid,
    generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    policy_version text NOT NULL,
    provider text NOT NULL CHECK (provider = 'google_gemini'),
    model text NOT NULL CHECK (char_length(model) BETWEEN 1 AND 120),
    purpose text NOT NULL,
    input_category text NOT NULL CHECK (
        input_category IN ('feedback_text', 'screenshot', 'audio', 'video')
    ),
    status text NOT NULL CHECK (
        status IN ('processing', 'completed', 'failed', 'discarded', 'quarantined')
    ),
    redaction_manifest jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (
        jsonb_typeof(redaction_manifest) = 'object'
    ),
    failure_reason text CHECK (
        failure_reason IS NULL OR char_length(failure_reason) <= 240
    ),
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ai_processing_runs_tenant_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT ai_processing_runs_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT ai_processing_runs_tenant_feedback_fkey FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES feedbacks(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT ai_processing_runs_tenant_attachment_fkey FOREIGN KEY (tenant_id, attachment_id)
        REFERENCES attachments(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT ai_processing_runs_resource_kind CHECK (
        (input_category = 'feedback_text' AND attachment_id IS NULL)
        OR (input_category <> 'feedback_text' AND attachment_id IS NOT NULL)
    ),
    CONSTRAINT ai_processing_runs_terminal_time CHECK (
        (status = 'processing' AND completed_at IS NULL)
        OR (status <> 'processing' AND completed_at IS NOT NULL)
    )
);

CREATE INDEX idx_ai_processing_runs_feedback
    ON ai_processing_runs (tenant_id, feedback_id, started_at DESC);
CREATE INDEX idx_ai_processing_runs_processing
    ON ai_processing_runs (started_at, id) WHERE status = 'processing';
CREATE UNIQUE INDEX idx_ai_processing_runs_text_generation
    ON ai_processing_runs (tenant_id, feedback_id, generation)
    WHERE attachment_id IS NULL;
CREATE UNIQUE INDEX idx_ai_processing_runs_attachment_attempt
    ON ai_processing_runs (tenant_id, attachment_id, attempt)
    WHERE attachment_id IS NOT NULL;

ALTER TABLE ai_processing_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_processing_runs FORCE ROW LEVEL SECURITY;

CREATE POLICY ai_processing_runs_tenant_isolation ON ai_processing_runs
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON ai_processing_runs TO fidibeki_app, fidibeki_admin;

ALTER TABLE ai_gemini_files
    ADD COLUMN processing_run_id uuid,
    ADD CONSTRAINT ai_gemini_files_processing_run_fkey
        FOREIGN KEY (processing_run_id)
        REFERENCES ai_processing_runs(id) ON DELETE SET NULL;

CREATE INDEX idx_ai_gemini_files_processing_run
    ON ai_gemini_files (processing_run_id)
    WHERE processing_run_id IS NOT NULL;
