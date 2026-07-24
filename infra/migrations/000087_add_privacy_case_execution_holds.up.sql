CREATE TABLE privacy_case_legal_holds (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    privacy_case_id uuid NOT NULL,
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 2000),
    legal_reference text CHECK (
        legal_reference IS NULL OR char_length(legal_reference) BETWEEN 1 AND 500
    ),
    active boolean NOT NULL DEFAULT true,
    placed_by_member_id uuid NOT NULL,
    placed_at timestamptz NOT NULL,
    released_by_member_id uuid,
    released_at timestamptz,
    release_reason text CHECK (
        release_reason IS NULL OR char_length(release_reason) BETWEEN 1 AND 2000
    ),
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT privacy_case_legal_holds_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_legal_holds_tenant_case_fkey
        FOREIGN KEY (tenant_id, privacy_case_id)
        REFERENCES privacy_cases(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_legal_holds_tenant_placer_fkey
        FOREIGN KEY (tenant_id, placed_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT privacy_case_legal_holds_tenant_releaser_fkey
        FOREIGN KEY (tenant_id, released_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (released_by_member_id),
    CONSTRAINT privacy_case_legal_holds_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT privacy_case_legal_holds_idempotency_key
        UNIQUE (tenant_id, privacy_case_id, idempotency_key),
    CONSTRAINT privacy_case_legal_holds_state_consistent CHECK (
        (
            active
            AND released_by_member_id IS NULL
            AND released_at IS NULL
            AND release_reason IS NULL
        )
        OR (
            NOT active
            AND released_at IS NOT NULL
            AND release_reason IS NOT NULL
        )
    )
);

CREATE INDEX idx_privacy_case_legal_holds_case
    ON privacy_case_legal_holds (tenant_id, privacy_case_id, placed_at DESC, id DESC);
CREATE INDEX idx_privacy_case_legal_holds_active
    ON privacy_case_legal_holds (tenant_id, privacy_case_id)
    WHERE active;

CREATE TRIGGER privacy_case_legal_holds_set_updated_at
BEFORE UPDATE ON privacy_case_legal_holds
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE privacy_case_legal_holds ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_case_legal_holds FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_case_legal_holds_tenant_isolation
    ON privacy_case_legal_holds
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON privacy_case_legal_holds TO fidibeki_app, fidibeki_admin;
