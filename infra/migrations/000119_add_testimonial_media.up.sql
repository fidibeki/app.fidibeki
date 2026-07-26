CREATE TYPE testimonial_media_role AS ENUM ('avatar', 'video');
CREATE TYPE testimonial_transcode_status AS ENUM ('pending', 'processing', 'ready', 'failed');
CREATE TYPE testimonial_rendition_kind AS ENUM (
    'mp4_720p',
    'webm_720p',
    'poster',
    'thumbnail',
    'captions_vtt'
);

GRANT USAGE ON TYPE testimonial_media_role TO fidibeki_app, fidibeki_admin;
GRANT USAGE ON TYPE testimonial_transcode_status TO fidibeki_app, fidibeki_admin;
GRANT USAGE ON TYPE testimonial_rendition_kind TO fidibeki_app, fidibeki_admin;

-- The logical asset. testimonial_id is nullable because media is uploaded during collection,
-- before the testimonial row is finalised; request_id carries it in the meantime.
--
-- Reuses attachment_safety_status so testimonial media moves through the same vocabulary as
-- feedback attachments and can share the verification worker.
CREATE TABLE testimonial_media (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    testimonial_id uuid,
    request_id uuid,
    public_id text NOT NULL,
    role testimonial_media_role NOT NULL,
    source_storage_key text NOT NULL CHECK (char_length(source_storage_key) BETWEEN 1 AND 1024),
    content_type text NOT NULL CHECK (char_length(content_type) BETWEEN 1 AND 255),
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    duration_seconds integer CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
    width integer CHECK (width IS NULL OR width > 0),
    height integer CHECK (height IS NULL OR height > 0),
    safety_status attachment_safety_status NOT NULL DEFAULT 'pending',
    transcode_status testimonial_transcode_status NOT NULL DEFAULT 'pending',
    transcode_error text CHECK (transcode_error IS NULL OR char_length(transcode_error) <= 2000),
    transcode_attempts smallint NOT NULL DEFAULT 0 CHECK (transcode_attempts >= 0),
    -- Content addressing is what makes the public media path safe to cache.
    content_hash text CHECK (content_hash IS NULL OR char_length(content_hash) = 64),
    complete_token_hash text CHECK (
        complete_token_hash IS NULL OR char_length(complete_token_hash) = 64
    ),
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT testimonial_media_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT testimonial_media_tenant_testimonial_fkey FOREIGN KEY (tenant_id, testimonial_id)
        REFERENCES testimonials(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT testimonial_media_tenant_request_fkey FOREIGN KEY (tenant_id, request_id)
        REFERENCES testimonial_requests(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT testimonial_media_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT testimonial_media_public_id_key UNIQUE (public_id),
    CONSTRAINT testimonial_media_public_id_format CHECK (public_id ~ '^tm_[A-Za-z0-9]{16}$'),
    CONSTRAINT testimonial_media_belongs_somewhere CHECK (
        testimonial_id IS NOT NULL OR request_id IS NOT NULL
    ),
    CONSTRAINT testimonial_media_completed_has_hash CHECK (
        completed_at IS NULL OR content_hash IS NOT NULL
    )
);

-- One avatar and one video per testimonial, after finalisation...
CREATE UNIQUE INDEX idx_testimonial_media_role_per_testimonial
    ON testimonial_media (tenant_id, testimonial_id, role)
    WHERE testimonial_id IS NOT NULL;

-- ...and during collection, when testimonial_id is still null and the constraint above cannot
-- fire. Without this an author who uploads two avatars produces two rows with no defined winner.
CREATE UNIQUE INDEX idx_testimonial_media_role_per_request
    ON testimonial_media (tenant_id, request_id, role)
    WHERE request_id IS NOT NULL AND testimonial_id IS NULL;

CREATE INDEX idx_testimonial_media_transcode
    ON testimonial_media (tenant_id, transcode_status)
    WHERE transcode_status IN ('pending', 'processing');

CREATE TRIGGER testimonial_media_set_updated_at
BEFORE UPDATE ON testimonial_media
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Derived files. Separate from the asset so the embed payload has a natural "one video, N
-- renditions" shape and so safety/transcode state is not duplicated per file.
CREATE TABLE testimonial_media_renditions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    media_id uuid NOT NULL,
    kind testimonial_rendition_kind NOT NULL,
    storage_key text NOT NULL CHECK (char_length(storage_key) BETWEEN 1 AND 1024),
    content_type text NOT NULL CHECK (char_length(content_type) BETWEEN 1 AND 255),
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    width integer CHECK (width IS NULL OR width > 0),
    height integer CHECK (height IS NULL OR height > 0),
    content_hash text NOT NULL CHECK (char_length(content_hash) = 64),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT testimonial_media_renditions_tenant_id_fkey FOREIGN KEY (tenant_id)
        REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT testimonial_media_renditions_tenant_media_fkey FOREIGN KEY (tenant_id, media_id)
        REFERENCES testimonial_media(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT testimonial_media_renditions_tenant_id_id_key UNIQUE (tenant_id, id),
    -- Also what makes transcode retries idempotent.
    CONSTRAINT testimonial_media_renditions_media_kind_key UNIQUE (tenant_id, media_id, kind)
);

CREATE TRIGGER testimonial_media_renditions_set_updated_at
BEFORE UPDATE ON testimonial_media_renditions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE testimonial_media ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonial_media FORCE ROW LEVEL SECURITY;
CREATE POLICY testimonial_media_tenant_isolation ON testimonial_media
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE testimonial_media_renditions ENABLE ROW LEVEL SECURITY;
ALTER TABLE testimonial_media_renditions FORCE ROW LEVEL SECURITY;
CREATE POLICY testimonial_media_renditions_tenant_isolation ON testimonial_media_renditions
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON testimonial_media TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE, DELETE ON testimonial_media_renditions TO fidibeki_app, fidibeki_admin;
