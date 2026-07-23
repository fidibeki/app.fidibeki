CREATE TYPE attachment_safety_status AS ENUM (
    'pending',
    'quarantined',
    'safe',
    'rejected',
    'expired'
);

ALTER TABLE attachments
    DROP CONSTRAINT attachments_completion_consistent,
    ADD COLUMN safety_status attachment_safety_status,
    ADD COLUMN upload_expires_at timestamptz,
    ADD COLUMN safety_checked_at timestamptz,
    ADD COLUMN safety_error_reason text,
    ADD COLUMN detected_content_type text,
    ADD COLUMN verification_generation integer NOT NULL DEFAULT 1,
    ADD COLUMN verification_enqueued_at timestamptz,
    ADD COLUMN verification_attempts integer NOT NULL DEFAULT 0;

UPDATE attachments
SET upload_expires_at = upload_refreshed_at + interval '10 minutes',
    safety_status = CASE
        WHEN completed_at IS NOT NULL THEN 'safe'::attachment_safety_status
        WHEN upload_refreshed_at + interval '10 minutes' <= now()
            THEN 'expired'::attachment_safety_status
        ELSE 'pending'::attachment_safety_status
    END,
    safety_checked_at = CASE
        WHEN completed_at IS NOT NULL THEN completed_at
        WHEN upload_refreshed_at + interval '10 minutes' <= now()
            THEN upload_refreshed_at + interval '10 minutes'
        ELSE NULL
    END,
    safety_error_reason = CASE
        WHEN completed_at IS NULL
         AND upload_refreshed_at + interval '10 minutes' <= now()
            THEN 'upload_expired'
        ELSE NULL
    END,
    detected_content_type = CASE
        WHEN completed_at IS NOT NULL THEN content_type
        ELSE NULL
    END,
    complete_token_hash = CASE
        WHEN completed_at IS NULL
         AND upload_refreshed_at + interval '10 minutes' <= now()
            THEN NULL
        ELSE complete_token_hash
    END;

ALTER TABLE attachments
    ALTER COLUMN safety_status SET DEFAULT 'pending',
    ALTER COLUMN safety_status SET NOT NULL,
    ALTER COLUMN upload_expires_at SET DEFAULT (now() + interval '10 minutes'),
    ALTER COLUMN upload_expires_at SET NOT NULL,
    ADD CONSTRAINT attachments_safety_error_reason_length CHECK (
        safety_error_reason IS NULL OR char_length(safety_error_reason) BETWEEN 1 AND 200
    ),
    ADD CONSTRAINT attachments_detected_content_type_length CHECK (
        detected_content_type IS NULL
        OR char_length(detected_content_type) BETWEEN 1 AND 120
    ),
    ADD CONSTRAINT attachments_verification_generation_positive CHECK (
        verification_generation >= 1
    ),
    ADD CONSTRAINT attachments_verification_attempts_nonnegative CHECK (
        verification_attempts >= 0
    ),
    ADD CONSTRAINT attachments_upload_expiry_after_activity CHECK (
        upload_expires_at > upload_refreshed_at
    ),
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
