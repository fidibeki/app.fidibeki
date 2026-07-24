# Retention Cleanup Health Design

**Date:** 2026-07-24
**Status:** Approved through the founder's standing approval for recommended plans
**Issue:** #62
**Target:** Local implementation and staging validation only

## Goal

Make the two enforced magic-link retention cleaners operationally visible without storing personal
data or inventing a new retention period. Workspace operators must be able to see whether the
cleaners have run successfully, whether a run failed or exhausted its bounded work budget, when the
next run is expected, and when an operation is overdue.

This is a vertical slice for the existing `auth_magic_links` policy. It does not claim that every
retention category is now observable and it does not close #62.

## Audit

`tenantmember.Maintainer` and `platformadmin.Maintainer` already run immediately at process start and
then hourly. Each sweep is bounded, concurrency-safe, and logs the deleted count, failures, and
budget exhaustion. The logs are not queryable by the product, do not survive as a stable current
state, and cannot support a deterministic overdue calculation.

`GET /v1/privacy/retention` already returns the executable retention registry and the Data & privacy
settings surface renders its lifecycle status. It does not return operational cleanup health.

The repository has no general metrics backend. Adding provider-specific metrics or deployment
configuration would be infrastructure work and is outside the staging-only boundary.

## Considered Approaches

### Structured logs only

Extend the current log fields and rely on the runtime log provider for alerts. This is small, but it
does not provide restart-stable product state, requires infrastructure configuration, and cannot
power the existing retention settings surface.

### Append-only run history

Persist every cleanup run and aggregate it for the product. This offers richer history, but creates
a new unbounded operational dataset that itself needs an approved retention period and cleaner.

### Current-state operation ledger

Persist exactly one mutable, aggregate row per registered cleanup operation. Each run replaces the
last-run fields and increments bounded counters. Alert state is derived at read time from the
registered schedule, grace window, last success, last result, and budget flag.

This is the selected approach. It survives restarts and powers the product without creating a new
history-retention problem.

## Data Model

Migration 105 creates `retention_maintenance_status` with one seeded row for each existing
magic-link cleaner:

- `tenant_magic_links`;
- `platform_admin_magic_links`.

Each row stores only:

- a bounded operation key and retention category;
- registration time, expected interval, and alert grace period;
- last attempt and last successful attempt;
- last result (`succeeded` or `failed`);
- last processed count and budget-exhausted flag;
- total runs, failures, processed rows, and consecutive failures;
- a bounded generic failure code;
- update time.

There are no tenant IDs, member/admin IDs, credential identifiers, emails, tokens, error messages,
IP addresses, user agents, or provider payloads. The row is current aggregate state, not run
history. `fidibeki_app` and `fidibeki_admin` may read and update the fixed rows but may not delete
them. A database constraint prevents arbitrary operation keys, categories, statuses, negative
counters, or unsafe failure text.

The table is registered under `auth_magic_links`, because it is implementation evidence for that
enforced policy rather than a new personal-data lifecycle category. The executable inventory count
increases from 71 to 72 PostgreSQL tables.

## Runtime Contract

A small `retentionhealth` package owns the operation keys, result model, recorder, reader, health
derivation, and summary. The two maintainers depend on its narrow `Recorder` interface.

After every bounded sweep, including a failed or budget-exhausted sweep, the maintainer records a
result. The database receives only a success/failure enum and a generic `cleanup_failed` code. Raw
errors continue to be available only to the structured application log.

Recording is part of the maintainer's observable contract. If cleanup succeeds but recording fails,
`MaintainOnce` returns an error so operations receives a visible failure instead of silently losing
evidence. The cleanup is not rolled back and remains idempotent on the next scheduled run.

The database update is atomic and:

- advances `last_success_at` only for successful runs;
- resets consecutive failures on success;
- increments failures on a failed run;
- accumulates only non-negative processed counts;
- replaces the prior budget-exhausted flag with the latest run's value.

## Health Derivation

The read model exposes one of five states:

- `healthy`: the latest run succeeded within its interval plus grace window and did not exhaust its
  work budget;
- `attention`: the latest run succeeded but exhausted its bounded work budget;
- `failed`: the latest recorded run failed;
- `overdue`: no successful run exists by the first expected deadline, or the last success is older
  than the expected interval plus grace window;
- `not_observed`: the operation was registered recently enough that its first run is not yet due.

`overdue` takes precedence over a stale historical failure, while a current failure takes precedence
before the deadline. The response includes the derived alert reason, last attempt/success, next
expected deadline, last and total processed counts, and consecutive failures.

## API And Product

`GET /v1/privacy/retention` remains backward compatible and adds a sibling `health` object beside
`retention`. The route keeps the existing `tenant:read` authorization. The health data is global,
aggregate, and contains no tenant or personal data.

The Data & privacy settings surface adds a compact operational-health band immediately above the
retention policy table. It shows:

- summary counts by state;
- operation name and mapped policy;
- current state and alert reason;
- last success and next expected run;
- last/total processed rows and consecutive failures.

The UI uses the existing table/band visual language, stays responsive, and includes English and
Portuguese copy. It does not claim that uninstrumented categories are healthy.

## Failure And Security Behavior

- Missing or invalid seeded rows fail the health read rather than fabricate health.
- A recorder update for an unknown operation fails.
- Raw cleanup errors never enter the status table or API.
- Counters and operation metadata are database-constrained.
- The endpoint remains `Cache-Control: no-store` through the existing API middleware behavior.
- No production deployment, monitoring-provider configuration, or external alert is added.

## Tests

- Unit tests cover state precedence, first-run grace, overdue boundaries, failure, budget
  exhaustion, healthy recovery, and summary counts.
- PostgreSQL tests cover seeded rows, grants, success/failure updates, counter behavior, unknown
  operations, and migration 105 down/up.
- Both maintainers cover successful, failed, budget-exhausted, and recorder-failure paths without
  leaking raw errors into the recorded result.
- Privacy HTTP integration covers authorization and the additive health response.
- Web typecheck and responsive Playwright coverage verify English and Portuguese rendering.
- Repository gates include backend tests, vet, build, formatting, governance, frontend typecheck,
  and browser tests.

## Remaining Scope

Later #62 slices may instrument the other enforced cleaners, add tenant-configurable shorter
retention, implement approved holds and lifecycle policies, minimize authentication metadata, and
configure external telemetry retention. This slice does not choose any legal retention period.
