CREATE TYPE reporter_privacy_action AS ENUM ('export', 'anonymize');
CREATE TYPE reporter_privacy_status AS ENUM (
    'pending_verification',
    'processing',
    'completed',
    'failed',
    'expired'
);

ALTER TABLE feedbacks
    ADD COLUMN reporter_consent_granted_at timestamptz,
    ADD COLUMN reporter_consent_withdrawn_at timestamptz;

ALTER TABLE issues
    ADD COLUMN reporter_consent_granted_at timestamptz,
    ADD COLUMN reporter_consent_withdrawn_at timestamptz;

UPDATE feedbacks
SET reporter_consent_granted_at = created_at
WHERE notify_reporter = true;

UPDATE issues
SET reporter_consent_granted_at = created_at
WHERE notify_reporter = true;

CREATE FUNCTION track_reporter_consent() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.notify_reporter THEN
        NEW.reporter_consent_granted_at := COALESCE(
            NEW.reporter_consent_granted_at,
            CASE WHEN TG_OP = 'UPDATE' THEN OLD.reporter_consent_granted_at ELSE NULL END,
            now()
        );
        NEW.reporter_consent_withdrawn_at := NULL;
    ELSIF TG_OP = 'UPDATE' AND OLD.notify_reporter THEN
        NEW.reporter_consent_withdrawn_at := COALESCE(
            NEW.reporter_consent_withdrawn_at,
            now()
        );
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER feedbacks_track_reporter_consent
BEFORE INSERT OR UPDATE OF notify_reporter, reporter_consent_granted_at, reporter_consent_withdrawn_at
ON feedbacks
FOR EACH ROW EXECUTE FUNCTION track_reporter_consent();

CREATE TRIGGER issues_track_reporter_consent
BEFORE INSERT OR UPDATE OF notify_reporter, reporter_consent_granted_at, reporter_consent_withdrawn_at
ON issues
FOR EACH ROW EXECUTE FUNCTION track_reporter_consent();

ALTER TABLE feedbacks
    ADD CONSTRAINT feedbacks_reporter_consent_consistent CHECK (
        (notify_reporter AND reporter_consent_granted_at IS NOT NULL
            AND reporter_consent_withdrawn_at IS NULL)
        OR (NOT notify_reporter AND (
            reporter_consent_granted_at IS NULL OR reporter_consent_withdrawn_at IS NOT NULL
        ))
    );

ALTER TABLE issues
    ADD CONSTRAINT issues_reporter_consent_consistent CHECK (
        (notify_reporter AND reporter_consent_granted_at IS NOT NULL
            AND reporter_consent_withdrawn_at IS NULL)
        OR (NOT notify_reporter AND (
            reporter_consent_granted_at IS NULL OR reporter_consent_withdrawn_at IS NOT NULL
        ))
    );

