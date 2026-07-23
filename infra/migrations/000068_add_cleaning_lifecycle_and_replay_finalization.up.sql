DROP INDEX idx_attachments_upload_expiry;
DROP INDEX idx_attachments_safety_verification;

ALTER TABLE attachments
    DROP CONSTRAINT attachments_safety_state_consistent,
    ALTER COLUMN safety_status DROP DEFAULT;

ALTER TYPE attachment_safety_status RENAME TO attachment_safety_status_v65;
CREATE TYPE attachment_safety_status AS ENUM (
    'pending',
    'cleaning',
    'quarantined',
    'safe',
    'rejected',
    'expired'
);

ALTER TABLE attachments
    ALTER COLUMN safety_status TYPE attachment_safety_status
        USING safety_status::text::attachment_safety_status,
    ALTER COLUMN safety_status SET DEFAULT 'pending';

DROP TYPE attachment_safety_status_v65;

UPDATE attachments
SET safety_status = 'cleaning',
    safety_checked_at = NULL,
    safety_error_reason = NULL,
    detected_content_type = NULL,
    verification_enqueued_at = NULL
WHERE safety_status = 'expired'
  AND completed_at IS NULL;

ALTER TABLE attachments
    ADD CONSTRAINT attachments_safety_state_consistent CHECK (
        (
            safety_status = 'pending'
            AND completed_at IS NULL
            AND complete_token_hash IS NOT NULL
            AND safety_checked_at IS NULL
            AND safety_error_reason IS NULL
            AND detected_content_type IS NULL
            AND verification_enqueued_at IS NULL
        )
        OR (
            safety_status = 'cleaning'
            AND completed_at IS NULL
            AND complete_token_hash IS NULL
            AND safety_checked_at IS NULL
            AND safety_error_reason IS NULL
            AND detected_content_type IS NULL
        )
        OR (
            safety_status = 'expired'
            AND completed_at IS NULL
            AND complete_token_hash IS NULL
            AND safety_checked_at IS NOT NULL
            AND safety_error_reason = 'upload_expired'
            AND detected_content_type IS NULL
            AND verification_enqueued_at IS NULL
        )
        OR (
            safety_status = 'quarantined'
            AND completed_at IS NOT NULL
            AND complete_token_hash IS NULL
            AND safety_checked_at IS NULL
            AND safety_error_reason IS NULL
            AND detected_content_type IS NULL
        )
        OR (
            safety_status = 'safe'
            AND completed_at IS NOT NULL
            AND complete_token_hash IS NULL
            AND safety_checked_at IS NOT NULL
            AND safety_error_reason IS NULL
            AND detected_content_type IS NOT NULL
        )
        OR (
            safety_status = 'rejected'
            AND completed_at IS NOT NULL
            AND complete_token_hash IS NULL
            AND safety_checked_at IS NOT NULL
            AND safety_error_reason IS NOT NULL
        )
    );

CREATE INDEX idx_attachments_safety_verification
    ON attachments (verification_enqueued_at, completed_at, id)
    WHERE safety_status = 'quarantined';
CREATE INDEX idx_attachments_upload_expiry
    ON attachments (upload_expires_at, id)
    WHERE safety_status IN ('pending', 'cleaning');

GRANT USAGE ON TYPE attachment_safety_status TO fidibeki_app, fidibeki_admin;

DROP INDEX idx_session_batches_upload_expiry;
DROP INDEX idx_session_batches_safety_verification;

ALTER TABLE session_batches
    DROP CONSTRAINT session_batches_safety_state_consistent,
    ALTER COLUMN safety_status DROP DEFAULT;

ALTER TYPE replay_batch_safety_status RENAME TO replay_batch_safety_status_v66;
CREATE TYPE replay_batch_safety_status AS ENUM (
    'pending',
    'cleaning',
    'quarantined',
    'safe',
    'rejected',
    'expired'
);

ALTER TABLE session_batches
    ALTER COLUMN safety_status TYPE replay_batch_safety_status
        USING safety_status::text::replay_batch_safety_status,
    ALTER COLUMN safety_status SET DEFAULT 'pending';

DROP TYPE replay_batch_safety_status_v66;

UPDATE session_batches
SET safety_status = 'cleaning',
    safety_checked_at = NULL,
    safety_error_reason = NULL,
    verification_enqueued_at = NULL
WHERE safety_status = 'expired'
  AND completed_at IS NULL;

ALTER TABLE session_batches
    ADD CONSTRAINT session_batches_safety_state_consistent CHECK (
        (
            safety_status = 'pending'
            AND completed_at IS NULL
            AND complete_token_hash IS NOT NULL
            AND safety_checked_at IS NULL
            AND safety_error_reason IS NULL
            AND verification_enqueued_at IS NULL
        )
        OR (
            safety_status = 'cleaning'
            AND completed_at IS NULL
            AND complete_token_hash IS NULL
            AND safety_checked_at IS NULL
            AND safety_error_reason IS NULL
        )
        OR (
            safety_status = 'expired'
            AND completed_at IS NULL
            AND complete_token_hash IS NULL
            AND safety_checked_at IS NOT NULL
            AND safety_error_reason = 'upload_expired'
            AND verification_enqueued_at IS NULL
        )
        OR (
            safety_status = 'quarantined'
            AND completed_at IS NOT NULL
            AND complete_token_hash IS NULL
            AND safety_checked_at IS NULL
            AND safety_error_reason IS NULL
        )
        OR (
            safety_status = 'safe'
            AND completed_at IS NOT NULL
            AND complete_token_hash IS NULL
            AND safety_checked_at IS NOT NULL
            AND safety_error_reason IS NULL
        )
        OR (
            safety_status = 'rejected'
            AND completed_at IS NOT NULL
            AND complete_token_hash IS NULL
            AND safety_checked_at IS NOT NULL
            AND safety_error_reason IS NOT NULL
        )
    );

CREATE INDEX idx_session_batches_safety_verification
    ON session_batches (verification_enqueued_at, completed_at, id)
    WHERE safety_status = 'quarantined';
CREATE INDEX idx_session_batches_upload_expiry
    ON session_batches (upload_expires_at, id)
    WHERE safety_status IN ('pending', 'cleaning');

GRANT USAGE ON TYPE replay_batch_safety_status TO fidibeki_app, fidibeki_admin;

ALTER TABLE sessions
    ADD COLUMN finalize_requested_at timestamptz;

CREATE INDEX idx_sessions_finalize_pending
    ON sessions (finalize_requested_at, id)
    WHERE finalize_requested_at IS NOT NULL AND ended_at IS NULL;
