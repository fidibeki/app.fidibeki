-- Restricted external-provider incident intake for issue #64. These rows
-- freeze the code-owned provider-governance facts known when a notice is
-- received; they do not classify every provider as a subprocessor.

CREATE TABLE breach_provider_incident_intakes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    breach_incident_id uuid NOT NULL
        REFERENCES breach_incidents(id) ON DELETE CASCADE,
    provider_id text NOT NULL
        CHECK (char_length(provider_id) BETWEEN 1 AND 120),
    governance_registry_version integer NOT NULL
        CHECK (governance_registry_version > 0),
    provider_display_name text NOT NULL
        CHECK (char_length(provider_display_name) BETWEEN 1 AND 200),
    boundary_kind text NOT NULL
        CHECK (
            boundary_kind IN (
                'infrastructure_provider',
                'service_provider',
                'customer_configured_destination'
            )
        ),
    approval_status text NOT NULL
        CHECK (approval_status IN ('pending', 'approved')),
    contract_status text NOT NULL
        CHECK (contract_status IN ('unknown', 'pending', 'executed')),
    primary_retention_category text NOT NULL
        CHECK (char_length(primary_retention_category) BETWEEN 1 AND 120),
    provider_notice_reference text NOT NULL
        CHECK (char_length(provider_notice_reference) BETWEEN 1 AND 300),
    provider_reported_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    summary text NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 4000),
    evidence_references text[] NOT NULL DEFAULT '{}',
    affected_data_flow_categories text[] NOT NULL,
    actor_platform_admin_id uuid NOT NULL
        REFERENCES platform_admins(id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL
        CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL
        CHECK (octet_length(request_fingerprint) = 32),
    created_at timestamptz NOT NULL,
    CONSTRAINT breach_provider_incident_intakes_identity_key
        UNIQUE (id, breach_incident_id),
    CONSTRAINT breach_provider_incident_intakes_idempotency_key
        UNIQUE (breach_incident_id, idempotency_key),
    CONSTRAINT breach_provider_incident_intakes_received_matches_created CHECK (
        received_at = created_at
    ),
    CONSTRAINT breach_provider_incident_intakes_provider_time CHECK (
        provider_reported_at <= received_at
    ),
    CONSTRAINT breach_provider_incident_intakes_evidence_bounded CHECK (
        cardinality(evidence_references) <= 20
        AND array_position(evidence_references, NULL) IS NULL
        AND NOT ('' = ANY(evidence_references))
        AND char_length(array_to_string(evidence_references, ',')) <= 4000
    ),
    CONSTRAINT breach_provider_incident_intakes_data_flows_bounded CHECK (
        cardinality(affected_data_flow_categories) BETWEEN 1 AND 32
        AND array_position(affected_data_flow_categories, NULL) IS NULL
        AND NOT ('' = ANY(affected_data_flow_categories))
        AND primary_retention_category = ANY(affected_data_flow_categories)
    )
);

CREATE INDEX idx_breach_provider_incident_intakes_incident
    ON breach_provider_incident_intakes (breach_incident_id, sequence);

CREATE FUNCTION breach_provider_incident_intake_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    incident_awareness_at timestamptz;
BEGIN
    SELECT awareness_at
    INTO incident_awareness_at
    FROM breach_incidents
    WHERE id = NEW.breach_incident_id;

    IF incident_awareness_at IS NOT NULL
        AND NEW.received_at < incident_awareness_at THEN
        RAISE EXCEPTION 'provider incident intake receipt predates incident awareness'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

REVOKE EXECUTE ON FUNCTION breach_provider_incident_intake_scope_guard()
    FROM PUBLIC, fidibeki_app;
GRANT EXECUTE ON FUNCTION breach_provider_incident_intake_scope_guard()
    TO fidibeki_admin;

CREATE TRIGGER breach_provider_incident_intakes_scope_guard
BEFORE INSERT ON breach_provider_incident_intakes
FOR EACH ROW EXECUTE FUNCTION breach_provider_incident_intake_scope_guard();

CREATE TABLE breach_provider_incident_tenants (
    provider_incident_intake_id uuid NOT NULL,
    breach_incident_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (provider_incident_intake_id, tenant_id),
    CONSTRAINT breach_provider_incident_tenants_intake_fkey
        FOREIGN KEY (provider_incident_intake_id, breach_incident_id)
        REFERENCES breach_provider_incident_intakes(id, breach_incident_id)
        ON DELETE CASCADE,
    CONSTRAINT breach_provider_incident_tenants_incident_tenant_fkey
        FOREIGN KEY (breach_incident_id, tenant_id)
        REFERENCES breach_incident_tenants(breach_incident_id, tenant_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_breach_provider_incident_tenants_incident
    ON breach_provider_incident_tenants (breach_incident_id, tenant_id);

CREATE TRIGGER breach_provider_incident_intakes_immutable
BEFORE UPDATE OR DELETE ON breach_provider_incident_intakes
FOR EACH ROW EXECUTE FUNCTION breach_register_append_only();

CREATE TRIGGER breach_provider_incident_tenants_immutable
BEFORE UPDATE OR DELETE ON breach_provider_incident_tenants
FOR EACH ROW EXECUTE FUNCTION breach_register_append_only();

REVOKE ALL PRIVILEGES ON breach_provider_incident_intakes,
    breach_provider_incident_tenants
    FROM PUBLIC, fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT ON breach_provider_incident_intakes,
    breach_provider_incident_tenants
    TO fidibeki_admin;
REVOKE ALL PRIVILEGES ON SEQUENCE breach_provider_incident_intakes_sequence_seq
    FROM PUBLIC, fidibeki_app, fidibeki_admin;
GRANT USAGE, SELECT ON SEQUENCE breach_provider_incident_intakes_sequence_seq
    TO fidibeki_admin;
