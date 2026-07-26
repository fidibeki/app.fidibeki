CREATE TYPE testimonial_status AS ENUM ('draft', 'pending_review', 'approved', 'archived');
CREATE TYPE testimonial_consent_state AS ENUM (
    'pending',
    'granted',
    'pending_reconsent',
    'revoked',
    'expired'
);
CREATE TYPE testimonial_source AS ENUM ('feedback', 'form', 'manual');

GRANT USAGE ON TYPE testimonial_status TO fidibeki_app, fidibeki_admin;
GRANT USAGE ON TYPE testimonial_consent_state TO fidibeki_app, fidibeki_admin;
GRANT USAGE ON TYPE testimonial_source TO fidibeki_app, fidibeki_admin;

-- A testimonial is a separate aggregate from the feedback it came from: the author may rewrite it,
-- add media, and later revoke it, none of which should mutate the original feedback record.
--
-- status and consent_state are deliberately orthogonal. Conflating them is how a customer approves
-- something whose author revoked consent yesterday.
CREATE TABLE testimonials (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    public_id text NOT NULL,
    project_id uuid,
    source testimonial_source NOT NULL,
    source_feedback_id uuid,
    -- The foreign key is added in 000118, once testimonial_requests exists. The two tables
    -- reference each other, so neither can be created with both constraints in place.
    request_id uuid,

    -- consented_text is what the author actually agreed to publish and is never edited.
    -- quote is what is displayed. The service enforces that they match after NFC normalisation
    -- and whitespace trimming; anything else moves consent_state to 'pending_reconsent'.
    consented_text text NOT NULL CHECK (char_length(consented_text) BETWEEN 1 AND 2000),
    quote text NOT NULL CHECK (char_length(quote) BETWEEN 1 AND 2000),
    headline text CHECK (headline IS NULL OR char_length(headline) <= 200),

    author_name text CHECK (author_name IS NULL OR char_length(author_name) <= 120),
    author_role text CHECK (author_role IS NULL OR char_length(author_role) <= 120),
    author_company text CHECK (author_company IS NULL OR char_length(author_company) <= 120),
    author_company_url text CHECK (author_company_url IS NULL OR char_length(author_company_url) <= 2048),
    author_locale text CHECK (author_locale IS NULL OR char_length(author_locale) <= 20),
    rating smallint CHECK (rating IS NULL OR rating BETWEEN 1 AND 5),

    status testimonial_status NOT NULL DEFAULT 'draft',
    consent_state testimonial_consent_state NOT NULL DEFAULT 'pending',
    -- Each scope is granted separately: people will publish their words but not their face.
    consent_scopes jsonb NOT NULL DEFAULT '{}'::jsonb,
    consent_notice_version text,
    consent_controller_profile_version integer,
    consent_locale text CHECK (consent_locale IS NULL OR char_length(consent_locale) <= 20),
    consent_granted_at timestamptz,
    -- UA family, coarse region and a notice-snapshot hash. Never a raw IP address.
    consent_evidence jsonb,

    revocation_token_hash text CHECK (
        revocation_token_hash IS NULL OR char_length(revocation_token_hash) = 64
    ),
    revoked_at timestamptz,
    revocation_reason text CHECK (revocation_reason IS NULL OR char_length(revocation_reason) <= 2000),

    review_note text CHECK (review_note IS NULL OR char_length(review_note) <= 2000),

    collected_at timestamptz,
    approved_at timestamptz,
    approved_by uuid,
    archived_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT testimonials_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT testimonials_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE SET NULL (project_id),
    CONSTRAINT testimonials_tenant_feedback_fkey FOREIGN KEY (tenant_id, source_feedback_id)
        REFERENCES feedbacks(tenant_id, id) ON DELETE SET NULL (source_feedback_id),
    CONSTRAINT testimonials_tenant_approved_by_fkey FOREIGN KEY (tenant_id, approved_by)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (approved_by),
    CONSTRAINT testimonials_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT testimonials_tenant_public_id_key UNIQUE (tenant_id, public_id),
    CONSTRAINT testimonials_revocation_token_hash_key UNIQUE (revocation_token_hash),
    -- 16 characters, not the 8 used for internal identifiers: a testimonial public id appears in
    -- published snapshots, and 8 characters is thin against enumeration across every tenant.
    CONSTRAINT testimonials_public_id_format CHECK (public_id ~ '^ts_[A-Za-z0-9]{16}$'),
    CONSTRAINT testimonials_granted_requires_time CHECK (
        consent_state <> 'granted' OR consent_granted_at IS NOT NULL
    ),
    CONSTRAINT testimonials_revoked_requires_time CHECK (
        (consent_state = 'revoked') = (revoked_at IS NOT NULL)
    ),
    CONSTRAINT testimonials_approved_by_requires_time CHECK (
        (approved_at IS NULL) = (approved_by IS NULL)
    ),
    CONSTRAINT testimonials_archived_requires_time CHECK (
        (status = 'archived') = (archived_at IS NOT NULL)
    ),
    CONSTRAINT testimonials_feedback_source_has_feedback CHECK (
        source <> 'feedback' OR source_feedback_id IS NOT NULL
    )
);

-- One *live* testimonial per feedback. The partial predicate matters: if an author revokes and the
-- customer archives the result, a fresh request for the same feedback must still be possible.
CREATE UNIQUE INDEX idx_testimonials_live_per_feedback
    ON testimonials (tenant_id, source_feedback_id)
    WHERE source_feedback_id IS NOT NULL AND status <> 'archived';

CREATE INDEX idx_testimonials_status ON testimonials (tenant_id, status);
CREATE INDEX idx_testimonials_consent_state ON testimonials (tenant_id, consent_state);
CREATE INDEX idx_testimonials_project ON testimonials (tenant_id, project_id);
CREATE INDEX idx_testimonials_created_at ON testimonials (tenant_id, created_at DESC);

CREATE TRIGGER testimonials_set_updated_at
BEFORE UPDATE ON testimonials
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Reuses the tenant tag catalogue that feedback and issues already share, so a workspace has one
-- vocabulary rather than three.
CREATE TABLE testimonial_tags (
    tenant_id uuid NOT NULL,
    testimonial_id uuid NOT NULL,
    tag_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT testimonial_tags_pkey PRIMARY KEY (tenant_id, testimonial_id, tag_id),
    CONSTRAINT testimonial_tags_tenant_testimonial_fkey FOREIGN KEY (tenant_id, testimonial_id)
        REFERENCES testimonials(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT testimonial_tags_tenant_tag_fkey FOREIGN KEY (tenant_id, tag_id)
        REFERENCES tags(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_testimonial_tags_tag_id ON testimonial_tags (tenant_id, tag_id);

ALTER TABLE testimonials ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonials FORCE ROW LEVEL SECURITY;
CREATE POLICY testimonials_tenant_isolation ON testimonials
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE testimonial_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonial_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY testimonial_tags_tenant_isolation ON testimonial_tags
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON testimonials TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON testimonial_tags TO fidibeki_app, fidibeki_admin;
