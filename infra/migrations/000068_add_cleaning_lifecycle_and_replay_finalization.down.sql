DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM attachments WHERE safety_status = 'cleaning') THEN
        RAISE EXCEPTION
            'cannot roll back migration 68 while attachment cleanup is pending';
    END IF;
    IF EXISTS (SELECT 1 FROM session_batches WHERE safety_status = 'cleaning') THEN
        RAISE EXCEPTION
            'cannot roll back migration 68 while replay batch cleanup is pending';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM sessions
        WHERE finalize_requested_at IS NOT NULL
          AND ended_at IS NULL
    ) THEN
        RAISE EXCEPTION
            'cannot roll back migration 68 while replay finalization is pending';
    END IF;
END
$$;

DROP INDEX idx_sessions_finalize_pending;
ALTER TABLE sessions DROP COLUMN finalize_requested_at;

DROP INDEX idx_attachments_upload_expiry;
DROP INDEX idx_attachments_safety_verification;

ALTER TABLE attachments
    DROP CONSTRAINT attachments_safety_state_consistent,
    ALTER COLUMN safety_status DROP DEFAULT;

ALTER TYPE attachment_safety_status RENAME TO attachment_safety_status_v68;
CREATE TYPE attachment_safety_status AS ENUM (
    'pending',
    'quarantined',
    'safe',
    'rejected',
    'expired'
);

ALTER TABLE attachments
    ALTER COLUMN safety_status TYPE attachment_safety_status
        USING safety_status::text::attachment_safety_status,
    ALTER COLUMN safety_status SET DEFAULT 'pending';

DROP TYPE attachment_safety_status_v68;

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
    WHERE safety_status = 'pending';

GRANT USAGE ON TYPE attachment_safety_status TO fidibeki_app, fidibeki_admin;

DROP INDEX idx_session_batches_upload_expiry;
DROP INDEX idx_session_batches_safety_verification;

ALTER TABLE session_batches
    DROP CONSTRAINT session_batches_safety_state_consistent,
    ALTER COLUMN safety_status DROP DEFAULT;

ALTER TYPE replay_batch_safety_status RENAME TO replay_batch_safety_status_v68;
CREATE TYPE replay_batch_safety_status AS ENUM (
    'pending',
    'quarantined',
    'safe',
    'rejected',
    'expired'
);

ALTER TABLE session_batches
    ALTER COLUMN safety_status TYPE replay_batch_safety_status
        USING safety_status::text::replay_batch_safety_status,
    ALTER COLUMN safety_status SET DEFAULT 'pending';

DROP TYPE replay_batch_safety_status_v68;

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
    WHERE safety_status = 'pending';

GRANT USAGE ON TYPE replay_batch_safety_status TO fidibeki_app, fidibeki_admin;
