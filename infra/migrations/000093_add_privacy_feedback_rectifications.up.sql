ALTER TYPE event_type ADD VALUE IF NOT EXISTS 'feedback.rectified';

CREATE TABLE privacy_case_feedback_rectifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    privacy_case_id uuid NOT NULL,
    feedback_id uuid NOT NULL,
    previous_description text,
    description text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 10000),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 2000),
    derived_data_invalidated boolean NOT NULL DEFAULT true,
    applied_by_member_id uuid NOT NULL,
    applied_at timestamptz NOT NULL,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT privacy_case_feedback_rectifications_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_feedback_rectifications_tenant_case_fkey
        FOREIGN KEY (tenant_id, privacy_case_id)
        REFERENCES privacy_cases(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_feedback_rectifications_tenant_feedback_fkey
        FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES feedbacks(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT privacy_case_feedback_rectifications_tenant_actor_fkey
        FOREIGN KEY (tenant_id, applied_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT privacy_case_feedback_rectifications_tenant_id_id_key
        UNIQUE (tenant_id, id),
    CONSTRAINT privacy_case_feedback_rectifications_idempotency_key
        UNIQUE (tenant_id, privacy_case_id, idempotency_key)
);

CREATE INDEX idx_privacy_case_feedback_rectifications_case
    ON privacy_case_feedback_rectifications (
        tenant_id, privacy_case_id, applied_at DESC, id DESC
    );

CREATE INDEX idx_privacy_case_feedback_rectifications_feedback
    ON privacy_case_feedback_rectifications (
        tenant_id, feedback_id, applied_at DESC, id DESC
    );

CREATE TRIGGER privacy_case_feedback_rectifications_immutable
BEFORE UPDATE OR DELETE ON privacy_case_feedback_rectifications
FOR EACH ROW EXECUTE FUNCTION privacy_case_events_append_only();

ALTER TABLE privacy_case_feedback_rectifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_case_feedback_rectifications FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_case_feedback_rectifications_tenant_isolation
    ON privacy_case_feedback_rectifications
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON privacy_case_feedback_rectifications TO fidibeki_app, fidibeki_admin;
