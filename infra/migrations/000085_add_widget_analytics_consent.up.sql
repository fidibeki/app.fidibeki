-- Widget funnel analytics (matched/impression/interaction/submitted/dismissed events, the
-- persistent visitor identifier, and the durable browser outbox) remain disabled until a rule has
-- an explicit `widget_analytics` consent contract. Existing rules migrate to the safe "none" mode,
-- which matches the pre-migration technical default of no configured consent contract. This is a
-- deliberate compatibility trade-off: existing installations will not transmit funnel analytics
-- until a tenant explicitly configures a consent mode, because collection before any purpose-
-- specific grant is not permitted. This is a distinct purpose and evidence ledger from
-- `replay_consent`/`replay_consents` (see 000080) and from reporter-contact consent evidence (see
-- 000076); analytics grants must never be inferred from, or joined with, those purposes.
ALTER TABLE widget_rules
    ADD COLUMN analytics_consent jsonb NOT NULL DEFAULT '{"mode":"none"}'::jsonb,
    ADD CONSTRAINT widget_rules_analytics_consent_object CHECK (
        jsonb_typeof(analytics_consent) = 'object'
        AND analytics_consent->>'mode' IN ('none', 'fidibeki_managed', 'customer_cmp')
    );

CREATE TYPE widget_analytics_consent_state AS ENUM ('active', 'denied', 'withdrawn', 'expired');

CREATE TABLE widget_analytics_consents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid NOT NULL,
    widget_rule_id uuid NOT NULL,
    purpose text NOT NULL CHECK (purpose = 'widget_analytics'),
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
    state widget_analytics_consent_state NOT NULL,
    accepted_at timestamptz,
    withdrawn_at timestamptz,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT widget_analytics_consents_tenant_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT widget_analytics_consents_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT widget_analytics_consents_project_rule_fkey FOREIGN KEY (project_id, widget_rule_id)
        REFERENCES widget_rules(project_id, id) ON DELETE CASCADE,
    CONSTRAINT widget_analytics_consents_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT widget_analytics_consents_state_consistent CHECK (
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
    CONSTRAINT widget_analytics_consents_source_reference CHECK (
        (source = 'fidibeki_managed' AND customer_consent_reference IS NULL)
        OR source = 'customer_cmp'
    )
);

CREATE INDEX idx_widget_analytics_consents_scope
    ON widget_analytics_consents (tenant_id, project_id, widget_rule_id, created_at DESC);
CREATE INDEX idx_widget_analytics_consents_active_expiry
    ON widget_analytics_consents (expires_at, id)
    WHERE state = 'active';

ALTER TABLE widget_analytics_consents ENABLE ROW LEVEL SECURITY;
ALTER TABLE widget_analytics_consents FORCE ROW LEVEL SECURITY;

CREATE POLICY widget_analytics_consents_tenant_isolation ON widget_analytics_consents
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON widget_analytics_consents TO fidibeki_app, fidibeki_admin;
GRANT USAGE ON TYPE widget_analytics_consent_state TO fidibeki_app, fidibeki_admin;
