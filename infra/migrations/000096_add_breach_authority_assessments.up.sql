-- Second breach register tracer bullet (issue #64): server-authoritative
-- 72-hour authority-decision deadline plus append-only assessment evidence.
-- Same enforcement boundary as migration 92: no fidibeki_app grant is issued
-- below, so tenant credentials cannot reach this data regardless of
-- application wiring, and the new table rejects UPDATE/DELETE outright.

-- The 72-hour clock starts at awareness, not at completion of the
-- investigation (see issue #64's incident contract). This can't be a
-- GENERATED column: timestamptz + interval is only STABLE in Postgres (civil
-- calendar arithmetic can be timezone-dependent), and generated-column
-- expressions must be IMMUTABLE. Instead the store computes and stores the
-- value itself -- the same server-authoritative pattern already used for
-- created_at/occurred_at elsewhere in this register -- and the CHECK below
-- enforces that it can never drift from awareness_at.
--
-- Existing incidents must be backfilled during upgrade. Temporarily remove
-- the append-only trigger inside this migration transaction so the one
-- controlled backfill can run; recreate it immediately afterwards.
DROP TRIGGER breach_incidents_immutable ON breach_incidents;
ALTER TABLE breach_incidents ADD COLUMN authority_notification_deadline_at timestamptz;
UPDATE breach_incidents
    SET authority_notification_deadline_at = awareness_at + interval '72 hours'
    WHERE authority_notification_deadline_at IS NULL;
ALTER TABLE breach_incidents
    ALTER COLUMN authority_notification_deadline_at SET NOT NULL;
ALTER TABLE breach_incidents ADD CONSTRAINT breach_incidents_deadline_computed CHECK (
    authority_notification_deadline_at = awareness_at + interval '72 hours'
);
CREATE TRIGGER breach_incidents_immutable
BEFORE UPDATE OR DELETE ON breach_incidents
FOR EACH ROW EXECUTE FUNCTION breach_register_append_only();

CREATE TYPE breach_authority_notification_decision AS ENUM (
    'assessment_pending', 'notification_required', 'notification_not_required'
);

CREATE TYPE breach_risk_decision AS ENUM (
    'assessment_pending', 'high_risk', 'not_high_risk'
);

CREATE TYPE breach_communication_decision AS ENUM (
    'assessment_pending', 'communication_required', 'communication_not_required'
);

-- Each row is one operator-entered assessment snapshot: the responsible
-- controller's reasoned decision plus the facts it was based on. Nothing
-- here is computed automatically -- every decision column requires an
-- explicit enum value and a non-empty reasoning note from the recording
-- platform admin (see issue #64: "do not automatically claim that every
-- incident requires authority or data-subject notification"). Like
-- breach_chronology_entries, later facts are recorded as new rows rather
-- than by rewriting a prior assessment.
CREATE TABLE breach_assessments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    breach_incident_id uuid NOT NULL REFERENCES breach_incidents(id) ON DELETE CASCADE,
    authority_notification_decision breach_authority_notification_decision NOT NULL,
    authority_notification_reasoning text NOT NULL
        CHECK (char_length(authority_notification_reasoning) BETWEEN 1 AND 4000),
    high_risk_to_individuals breach_risk_decision NOT NULL,
    high_risk_reasoning text NOT NULL
        CHECK (char_length(high_risk_reasoning) BETWEEN 1 AND 4000),
    data_subject_communication_decision breach_communication_decision NOT NULL,
    data_subject_communication_reasoning text NOT NULL
        CHECK (char_length(data_subject_communication_reasoning) BETWEEN 1 AND 4000),
    estimated_affected_subjects_min bigint,
    estimated_affected_subjects_max bigint,
    estimated_affected_records_min bigint,
    estimated_affected_records_max bigint,
    likely_consequences text CHECK (
        likely_consequences IS NULL OR char_length(likely_consequences) BETWEEN 1 AND 4000
    ),
    recovery_measures text CHECK (
        recovery_measures IS NULL OR char_length(recovery_measures) BETWEEN 1 AND 4000
    ),
    evidence_references text[] NOT NULL DEFAULT '{}',
    assessed_by_platform_admin_id uuid NOT NULL
        REFERENCES platform_admins(id) ON DELETE RESTRICT,
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 200),
    request_fingerprint bytea NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    assessed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT breach_assessments_idempotency_key UNIQUE (breach_incident_id, idempotency_key),
    -- Same server-clock-read rule as breach_chronology_entries: no
    -- client-supplied backdating in this tracer bullet.
    CONSTRAINT breach_assessments_assessed_matches_created CHECK (assessed_at = created_at),
    CONSTRAINT breach_assessments_subjects_pair CHECK (
        (estimated_affected_subjects_min IS NULL) = (estimated_affected_subjects_max IS NULL)
    ),
    CONSTRAINT breach_assessments_subjects_bounded CHECK (
        estimated_affected_subjects_min IS NULL
        OR (estimated_affected_subjects_min >= 0
            AND estimated_affected_subjects_min <= estimated_affected_subjects_max
            AND estimated_affected_subjects_max <= 1000000000000)
    ),
    CONSTRAINT breach_assessments_records_pair CHECK (
        (estimated_affected_records_min IS NULL) = (estimated_affected_records_max IS NULL)
    ),
    CONSTRAINT breach_assessments_records_bounded CHECK (
        estimated_affected_records_min IS NULL
        OR (estimated_affected_records_min >= 0
            AND estimated_affected_records_min <= estimated_affected_records_max
            AND estimated_affected_records_max <= 1000000000000)
    ),
    CONSTRAINT breach_assessments_evidence_bounded CHECK (
        cardinality(evidence_references) <= 20
        AND NOT ('' = ANY(evidence_references))
        AND char_length(array_to_string(evidence_references, ',')) <= 4000
    )
);

CREATE INDEX idx_breach_assessments_incident
    ON breach_assessments (breach_incident_id, sequence);

CREATE TRIGGER breach_assessments_immutable
BEFORE UPDATE OR DELETE ON breach_assessments
FOR EACH ROW EXECUTE FUNCTION breach_register_append_only();

GRANT USAGE ON TYPE breach_authority_notification_decision, breach_risk_decision,
    breach_communication_decision TO fidibeki_admin;
GRANT SELECT, INSERT ON breach_assessments TO fidibeki_admin;
GRANT USAGE, SELECT ON SEQUENCE breach_assessments_sequence_seq TO fidibeki_admin;
