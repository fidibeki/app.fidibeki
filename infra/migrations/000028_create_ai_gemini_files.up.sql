ALTER TABLE attachments
    ADD CONSTRAINT attachments_tenant_id_id_key UNIQUE (tenant_id, id);

CREATE TABLE ai_gemini_files (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    attachment_id uuid NOT NULL,
    file_name text NOT NULL UNIQUE,
    delete_after timestamptz NOT NULL,
    delete_enqueued_at timestamptz,
    delete_attempts integer NOT NULL DEFAULT 0 CHECK (delete_attempts >= 0),
    last_error text,
    deleted_at timestamptz,
    failed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ai_gemini_files_tenant_attachment_fkey
        FOREIGN KEY (tenant_id, attachment_id)
        REFERENCES attachments(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT ai_gemini_files_terminal_state CHECK (deleted_at IS NULL OR failed_at IS NULL),
    CONSTRAINT ai_gemini_files_error_length CHECK (
        last_error IS NULL OR char_length(last_error) <= 2000
    )
);

CREATE INDEX idx_ai_gemini_files_tenant_id ON ai_gemini_files (tenant_id);
CREATE INDEX idx_ai_gemini_files_due
    ON ai_gemini_files (delete_after, id)
    WHERE deleted_at IS NULL AND failed_at IS NULL AND delete_enqueued_at IS NULL;

ALTER TABLE ai_gemini_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_gemini_files FORCE ROW LEVEL SECURITY;

CREATE POLICY ai_gemini_files_tenant_isolation ON ai_gemini_files
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON ai_gemini_files TO fidibeki_app, fidibeki_admin;
