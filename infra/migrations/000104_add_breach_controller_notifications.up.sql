CREATE TYPE breach_controller_notification_kind AS ENUM (
    'initial', 'update', 'closure'
);
CREATE TYPE breach_controller_notification_status AS ENUM (
    'draft', 'pending', 'processing', 'sent', 'failed'
);
CREATE TYPE breach_controller_notification_attempt_status AS ENUM (
    'sent', 'failed'
);

CREATE TABLE breach_controller_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    breach_incident_id uuid NOT NULL
        REFERENCES breach_incidents(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    notification_kind breach_controller_notification_kind NOT NULL,
    sequence integer NOT NULL CHECK (sequence > 0),
    template_version integer NOT NULL CHECK (template_version > 0),
    incident_public_id text NOT NULL
        CHECK (char_length(incident_public_id) BETWEEN 1 AND 80),
    workspace_name text NOT NULL CHECK (char_length(workspace_name) BETWEEN 1 AND 200),
    recipient_email citext NOT NULL
        CHECK (
            char_length(recipient_email::text) BETWEEN 3 AND 320
            AND recipient_email::text ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'
        ),
    privacy_profile_version integer NOT NULL CHECK (privacy_profile_version >= 0),
    awareness_at timestamptz NOT NULL,
    affected_environments text[] NOT NULL,
    affected_system_categories text[] NOT NULL,
    affected_data_categories text[] NOT NULL,
    affected_subject_categories text[] NOT NULL,
    confidentiality_impact boolean NOT NULL,
    integrity_impact boolean NOT NULL,
    availability_impact boolean NOT NULL,
    summary text NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 4000),
    measures_taken text NOT NULL CHECK (char_length(measures_taken) BETWEEN 1 AND 4000),
    next_update text
        CHECK (next_update IS NULL OR char_length(next_update) BETWEEN 1 AND 1000),
    status breach_controller_notification_status NOT NULL DEFAULT 'draft',
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text CHECK (last_error IS NULL OR char_length(last_error) BETWEEN 1 AND 500),
    provider_message_id text
        CHECK (provider_message_id IS NULL OR char_length(provider_message_id) BETWEEN 1 AND 500),
    last_attempt_at timestamptz,
    queued_at timestamptz,
    sent_at timestamptz,
    created_by_platform_admin_id uuid NOT NULL
        REFERENCES platform_admins(id) ON DELETE RESTRICT,
    queued_by_platform_admin_id uuid
        REFERENCES platform_admins(id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT breach_controller_notifications_incident_tenant_fkey
        FOREIGN KEY (breach_incident_id, tenant_id)
        REFERENCES breach_incident_tenants(breach_incident_id, tenant_id)
        ON DELETE CASCADE,
    CONSTRAINT breach_controller_notifications_idempotency_key
        UNIQUE (breach_incident_id, idempotency_key),
    CONSTRAINT breach_controller_notifications_sequence_key
        UNIQUE (breach_incident_id, tenant_id, sequence),
    CONSTRAINT breach_controller_notifications_tenant_id_id_key
        UNIQUE (tenant_id, id),
    CONSTRAINT breach_controller_notifications_identity_key
        UNIQUE (breach_incident_id, tenant_id, id),
    CONSTRAINT breach_controller_notifications_impact_present CHECK (
        confidentiality_impact OR integrity_impact OR availability_impact
    ),
    CONSTRAINT breach_controller_notifications_snapshot_sets CHECK (
        cardinality(affected_environments) BETWEEN 1 AND 10
        AND cardinality(affected_system_categories) BETWEEN 1 AND 20
        AND cardinality(affected_data_categories) BETWEEN 1 AND 20
        AND cardinality(affected_subject_categories) BETWEEN 1 AND 10
        AND NOT ('' = ANY(affected_environments))
        AND NOT ('' = ANY(affected_system_categories))
        AND NOT ('' = ANY(affected_data_categories))
        AND NOT ('' = ANY(affected_subject_categories))
    ),
    CONSTRAINT breach_controller_notifications_delivery_state CHECK (
        (status = 'draft' AND attempts = 0 AND last_error IS NULL
            AND provider_message_id IS NULL AND last_attempt_at IS NULL
            AND queued_at IS NULL AND sent_at IS NULL
            AND queued_by_platform_admin_id IS NULL)
        OR (status = 'pending' AND last_error IS NULL
            AND provider_message_id IS NULL AND sent_at IS NULL
            AND queued_at IS NOT NULL AND queued_by_platform_admin_id IS NOT NULL)
        OR (status = 'processing' AND last_error IS NULL
            AND provider_message_id IS NULL AND sent_at IS NULL
            AND last_attempt_at IS NOT NULL AND queued_at IS NOT NULL
            AND queued_by_platform_admin_id IS NOT NULL)
        OR (status = 'sent' AND last_error IS NULL
            AND provider_message_id IS NOT NULL AND sent_at IS NOT NULL
            AND last_attempt_at IS NOT NULL AND queued_at IS NOT NULL
            AND queued_by_platform_admin_id IS NOT NULL)
        OR (status = 'failed' AND last_error IS NOT NULL
            AND provider_message_id IS NULL AND sent_at IS NULL
            AND last_attempt_at IS NOT NULL AND queued_at IS NOT NULL
            AND queued_by_platform_admin_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX breach_controller_notifications_initial_once
    ON breach_controller_notifications (breach_incident_id, tenant_id)
    WHERE notification_kind = 'initial';
CREATE UNIQUE INDEX breach_controller_notifications_closure_once
    ON breach_controller_notifications (breach_incident_id, tenant_id)
    WHERE notification_kind = 'closure';
CREATE INDEX idx_breach_controller_notifications_incident
    ON breach_controller_notifications (
        breach_incident_id, tenant_id, sequence
    );

CREATE TABLE breach_controller_notification_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    breach_incident_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    notification_id uuid NOT NULL,
    attempt_number integer NOT NULL CHECK (attempt_number > 0),
    status breach_controller_notification_attempt_status NOT NULL,
    provider_message_id text
        CHECK (provider_message_id IS NULL OR char_length(provider_message_id) BETWEEN 1 AND 500),
    error_message text
        CHECK (error_message IS NULL OR char_length(error_message) BETWEEN 1 AND 500),
    attempted_at timestamptz NOT NULL,
    CONSTRAINT breach_controller_notification_attempts_notification_fkey
        FOREIGN KEY (breach_incident_id, tenant_id, notification_id)
        REFERENCES breach_controller_notifications(
            breach_incident_id, tenant_id, id
        ) ON DELETE CASCADE,
    CONSTRAINT breach_controller_notification_attempts_number_key
        UNIQUE (notification_id, attempt_number),
    CONSTRAINT breach_controller_notification_attempts_state CHECK (
        (status = 'sent' AND provider_message_id IS NOT NULL AND error_message IS NULL)
        OR (status = 'failed' AND provider_message_id IS NULL AND error_message IS NOT NULL)
    )
);

CREATE INDEX idx_breach_controller_notification_attempts_notification
    ON breach_controller_notification_attempts (notification_id, attempt_number);

CREATE TABLE breach_controller_notification_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    notification_id uuid NOT NULL,
    event_key text NOT NULL CHECK (char_length(event_key) BETWEEN 1 AND 240),
    queued_by_platform_admin_id uuid NOT NULL
        REFERENCES platform_admins(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    dispatched_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text CHECK (last_error IS NULL OR char_length(last_error) <= 2000),
    CONSTRAINT breach_controller_notification_outbox_notification_fkey
        FOREIGN KEY (tenant_id, notification_id)
        REFERENCES breach_controller_notifications(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT breach_controller_notification_outbox_event_key
        UNIQUE (notification_id, event_key)
);

CREATE INDEX idx_breach_controller_notification_outbox_pending
    ON breach_controller_notification_outbox (created_at, id)
    WHERE dispatched_at IS NULL;

CREATE FUNCTION breach_controller_notification_content_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id <> OLD.id
        OR NEW.breach_incident_id <> OLD.breach_incident_id
        OR NEW.tenant_id <> OLD.tenant_id
        OR NEW.notification_kind <> OLD.notification_kind
        OR NEW.sequence <> OLD.sequence
        OR NEW.template_version <> OLD.template_version
        OR NEW.incident_public_id <> OLD.incident_public_id
        OR NEW.workspace_name <> OLD.workspace_name
        OR NEW.recipient_email <> OLD.recipient_email
        OR NEW.privacy_profile_version <> OLD.privacy_profile_version
        OR NEW.awareness_at <> OLD.awareness_at
        OR NEW.affected_environments <> OLD.affected_environments
        OR NEW.affected_system_categories <> OLD.affected_system_categories
        OR NEW.affected_data_categories <> OLD.affected_data_categories
        OR NEW.affected_subject_categories <> OLD.affected_subject_categories
        OR NEW.confidentiality_impact <> OLD.confidentiality_impact
        OR NEW.integrity_impact <> OLD.integrity_impact
        OR NEW.availability_impact <> OLD.availability_impact
        OR NEW.summary <> OLD.summary
        OR NEW.measures_taken <> OLD.measures_taken
        OR NEW.next_update IS DISTINCT FROM OLD.next_update
        OR NEW.created_by_platform_admin_id <> OLD.created_by_platform_admin_id
        OR NEW.idempotency_key <> OLD.idempotency_key
        OR NEW.request_fingerprint <> OLD.request_fingerprint
        OR NEW.created_at <> OLD.created_at
    THEN
        RAISE EXCEPTION 'breach controller notification content is immutable'
            USING ERRCODE = 'raise_exception';
    END IF;
    RETURN NEW;
END;
$$;

REVOKE EXECUTE ON FUNCTION breach_controller_notification_content_guard()
    FROM PUBLIC, fidibeki_app;
GRANT EXECUTE ON FUNCTION breach_controller_notification_content_guard()
    TO fidibeki_admin;

CREATE TRIGGER breach_controller_notifications_content_immutable
BEFORE UPDATE ON breach_controller_notifications
FOR EACH ROW EXECUTE FUNCTION breach_controller_notification_content_guard();

CREATE TRIGGER breach_controller_notifications_delete_immutable
BEFORE DELETE ON breach_controller_notifications
FOR EACH ROW EXECUTE FUNCTION breach_register_append_only();

CREATE TRIGGER breach_controller_notification_attempts_immutable
BEFORE UPDATE OR DELETE ON breach_controller_notification_attempts
FOR EACH ROW EXECUTE FUNCTION breach_register_append_only();

GRANT USAGE ON TYPE breach_controller_notification_kind,
    breach_controller_notification_status,
    breach_controller_notification_attempt_status
    TO fidibeki_admin;
REVOKE ALL PRIVILEGES ON breach_controller_notifications,
    breach_controller_notification_attempts,
    breach_controller_notification_outbox
    FROM PUBLIC, fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT ON breach_controller_notifications,
    breach_controller_notification_attempts,
    breach_controller_notification_outbox
    TO fidibeki_admin;
GRANT UPDATE (
    status, attempts, last_error, provider_message_id, last_attempt_at,
    queued_at, sent_at, queued_by_platform_admin_id, updated_at
) ON breach_controller_notifications TO fidibeki_admin;
GRANT UPDATE (
    dispatched_at, attempts, last_error
) ON breach_controller_notification_outbox TO fidibeki_admin;
