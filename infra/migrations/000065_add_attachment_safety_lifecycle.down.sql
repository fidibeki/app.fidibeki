DROP INDEX IF EXISTS idx_attachments_safety_verification;
DROP INDEX IF EXISTS idx_attachments_upload_expiry;

ALTER TABLE attachments
    DROP CONSTRAINT IF EXISTS attachments_safety_state_consistent;

UPDATE attachments
SET complete_token_hash = encode(
        sha256(('attachment-rollback:' || id::text)::bytea),
        'hex'
    ),
    completed_at = NULL
WHERE completed_at IS NULL
  AND complete_token_hash IS NULL;

ALTER TABLE attachments
    ADD CONSTRAINT attachments_completion_consistent CHECK (
        (completed_at IS NULL AND complete_token_hash IS NOT NULL) OR
        (completed_at IS NOT NULL AND complete_token_hash IS NULL)
    ),
    DROP COLUMN IF EXISTS verification_attempts,
    DROP COLUMN IF EXISTS verification_enqueued_at,
    DROP COLUMN IF EXISTS verification_generation,
    DROP COLUMN IF EXISTS detected_content_type,
    DROP COLUMN IF EXISTS safety_error_reason,
    DROP COLUMN IF EXISTS safety_checked_at,
    DROP COLUMN IF EXISTS upload_expires_at,
    DROP COLUMN IF EXISTS safety_status;

DROP TYPE attachment_safety_status;
