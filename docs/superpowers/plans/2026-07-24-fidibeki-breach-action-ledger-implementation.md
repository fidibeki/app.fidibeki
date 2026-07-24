# Breach Containment And Corrective Action Ledger Implementation Plan

> **Scope mode:** LOCKED
>
> **Design:** `docs/superpowers/specs/2026-07-24-breach-containment-action-ledger-design.md`
>
> **Boundary:** staging-only product work. No deployment, provider call, notification,
> tenant-facing route, or operational credential/session action.

## Shared Context

- The existing breach register is platform-admin-only and append-only.
- Action state is derived from complete immutable revisions, not a mutable projection.
- Nine code-owned containment controls are created atomically with every incident and backfilled
  for existing incidents.
- Post-incident actions require a valid platform-admin owner and a future due date.
- Updates require `expected_revision`; retries are idempotent and concurrent stale updates conflict.
- Existing `platform:breach:read` and `platform:breach:append` permissions remain the boundary.
- Incident detail gains additive `actions` and `action_events` fields.
- The new table remains `decision_required` in the executable retention registry.

## Task 1: Encode The Append-Only Storage Contract

**Depends on:** None

**Outcome:** Migration `101` creates the action enums/table, constraints, grants, indexes, trigger,
legacy backfill, and a reversible down migration.

**Why:** Every later layer depends on the database enforcing the same invariants as the API.

**Acceptance:** The focused migration test proves up/down/up, nine backfilled controls, transition
constraints, append-only behavior, and the exact `fidibeki_admin`/`fidibeki_app` grants.

**Autonomy:** AFK

## Task 2: Implement Domain Contracts And Validation

**Depends on:** Task 1

**Outcome:** Go types, the canonical containment catalog, request normalization, transition rules,
and fingerprints encode the approved action lifecycle.

**Why:** Store and HTTP code need one deterministic definition of valid actions and revisions.

**Acceptance:** `go test ./internal/breachregister -run 'Test.*Action'` passes model tests covering
all enum, bounds, ownership, due-date, completion-evidence, not-applicable, and reopen rules.

**Autonomy:** AFK

## Task 3: Add Transactional Action Persistence

**Depends on:** Tasks 1 and 2

**Outcome:** Incident creation seeds the checklist atomically; action create/update methods enforce
owner existence, idempotent replay, optimistic revision conflicts, and latest/history reads.

**Why:** This is the operational core and the highest concurrency/idempotency risk in the slice.

**Acceptance:** The breach-register PostgreSQL integration test passes checklist creation,
idempotent create/update, changed-key conflict, stale and concurrent revision conflict, current
projection/history ordering, unknown boundaries, and atomic rollback cases.

**Autonomy:** AFK

## Task 4: Expose The Additive Platform API

**Depends on:** Task 3

**Outcome:** Platform-admin routes create post-incident actions, append revisions, and return
current actions plus immutable events on incident detail.

**Why:** Operators need a tested application boundary without exposing the register to tenants.

**Acceptance:** HTTP tests pass for successful JSON contracts, malformed IDs/bodies, error mapping,
platform audience rejection, read/append permission separation, and additive incident detail.

**Autonomy:** AFK

## Task 5: Reconcile Privacy Coverage And Product Records

**Depends on:** Tasks 1 through 4

**Outcome:** The executable retention registry accounts for all 67 tables, the privacy data map
documents action records accurately, and the main product plan records only this completed `#64`
slice while preserving its remaining work.

**Why:** A new breach table must not create an undocumented or falsely enforced retention claim.

**Acceptance:** Privacy registry tests prove exact migration-table coverage with
`breach_action_events` in the `breach_register` decision-required category.

**Autonomy:** AFK

## Task 6: Run The Quality Gate And Commit

**Depends on:** Tasks 1 through 5

**Outcome:** The bounded vertical is formatted, fully verified, and recorded in one implementation
commit with no deployment or unrelated changes.

**Why:** The ledger changes shared database and API contracts and needs repository-wide regression.

**Acceptance:** `go test ./...`, `go vet ./...`, and `go build ./...` pass from `backend`, and the
worktree is clean apart from the user-owned `.superpowers/` directory.

**Autonomy:** AFK

## Critical Gate Review

- Each task has one concrete acceptance command or test result.
- Dependencies are ordered and contain no undefined external setup.
- The migration and transactional concurrency risks are front-loaded.
- Every task is deterministic and AFK under the founder's standing approval.
- The slice stops at an auditable record; it never executes containment or sends a notification.
