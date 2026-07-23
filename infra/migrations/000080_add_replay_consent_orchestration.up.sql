-- Session replay remains disabled until a rule has an explicit consent contract. Existing replay
-- settings are preserved, but receive the safe "none" mode rather than inferred permission.
ALTER TABLE widget_rules
    ADD COLUMN replay_consent jsonb NOT NULL DEFAULT '{"mode":"none"}'::jsonb,
    ADD CONSTRAINT widget_rules_replay_consent_object CHECK (
        jsonb_typeof(replay_consent) = 'object'
        AND replay_consent->>'mode' IN ('none', 'fidibeki_managed', 'customer_cmp')
    );

CREATE TYPE replay_consent_state AS ENUM ('active', 'denied', 'withdrawn', 'expired');

CREATE TABLE replay_consents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    widget_rule_id uuid NOT NULL,
    purpose text NOT NULL CHECK (purpose = 'session_replay'),
    notice_version text NOT NULL CHECK (
        notice_version ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'
    ),
    locale text NOT NULL CHECK (
        locale ~ '^[a-z]{2,3}(-[a-z0-9]{2,8})*$'
    ),
    source text NOT NULL CHECK (source IN ('fidibeki_managed', 'customer_cmp')),
    customer_consent_reference text CHECK (
        customer_consent_reference IS NULL
        OR char_length(customer_consent_reference) BETWEEN 1 AND 200
    ),
    token_hash text UNIQUE CHECK (
        token_hash IS NULL OR token_hash ~ '^[0-9a-f]{64}$'
    ),
    state replay_consent_state NOT NULL,
    accepted_at timestamptz,
    withdrawn_at timestamptz,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT replay_consents_tenant_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT replay_consents_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT replay_consents_project_rule_fkey FOREIGN KEY (project_id, widget_rule_id)
        REFERENCES widget_rules(project_id, id) ON DELETE CASCADE,
    CONSTRAINT replay_consents_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT replay_consents_state_consistent CHECK (
        (
            state = 'active'
            AND token_hash IS NOT NULL
            AND accepted_at IS NOT NULL
            AND withdrawn_at IS NULL
            AND expires_at > accepted_at
        )
        OR (
            state = 'denied'
            AND token_hash IS NULL
            AND accepted_at IS NULL
            AND withdrawn_at IS NULL
            AND expires_at > created_at
        )
        OR (
            state = 'withdrawn'
            AND token_hash IS NOT NULL
            AND accepted_at IS NOT NULL
            AND withdrawn_at >= accepted_at
            AND expires_at > accepted_at
        )
        OR (
            state = 'expired'
            AND token_hash IS NOT NULL
            AND accepted_at IS NOT NULL
            AND withdrawn_at IS NULL
            AND expires_at > accepted_at
        )
    ),
    CONSTRAINT replay_consents_source_reference CHECK (
        (source = 'fidibeki_managed' AND customer_consent_reference IS NULL)
        OR source = 'customer_cmp'
    )
);

CREATE INDEX idx_replay_consents_scope
    ON replay_consents (tenant_id, project_id, widget_rule_id, created_at DESC);
CREATE INDEX idx_replay_consents_active_expiry
    ON replay_consents (expires_at, id)
    WHERE state = 'active';

ALTER TABLE sessions
    ADD COLUMN replay_consent_id uuid,
    ADD CONSTRAINT sessions_tenant_replay_consent_fkey
        FOREIGN KEY (tenant_id, replay_consent_id)
        REFERENCES replay_consents(tenant_id, id) ON DELETE RESTRICT;

CREATE INDEX idx_sessions_replay_consent
    ON sessions (tenant_id, replay_consent_id)
    WHERE replay_consent_id IS NOT NULL;

ALTER TABLE replay_consents ENABLE ROW LEVEL SECURITY;
ALTER TABLE replay_consents FORCE ROW LEVEL SECURITY;

CREATE POLICY replay_consents_tenant_isolation ON replay_consents
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON replay_consents TO fidibeki_app, fidibeki_admin;
GRANT USAGE ON TYPE replay_consent_state TO fidibeki_app, fidibeki_admin;
