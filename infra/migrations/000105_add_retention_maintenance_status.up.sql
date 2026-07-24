-- Current, aggregate operational health for enforced retention cleaners.
-- This is deliberately not an append-only run history: one bounded row per
-- code-owned operation avoids creating another unbounded retention category.

CREATE TABLE retention_maintenance_status (
    operation_key text PRIMARY KEY
        CHECK (operation_key IN (
            'tenant_magic_links',
            'platform_admin_magic_links'
        )),
    category text NOT NULL CHECK (category = 'auth_magic_links'),
    registered_at timestamptz NOT NULL DEFAULT now(),
    expected_interval_seconds integer NOT NULL
        CHECK (expected_interval_seconds BETWEEN 60 AND 604800),
    alert_grace_seconds integer NOT NULL
        CHECK (alert_grace_seconds BETWEEN 0 AND 86400),
    last_attempt_at timestamptz,
    last_success_at timestamptz,
    last_result text CHECK (last_result IN ('succeeded', 'failed')),
    last_processed bigint NOT NULL DEFAULT 0 CHECK (last_processed >= 0),
    last_budget_exhausted boolean NOT NULL DEFAULT false,
    consecutive_failures integer NOT NULL DEFAULT 0
        CHECK (consecutive_failures >= 0),
    total_runs bigint NOT NULL DEFAULT 0 CHECK (total_runs >= 0),
    total_failures bigint NOT NULL DEFAULT 0
        CHECK (total_failures >= 0 AND total_failures <= total_runs),
    total_processed bigint NOT NULL DEFAULT 0 CHECK (total_processed >= 0),
    failure_code text CHECK (failure_code IN ('cleanup_failed')),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT retention_maintenance_status_initial_state CHECK (
        (
            last_result IS NULL
            AND last_attempt_at IS NULL
            AND last_success_at IS NULL
            AND last_processed = 0
            AND NOT last_budget_exhausted
            AND consecutive_failures = 0
            AND total_runs = 0
            AND total_failures = 0
            AND total_processed = 0
            AND failure_code IS NULL
        )
        OR (
            last_result = 'succeeded'
            AND last_attempt_at IS NOT NULL
            AND last_success_at = last_attempt_at
            AND consecutive_failures = 0
            AND failure_code IS NULL
            AND total_runs > 0
        )
        OR (
            last_result = 'failed'
            AND last_attempt_at IS NOT NULL
            AND (last_success_at IS NULL OR last_success_at <= last_attempt_at)
            AND consecutive_failures > 0
            AND failure_code = 'cleanup_failed'
            AND total_runs > 0
            AND total_failures > 0
        )
    ),
    CONSTRAINT retention_maintenance_status_timestamps CHECK (
        registered_at <= updated_at
        AND (last_attempt_at IS NULL OR last_attempt_at <= updated_at)
    )
);

INSERT INTO retention_maintenance_status (
    operation_key,
    category,
    expected_interval_seconds,
    alert_grace_seconds
) VALUES
    ('tenant_magic_links', 'auth_magic_links', 3600, 900),
    ('platform_admin_magic_links', 'auth_magic_links', 3600, 900);

REVOKE ALL PRIVILEGES ON retention_maintenance_status
    FROM PUBLIC, fidibeki_app, fidibeki_admin;
GRANT SELECT ON retention_maintenance_status TO fidibeki_app, fidibeki_admin;
GRANT UPDATE (
    last_attempt_at,
    last_success_at,
    last_result,
    last_processed,
    last_budget_exhausted,
    consecutive_failures,
    total_runs,
    total_failures,
    total_processed,
    failure_code,
    updated_at
) ON retention_maintenance_status TO fidibeki_admin;
