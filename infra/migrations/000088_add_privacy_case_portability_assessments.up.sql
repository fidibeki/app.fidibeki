CREATE TABLE privacy_case_portability_assessments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    privacy_case_id uuid NOT NULL,
    decision text NOT NULL CHECK (
        decision IN ('approved', 'partially_approved', 'not_applicable', 'refused')
    ),
    data_categories text[] NOT NULL DEFAULT '{}',
    export_format text,
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 2000),
    assessed_by_member_id uuid NOT NULL,
    assessed_at timestamptz NOT NULL,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT privacy_case_portability_assessments_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_portability_assessments_tenant_case_fkey
        FOREIGN KEY (tenant_id, privacy_case_id)
        REFERENCES privacy_cases(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_portability_assessments_tenant_member_fkey
        FOREIGN KEY (tenant_id, assessed_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT privacy_case_portability_assessments_tenant_id_id_key
        UNIQUE (tenant_id, id),
    CONSTRAINT privacy_case_portability_assessments_idempotency_key
        UNIQUE (tenant_id, privacy_case_id, idempotency_key),
    CONSTRAINT privacy_case_portability_assessments_decision_consistent CHECK (
        (
            decision IN ('approved', 'partially_approved')
            AND cardinality(data_categories) > 0
            AND export_format = 'jsonl_zip'
        )
        OR (
            decision IN ('not_applicable', 'refused')
            AND cardinality(data_categories) = 0
            AND export_format IS NULL
        )
    )
);

CREATE INDEX idx_privacy_case_portability_assessments_case
    ON privacy_case_portability_assessments (
        tenant_id, privacy_case_id, assessed_at DESC, id DESC
    );

CREATE TRIGGER privacy_case_portability_assessments_immutable
BEFORE UPDATE OR DELETE ON privacy_case_portability_assessments
FOR EACH ROW EXECUTE FUNCTION privacy_case_events_append_only();

ALTER TABLE privacy_case_portability_assessments ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_case_portability_assessments FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_case_portability_assessments_tenant_isolation
    ON privacy_case_portability_assessments
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON privacy_case_portability_assessments TO fidibeki_app, fidibeki_admin;
