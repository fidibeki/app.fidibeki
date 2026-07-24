# Platform Admin Magic-Link Retention Implementation Plan

**Issue:** #62
**Design:** `docs/superpowers/specs/2026-07-24-platform-admin-magic-link-retention-design.md`
**Delivery:** One isolated implementation commit, no push, PR, merge, deployment, or production work

## Task 1: Add the global cleanup-evidence boundary

Files:

- `infra/migrations/000102_add_platform_magic_link_retention.up.sql`
- `infra/migrations/000102_add_platform_magic_link_retention.down.sql`
- `backend/internal/db/migrations_test.go`

Steps:

1. Add the ordered expiry index and minimized `platform_security_events` table.
2. Grant only `SELECT, INSERT` to `fidibeki_admin`; grant nothing to `fidibeki_app`.
3. Assert schema constraints, privilege boundaries, append-only runtime grants, and up/down/up replay.
4. Keep existing link timestamps untouched.

## Task 2: Implement atomic bounded cleanup

Files:

- `backend/internal/platformadmin/maintenance_store.go`
- `backend/internal/platformadmin/maintenance_integration_test.go`

Steps:

1. Start with failing integration tests for boundary time, consumed/unconsumed rows, minimization,
   idempotency, batching, concurrency, and rollback when audit insertion fails.
2. Implement `ExpiredMagicLinks` as one candidate/delete/event CTE using
   `FOR UPDATE SKIP LOCKED`.
3. Confirm failures preserve the original link for retry.

## Task 3: Add and wire the maintainer

Files:

- `backend/internal/platformadmin/maintenance.go`
- `backend/internal/platformadmin/maintenance_test.go`
- `backend/cmd/fidibeki-api/main.go`

Steps:

1. Implement the bounded drain with the existing tenant maintainer defaults and injectable clock.
2. Cover drain, maximum budget, cancellation, timeout, error, and non-content logging behavior.
3. Wire an hourly worker using the admin database pool.

## Task 4: Reconcile executable policy and documentation

Files:

- `backend/internal/privacy/retention_registry.go`
- `backend/internal/privacy/retention_test.go`
- `docs/privacy-data-map.md`
- `plans/fidibeki.plan.md`

Steps:

1. Move `platform_admin_magic_links` into enforced `auth_magic_links`.
2. Add `platform_security_events` to unresolved authentication/security evidence.
3. Update exact coverage to 68 tables while retaining 27 lifecycle categories.
4. Record this tracer as complete without marking #62 complete.

## Task 5: Validate and hand off

Run:

```bash
cd backend && go test ./internal/platformadmin ./internal/db ./internal/privacy -count=1
cd backend && go test ./... -count=1
make format-check
make lint
git diff --check
```

Review the complete diff for accidental content retention, grant escalation, unbounded work, false
policy claims, or production changes. Commit the implementation and provide the commit SHA, test
results, changed files, known limitations, and integration notes to the primary agent.
