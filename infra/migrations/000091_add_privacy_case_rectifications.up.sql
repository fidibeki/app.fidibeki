CREATE TABLE privacy_case_rectifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    privacy_case_id uuid NOT NULL,
    subject_member_id uuid NOT NULL,
    changes jsonb NOT NULL CHECK (
        jsonb_typeof(changes) = 'object'
        AND changes <> '{}'::jsonb
        AND changes - 'name' - 'locale' - 'timezone' = '{}'::jsonb
    ),
    previous_values jsonb NOT NULL CHECK (
        jsonb_typeof(previous_values) = 'object'
        AND previous_values <> '{}'::jsonb
        AND previous_values - 'name' - 'locale' - 'timezone' = '{}'::jsonb
    ),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 2000),
    applied_by_member_id uuid NOT NULL,
    applied_at timestamptz NOT NULL,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT privacy_case_rectifications_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_rectifications_tenant_case_fkey
        FOREIGN KEY (tenant_id, privacy_case_id)
        REFERENCES privacy_cases(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_rectifications_tenant_subject_fkey
        FOREIGN KEY (tenant_id, subject_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT privacy_case_rectifications_tenant_actor_fkey
        FOREIGN KEY (tenant_id, applied_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT privacy_case_rectifications_tenant_id_id_key
        UNIQUE (tenant_id, id),
    CONSTRAINT privacy_case_rectifications_idempotency_key
        UNIQUE (tenant_id, privacy_case_id, idempotency_key)
);

CREATE INDEX idx_privacy_case_rectifications_case
    ON privacy_case_rectifications (
        tenant_id, privacy_case_id, applied_at DESC, id DESC
    );

CREATE TRIGGER privacy_case_rectifications_immutable
BEFORE UPDATE OR DELETE ON privacy_case_rectifications
FOR EACH ROW EXECUTE FUNCTION privacy_case_events_append_only();

ALTER TABLE privacy_case_rectifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_case_rectifications FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_case_rectifications_tenant_isolation
    ON privacy_case_rectifications
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON privacy_case_rectifications TO fidibeki_app, fidibeki_admin;
