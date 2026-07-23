-- Existing replay session page URLs may still carry credentials, a query string, or a
-- fragment captured before the widget/backend enforced URL canonicalization (issue #53).
-- This strips only those well-defined URL syntax components (not a general identity-text
-- redaction heuristic) so persisted replay metadata matches the origin+pathname contract.
WITH stripped AS (
    SELECT id, regexp_replace(
        regexp_replace(
            regexp_replace(page_url, '#.*$', ''),
            '\?.*$', ''
        ),
        '^(https?://)[^/@]*@', '\1'
    ) AS value
    FROM sessions
    WHERE page_url IS NOT NULL
)
UPDATE sessions AS session
SET page_url = CASE
    WHEN stripped.value ~ '^https?://[^/]+/*$'
        THEN regexp_replace(stripped.value, '/+$', '') || '/'
    ELSE regexp_replace(stripped.value, '/+$', '')
END
FROM stripped
WHERE session.id = stripped.id;

-- Existing replay objects were captured before server-side structural validation and may contain
-- identity-bearing text, attributes, URLs, or canvas pixels. Version zero keeps those manifests
-- visible as expired while the retention worker removes every referenced object. New sessions use
-- version one and are playable only after their batches pass the anonymous-event validator.
ALTER TABLE sessions
    ADD COLUMN replay_privacy_version smallint NOT NULL DEFAULT 0,
    ADD CONSTRAINT sessions_replay_privacy_version_check
        CHECK (replay_privacy_version BETWEEN 0 AND 1);

ALTER TABLE sessions
    ALTER COLUMN replay_privacy_version SET DEFAULT 1;

CREATE INDEX idx_sessions_replay_privacy_cleanup
    ON sessions (id)
    WHERE retention_deleted_at IS NULL AND replay_privacy_version < 1;
