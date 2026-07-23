-- A completed anonymization job previously reused reporter_email to relocate the feedback/issue
-- rows belonging to a reporter. Structured anonymization nulls that email as part of the same
-- job, which destroys the only queryable link once manual review is still outstanding. Capture
-- the exact resource ids up front so later review/finalization can recheck the fixed scope
-- without email and without ever storing the reporter's raw address here.
CREATE TABLE reporter_privacy_request_scope (
    tenant_id uuid NOT NULL,
    reporter_request_id uuid NOT NULL,
    resource_type text NOT NULL CHECK (resource_type IN ('feedback', 'issue')),
    resource_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, reporter_request_id, resource_type, resource_id),
    CONSTRAINT reporter_privacy_request_scope_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT reporter_privacy_request_scope_tenant_request_fkey
        FOREIGN KEY (tenant_id, reporter_request_id)
        REFERENCES reporter_privacy_requests(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_reporter_privacy_request_scope_request
    ON reporter_privacy_request_scope (tenant_id, reporter_request_id);

ALTER TABLE reporter_privacy_request_scope ENABLE ROW LEVEL SECURITY;
ALTER TABLE reporter_privacy_request_scope FORCE ROW LEVEL SECURITY;
CREATE POLICY reporter_privacy_request_scope_tenant_isolation ON reporter_privacy_request_scope
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON reporter_privacy_request_scope TO fidibeki_app, fidibeki_admin;

CREATE TYPE reporter_privacy_review_status AS ENUM (
    'pending',
    'redacted',
    'deleted',
    'not_personal_data',
    'retained_exception'
);

-- One itemized row per resource still carrying free-form content that automated anonymization
-- cannot safely interpret. field names only: never the flagged text itself, so the audit trail
-- and this queue can never leak the content under review.
CREATE TABLE reporter_privacy_review_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    reporter_request_id uuid NOT NULL,
    privacy_job_id uuid,
    resource_type text NOT NULL CHECK (
        resource_type IN ('feedback', 'issue', 'issue_comment', 'clarification_request', 'legacy_unlinked')
    ),
    resource_id uuid NOT NULL,
    fields text[] NOT NULL CHECK (
        cardinality(fields) > 0
        AND fields <@ ARRAY['description', 'comment', 'body', 'prompt', 'response', 'manual_review_count']::text[]
    ),
    status reporter_privacy_review_status NOT NULL DEFAULT 'pending',
    decision_reason text CHECK (decision_reason IS NULL OR char_length(decision_reason) BETWEEN 1 AND 2000),
    decided_by_member_id uuid,
    decided_at timestamptz,
    disclosed_destinations jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(disclosed_destinations) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reporter_privacy_review_items_tenant_id_fkey
        FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT reporter_privacy_review_items_tenant_request_fkey
        FOREIGN KEY (tenant_id, reporter_request_id)
        REFERENCES reporter_privacy_requests(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT reporter_privacy_review_items_tenant_job_fkey
        FOREIGN KEY (tenant_id, privacy_job_id)
        REFERENCES privacy_jobs(tenant_id, id) ON DELETE SET NULL (privacy_job_id),
    CONSTRAINT reporter_privacy_review_items_tenant_decider_fkey
        FOREIGN KEY (tenant_id, decided_by_member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE SET NULL (decided_by_member_id),
    CONSTRAINT reporter_privacy_review_items_unique_resource
        UNIQUE (tenant_id, reporter_request_id, resource_type, resource_id),
    CONSTRAINT reporter_privacy_review_items_decision_consistent CHECK (
        (status = 'pending' AND decided_at IS NULL AND decision_reason IS NULL)
        OR (status <> 'pending' AND decided_at IS NOT NULL)
    ),
    CONSTRAINT reporter_privacy_review_items_reason_required CHECK (
        status <> 'retained_exception' OR decision_reason IS NOT NULL
    )
);

CREATE INDEX idx_reporter_privacy_review_items_request
    ON reporter_privacy_review_items (tenant_id, reporter_request_id, status);

CREATE TRIGGER reporter_privacy_review_items_set_updated_at
BEFORE UPDATE ON reporter_privacy_review_items
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Decisions are permanent: once a human (or the system, for content that disappeared before
-- review) resolves an item, nothing may rewrite that record. Recalculation always inserts a
-- fresh row for newly detected content instead of reopening a decided one.
CREATE FUNCTION reporter_privacy_review_items_lock_decided() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status <> 'pending' THEN
        RAISE EXCEPTION 'reporter privacy review item % is immutable once decided', OLD.id
            USING ERRCODE = 'raise_exception';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reporter_privacy_review_items_immutable
BEFORE UPDATE ON reporter_privacy_review_items
FOR EACH ROW EXECUTE FUNCTION reporter_privacy_review_items_lock_decided();

ALTER TABLE reporter_privacy_review_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE reporter_privacy_review_items FORCE ROW LEVEL SECURITY;
CREATE POLICY reporter_privacy_review_items_tenant_isolation ON reporter_privacy_review_items
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT USAGE ON TYPE reporter_privacy_review_status TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE ON reporter_privacy_review_items TO fidibeki_app, fidibeki_admin;

-- Added here (unused by any constraint or query in this migration) so the values are committed
-- before migration 000079 relies on them; PostgreSQL forbids using a new enum value in the same
-- transaction that adds it.
ALTER TYPE privacy_job_status ADD VALUE IF NOT EXISTS 'awaiting_manual_review' AFTER 'processing';
ALTER TYPE reporter_privacy_status ADD VALUE IF NOT EXISTS 'awaiting_manual_review' AFTER 'processing';
