CREATE TYPE testimonial_request_status AS ENUM (
    'pending',
    'opened',
    'submitted',
    'expired',
    'cancelled'
);
CREATE TYPE testimonial_request_kind AS ENUM ('collection', 'reconsent');

GRANT USAGE ON TYPE testimonial_request_status TO fidibeki_app, fidibeki_admin;
GRANT USAGE ON TYPE testimonial_request_kind TO fidibeki_app, fidibeki_admin;

-- An invitation to expand a feedback into a testimonial, or to re-approve an edited quote.
-- Only the SHA-256 of the token is stored, matching reporter_access_tokens and tenant_magic_links.
CREATE TABLE testimonial_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid,
    feedback_id uuid,
    testimonial_id uuid,
    kind testimonial_request_kind NOT NULL DEFAULT 'collection',
    token_hash text NOT NULL CHECK (char_length(token_hash) = 64),
    recipient_email text NOT NULL CHECK (char_length(recipient_email) BETWEEN 3 AND 320),
    status testimonial_request_status NOT NULL DEFAULT 'pending',
    prompt text CHECK (prompt IS NULL OR char_length(prompt) <= 2000),
    requested_fields jsonb NOT NULL DEFAULT '{}'::jsonb,
    locale text CHECK (locale IS NULL OR char_length(locale) <= 20),
    -- The proposed wording for a reconsent request. Null for collection requests.
    proposed_quote text CHECK (proposed_quote IS NULL OR char_length(proposed_quote) <= 2000),
    expires_at timestamptz NOT NULL,
    sent_at timestamptz,
    opened_at timestamptz,
    submitted_at timestamptz,
    reminder_count smallint NOT NULL DEFAULT 0 CHECK (reminder_count >= 0),
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT testimonial_requests_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT testimonial_requests_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE SET NULL (project_id),
    CONSTRAINT testimonial_requests_tenant_feedback_fkey FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES feedbacks(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT testimonial_requests_tenant_created_by_fkey FOREIGN KEY (tenant_id, created_by)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (created_by),
    CONSTRAINT testimonial_requests_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT testimonial_requests_token_hash_key UNIQUE (token_hash),
    CONSTRAINT testimonial_requests_reconsent_has_testimonial CHECK (
        kind <> 'reconsent' OR (testimonial_id IS NOT NULL AND proposed_quote IS NOT NULL)
    ),
    CONSTRAINT testimonial_requests_submitted_requires_time CHECK (
        (status = 'submitted') = (submitted_at IS NOT NULL)
    )
);

CREATE INDEX idx_testimonial_requests_status
    ON testimonial_requests (tenant_id, status, expires_at);
CREATE INDEX idx_testimonial_requests_feedback
    ON testimonial_requests (tenant_id, feedback_id) WHERE feedback_id IS NOT NULL;

CREATE TRIGGER testimonial_requests_set_updated_at
BEFORE UPDATE ON testimonial_requests
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The other half of the circular reference declared in 000117.
ALTER TABLE testimonial_requests
    ADD CONSTRAINT testimonial_requests_tenant_testimonial_fkey
        FOREIGN KEY (tenant_id, testimonial_id)
        REFERENCES testimonials(tenant_id, id) ON DELETE CASCADE;

ALTER TABLE testimonials
    ADD CONSTRAINT testimonials_tenant_request_fkey
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES testimonial_requests(tenant_id, id) ON DELETE SET NULL (request_id);

-- A request can produce at most one testimonial. Checking the request status alone does not
-- survive concurrency: two simultaneous submissions both read 'pending' and both insert.
CREATE UNIQUE INDEX idx_testimonials_one_per_request
    ON testimonials (tenant_id, request_id)
    WHERE request_id IS NOT NULL;

-- A reusable public collection link, not tied to any single feedback.
CREATE TABLE testimonial_forms (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid,
    public_id text NOT NULL,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    prompt text CHECK (prompt IS NULL OR char_length(prompt) <= 2000),
    requested_fields jsonb NOT NULL DEFAULT '{}'::jsonb,
    locale text CHECK (locale IS NULL OR char_length(locale) <= 20),
    is_enabled boolean NOT NULL DEFAULT false,
    submission_limit_per_day integer NOT NULL DEFAULT 50 CHECK (submission_limit_per_day > 0),
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT testimonial_forms_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT testimonial_forms_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE SET NULL (project_id),
    CONSTRAINT testimonial_forms_tenant_created_by_fkey FOREIGN KEY (tenant_id, created_by)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (created_by),
    CONSTRAINT testimonial_forms_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT testimonial_forms_public_id_key UNIQUE (public_id),
    -- A public collection form is exactly as enumerable as a wall, and enumerating one lets an
    -- attacker submit forged testimonials to a tenant they have no relationship with.
    CONSTRAINT testimonial_forms_public_id_format CHECK (public_id ~ '^tf_[A-Za-z0-9]{16}$')
);

CREATE INDEX idx_testimonial_forms_enabled
    ON testimonial_forms (tenant_id) WHERE is_enabled;

CREATE TRIGGER testimonial_forms_set_updated_at
BEFORE UPDATE ON testimonial_forms
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE testimonial_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonial_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY testimonial_requests_tenant_isolation ON testimonial_requests
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE testimonial_forms ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonial_forms FORCE ROW LEVEL SECURITY;
CREATE POLICY testimonial_forms_tenant_isolation ON testimonial_forms
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON testimonial_requests TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON testimonial_forms TO fidibeki_app, fidibeki_admin;
