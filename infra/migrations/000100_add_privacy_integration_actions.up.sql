CREATE TABLE privacy_integration_actions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    privacy_case_id uuid NOT NULL,
    integration_id uuid,
    destination_key text NOT NULL CHECK (char_length(destination_key) BETWEEN 1 AND 100),
    destination_kind integration_kind,
    destination_name text NOT NULL CHECK (char_length(destination_name) BETWEEN 1 AND 200),
    action_kind privacy_recipient_action_kind NOT NULL,
    handling_mode text NOT NULL CHECK (handling_mode IN ('manual', 'api')),
    status text NOT NULL CHECK (
        status IN ('manual_required', 'api_pending', 'completed', 'failed')
    ),
    capability_snapshot jsonb NOT NULL
        CHECK (jsonb_typeof(capability_snapshot) = 'object'),
    data_categories text[] NOT NULL DEFAULT '{}',
    disclosure_count integer NOT NULL CHECK (disclosure_count > 0),
    first_disclosed_at timestamptz NOT NULL,
    last_disclosed_at timestamptz NOT NULL,
    completed_by_member_id uuid,
    completed_at timestamptz,
    completion_note text CHECK (
        completion_note IS NULL OR char_length(completion_note) BETWEEN 1 AND 2000
    ),
    completion_idempotency_key text CHECK (
        completion_idempotency_key IS NULL
        OR char_length(completion_idempotency_key) BETWEEN 1 AND 200
    ),
    completion_fingerprint bytea CHECK (
        completion_fingerprint IS NULL OR octet_length(completion_fingerprint) = 32
    ),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT privacy_integration_actions_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_integration_actions_tenant_case_fkey
        FOREIGN KEY (tenant_id, privacy_case_id)
        REFERENCES privacy_cases(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT privacy_integration_actions_tenant_integration_fkey
        FOREIGN KEY (tenant_id, integration_id)
        REFERENCES integrations(tenant_id, id) ON DELETE SET NULL (integration_id),
    CONSTRAINT privacy_integration_actions_tenant_completer_fkey
        FOREIGN KEY (tenant_id, completed_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (completed_by_member_id),
    CONSTRAINT privacy_integration_actions_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT privacy_integration_actions_destination_key UNIQUE (
        tenant_id, privacy_case_id, destination_key, action_kind
    ),
    CONSTRAINT privacy_integration_actions_state_consistent CHECK (
        (
            status IN ('manual_required', 'api_pending', 'failed')
            AND completed_by_member_id IS NULL
            AND completed_at IS NULL
            AND completion_note IS NULL
            AND completion_idempotency_key IS NULL
            AND completion_fingerprint IS NULL
        )
        OR (
            status = 'completed'
            AND completed_by_member_id IS NOT NULL
            AND completed_at IS NOT NULL
            AND completion_note IS NOT NULL
            AND completion_idempotency_key IS NOT NULL
            AND completion_fingerprint IS NOT NULL
        )
    ),
    CONSTRAINT privacy_integration_actions_mode_consistent CHECK (
        (handling_mode = 'manual' AND status <> 'api_pending')
        OR (handling_mode = 'api' AND status <> 'manual_required')
    ),
    CONSTRAINT privacy_integration_actions_disclosure_window CHECK (
        last_disclosed_at >= first_disclosed_at
    )
);

CREATE INDEX idx_privacy_integration_actions_case
    ON privacy_integration_actions (
        tenant_id, privacy_case_id, status, last_disclosed_at DESC, id DESC
    );

ALTER TABLE privacy_integration_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_integration_actions FORCE ROW LEVEL SECURITY;

CREATE POLICY privacy_integration_actions_tenant_isolation
    ON privacy_integration_actions
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON privacy_integration_actions TO fidibeki_app, fidibeki_admin;
GRANT UPDATE (
    integration_id, destination_kind, destination_name, handling_mode, status,
    capability_snapshot, data_categories, disclosure_count, first_disclosed_at,
    last_disclosed_at, completed_by_member_id, completed_at, completion_note,
    completion_idempotency_key, completion_fingerprint, updated_at
) ON privacy_integration_actions TO fidibeki_app, fidibeki_admin;

ALTER TABLE privacy_recipient_actions
    ADD COLUMN privacy_integration_action_id uuid,
    ADD CONSTRAINT privacy_recipient_actions_tenant_integration_action_fkey
        FOREIGN KEY (tenant_id, privacy_integration_action_id)
        REFERENCES privacy_integration_actions(tenant_id, id)
        ON DELETE SET NULL (privacy_integration_action_id);

CREATE INDEX idx_privacy_recipient_actions_integration_action
    ON privacy_recipient_actions (tenant_id, privacy_integration_action_id)
    WHERE privacy_integration_action_id IS NOT NULL;
