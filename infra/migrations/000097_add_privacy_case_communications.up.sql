ALTER TYPE event_type ADD VALUE IF NOT EXISTS 'privacy.case_communication.requested';

ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_resource_type_check;
ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_resource_type_check
    CHECK (resource_type IN (
        'feedback', 'issue', 'tenant',
        'privacy_recipient_action', 'privacy_case_communication'
    ));

CREATE TYPE privacy_case_communication_kind AS ENUM (
    'progress_update', 'clarification_request', 'decision_notice'
);
CREATE TYPE privacy_case_communication_status AS ENUM (
    'pending', 'processing', 'sent', 'failed'
);
CREATE TYPE privacy_case_communication_delivery_status AS ENUM (
    'pending', 'sent', 'failed'
);

CREATE TABLE privacy_case_communications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    privacy_case_id uuid NOT NULL,
    communication_kind privacy_case_communication_kind NOT NULL,
    recipient_email text NOT NULL CHECK (char_length(recipient_email) BETWEEN 3 AND 320),
    subject_line text NOT NULL CHECK (char_length(subject_line) BETWEEN 1 AND 200),
    message_body text NOT NULL CHECK (char_length(message_body) BETWEEN 1 AND 4000),
    status privacy_case_communication_status NOT NULL DEFAULT 'pending',
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text CHECK (last_error IS NULL OR char_length(last_error) BETWEEN 1 AND 500),
    provider_message_id text
        CHECK (provider_message_id IS NULL OR char_length(provider_message_id) BETWEEN 1 AND 500),
    last_attempt_at timestamptz,
    sent_at timestamptz,
    created_by_member_id uuid NOT NULL,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT privacy_case_communications_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_communications_tenant_case_fkey
        FOREIGN KEY (tenant_id, privacy_case_id)
        REFERENCES privacy_cases(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_communications_tenant_actor_fkey
        FOREIGN KEY (tenant_id, created_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT privacy_case_communications_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT privacy_case_communications_idempotency_key
        UNIQUE (tenant_id, privacy_case_id, idempotency_key),
    CONSTRAINT privacy_case_communications_delivery_state CHECK (
        (status = 'pending' AND sent_at IS NULL
            AND provider_message_id IS NULL AND last_error IS NULL)
        OR (status = 'processing' AND last_attempt_at IS NOT NULL AND sent_at IS NULL
            AND provider_message_id IS NULL AND last_error IS NULL)
        OR (status = 'sent' AND sent_at IS NOT NULL AND provider_message_id IS NOT NULL
            AND last_error IS NULL)
        OR (status = 'failed' AND sent_at IS NULL AND provider_message_id IS NULL
            AND last_error IS NOT NULL)
    )
);

CREATE INDEX idx_privacy_case_communications_case
    ON privacy_case_communications (
        tenant_id, privacy_case_id, created_at DESC, id DESC
    );

CREATE TABLE privacy_case_communication_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    privacy_case_communication_id uuid NOT NULL,
    attempt_number integer NOT NULL CHECK (attempt_number > 0),
    status privacy_case_communication_delivery_status NOT NULL DEFAULT 'pending',
    provider_message_id text
        CHECK (provider_message_id IS NULL OR char_length(provider_message_id) BETWEEN 1 AND 500),
    error_message text
        CHECK (error_message IS NULL OR char_length(error_message) BETWEEN 1 AND 500),
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    CONSTRAINT privacy_case_communication_attempts_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_communication_attempts_tenant_communication_fkey
        FOREIGN KEY (tenant_id, privacy_case_communication_id)
        REFERENCES privacy_case_communications(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_communication_attempts_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT privacy_case_communication_attempts_number_key
        UNIQUE (tenant_id, privacy_case_communication_id, attempt_number),
    CONSTRAINT privacy_case_communication_attempts_delivery_state CHECK (
        (status = 'pending' AND completed_at IS NULL
            AND provider_message_id IS NULL AND error_message IS NULL)
        OR (status = 'sent' AND completed_at IS NOT NULL
            AND provider_message_id IS NOT NULL AND error_message IS NULL)
        OR (status = 'failed' AND completed_at IS NOT NULL
            AND provider_message_id IS NULL AND error_message IS NOT NULL)
    )
);

CREATE INDEX idx_privacy_case_communication_attempts_communication
    ON privacy_case_communication_attempts (
        tenant_id, privacy_case_communication_id, attempt_number DESC
    );

ALTER TABLE privacy_case_communications ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_case_communications FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_case_communications_tenant_isolation
    ON privacy_case_communications
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE privacy_case_communication_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_case_communication_attempts FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_case_communication_attempts_tenant_isolation
    ON privacy_case_communication_attempts
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON TYPE privacy_case_communication_kind,
    privacy_case_communication_status, privacy_case_communication_delivery_status
    TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT ON privacy_case_communications,
    privacy_case_communication_attempts TO fidibeki_app, fidibeki_admin;
GRANT UPDATE (
    status, attempts, last_error, provider_message_id,
    last_attempt_at, sent_at, updated_at
) ON privacy_case_communications TO fidibeki_app, fidibeki_admin;
GRANT UPDATE (
    status, provider_message_id, error_message, completed_at
) ON privacy_case_communication_attempts TO fidibeki_app, fidibeki_admin;
