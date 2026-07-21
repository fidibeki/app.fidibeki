CREATE TYPE attachment_kind AS ENUM ('screenshot', 'audio', 'video', 'replay');

GRANT USAGE ON TYPE attachment_kind TO fidibeki_app, fidibeki_admin;

CREATE TABLE attachments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    feedback_id uuid NOT NULL,
    kind attachment_kind NOT NULL,
    client_attachment_id uuid NOT NULL,
    storage_key text NOT NULL UNIQUE,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    duration_seconds integer CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
    processing_status processing_status NOT NULL DEFAULT 'pending',
    complete_token_hash text,
    completed_at timestamptz,
    transcription text,
    sentiment_score numeric(3,2),
    moderation_flags text[] NOT NULL DEFAULT '{}',
    processed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attachments_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT attachments_tenant_feedback_fkey FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES feedbacks(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT attachments_feedback_client_attachment_id_key UNIQUE (feedback_id, client_attachment_id),
    CONSTRAINT attachments_completion_consistent CHECK (
        (completed_at IS NULL AND complete_token_hash IS NOT NULL) OR
        (completed_at IS NOT NULL AND complete_token_hash IS NULL)
    ),
    CONSTRAINT attachments_sentiment_range CHECK (
        sentiment_score IS NULL OR sentiment_score BETWEEN -1.0 AND 1.0
    )
);

CREATE INDEX idx_attachments_tenant_id ON attachments (tenant_id);
CREATE INDEX idx_attachments_feedback_id ON attachments (feedback_id);
CREATE INDEX idx_attachments_kind ON attachments (tenant_id, kind);
CREATE INDEX idx_attachments_processing_status ON attachments (tenant_id, processing_status);

ALTER TABLE attachments ENABLE ROW LEVEL SECURITY;
ALTER TABLE attachments FORCE ROW LEVEL SECURITY;

CREATE POLICY attachments_tenant_isolation ON attachments
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON attachments TO fidibeki_app, fidibeki_admin;