CREATE TABLE reporter_privacy_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    action reporter_privacy_action NOT NULL,
    status reporter_privacy_status NOT NULL DEFAULT 'pending_verification',
    reporter_email text,
    email_digest bytea NOT NULL CHECK (octet_length(email_digest) = 32),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    actor_member_id uuid,
    verification_expires_at timestamptz NOT NULL,
    verified_at timestamptz,
    access_expires_at timestamptz,
    completed_at timestamptz,
    record_count integer CHECK (record_count IS NULL OR record_count >= 0),
    manual_review_count integer CHECK (manual_review_count IS NULL OR manual_review_count >= 0),
    error_message text CHECK (error_message IS NULL OR char_length(error_message) <= 2000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reporter_privacy_requests_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT reporter_privacy_requests_tenant_actor_fkey
        FOREIGN KEY (tenant_id, actor_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (actor_member_id),
    CONSTRAINT reporter_privacy_requests_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT reporter_privacy_requests_email_valid CHECK (
        reporter_email IS NULL OR (
            reporter_email = lower(btrim(reporter_email))
            AND char_length(reporter_email) BETWEEN 3 AND 320
        )
    ),
    CONSTRAINT reporter_privacy_requests_state_consistent CHECK (
        (status = 'pending_verification' AND reporter_email IS NOT NULL
            AND verified_at IS NULL AND access_expires_at IS NULL AND completed_at IS NULL)
        OR (status = 'processing' AND reporter_email IS NOT NULL
            AND verified_at IS NOT NULL AND access_expires_at IS NOT NULL AND completed_at IS NULL)
        OR (status = 'completed' AND verified_at IS NOT NULL
            AND access_expires_at IS NOT NULL AND completed_at IS NOT NULL
            AND (action = 'export' OR reporter_email IS NULL))
        OR (status = 'failed' AND verified_at IS NOT NULL
            AND access_expires_at IS NOT NULL AND completed_at IS NULL
            AND error_message IS NOT NULL)
        OR (status = 'expired' AND reporter_email IS NULL AND access_expires_at IS NOT NULL)
    ),
    CONSTRAINT reporter_privacy_requests_expiry_consistent CHECK (
        verification_expires_at > created_at
        AND (access_expires_at IS NULL OR (
            verified_at IS NOT NULL AND access_expires_at > verified_at
        ))
    )
);

CREATE UNIQUE INDEX idx_reporter_privacy_requests_active
    ON reporter_privacy_requests (tenant_id, email_digest, action)
    WHERE status IN ('pending_verification', 'processing');
CREATE INDEX idx_reporter_privacy_requests_tenant_created
    ON reporter_privacy_requests (tenant_id, created_at DESC, id DESC);
CREATE INDEX idx_reporter_privacy_requests_expiry
    ON reporter_privacy_requests (status, verification_expires_at, access_expires_at, id);

CREATE TRIGGER reporter_privacy_requests_set_updated_at
BEFORE UPDATE ON reporter_privacy_requests
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE reporter_privacy_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE reporter_privacy_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY reporter_privacy_requests_tenant_isolation ON reporter_privacy_requests
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE privacy_jobs
    ADD COLUMN reporter_request_id uuid,
    ADD CONSTRAINT privacy_jobs_tenant_reporter_request_fkey
        FOREIGN KEY (tenant_id, reporter_request_id)
        REFERENCES reporter_privacy_requests(tenant_id, id) ON DELETE CASCADE,
    ADD CONSTRAINT privacy_jobs_kind_request_consistent CHECK (
        (kind = 'workspace_export' AND reporter_request_id IS NULL)
        OR (kind IN ('reporter_export', 'reporter_anonymize') AND reporter_request_id IS NOT NULL)
    );

CREATE UNIQUE INDEX idx_privacy_jobs_active_reporter_request
    ON privacy_jobs (tenant_id, reporter_request_id)
    WHERE reporter_request_id IS NOT NULL AND status IN ('pending', 'processing');

ALTER TABLE privacy_jobs DROP CONSTRAINT privacy_jobs_state_consistent;
ALTER TABLE privacy_jobs ADD CONSTRAINT privacy_jobs_state_consistent CHECK (
    (status = 'pending' AND started_at IS NULL AND completed_at IS NULL
        AND expires_at IS NULL AND object_key IS NULL AND object_size_bytes IS NULL)
    OR (status = 'processing' AND started_at IS NOT NULL AND heartbeat_at IS NOT NULL
        AND completed_at IS NULL AND expires_at IS NULL AND object_key IS NULL
        AND object_size_bytes IS NULL)
    OR (status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL
        AND expires_at IS NOT NULL AND (
            (kind = 'reporter_anonymize' AND object_key IS NULL AND object_size_bytes = 0)
            OR (kind <> 'reporter_anonymize' AND object_key IS NOT NULL
                AND object_size_bytes IS NOT NULL)
        ))
    OR (status = 'failed' AND completed_at IS NULL AND expires_at IS NULL
        AND object_key IS NULL AND object_size_bytes IS NULL AND error_message IS NOT NULL)
    OR (status = 'expired' AND completed_at IS NOT NULL AND expires_at IS NOT NULL
        AND object_key IS NULL AND object_size_bytes IS NOT NULL)
);

ALTER TABLE attachments
    ADD COLUMN retention_deleted_at timestamptz,
    ALTER COLUMN storage_key DROP NOT NULL,
    ADD CONSTRAINT attachments_retention_consistent CHECK (
        retention_deleted_at IS NULL
        OR (storage_key IS NULL AND completed_at IS NOT NULL AND complete_token_hash IS NULL)
    );

ALTER TABLE sessions
    ADD COLUMN retention_deleted_at timestamptz,
    DROP CONSTRAINT sessions_merge_consistent,
    ADD CONSTRAINT sessions_merge_consistent CHECK (
        (merged_at IS NULL AND storage_key IS NULL AND retention_deleted_at IS NULL)
        OR (merged_at IS NULL AND storage_key IS NULL AND retention_deleted_at IS NOT NULL)
        OR (merged_at IS NOT NULL AND storage_key IS NOT NULL
            AND merge_requested_at IS NOT NULL AND retention_deleted_at IS NULL)
        OR (merged_at IS NOT NULL AND storage_key IS NULL
            AND merge_requested_at IS NOT NULL AND retention_deleted_at IS NOT NULL)
    );

ALTER TABLE session_batches
    ADD COLUMN retention_deleted_at timestamptz,
    ALTER COLUMN storage_key DROP NOT NULL,
    ADD CONSTRAINT session_batches_retention_consistent CHECK (
        retention_deleted_at IS NULL
        OR (storage_key IS NULL AND completed_at IS NOT NULL AND complete_token_hash IS NULL)
    );

CREATE INDEX idx_attachments_retention_pending
    ON attachments (tenant_id, completed_at, id)
    WHERE completed_at IS NOT NULL AND retention_deleted_at IS NULL;
CREATE INDEX idx_sessions_retention_pending
    ON sessions (tenant_id, COALESCE(ended_at, created_at), id)
    WHERE retention_deleted_at IS NULL;

GRANT USAGE ON TYPE reporter_privacy_action, reporter_privacy_status
    TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE ON reporter_privacy_requests
    TO fidibeki_app, fidibeki_admin;
