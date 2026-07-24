-- Third breach-register tracer bullet (issue #64): an append-only containment
-- checklist and post-incident corrective-action ledger. Recording an action
-- never executes containment or contacts a provider.

CREATE TYPE breach_action_kind AS ENUM ('containment', 'post_incident');

CREATE TYPE breach_action_status AS ENUM (
    'pending', 'in_progress', 'completed', 'not_applicable'
);

CREATE TYPE breach_containment_area AS ENUM (
    'api_keys',
    'sessions',
    'integrations',
    'object_storage',
    'database_access',
    'email',
    'ai',
    'analytics',
    'replay'
);

CREATE TABLE breach_action_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    breach_incident_id uuid NOT NULL REFERENCES breach_incidents(id) ON DELETE CASCADE,
    action_id uuid NOT NULL,
    revision integer NOT NULL CHECK (revision > 0),
    action_kind breach_action_kind NOT NULL,
    containment_area breach_containment_area,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
    status breach_action_status NOT NULL,
    owner_platform_admin_id uuid REFERENCES platform_admins(id) ON DELETE RESTRICT,
    due_at timestamptz,
    outcome_summary text CHECK (
        outcome_summary IS NULL OR char_length(outcome_summary) BETWEEN 1 AND 4000
    ),
    corrective_reference text CHECK (
        corrective_reference IS NULL
        OR char_length(corrective_reference) BETWEEN 1 AND 300
    ),
    evidence_references text[] NOT NULL DEFAULT '{}',
    actor_platform_admin_id uuid NOT NULL
        REFERENCES platform_admins(id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL
        CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL
        CHECK (octet_length(request_fingerprint) = 32),
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT breach_action_events_revision_key
        UNIQUE (breach_incident_id, action_id, revision),
    CONSTRAINT breach_action_events_idempotency_key
        UNIQUE (breach_incident_id, idempotency_key),
    CONSTRAINT breach_action_events_occurred_matches_created CHECK (
        occurred_at = created_at
    ),
    CONSTRAINT breach_action_events_kind_shape CHECK (
        (action_kind = 'containment' AND containment_area IS NOT NULL)
        OR (action_kind = 'post_incident' AND containment_area IS NULL)
    ),
    CONSTRAINT breach_action_events_post_incident_assignment CHECK (
        action_kind <> 'post_incident'
        OR (owner_platform_admin_id IS NOT NULL AND due_at IS NOT NULL)
    ),
    CONSTRAINT breach_action_events_initial_due_future CHECK (
        action_kind <> 'post_incident'
        OR revision <> 1
        OR due_at > created_at
    ),
    CONSTRAINT breach_action_events_status_requirements CHECK (
        (status = 'pending')
        OR (status = 'in_progress' AND owner_platform_admin_id IS NOT NULL)
        OR (
            status = 'completed'
            AND owner_platform_admin_id IS NOT NULL
            AND outcome_summary IS NOT NULL
            AND cardinality(evidence_references) > 0
        )
        OR (
            status = 'not_applicable'
            AND owner_platform_admin_id IS NOT NULL
            AND outcome_summary IS NOT NULL
        )
    ),
    CONSTRAINT breach_action_events_evidence_bounded CHECK (
        cardinality(evidence_references) <= 20
        AND array_position(evidence_references, NULL) IS NULL
        AND NOT ('' = ANY(evidence_references))
        AND char_length(array_to_string(evidence_references, ',')) <= 4000
    )
);

CREATE UNIQUE INDEX idx_breach_action_events_initial_containment
    ON breach_action_events (breach_incident_id, containment_area)
    WHERE action_kind = 'containment' AND revision = 1;

CREATE INDEX idx_breach_action_events_current
    ON breach_action_events (breach_incident_id, action_id, revision DESC);

CREATE INDEX idx_breach_action_events_history
    ON breach_action_events (breach_incident_id, sequence);

-- Backfill the same code-owned checklist used for new incidents. The original
-- incident creator and timestamp are retained so the migration does not imply
-- that containment work began at deployment time.
INSERT INTO breach_action_events (
    id, breach_incident_id, action_id, revision, action_kind, containment_area,
    title, status, actor_platform_admin_id, idempotency_key,
    request_fingerprint, occurred_at, created_at
)
SELECT
    gen_random_uuid(), incident.id, gen_random_uuid(), 1, 'containment',
    control.area::breach_containment_area, control.title, 'pending',
    incident.created_by_platform_admin_id,
    'seed:containment:' || control.area,
    digest('seed:containment:' || control.area || ':' || control.title, 'sha256'),
    incident.created_at, incident.created_at
FROM breach_incidents AS incident
CROSS JOIN (VALUES
    ('api_keys', 'Review and rotate affected API keys'),
    ('sessions', 'Review and revoke affected sessions'),
    ('integrations', 'Review and isolate affected integrations'),
    ('object_storage', 'Review and restrict affected object storage'),
    ('database_access', 'Review and restrict affected database access'),
    ('email', 'Review and contain affected email delivery'),
    ('ai', 'Review and contain affected AI processing'),
    ('analytics', 'Review and contain affected analytics collection'),
    ('replay', 'Review and contain affected session replay')
) AS control(area, title);

-- Enforce the immutable identity and transition graph on INSERT as well as in
-- the application. UPDATE/DELETE are rejected by the shared append-only
-- trigger below.
CREATE FUNCTION breach_action_revision_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    latest breach_action_events%ROWTYPE;
BEGIN
    IF NEW.revision = 1 THEN
        IF EXISTS (
            SELECT 1
            FROM breach_action_events
            WHERE breach_incident_id = NEW.breach_incident_id
              AND action_id = NEW.action_id
        ) THEN
            RAISE EXCEPTION 'breach action revision must advance monotonically'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END IF;

    SELECT *
    INTO latest
    FROM breach_action_events
    WHERE breach_incident_id = NEW.breach_incident_id
      AND action_id = NEW.action_id
    ORDER BY revision DESC
    LIMIT 1;

    IF NOT FOUND OR NEW.revision <> latest.revision + 1 THEN
        RAISE EXCEPTION 'breach action revision must advance monotonically'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.action_kind <> latest.action_kind
        OR NEW.containment_area IS DISTINCT FROM latest.containment_area
        OR NEW.title <> latest.title THEN
        RAISE EXCEPTION 'breach action identity is immutable'
            USING ERRCODE = 'check_violation';
    END IF;
    IF latest.status IN ('completed', 'not_applicable')
        AND NEW.status <> 'in_progress' THEN
        RAISE EXCEPTION 'terminal breach actions must reopen in progress'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER breach_action_events_revision_guard
BEFORE INSERT ON breach_action_events
FOR EACH ROW EXECUTE FUNCTION breach_action_revision_guard();

CREATE TRIGGER breach_action_events_immutable
BEFORE UPDATE OR DELETE ON breach_action_events
FOR EACH ROW EXECUTE FUNCTION breach_register_append_only();

GRANT USAGE ON TYPE breach_action_kind, breach_action_status, breach_containment_area
    TO fidibeki_admin;
REVOKE ALL PRIVILEGES ON breach_action_events FROM PUBLIC, fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT ON breach_action_events TO fidibeki_admin;
GRANT USAGE, SELECT ON SEQUENCE breach_action_events_sequence_seq TO fidibeki_admin;
