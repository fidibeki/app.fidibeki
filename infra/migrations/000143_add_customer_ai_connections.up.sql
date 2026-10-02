CREATE TABLE ai_connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    provider text NOT NULL CHECK (provider IN ('google_gemini','openai','anthropic','amazon_bedrock','openai_compatible','anthropic_compatible')),
    base_url text NOT NULL DEFAULT '', region text NOT NULL DEFAULT '',
    auth_mode text NOT NULL CHECK (auth_mode IN ('api_key','role')),
    role_arn text NOT NULL DEFAULT '', external_id text NOT NULL DEFAULT '',
    credential_version bigint NOT NULL DEFAULT 1 CHECK (credential_version > 0),
    config_version bigint NOT NULL DEFAULT 1 CHECK (config_version > 0),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    enabled boolean NOT NULL DEFAULT true, deleted boolean NOT NULL DEFAULT false,
    rpm integer NOT NULL DEFAULT 15 CHECK (rpm BETWEEN 1 AND 120),
    concurrency integer NOT NULL DEFAULT 2 CHECK (concurrency BETWEEN 1 AND 8),
    default_model text NOT NULL DEFAULT '' CHECK (char_length(default_model) <= 512),
    is_default boolean NOT NULL DEFAULT false,
    health text NOT NULL DEFAULT 'unvalidated',
    credential_expires_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id,id)
);
CREATE UNIQUE INDEX ai_connections_workspace_default ON ai_connections(tenant_id) WHERE is_default AND NOT deleted;
CREATE TABLE ai_connection_credentials (
    tenant_id uuid NOT NULL, connection_id uuid NOT NULL, version bigint NOT NULL CHECK(version>0),
    envelope bytea NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id,connection_id,version),
    FOREIGN KEY (tenant_id,connection_id) REFERENCES ai_connections(tenant_id,id) ON DELETE CASCADE
);
CREATE TABLE ai_connection_models (
    tenant_id uuid NOT NULL, connection_id uuid NOT NULL, model_id text NOT NULL CHECK(char_length(model_id) BETWEEN 1 AND 512),
    config_version bigint NOT NULL, credential_version bigint NOT NULL,
    input_categories text[] NOT NULL CHECK (input_categories <@ ARRAY['feedback_text','screenshot','audio','video']::text[]),
    validated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id,connection_id,model_id,config_version,credential_version),
    FOREIGN KEY (tenant_id,connection_id) REFERENCES ai_connections(tenant_id,id) ON DELETE CASCADE
);
DO $$ DECLARE table_name text; BEGIN
    FOREACH table_name IN ARRAY ARRAY['ai_connections','ai_connection_credentials','ai_connection_models'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',table_name);
        EXECUTE format('CREATE POLICY tenant_isolation ON %I FOR ALL TO fidibeki_app USING (tenant_id = NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid)',table_name);
        EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO fidibeki_app,fidibeki_admin',table_name);
    END LOOP;
END $$;

ALTER TABLE widget_rules DROP CONSTRAINT widget_rules_ai_processing_contract;
ALTER TABLE widget_rules ADD CONSTRAINT widget_rules_ai_processing_contract CHECK (
    (ai_processing->>'enabled')::boolean=false OR COALESCE((
        ai_processing->>'policy_version' IN ('2026-07-24','2026-10-02')
        AND ai_processing->>'provider' IN ('google_gemini','openai','anthropic','amazon_bedrock','openai_compatible','anthropic_compatible')
        AND ai_processing->>'sensitive_data_mode'='prohibit_and_quarantine'
        AND jsonb_typeof(ai_processing->'purposes')='array' AND jsonb_array_length(ai_processing->'purposes')>0
        AND jsonb_typeof(ai_processing->'input_categories')='array' AND jsonb_array_length(ai_processing->'input_categories')>0
        AND ai_processing->>'privacy_policy_url' ~ '^https://'
        AND ai_processing->>'acknowledged'='true'
        AND ai_processing->>'acknowledged_at' IS NOT NULL AND ai_processing->>'acknowledged_by_member_id' IS NOT NULL
        AND (
            (COALESCE(ai_processing->>'billing_mode','managed')='managed' AND ai_processing->>'provider'='google_gemini' AND COALESCE(ai_processing->>'connection_id','')='')
            OR (ai_processing->>'billing_mode'='customer' AND ai_processing->>'connection_id' IS NOT NULL AND ai_processing->>'model' IS NOT NULL AND ai_processing->>'config_version' IS NOT NULL AND ai_processing->>'policy_version'='2026-10-02')
        )
    ),false)
);
ALTER TABLE feedbacks ADD COLUMN ai_execution jsonb;
ALTER TABLE attachments ADD COLUMN ai_execution jsonb;
ALTER TABLE ai_processing_runs DROP CONSTRAINT ai_processing_runs_provider_check;
ALTER TABLE ai_processing_runs DROP CONSTRAINT ai_processing_runs_model_check;
ALTER TABLE ai_processing_runs ADD CONSTRAINT ai_processing_runs_provider_check CHECK(provider IN ('google_gemini','openai','anthropic','amazon_bedrock','openai_compatible','anthropic_compatible'));
ALTER TABLE ai_processing_runs ADD CONSTRAINT ai_processing_runs_model_check CHECK(char_length(model) BETWEEN 1 AND 512);
ALTER TABLE ai_processing_runs ADD COLUMN connection_id uuid, ADD COLUMN config_version bigint,
    ADD COLUMN credential_version bigint, ADD COLUMN billing_mode text NOT NULL DEFAULT 'managed' CHECK(billing_mode IN ('managed','customer')),
    ADD COLUMN resolved_model text, ADD COLUMN token_usage jsonb,
    ADD COLUMN provider_attempts integer NOT NULL DEFAULT 0, ADD COLUMN dispatched_at timestamptz;

-- Keep the legacy ledger name for existing deletion tasks and exports. A row is
-- a cleanup tombstone and must survive deletion of the source or its workspace.
ALTER TABLE ai_gemini_files DROP CONSTRAINT ai_gemini_files_tenant_id_fkey;
ALTER TABLE ai_gemini_files DROP CONSTRAINT ai_gemini_files_tenant_attachment_fkey;
ALTER TABLE ai_gemini_files DROP CONSTRAINT ai_gemini_files_file_name_key;
ALTER TABLE ai_gemini_files ADD COLUMN connection_id uuid, ADD COLUMN config_version bigint,
    ADD COLUMN credential_version bigint, ADD COLUMN credential_envelope bytea,
    ADD COLUMN provider text NOT NULL DEFAULT 'google_gemini' CHECK(provider='google_gemini');
CREATE UNIQUE INDEX ai_provider_file_identity ON ai_gemini_files(COALESCE(connection_id,'00000000-0000-0000-0000-000000000000'::uuid),file_name);

-- Existing owners receive the new permissions. Custom grants remain explicit.
UPDATE tenant_members SET permissions=ARRAY(SELECT DISTINCT p FROM unnest(permissions || ARRAY['ai_connection:read','ai_connection:manage']) AS p)
WHERE EXISTS(SELECT 1 FROM tenants t WHERE t.id=tenant_members.tenant_id AND t.owner_member_id=tenant_members.id)
   OR permissions @> ARRAY['tenant:update','member:update','integration:create','integration:update'];
