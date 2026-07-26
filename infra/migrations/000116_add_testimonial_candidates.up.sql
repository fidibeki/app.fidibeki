CREATE TYPE testimonial_eligibility AS ENUM ('eligible', 'ineligible');

GRANT USAGE ON TYPE testimonial_eligibility TO fidibeki_app, fidibeki_admin;

-- One row per scored feedback, upserted by the scoring job. Ranking lives here rather than on
-- feedbacks so a rescore never rewrites the feedback row, and so the Suggested queue can be
-- indexed independently of the triage queue.
CREATE TABLE testimonial_candidates (
    tenant_id uuid NOT NULL,
    feedback_id uuid NOT NULL,
    project_id uuid NOT NULL,
    -- base_score is the undecayed weighting. numeric(6,5) rather than numeric(5,5): the latter
    -- admits values below 1.0 only, so it could not represent a perfect score. The CHECK, not the
    -- precision, is what constrains the range.
    base_score numeric(6,5) NOT NULL CHECK (base_score BETWEEN 0 AND 1),
    -- decayed_score is materialised by the nightly job. Applying the decay at read time instead
    -- would make the index below unusable for ORDER BY, turning every Suggested load into a full
    -- scan and sort of the tenant's eligible candidates.
    decayed_score numeric(6,5) NOT NULL CHECK (decayed_score BETWEEN 0 AND 1),
    score_version smallint NOT NULL CHECK (score_version > 0),
    eligibility testimonial_eligibility NOT NULL,
    ineligible_reasons text[] NOT NULL DEFAULT '{}',
    reasons text[] NOT NULL DEFAULT '{}',
    signals jsonb NOT NULL DEFAULT '{}'::jsonb,
    suggested_excerpt text CHECK (suggested_excerpt IS NULL OR char_length(suggested_excerpt) <= 2000),
    suggested_headline text CHECK (suggested_headline IS NULL OR char_length(suggested_headline) <= 200),
    dismissed_at timestamptz,
    dismissed_by uuid,
    computed_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT testimonial_candidates_pkey PRIMARY KEY (tenant_id, feedback_id),
    CONSTRAINT testimonial_candidates_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT testimonial_candidates_tenant_feedback_fkey FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES feedbacks(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT testimonial_candidates_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT testimonial_candidates_tenant_dismissed_by_fkey
        FOREIGN KEY (tenant_id, dismissed_by)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (dismissed_by),
    -- An ineligible candidate carries no score: a score would invite someone to surface it anyway.
    CONSTRAINT testimonial_candidates_ineligible_has_no_score CHECK (
        eligibility = 'eligible' OR (base_score = 0 AND decayed_score = 0 AND reasons = '{}')
    ),
    CONSTRAINT testimonial_candidates_dismissed_by_requires_time CHECK (
        (dismissed_at IS NULL) = (dismissed_by IS NULL)
    )
);

-- The Suggested queue: eligible, not dismissed, best first.
CREATE INDEX idx_testimonial_candidates_queue
    ON testimonial_candidates (tenant_id, eligibility, decayed_score DESC)
    WHERE dismissed_at IS NULL;

CREATE INDEX idx_testimonial_candidates_project
    ON testimonial_candidates (tenant_id, project_id);

-- Drives the rescore job when the weighting changes.
CREATE INDEX idx_testimonial_candidates_score_version
    ON testimonial_candidates (tenant_id, score_version);

CREATE TRIGGER testimonial_candidates_set_updated_at
BEFORE UPDATE ON testimonial_candidates
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE testimonial_candidates ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonial_candidates FORCE ROW LEVEL SECURITY;

CREATE POLICY testimonial_candidates_tenant_isolation ON testimonial_candidates
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON testimonial_candidates TO fidibeki_app, fidibeki_admin;
