-- Restricted personal-data breach register (issue #64, first tracer bullet).
-- Platform-admin only: no fidibeki_app grant is issued on any table below, so
-- ordinary tenant members and tenant API keys (which always connect as
-- fidibeki_app) cannot see or write this data regardless of application bugs.
-- fidibeki_admin already bypasses RLS (see ConfigureRuntimeRoles), so no RLS
-- policy is required; the missing GRANT is the enforcement boundary.

CREATE TYPE breach_controller_role AS ENUM ('controller', 'processor');

CREATE TYPE breach_detection_source AS ENUM (
    'internal_monitoring',
    'customer_report',
    'subprocessor_notice',
    'security_researcher',
    'employee_report',
    'automated_alert',
    'audit_finding',
    'other'
);

CREATE TYPE breach_chronology_entry_kind AS ENUM (
    'detected',
    'triaged',
    'contained',
    'eradicated',
    'recovered',
    'assessed',
    'evidence_added',
    'containment_updated',
    'escalated',
    'note',
    'closed'
);

-- The incident row captures the facts known at creation time. It is never
-- updated or deleted by the application (see the immutability trigger below);
-- anything that changes afterward — containment progress, assessment,
-- notifications — is recorded as a new breach_chronology_entries row instead
-- of rewriting this record.
CREATE TABLE breach_incidents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    public_id text NOT NULL,
    awareness_at timestamptz NOT NULL,
    detection_source breach_detection_source NOT NULL,
    controller_role breach_controller_role NOT NULL,
    affected_environments text[] NOT NULL,
    affected_system_categories text[] NOT NULL,
    affected_data_categories text[] NOT NULL,
    affected_subject_categories text[] NOT NULL,
    confidentiality_impact boolean NOT NULL,
    integrity_impact boolean NOT NULL,
    availability_impact boolean NOT NULL,
    containment_summary text NOT NULL
        CHECK (char_length(containment_summary) BETWEEN 1 AND 4000),
    created_by_platform_admin_id uuid NOT NULL
        REFERENCES platform_admins(id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    created_at timestamptz NOT NULL,
    CONSTRAINT breach_incidents_public_id_key UNIQUE (public_id),
    CONSTRAINT breach_incidents_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT breach_incidents_awareness_not_future CHECK (awareness_at <= created_at),
    CONSTRAINT breach_incidents_impact_present CHECK (
        confidentiality_impact OR integrity_impact OR availability_impact
    ),
    CONSTRAINT breach_incidents_environments_valid CHECK (
        cardinality(affected_environments) BETWEEN 1 AND 10
        AND affected_environments <@ ARRAY[
            'production', 'staging', 'third_party_subprocessor', 'corporate_it', 'other'
        ]::text[]
    ),
    CONSTRAINT breach_incidents_system_categories_valid CHECK (
        cardinality(affected_system_categories) BETWEEN 1 AND 20
        AND affected_system_categories <@ ARRAY[
            'api', 'database', 'object_storage', 'email', 'integration',
            'replay', 'ai', 'analytics', 'widget', 'authentication', 'other'
        ]::text[]
    ),
    CONSTRAINT breach_incidents_data_categories_valid CHECK (
        cardinality(affected_data_categories) BETWEEN 1 AND 20
        AND affected_data_categories <@ ARRAY[
            'contact_info', 'authentication_credentials', 'usage_analytics',
            'content_text', 'attachments_media', 'session_replay',
            'billing_payment', 'technical_identifiers', 'other'
        ]::text[]
    ),
    CONSTRAINT breach_incidents_subject_categories_valid CHECK (
        cardinality(affected_subject_categories) BETWEEN 1 AND 10
        AND affected_subject_categories <@ ARRAY[
            'tenant_members', 'reporters_end_users', 'prospects_leads',
            'employees_contractors', 'other'
        ]::text[]
    )
);

CREATE INDEX idx_breach_incidents_created ON breach_incidents (created_at DESC, id DESC);

-- Affected tenant references. Deliberately holds only the tenant ID: readers
-- of the register must never receive tenant name/slug/billing content
-- through this table, only the opaque reference.
CREATE TABLE breach_incident_tenants (
    breach_incident_id uuid NOT NULL REFERENCES breach_incidents(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (breach_incident_id, tenant_id)
);

CREATE INDEX idx_breach_incident_tenants_tenant
    ON breach_incident_tenants (tenant_id, breach_incident_id);

CREATE TABLE breach_chronology_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    breach_incident_id uuid NOT NULL REFERENCES breach_incidents(id) ON DELETE CASCADE,
    entry_kind breach_chronology_entry_kind NOT NULL,
    summary text NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 4000),
    evidence_references text[] NOT NULL DEFAULT '{}',
    actor_platform_admin_id uuid NOT NULL
        REFERENCES platform_admins(id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT breach_chronology_entries_idempotency_key
        UNIQUE (breach_incident_id, idempotency_key),
    -- This tracer bullet stamps both timestamps from the same server clock
    -- read (no client-supplied backdating). The columns stay distinct so a
    -- later slice can allow a reasoned, still server-validated occurred_at.
    CONSTRAINT breach_chronology_entries_occurred_matches_created
        CHECK (occurred_at = created_at),
    CONSTRAINT breach_chronology_entries_evidence_bounded CHECK (
        cardinality(evidence_references) <= 20
        AND NOT ('' = ANY(evidence_references))
        AND char_length(array_to_string(evidence_references, ',')) <= 4000
    )
);

CREATE INDEX idx_breach_chronology_entries_incident
    ON breach_chronology_entries (breach_incident_id, sequence);

CREATE FUNCTION breach_register_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'breach register records are append-only'
        USING ERRCODE = 'raise_exception';
END;
$$;

CREATE TRIGGER breach_incidents_immutable
BEFORE UPDATE OR DELETE ON breach_incidents
FOR EACH ROW EXECUTE FUNCTION breach_register_append_only();

CREATE TRIGGER breach_chronology_entries_immutable
BEFORE UPDATE OR DELETE ON breach_chronology_entries
FOR EACH ROW EXECUTE FUNCTION breach_register_append_only();

GRANT USAGE ON TYPE breach_controller_role, breach_detection_source, breach_chronology_entry_kind
    TO fidibeki_admin;
GRANT SELECT, INSERT ON breach_incidents, breach_incident_tenants, breach_chronology_entries
    TO fidibeki_admin;
GRANT USAGE, SELECT ON SEQUENCE breach_chronology_entries_sequence_seq TO fidibeki_admin;
