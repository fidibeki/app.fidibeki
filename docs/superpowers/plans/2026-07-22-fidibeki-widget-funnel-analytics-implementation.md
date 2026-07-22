# Fidibeki Widget Funnel Analytics Implementation Plan

**Design:** `docs/superpowers/specs/2026-07-22-fidibeki-widget-funnel-analytics-design.md`  
**Scope:** Product Slice 4  
**Deployment:** Staging only; production remains untouched

## Progress

- Tasks 1-5 are complete and locally verified.
- Task 6, reconciliation and retention, is in progress.
- Task 7 staging validation remains pending. No deployment occurs before Task 6 is complete, and production
  remains out of scope until explicitly authorized.

## Architectural Decisions

- The funnel contains `matched`, `impression`, `interaction`, `submitted`, and `dismissed`.
  `widget/connected` remains an installation diagnostic and never contributes to analytics.
- The winning rule emits matched before recurrence suppression; impression requires visible UI;
  interaction requires the first meaningful action; submitted is created only by accepted feedback;
  dismissed requires an explicit pre-submission close.
- Anonymous visitor, page-view, and event UUIDs are random. Page views use the same canonical origin
  and pathname contract as targeting and recurrence and never include query parameters or fragments.
- Migration 49 creates immutable `widget_funnel_events` plus idempotent UTC
  `widget_funnel_daily` aggregates. Rule identity/name are historical snapshots so rule deletion does
  not erase analytics.
- The public batch route is `POST /v1/widget/events`; the tenant read route is
  `GET /v1/analytics/widget`. Both reuse existing origin, tenant, transaction, and RBAC boundaries.
- Non-submission events use a bounded browser outbox with fetch batching and page-hide beacon flush.
  Submitted analytics context travels inside the feedback request and is persisted atomically.
- Raw events live for 90 days and daily aggregates for 730 days. The worker reconciles recent days
  before bounded retention cleanup.
- Dispatch receives a dedicated `/analytics` page. Overview and all current dashboard contracts stay
  unchanged.
- Each task begins with a focused failing public-interface test. Production is never deployed or
  modified.

## Task 1: Trace Targeting Reach End To End

**Depends on:** None

**Outcome:** One winning widget rule emits one anonymous `matched` event per canonical page view,
the public endpoint stores it idempotently, its daily count is immediately queryable, and repeated
configuration/navigation evaluation cannot double count it.

**Why:** This establishes the complete client-to-database-to-tenant-read spine before adding later
funnel stages.

**Acceptance:** Migration integration tests prove schema constraints, grants, RLS, project cascade,
historical rule snapshot behavior, and clean up/down/up. Backend HTTP tests prove public-key/origin,
rule, canonical-page, UUID, timestamp, tenant, and duplicate validation. Widget tests prove visitor
and page-view identity, query/hash/trailing-slash equivalence, distinct-path rotation, winning-rule
selection, matched-before-suppression, and repeated-evaluation deduplication. An authenticated
analytics API request returns the exact matched count.

## Task 2: Measure Real Visibility And First Interaction

**Depends on:** Task 1

**Outcome:** A form emits impression only after visible mount and interaction only on its first
meaningful action, while suppressed, untriggered, passive, and repeatedly evaluated experiences do
not inflate either stage.

**Why:** Interaction and submit rates are trustworthy only if their visibility denominator reflects
what a visitor could actually use.

**Acceptance:** Widget unit and browser tests cover every trigger type, suppressed rules, visible
mount, rating, text, attachment, submission attempt, passive focus/pointer activity, and exactly-once
interaction. Backend ingestion and daily aggregate tests observe the correct stage counts and reject
out-of-order identities that have no matched page view.

## Task 3: Complete Dismissed And Atomic Submitted Paths

**Depends on:** Tasks 1-2

**Outcome:** Explicit pre-submission close emits dismissed, and accepted feedback persists submitted
with the feedback in one transaction using the same anonymous funnel context.

**Why:** This completes the business funnel and anchors its final stage to the authoritative backend
acceptance boundary.

**Acceptance:** Feedback HTTP/store integration tests prove additive legacy compatibility, analytics
context validation, one submitted event for a new feedback, no duplicate event for an idempotent
retry, and complete rollback on persistence failure. Widget browser tests prove close without
interaction, interacted close, failed submission, successful submission, attachment partial
success, and no post-submit dismissal.

## Task 4: Make Browser Delivery Bounded And Recoverable

**Depends on:** Tasks 1-3

**Outcome:** Non-submission events share a 100-item/24-hour browser outbox, flush in batches of at
most 20, use keepalive fetch normally and sendBeacon on page hide, retry transient failures, and
remain invisible to host-page behavior.

**Why:** Lifecycle semantics are insufficient if navigation routinely drops events or retries create
duplicates.

**Acceptance:** Deterministic timer/storage tests cover batch thresholds, caps, expiry, acknowledgments,
duplicate delivery, beacon fallback, malformed/unavailable storage, and transient/permanent errors.
Public endpoint tests enforce a 64 KiB body, 1-20 events, all-or-none validation, 300 event/minute
limiting, and accepted-versus-duplicate response IDs.

## Task 5: Deliver The Dispatch Analytics Workspace

**Depends on:** Tasks 1-4

**Outcome:** `/analytics` shows correct metric totals and rates, a daily trend, and Projects/Rules/
Pages breakdown tabs with period, project, rule, and canonical-page filters.

**Why:** The slice creates product value only when an operator can compare reach, engagement, and
completion without querying storage.

**Acceptance:** Tenant API tests prove 7/30/90 UTC calendar windows, filter isolation, deleted-rule
snapshots, stable sorting, same-snapshot counts/rates, and `null` zero-denominator rates. Web unit and
Playwright tests cover loading, error, empty, filter, tab, and data states on desktop/mobile. Visual
inspection confirms stable metrics, no overlap, no nested cards, and Dispatch consistency.

## Task 6: Reconcile And Retain Analytics Safely

**Depends on:** Tasks 1-5

**Outcome:** The worker repairs the latest three UTC days from raw events, deletes raw data older
than 90 days and aggregates older than 730 days in bounded batches, and exposes failures through
existing structured logs without stopping other worker pools.

**Why:** Inline aggregates need an independent repair path, and anonymous raw data must not live
forever.

**Acceptance:** Store/worker tests corrupt a daily row, reconcile it exactly, preserve older valid
aggregates, delete only expired records, respect tenant/project cascades, resume after partial work,
and prove cleanup runs after reconciliation. Worker shutdown and unrelated queues remain healthy.

## Task 7: Regress And Validate The Complete Funnel In Staging

**Depends on:** Tasks 1-6

**Outcome:** The real staging widget produces a reversible funnel that appears correctly in the
staging Analytics page, with no residual QA data and no production change.

**Why:** Browser timing, origin authorization, batching, database aggregation, and Dispatch must
agree across deployed services.

**Acceptance:** Full backend, widget, and web unit/type/build/E2E suites pass. Migration up/down/up
ends clean at version 51. Staging deploys API/migration and worker, then widget, then web. A controlled
QA journey covers matched-but-suppressed, visible, interacted, dismissed, submitted, equivalent
query/hash/trailing-slash URLs, and a distinct path; it verifies API/dashboard counts, restores the
rule/domains, deletes smoke raw/aggregate rows, and confirms every staging healthcheck. Production
services and configuration remain untouched.
