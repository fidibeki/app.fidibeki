-- Per-member, per-workspace preferences for optional product notifications, plus the
-- delivery ledger that makes scheduled digests idempotent under retries.
--
-- Mandatory authentication, security, account-deletion, and critical billing mail is
-- deliberately absent from this table: those messages keep their existing delivery paths
-- and are never gated on a member preference. Reporter consent and reporter unsubscribe
-- stay on the feedback/issue rows they already live on.
CREATE TABLE member_notification_preferences (
    tenant_id uuid NOT NULL,
    member_id uuid NOT NULL,
    -- Directly addressed work reaches the member immediately by default.
    assignments text NOT NULL DEFAULT 'immediate'
        CHECK (assignments IN ('immediate', 'daily', 'weekly', 'off')),
    mentions text NOT NULL DEFAULT 'immediate'
        CHECK (mentions IN ('immediate', 'daily', 'weekly', 'off')),
    -- Ambient activity is low-noise by default: silent, or digested.
    comments text NOT NULL DEFAULT 'off'
        CHECK (comments IN ('immediate', 'daily', 'weekly', 'off')),
    issue_changes text NOT NULL DEFAULT 'daily'
        CHECK (issue_changes IN ('immediate', 'daily', 'weekly', 'off')),
    -- New feedback is summarized only. Omitting 'immediate' here is what makes
    -- "one email per feedback submission" unreachable, not just an unset default.
    feedback_summary text NOT NULL DEFAULT 'weekly'
        CHECK (feedback_summary IN ('daily', 'weekly', 'off')),
    digest_hour smallint NOT NULL DEFAULT 8
        CHECK (digest_hour BETWEEN 0 AND 23),
    -- ISO-8601 weekday for the weekly digest: 1 = Monday through 7 = Sunday.
    digest_weekday smallint NOT NULL DEFAULT 1
        CHECK (digest_weekday BETWEEN 1 AND 7),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, member_id),
    CONSTRAINT member_notification_preferences_member_fkey FOREIGN KEY (tenant_id, member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE CASCADE
);

-- One row per member, cadence, and window. The unique window key is the claim: a
-- scheduler retry re-selects the same window and conflicts instead of sending twice,
-- and window_end doubles as the delivery cursor for the next window. A window with
-- nothing in it is never claimed, so an empty digest leaves no row and no send.
CREATE TABLE member_digest_deliveries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    member_id uuid NOT NULL,
    cadence text NOT NULL CHECK (cadence IN ('immediate', 'daily', 'weekly')),
    window_start timestamptz NOT NULL,
    window_end timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'claimed'
        CHECK (status IN ('claimed', 'sent', 'suppressed', 'failed')),
    event_count integer NOT NULL DEFAULT 0 CHECK (event_count >= 0),
    provider_message_id text,
    error_message text CHECK (error_message IS NULL OR char_length(error_message) <= 2000),
    sent_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT member_digest_deliveries_window_check CHECK (window_end > window_start),
    CONSTRAINT member_digest_deliveries_window_key
        UNIQUE (tenant_id, member_id, cadence, window_start),
    CONSTRAINT member_digest_deliveries_sent_consistent CHECK (
        (status = 'sent' AND sent_at IS NOT NULL) OR status <> 'sent'
    ),
    CONSTRAINT member_digest_deliveries_member_fkey FOREIGN KEY (tenant_id, member_id)
        REFERENCES tenant_members(tenant_id, id) ON DELETE CASCADE
);

-- Digest windows scan a workspace's activity by time without knowing the issue,
-- which the existing (tenant_id, issue_id, created_at, id) index cannot serve.
CREATE INDEX idx_issue_activities_tenant_created
    ON issue_activities (tenant_id, created_at, id);

CREATE INDEX idx_member_digest_deliveries_cursor
    ON member_digest_deliveries (tenant_id, member_id, cadence, window_end DESC);
CREATE INDEX idx_member_digest_deliveries_status
    ON member_digest_deliveries (tenant_id, status, created_at DESC);

ALTER TABLE member_notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_notification_preferences FORCE ROW LEVEL SECURITY;
CREATE POLICY member_notification_preferences_tenant_isolation
    ON member_notification_preferences
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE member_digest_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE member_digest_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY member_digest_deliveries_tenant_isolation ON member_digest_deliveries
    FOR ALL TO fidibeki_app
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON member_notification_preferences
    TO fidibeki_app, fidibeki_admin;
GRANT SELECT, INSERT, UPDATE ON member_digest_deliveries TO fidibeki_app, fidibeki_admin;
