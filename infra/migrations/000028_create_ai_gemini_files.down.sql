DROP TABLE IF EXISTS ai_gemini_files;

ALTER TABLE attachments
    DROP CONSTRAINT IF EXISTS attachments_tenant_id_id_key;
