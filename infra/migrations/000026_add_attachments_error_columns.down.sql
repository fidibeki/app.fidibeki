ALTER TABLE attachments
    DROP CONSTRAINT IF EXISTS attachments_error_message_length,
    DROP CONSTRAINT IF EXISTS attachments_processing_attempts_nonnegative,
    DROP COLUMN IF EXISTS processing_attempts,
    DROP COLUMN IF EXISTS error_message;
