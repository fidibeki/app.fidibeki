# Retention health coverage for all enforced cleaners (issue #62 slice)

Status: designed 2026-07-24; decision-independent (adds no deletion authority, no retention
period, and no legal claim). Implements the #62 acceptance items "Show effective periods, next
cleanup, held data, failures, and last successful reconciliation in the backoffice" and the
telemetry half of "Add metrics and alerts for overdue cleanup" for the cleaners that already
exist.

## Problem

`retention_maintenance_status` (migration 105) instruments only the two magic-link cleaners.
The other enforced cleaners — attachments, export archives, reporter-request expiry, replay
sessions, widget funnel events/daily, abuse diagnostics, AI budget ledgers, and identity asset
deletions — emit logs only. The Data & privacy surface therefore shows live health for two
operations and nothing for the rest, and `docs/privacy-data-map.md` explicitly documents the gap
("Uninstrumented retention categories are not represented as healthy"). An operator cannot see an
overdue attachments sweep without reading provider logs.

## Approach

Reuse the existing `retentionhealth` recorder contract unchanged (bounded current-state row per
operation, no run history, no tenant/person/credential identifiers, generic `cleanup_failed`
code, recorder failure surfaced without rolling back completed cleanup, 15-minute grace).

1. Migration 109 widens the `retention_maintenance_status` operation CHECK constraint and seeds
   one `not_observed` row per new operation with its schedule metadata:

   | operation | maintainer | interval |
   | --- | --- | --- |
   | `attachments` | privacy.RetentionMaintainer | 24h |
   | `privacy_export_archives` | privacy.RetentionMaintainer | 24h |
   | `reporter_privacy_requests` | privacy.RetentionMaintainer | 24h |
   | `replay_sessions` | replay.Cleaner | 24h |
   | `widget_funnel_events` | widgetanalytics.Maintainer | 1h |
   | `widget_funnel_daily` | widgetanalytics.Maintainer | 1h |
   | `abuse_diagnostics` | abuse.Maintainer | 1h |
   | `ai_budget_ledgers` | aiprocessing.BudgetMaintainer | 24h |
   | `identity_asset_deletions` | identity.CleanupMaintainer | 1m |

   The down migration restores the two-operation constraint and removes the seeded rows.

2. Each maintainer records every sweep through the shared recorder exactly like the magic-link
   maintainers: success with processed counts, failure with the generic code, budget exhaustion
   flagged. Maintainers that run several phases record each phase's operation independently so a
   failing phase cannot hide behind a succeeding one.

3. No read-path change: `retentionhealth.Read` already derives
   `healthy|attention|failed|overdue|not_observed` for every row, and `GET /v1/privacy/retention`
   already returns the full report. The Data & privacy surface needs only the new operation
   labels in `admin.en.ts`/`admin.pt.ts`.

4. The executable retention registry rows for the covered categories gain the
   `retention_maintenance_status` resource reference where missing, keeping the
   registry-validation gate truthful about instrumentation.

## Explicitly out of scope (separate slices)

- Hold-aware physical sweeps (wiring `privacy_case_legal_holds` into attachment/replay cleanup)
  — needs its own design for case-scope-to-row mapping.
- Any new cleaner or retention period (`decision_required` categories stay unswept until the
  founder decision package is answered).
- External alert delivery; the health ledger and UI remain the alert surface (metrics export can
  follow once a telemetry provider decision exists).

## Tests

- Migration 109 up/down/up, constraint boundaries, seeded schedule rows, grants unchanged.
- Per maintainer: a PostgreSQL integration test that runs one sweep and asserts the recorder row
  advanced (last attempt/success, processed counts), and one that forces a failure and asserts
  `consecutive_failures`/`failure_code` without blocking the sweep's idempotent rerun.
- Health derivation: new operations appear `not_observed` before first run and `overdue` after
  the grace window, mirroring the existing magic-link coverage.
- Web: retention health panel renders the new operation labels in both locales (typecheck +
  existing Playwright coverage extended with the new keys).
