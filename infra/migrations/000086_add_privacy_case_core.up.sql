CREATE TYPE privacy_subject_type AS ENUM ('reporter', 'member');
CREATE TYPE privacy_right_type AS ENUM (
    'access',
    'rectification',
    'erasure',
    'restriction',
    'objection',
    'portability_assessment',
    'consent_withdrawal'
);
CREATE TYPE privacy_controller_role AS ENUM ('controller', 'customer_controller_assistance');
CREATE TYPE privacy_case_status AS ENUM (
    'received',
    'pending_verification',
    'in_progress',
    'awaiting_clarification',
    'paused',
    'awaiting_legal_review',
    'partially_fulfilled',
    'completed',
    'refused',
    'cancelled'
);

CREATE TABLE privacy_cases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    public_id text NOT NULL,
    subject_type privacy_subject_type NOT NULL,
    right_type privacy_right_type NOT NULL,
    controller_role privacy_controller_role NOT NULL,
    status privacy_case_status NOT NULL,
    subject_email text,
    subject_email_digest bytea,
    subject_member_id uuid,
    requested_by_member_id uuid NOT NULL,
    owner_member_id uuid,
    verification_token_hash bytea UNIQUE,
    verification_expires_at timestamptz,
    verification_method text,
    verified_at timestamptz,
    received_at timestamptz NOT NULL,
    deadline_at timestamptz NOT NULL,
    pause_started_at timestamptz,
    paused_seconds bigint NOT NULL DEFAULT 0 CHECK (paused_seconds >= 0),
    completed_at timestamptz,
    scope jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(scope) = 'object'),
    decision_summary text CHECK (
        decision_summary IS NULL OR char_length(decision_summary) BETWEEN 1 AND 4000
    ),
    legal_review_notes text CHECK (
        legal_review_notes IS NULL OR char_length(legal_review_notes) BETWEEN 1 AND 4000
    ),
    exception_reason text CHECK (
        exception_reason IS NULL OR char_length(exception_reason) BETWEEN 1 AND 4000
    ),
    legacy_reporter_request_id uuid,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT privacy_cases_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_cases_tenant_subject_member_fkey
        FOREIGN KEY (tenant_id, subject_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT privacy_cases_tenant_requester_fkey
        FOREIGN KEY (tenant_id, requested_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT privacy_cases_tenant_owner_fkey
        FOREIGN KEY (tenant_id, owner_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (owner_member_id),
    CONSTRAINT privacy_cases_tenant_legacy_request_fkey
        FOREIGN KEY (tenant_id, legacy_reporter_request_id)
        REFERENCES reporter_privacy_requests(tenant_id, id) ON DELETE SET NULL (legacy_reporter_request_id),
    CONSTRAINT privacy_cases_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT privacy_cases_tenant_public_id_key UNIQUE (tenant_id, public_id),
    CONSTRAINT privacy_cases_tenant_idempotency_key UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT privacy_cases_subject_consistent CHECK (
        (
            subject_type = 'reporter'
            AND subject_email IS NOT NULL
            AND subject_email = lower(btrim(subject_email))
            AND char_length(subject_email) BETWEEN 3 AND 320
            AND subject_email_digest IS NOT NULL
            AND octet_length(subject_email_digest) = 32
            AND subject_member_id IS NULL
        )
        OR (
            subject_type = 'member'
            AND subject_email IS NULL
            AND subject_email_digest IS NULL
            AND subject_member_id IS NOT NULL
        )
    ),
    CONSTRAINT privacy_cases_verification_consistent CHECK (
        (
            subject_type = 'reporter'
            AND (
                (
                    verified_at IS NULL
                    AND verification_token_hash IS NOT NULL
                    AND octet_length(verification_token_hash) = 32
                    AND verification_expires_at IS NOT NULL
                    AND verification_method IS NULL
                    AND status = 'pending_verification'
                )
                OR (
                    verified_at IS NOT NULL
                    AND verification_token_hash IS NULL
                    AND verification_expires_at IS NOT NULL
                    AND verification_method = 'email_link'
                    AND status <> 'pending_verification'
                )
            )
        )
        OR (
            subject_type = 'member'
            AND verified_at IS NOT NULL
            AND verification_token_hash IS NULL
            AND verification_expires_at IS NULL
            AND verification_method = 'workspace_membership'
            AND status <> 'pending_verification'
        )
    ),
    CONSTRAINT privacy_cases_timing_consistent CHECK (
        deadline_at > received_at
        AND (verification_expires_at IS NULL OR verification_expires_at > received_at)
        AND (
            (status = 'paused' AND pause_started_at IS NOT NULL)
            OR (status <> 'paused' AND pause_started_at IS NULL)
        )
        AND (
            (status IN ('partially_fulfilled', 'completed', 'refused', 'cancelled')
                AND completed_at IS NOT NULL)
            OR (status NOT IN ('partially_fulfilled', 'completed', 'refused', 'cancelled')
                AND completed_at IS NULL)
        )
    ),
    CONSTRAINT privacy_cases_reasoned_terminal_consistent CHECK (
        status NOT IN ('partially_fulfilled', 'refused')
        OR decision_summary IS NOT NULL
    )
);

CREATE INDEX idx_privacy_cases_tenant_deadline
    ON privacy_cases (tenant_id, deadline_at, id)
    WHERE status NOT IN ('partially_fulfilled', 'completed', 'refused', 'cancelled');
CREATE INDEX idx_privacy_cases_tenant_status
    ON privacy_cases (tenant_id, status, updated_at DESC, id DESC);
CREATE INDEX idx_privacy_cases_reporter_digest
    ON privacy_cases (tenant_id, subject_email_digest)
    WHERE subject_type = 'reporter';
CREATE INDEX idx_privacy_cases_member
    ON privacy_cases (tenant_id, subject_member_id)
    WHERE subject_type = 'member';

CREATE TRIGGER privacy_cases_set_updated_at
BEFORE UPDATE ON privacy_cases
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE privacy_case_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    tenant_id uuid NOT NULL,
    privacy_case_id uuid NOT NULL,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    event_type text NOT NULL CHECK (char_length(event_type) BETWEEN 1 AND 100),
    actor_member_id uuid,
    previous_status privacy_case_status,
    new_status privacy_case_status,
    data jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(data) = 'object'),
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT privacy_case_events_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_events_tenant_case_fkey
        FOREIGN KEY (tenant_id, privacy_case_id)
        REFERENCES privacy_cases(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT privacy_case_events_tenant_actor_fkey
        FOREIGN KEY (tenant_id, actor_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (actor_member_id),
    CONSTRAINT privacy_case_events_idempotency_key
        UNIQUE (tenant_id, privacy_case_id, idempotency_key)
);

CREATE INDEX idx_privacy_case_events_case
    ON privacy_case_events (tenant_id, privacy_case_id, sequence);

CREATE FUNCTION privacy_case_events_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'privacy case events are append-only'
        USING ERRCODE = 'raise_exception';
END;
$$;

CREATE TRIGGER privacy_case_events_immutable
BEFORE UPDATE OR DELETE ON privacy_case_events
FOR EACH ROW EXECUTE FUNCTION privacy_case_events_append_only();

CREATE TABLE privacy_processing_restrictions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    privacy_case_id uuid NOT NULL,
    purposes text[] NOT NULL,
    active boolean NOT NULL DEFAULT true,
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 2000),
    activated_at timestamptz NOT NULL,
    deactivated_at timestamptz,
    deactivated_by_member_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT privacy_processing_restrictions_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT privacy_processing_restrictions_tenant_case_fkey
        FOREIGN KEY (tenant_id, privacy_case_id)
        REFERENCES privacy_cases(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT privacy_processing_restrictions_tenant_deactivator_fkey
        FOREIGN KEY (tenant_id, deactivated_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (deactivated_by_member_id),
    CONSTRAINT privacy_processing_restrictions_case_key UNIQUE (tenant_id, privacy_case_id),
    CONSTRAINT privacy_processing_restrictions_purposes_valid CHECK (
        cardinality(purposes) > 0
        AND purposes <@ ARRAY[
            'ai',
            'integration',
            'reporter_email',
            'replay',
            'analytics',
            'background_processing'
        ]::text[]
    ),
    CONSTRAINT privacy_processing_restrictions_state_consistent CHECK (
        (active AND deactivated_at IS NULL AND deactivated_by_member_id IS NULL)
        OR (NOT active AND deactivated_at IS NOT NULL)
    )
);

CREATE INDEX idx_privacy_processing_restrictions_active
    ON privacy_processing_restrictions (tenant_id, privacy_case_id)
    WHERE active;

CREATE TRIGGER privacy_processing_restrictions_set_updated_at
BEFORE UPDATE ON privacy_processing_restrictions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE FUNCTION fidibeki_privacy_email_restricted(
    selected_tenant_id uuid,
    selected_email text,
    selected_purpose text
) RETURNS boolean
LANGUAGE sql STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM privacy_processing_restrictions AS restriction
        JOIN privacy_cases AS privacy_case
          ON privacy_case.tenant_id = restriction.tenant_id
         AND privacy_case.id = restriction.privacy_case_id
        WHERE restriction.tenant_id = selected_tenant_id
          AND restriction.active
          AND (
              selected_purpose = ANY(restriction.purposes)
              OR 'background_processing' = ANY(restriction.purposes)
          )
          AND privacy_case.verified_at IS NOT NULL
          AND privacy_case.subject_type = 'reporter'
          AND privacy_case.subject_email_digest = digest(lower(btrim(selected_email)), 'sha256')
    )
$$;

CREATE FUNCTION fidibeki_privacy_feedback_restricted(
    selected_tenant_id uuid,
    selected_feedback_id uuid,
    selected_purpose text
) RETURNS boolean
LANGUAGE sql STABLE
AS $$
    SELECT COALESCE((
        SELECT fidibeki_privacy_email_restricted(
            feedback.tenant_id,
            feedback.reporter_email,
            selected_purpose
        )
        FROM feedbacks AS feedback
        WHERE feedback.tenant_id = selected_tenant_id
          AND feedback.id = selected_feedback_id
          AND feedback.reporter_email IS NOT NULL
    ), false)
$$;

CREATE FUNCTION fidibeki_privacy_issue_restricted(
    selected_tenant_id uuid,
    selected_issue_id uuid,
    selected_purpose text
) RETURNS boolean
LANGUAGE sql STABLE
AS $$
    SELECT COALESCE((
        SELECT fidibeki_privacy_email_restricted(
            issue.tenant_id,
            issue.reporter_email,
            selected_purpose
        )
        FROM issues AS issue
        WHERE issue.tenant_id = selected_tenant_id
          AND issue.id = selected_issue_id
          AND issue.reporter_email IS NOT NULL
    ), false)
$$;

CREATE FUNCTION fidibeki_privacy_session_restricted(
    selected_tenant_id uuid,
    selected_session_id uuid,
    selected_purpose text
) RETURNS boolean
LANGUAGE sql STABLE
AS $$
    SELECT COALESCE((
        SELECT fidibeki_privacy_feedback_restricted(
            session.tenant_id,
            session.feedback_id,
            selected_purpose
        )
        FROM sessions AS session
        WHERE session.tenant_id = selected_tenant_id
          AND session.id = selected_session_id
          AND session.feedback_id IS NOT NULL
    ), false)
$$;

ALTER TABLE privacy_cases ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_cases FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_cases_tenant_isolation ON privacy_cases
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE privacy_case_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_case_events FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_case_events_tenant_isolation ON privacy_case_events
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE privacy_processing_restrictions ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_processing_restrictions FORCE ROW LEVEL SECURITY;
CREATE POLICY privacy_processing_restrictions_tenant_isolation
    ON privacy_processing_restrictions
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON TYPE privacy_subject_type, privacy_right_type, privacy_controller_role,
    privacy_case_status TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE ON privacy_cases TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT ON privacy_case_events TO fidibeki_app, fidibeki_admin;
GRANT USAGE, SELECT ON SEQUENCE privacy_case_events_sequence_seq TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE ON privacy_processing_restrictions TO fidibeki_app, fidibeki_admin;
GRANT EXECUTE ON FUNCTION
    fidibeki_privacy_email_restricted(uuid, text, text),
    fidibeki_privacy_feedback_restricted(uuid, uuid, text),
    fidibeki_privacy_issue_restricted(uuid, uuid, text),
    fidibeki_privacy_session_restricted(uuid, uuid, text)
    TO fidibeki_app, fidibeki_admin;
