ALTER TABLE ai_gemini_files
    DROP CONSTRAINT IF EXISTS ai_gemini_files_processing_run_fkey,
    DROP COLUMN IF EXISTS processing_run_id;

DROP TABLE IF EXISTS ai_processing_runs;

ALTER TABLE attachments
    DROP CONSTRAINT IF EXISTS attachments_ai_redaction_manifest_object,
    DROP COLUMN IF EXISTS ai_redaction_manifest;

ALTER TABLE feedbacks
    DROP CONSTRAINT IF EXISTS feedbacks_ai_redaction_manifest_object,
    DROP COLUMN IF EXISTS ai_redaction_manifest;

ALTER TABLE widget_rules
    DROP CONSTRAINT IF EXISTS widget_rules_ai_processing_contract,
    DROP CONSTRAINT IF EXISTS widget_rules_ai_processing_object,
    DROP COLUMN IF EXISTS ai_processing;
