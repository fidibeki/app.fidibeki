# Retention Cleanup Health Implementation Plan

**Date:** 2026-07-24
**Design:** `docs/superpowers/specs/2026-07-24-retention-cleanup-health-design.md`
**Issue:** #62

## 1. Persist Current Operation State

- Add migration 105 for the constrained `retention_maintenance_status` current-state table.
- Seed the tenant and platform-admin magic-link operations.
- Grant bounded read/update access without delete.
- Add migration lifecycle and privilege tests.
- Register the table in the executable retention inventory and update its table-count assertion.

## 2. Add The Retention Health Domain

- Create a focused `backend/internal/retentionhealth` package.
- Define closed operation/result/health enums and a generic failure code.
- Implement atomic run recording and deterministic read-time state derivation.
- Add unit and PostgreSQL integration tests for validation, counters, precedence, and boundaries.

## 3. Instrument Enforced Magic-Link Cleaners

- Add the shared recorder dependency to tenant-member and platform-admin maintainers.
- Record success, failure, processed count, and budget exhaustion after every sweep.
- Treat recorder failure as an observable maintainer failure without undoing completed cleanup.
- Wire the recorder through the existing admin database pool.
- Extend unit coverage for both maintainers.

## 4. Expose Additive Product Health

- Add health to the existing `/v1/privacy/retention` response.
- Keep the current permission and response fields unchanged.
- Add integration coverage for authorized human/API-key reads and health contents.
- Extend frontend API types.

## 5. Render The Backoffice Health Band

- Add responsive English and Portuguese health summary and operation rows to Data & privacy.
- Show derived status, alert reason, last success, next deadline, counts, and failures.
- Update the Playwright API fixture and desktop/mobile assertions.
- Run frontend typecheck and focused browser tests.

## 6. Reconcile Documentation And Validate

- Update `docs/privacy-data-map.md` and `plans/fidibeki.plan.md`.
- Run focused package and migration tests.
- Run `go test ./... -count=1`, `go vet ./...`, `go build ./...`, `make format-check`,
  `make governance-check`, frontend typecheck, focused Playwright tests, and `git diff --check`.
- Commit the implementation separately from this design/plan commit.
