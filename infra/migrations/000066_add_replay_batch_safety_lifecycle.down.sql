DROP INDEX IF EXISTS idx_session_batches_upload_expiry;
DROP INDEX IF EXISTS idx_session_batches_safety_verification;

ALTER TABLE session_batches
    DROP CONSTRAINT session_batches_safety_state_consistent;

UPDATE session_batches
SET complete_token_hash = encode(
        digest('replay-batch-rollback:' || id::text, 'sha256'),
        'hex'
    )
WHERE completed_at IS NULL AND complete_token_hash IS NULL;

ALTER TABLE session_batches
    DROP CONSTRAINT session_batches_upload_expiry_after_activity,
    DROP CONSTRAINT session_batches_verification_attempts_nonnegative,
    DROP CONSTRAINT session_batches_verification_generation_positive,
    DROP CONSTRAINT session_batches_safety_error_reason_length,
    DROP COLUMN verification_attempts,
    DROP COLUMN verification_enqueued_at,
    DROP COLUMN verification_generation,
    DROP COLUMN safety_error_reason,
    DROP COLUMN safety_checked_at,
    DROP COLUMN upload_expires_at,
    DROP COLUMN safety_status,
    ADD CONSTRAINT session_batches_completion_consistent CHECK (
        (completed_at IS NULL AND complete_token_hash IS NOT NULL) OR
        (completed_at IS NOT NULL AND complete_token_hash IS NULL)
    );

DROP TYPE replay_batch_safety_status;
