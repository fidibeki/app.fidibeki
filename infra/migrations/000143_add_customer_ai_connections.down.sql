-- Never discard credentials or remote-cleanup evidence as a rollback action.
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM ai_connections) OR EXISTS(SELECT 1 FROM ai_gemini_files WHERE connection_id IS NOT NULL)
       OR EXISTS(SELECT 1 FROM ai_processing_runs WHERE billing_mode='customer' OR char_length(model)>120)
       OR EXISTS(SELECT 1 FROM ai_gemini_files WHERE deleted_at IS NULL)
       OR EXISTS(SELECT 1 FROM widget_rules WHERE ai_processing->>'billing_mode'='customer') THEN
        RAISE EXCEPTION 'Disable BYOK and finish connection/file cleanup before rolling down migration 143';
    END IF;
END $$;
ALTER TABLE ai_gemini_files DROP COLUMN connection_id, DROP COLUMN config_version, DROP COLUMN credential_version, DROP COLUMN credential_envelope, DROP COLUMN provider;
ALTER TABLE ai_gemini_files ADD CONSTRAINT ai_gemini_files_file_name_key UNIQUE(file_name);
-- Historical orphan tombstones remain intact; enforce the legacy relationships
-- for new writes without deleting old cleanup evidence.
ALTER TABLE ai_gemini_files ADD CONSTRAINT ai_gemini_files_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE ai_gemini_files ADD CONSTRAINT ai_gemini_files_tenant_attachment_fkey FOREIGN KEY (tenant_id,attachment_id) REFERENCES attachments(tenant_id,id) ON DELETE CASCADE NOT VALID;
ALTER TABLE ai_processing_runs DROP COLUMN connection_id, DROP COLUMN config_version, DROP COLUMN credential_version, DROP COLUMN billing_mode, DROP COLUMN resolved_model, DROP COLUMN token_usage, DROP COLUMN provider_attempts, DROP COLUMN dispatched_at;
ALTER TABLE feedbacks DROP COLUMN ai_execution;
ALTER TABLE attachments DROP COLUMN ai_execution;
DROP TABLE ai_connection_models,ai_connection_credentials,ai_connections;
UPDATE tenant_members SET permissions=array_remove(array_remove(permissions,'ai_connection:read'),'ai_connection:manage');

ALTER TABLE ai_processing_runs DROP CONSTRAINT ai_processing_runs_provider_check;
ALTER TABLE ai_processing_runs ADD CONSTRAINT ai_processing_runs_provider_check CHECK(provider='google_gemini');
ALTER TABLE ai_processing_runs DROP CONSTRAINT ai_processing_runs_model_check;
ALTER TABLE ai_processing_runs ADD CONSTRAINT ai_processing_runs_model_check CHECK(char_length(model) BETWEEN 1 AND 120);

ALTER TABLE widget_rules DROP CONSTRAINT widget_rules_ai_processing_contract;
ALTER TABLE widget_rules ADD CONSTRAINT widget_rules_ai_processing_contract CHECK (
        (ai_processing->>'enabled')::boolean = false
        OR (
            ai_processing->>'policy_version' = '2026-07-24'
            AND ai_processing->>'provider' = 'google_gemini'
            AND ai_processing->>'sensitive_data_mode' = 'prohibit_and_quarantine'
            AND jsonb_typeof(ai_processing->'purposes') = 'array'
            AND jsonb_array_length(ai_processing->'purposes') > 0
            AND jsonb_typeof(ai_processing->'input_categories') = 'array'
            AND jsonb_array_length(ai_processing->'input_categories') > 0
            AND ai_processing->>'privacy_policy_url' ~ '^https://'
            AND ai_processing->>'acknowledged' = 'true'
            AND (ai_processing->>'acknowledged_at') IS NOT NULL
            AND (ai_processing->>'acknowledged_by_member_id') IS NOT NULL
        )
    );
