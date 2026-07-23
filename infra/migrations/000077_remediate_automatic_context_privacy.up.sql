-- Historical automatic context predates the rule-safe projection introduced by issue #54.
-- Existing paths cannot be proven static after the fact, so remediation deliberately keeps
-- only origin/. Referrers, raw user agents, and DOM captures are removed rather than guessed.
CREATE FUNCTION fidibeki_privacy_safe_origin(value text) RETURNS text
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    parts text[];
BEGIN
    parts := regexp_match(
        btrim(value),
        '^(https?)://([^/@]*@)?([^/?#[:space:]]+)',
        'i'
    );
    IF parts IS NULL THEN
        RETURN 'https://invalid.invalid/';
    END IF;
    RETURN lower(parts[1]) || '://' || lower(parts[3]) || '/';
END;
$$;

UPDATE feedbacks
SET page_url = fidibeki_privacy_safe_origin(page_url),
    referrer = NULL,
    user_agent = NULL,
    dom_snapshot_html = NULL,
    dom_snapshot_styles = NULL
WHERE page_url <> fidibeki_privacy_safe_origin(page_url)
   OR referrer IS NOT NULL
   OR user_agent IS NOT NULL
   OR dom_snapshot_html IS NOT NULL
   OR dom_snapshot_styles IS NOT NULL;

UPDATE sessions
SET page_url = fidibeki_privacy_safe_origin(page_url)
WHERE page_url IS NOT NULL
  AND page_url <> fidibeki_privacy_safe_origin(page_url);

UPDATE widget_funnel_events
SET page_url = fidibeki_privacy_safe_origin(page_url),
    page_key = digest(fidibeki_privacy_safe_origin(page_url), 'sha256')
WHERE page_url <> fidibeki_privacy_safe_origin(page_url)
   OR page_key <> digest(fidibeki_privacy_safe_origin(page_url), 'sha256');

-- Multiple historical path dimensions can collapse to the same safe origin/day key.
-- Rebuild the aggregate so the primary key remains valid and no funnel counts are lost.
CREATE TEMPORARY TABLE privacy_safe_widget_funnel_daily
ON COMMIT DROP
AS
SELECT tenant_id,
       project_id,
       rule_id,
       min(rule_name) AS rule_name,
       fidibeki_privacy_safe_origin(page_url) AS page_url,
       digest(fidibeki_privacy_safe_origin(page_url), 'sha256') AS page_key,
       event_date,
       sum(matched_count) AS matched_count,
       sum(impression_count) AS impression_count,
       sum(interaction_count) AS interaction_count,
       sum(submitted_count) AS submitted_count,
       sum(dismissed_count) AS dismissed_count,
       max(updated_at) AS updated_at
FROM widget_funnel_daily
GROUP BY tenant_id,
         project_id,
         rule_id,
         event_date,
         fidibeki_privacy_safe_origin(page_url);

DELETE FROM widget_funnel_daily;

INSERT INTO widget_funnel_daily (
    tenant_id,
    project_id,
    rule_id,
    rule_name,
    page_url,
    page_key,
    event_date,
    matched_count,
    impression_count,
    interaction_count,
    submitted_count,
    dismissed_count,
    updated_at
)
SELECT tenant_id,
       project_id,
       rule_id,
       rule_name,
       page_url,
       page_key,
       event_date,
       matched_count,
       impression_count,
       interaction_count,
       submitted_count,
       dismissed_count,
       updated_at
FROM privacy_safe_widget_funnel_daily;

-- Stored ZIP exports are derived objects and may contain the removed historical values.
-- Version zero archives become immediately unavailable to download and are selected by the
-- existing retention worker for physical object deletion. New binaries are marked version one
-- atomically by the updated completion paths.
ALTER TABLE privacy_jobs
    ADD COLUMN context_privacy_version smallint NOT NULL DEFAULT 0,
    ADD CONSTRAINT privacy_jobs_context_privacy_version_valid CHECK (
        context_privacy_version BETWEEN 0 AND 1
    );

CREATE INDEX idx_privacy_jobs_context_privacy_cleanup
    ON privacy_jobs (context_privacy_version, id)
    WHERE kind IN ('workspace_export', 'reporter_export')
      AND status = 'completed'
      AND object_key IS NOT NULL;

DROP FUNCTION fidibeki_privacy_safe_origin(text);
