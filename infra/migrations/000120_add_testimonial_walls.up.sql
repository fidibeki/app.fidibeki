CREATE TYPE testimonial_wall_layout AS ENUM ('grid', 'carousel', 'list', 'masonry', 'single');
CREATE TYPE testimonial_wall_status AS ENUM ('draft', 'published');

GRANT USAGE ON TYPE testimonial_wall_layout TO fidibeki_app, fidibeki_admin;
GRANT USAGE ON TYPE testimonial_wall_status TO fidibeki_app, fidibeki_admin;

-- A wall belongs to the workspace, not to a project: a customer with an app, a site and docs wants
-- one wall on the homepage. project_id is an optional display filter, not an ownership boundary.
CREATE TABLE testimonial_walls (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    project_id uuid,
    public_id text NOT NULL,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 120),
    layout testimonial_wall_layout NOT NULL DEFAULT 'grid',
    theme jsonb NOT NULL DEFAULT '{}'::jsonb,
    show_rating boolean NOT NULL DEFAULT true,
    show_avatar boolean NOT NULL DEFAULT true,
    show_company boolean NOT NULL DEFAULT true,
    show_video boolean NOT NULL DEFAULT true,
    max_items smallint NOT NULL DEFAULT 24 CHECK (max_items BETWEEN 1 AND 200),
    -- Enforced as a referrer allowlist, and labelled in the UI as casual reuse prevention rather
    -- than access control: wall data is public by definition and anyone can fetch it directly.
    allowed_origins text[] NOT NULL DEFAULT '{}',
    status testimonial_wall_status NOT NULL DEFAULT 'draft',
    published_at timestamptz,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT testimonial_walls_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT testimonial_walls_tenant_project_fkey FOREIGN KEY (tenant_id, project_id)
        REFERENCES projects(tenant_id, id) ON DELETE SET NULL (project_id),
    CONSTRAINT testimonial_walls_tenant_created_by_fkey FOREIGN KEY (tenant_id, created_by)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (created_by),
    CONSTRAINT testimonial_walls_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT testimonial_walls_tenant_slug_key UNIQUE (tenant_id, slug),
    CONSTRAINT testimonial_walls_public_id_key UNIQUE (public_id),
    CONSTRAINT testimonial_walls_public_id_format CHECK (public_id ~ '^wl_[A-Za-z0-9]{16}$'),
    CONSTRAINT testimonial_walls_published_requires_time CHECK (
        (status = 'published') = (published_at IS NOT NULL)
    )
);

CREATE INDEX idx_testimonial_walls_status ON testimonial_walls (tenant_id, status);

CREATE TRIGGER testimonial_walls_set_updated_at
BEFORE UPDATE ON testimonial_walls
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Membership is explicit and ordered: the platform suggests, the customer decides.
CREATE TABLE testimonial_wall_items (
    tenant_id uuid NOT NULL,
    wall_id uuid NOT NULL,
    testimonial_id uuid NOT NULL,
    position integer NOT NULL CHECK (position >= 0),
    pinned boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT testimonial_wall_items_pkey PRIMARY KEY (tenant_id, wall_id, testimonial_id),
    CONSTRAINT testimonial_wall_items_tenant_wall_fkey FOREIGN KEY (tenant_id, wall_id)
        REFERENCES testimonial_walls(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT testimonial_wall_items_tenant_testimonial_fkey
        FOREIGN KEY (tenant_id, testimonial_id)
        REFERENCES testimonials(tenant_id, id) ON DELETE CASCADE,
    -- Deferrable so a drag-reorder can renumber every row in one statement without tripping on
    -- transient collisions half way through.
    CONSTRAINT testimonial_wall_items_position_key UNIQUE (tenant_id, wall_id, position)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX idx_testimonial_wall_items_testimonial
    ON testimonial_wall_items (tenant_id, testimonial_id);

-- The published payload. The public embed reads one precomputed row with a strong ETag rather than
-- joining six RLS-protected tables per request. Keeping old versions makes unpublish and rollback
-- trivial, and records what was publicly visible when, which matters if consent is ever disputed.
CREATE TABLE testimonial_wall_snapshots (
    tenant_id uuid NOT NULL,
    wall_id uuid NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    payload jsonb NOT NULL,
    etag text NOT NULL CHECK (char_length(etag) = 64),
    is_current boolean NOT NULL DEFAULT false,
    -- Set synchronously when a consent revocation affects this wall. A stale snapshot is never
    -- served, so the flag is a correctness mechanism rather than a hint.
    is_stale boolean NOT NULL DEFAULT false,
    item_count integer NOT NULL DEFAULT 0 CHECK (item_count >= 0),
    published_at timestamptz NOT NULL DEFAULT now(),
    built_by uuid,
    CONSTRAINT testimonial_wall_snapshots_pkey PRIMARY KEY (tenant_id, wall_id, version),
    CONSTRAINT testimonial_wall_snapshots_tenant_wall_fkey FOREIGN KEY (tenant_id, wall_id)
        REFERENCES testimonial_walls(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT testimonial_wall_snapshots_tenant_built_by_fkey FOREIGN KEY (tenant_id, built_by)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (built_by)
);

CREATE UNIQUE INDEX idx_testimonial_wall_snapshots_current
    ON testimonial_wall_snapshots (tenant_id, wall_id) WHERE is_current;

-- Estimated views only. The beacon is unauthenticated and therefore trivially inflatable, so this
-- is labelled as an estimate in the UI and never used for billing or quota enforcement.
CREATE TABLE testimonial_wall_daily_views (
    tenant_id uuid NOT NULL,
    wall_id uuid NOT NULL,
    day date NOT NULL,
    impressions bigint NOT NULL DEFAULT 0 CHECK (impressions >= 0),
    CONSTRAINT testimonial_wall_daily_views_pkey PRIMARY KEY (tenant_id, wall_id, day),
    CONSTRAINT testimonial_wall_daily_views_tenant_wall_fkey FOREIGN KEY (tenant_id, wall_id)
        REFERENCES testimonial_walls(tenant_id, id) ON DELETE CASCADE
);

ALTER TABLE testimonial_walls ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonial_walls FORCE ROW LEVEL SECURITY;
CREATE POLICY testimonial_walls_tenant_isolation ON testimonial_walls
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE testimonial_wall_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonial_wall_items FORCE ROW LEVEL SECURITY;
CREATE POLICY testimonial_wall_items_tenant_isolation ON testimonial_wall_items
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE testimonial_wall_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonial_wall_snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY testimonial_wall_snapshots_tenant_isolation ON testimonial_wall_snapshots
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE testimonial_wall_daily_views ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonial_wall_daily_views FORCE ROW LEVEL SECURITY;
CREATE POLICY testimonial_wall_daily_views_tenant_isolation ON testimonial_wall_daily_views
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON testimonial_walls TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON testimonial_wall_items TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON testimonial_wall_snapshots TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON testimonial_wall_daily_views TO fidibeki_app, fidibeki_admin;
