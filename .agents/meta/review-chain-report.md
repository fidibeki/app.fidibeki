---
skill: review-chain
version: 1
date: 2026-07-22
status: final
---

# Review Chain Report

**Artifact**: Concurrent tenant/platform refresh-token recovery and migration 000044
**Date**: 2026-07-22
**Rounds**: 2

## Verdict: FIXED

## Issues Found

| # | Severity | Confidence | Location | Problem | Status |
|---|----------|------------|----------|---------|--------|
| 1 | major | 9/10 | tenantmember/store.go, platformadmin/store.go | Family revocation could miss a concurrently inserted successor. | Fixed with stable owner-row serialization and a concurrent race test. |
| 2 | major | 10/10 | tenantmember/store.go, platformadmin/store.go | Grace retries created unlimited independent successor sessions. | Fixed with one HMAC-derived, domain-separated, idempotent successor. |
| 3 | minor | 10/10 | auth integration tests | The original concurrency tests executed sequentially. | Fixed with synchronized concurrent requests and session-count assertions. |
| 4 | high | 9/10 | tenantmember/store.go, platformadmin/store.go | Logout through a rotated parent left its active descendant usable. | Fixed with recursive descendant-chain revocation under the owner lock. |
| 5 | medium | 10/10 | tenantmember/store.go, tenantmember/http.go | Tenant expiry revocation rolled back with the 401 response. | Fixed with an explicit expired-token result committed by middleware. |
| 6 | low | 9/10 | session retry queries | An expired successor could authorize a parent retry at short TTLs. | Fixed with strict successor TTL and exact-boundary checks. |
| 7 | high | 10/10 | railway.json | Railway's 30-second overlap could mix incompatible refresh protocols. | Fixed with zero overlap/draining plus a default-off, two-phase grace gate. |
| 8 | major | 8/10 | migration 000044 | DDL lock acquisition had no bound. | Fixed with a five-second migration lock timeout and rehearsed up/down/up. |
| 9 | minor | 9/10 | auth HTTP handlers | Confirmed reuse had no distinct operational signal. | Fixed with token-free structured warnings after committed revocation. |
| 10 | major | 10/10 | session tables | Revoked session history has no retention cleanup. | Declined as pre-existing; safe cleanup needs persisted per-session expiry and a separate retention design. |

## Input Quality Assessment

| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Rich | Staging reproduction, network outcomes, cookie behavior, and existing security contract were available. |
| Requirements clarity | Precise | Preserve theft detection while tolerating concurrent tabs; production excluded. |
| Upstream artifacts | Fresh | Current plan, runbook, Railway configuration, integration tests, and live staging evidence. |

## Simplifications Applied

One deterministic successor per parent removes retry fan-out and avoids storing plaintext refresh tokens.
Stable member/admin row locks provide a single serialization point without adding a session-family table.

## Changes Made

Added domain-separated HMAC refresh derivation, idempotent retry metadata, owner-row locking, recursive
logout revocation, committed expiry handling, structured reuse logs, a default-off rollout gate, migration
lock bounds, and real concurrency/regression tests for tenant and platform flows.

## Reviewer's Summary

Round one rejected the initial grace implementation because it weakened reuse guarantees under concurrency.
Round two found logout-chain, expiry-commit, exact-TTL, and rolling-deploy gaps; all blocking findings were fixed.

## Resolver's Notes

Revoked-row cleanup was intentionally not bundled into this authentication fix. The current schema lacks a
stable per-session expiry needed to define safe deletion independently of later configuration changes.

---

# Review Chain Report: Widget Funnel Analytics

**Artifact**: Slice 4.1 matched event pipeline, migration 000049, public ingest, tenant summary, and widget identity
**Date**: 2026-07-22
**Rounds**: 2

## Verdict: FIXED

## Issues Found

| # | Severity | Confidence | Location | Problem | Status |
|---|----------|------------|----------|---------|--------|
| 1 | medium | 9/10 | widget/src/loader.ts | Awaiting configuration before rotating page identity could miss rapid intermediate SPA paths. | Fixed by visiting the captured URL synchronously before any await, with a double-navigation E2E regression. |
| 2 | medium | 8/10 | backend/internal/widgetanalytics/store.go | Concurrent reversed batches could lock aggregate rows in opposite order and deadlock. | Fixed with a stable project-row transaction lock and concurrent PostgreSQL regression. |

## Input Quality Assessment

| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Rich | Approved analytics design, widget recurrence/targeting behavior, and product plan were available. |
| Requirements clarity | Precise | Task 1 defines lifecycle, identity, authorization, idempotency, aggregation, and acceptance tests. |
| Upstream artifacts | Fresh | Design and implementation plan were created on 2026-07-22. |

## Simplifications Applied

A project row provides the existing stable serialization boundary, avoiding a new advisory-lock scheme or
lock table. The browser tracker remains independent of delivery so later outbox work can be additive.

## Reviewer's Summary

The second round passed after synchronous page-view rotation and same-project transaction serialization were
added. Tenant isolation, canonical validation, lifecycle uniqueness, and atomic aggregate updates remained sound.

### Slice 4.2 Follow-up

The visibility and first-interaction extension passed an additional fresh-eyes review. The reviewer verified
current-mount impression timing, suppressed/untriggered behavior, meaningful-action deduplication, ordered
delivery, and the backend matched-predecessor constraint without finding further issues.

### Slice 4.3 Transactional Outcomes

**Artifact**: Transactional `submitted` events, explicit `dismissed` events, and widget submission state
**Date**: 2026-07-22
**Rounds**: 2

#### Verdict: FIXED

| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | medium | 10/10 | `ON CONFLICT DO NOTHING` treated an unrelated `event_id` collision as a successful duplicate and could commit feedback without its `submitted` event. | Fixed with globally serialized event IDs, an RLS-safe existence guard, immutable retry comparison, and rollback regressions. |
| 2 | medium | 9/10 | Closing during an in-flight request could emit `dismissed` before the same submission was accepted. | Fixed by disabling and guarding close while submitting, restoring it only after failure. |
| 3 | medium | 10/10 | The first collision fix remained ambiguous when a cross-tenant event ID and a visible local lifecycle duplicate existed together. | Fixed with migration 000050, a minimal `SECURITY DEFINER` existence predicate, an event-ID advisory lock, and a cross-tenant integration regression. |

Exact immutable retries and lifecycle duplicates remain idempotent. Concurrent same-ID attempts produce one
raw event and one aggregate increment. The final backend suite, 78 widget unit tests, typecheck, build, and all
20 Playwright tests pass. The two-round review cap was reached; the final RLS ambiguity was resolved and covered
by PostgreSQL integration tests rather than opening a third review round.

### Slice 4.4 Recoverable Delivery

**Artifact**: Browser analytics outbox, bounded fetch/beacon delivery, public ingest limits, and trusted client IP resolution
**Date**: 2026-07-22
**Rounds**: 2

#### Verdict: PASS

| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | high | 10/10 | Tabs sharing one mutable localStorage snapshot could overwrite each other's enqueues or acknowledgments. | Fixed with immutable v2 per-event keys, project-wide recovery, exact-key ACK removal, and an interleaving regression. |
| 2 | medium | 10/10 | A 20-event batch could exceed the 64 KiB API and browser keepalive/beacon budgets. | Fixed with one shared 60 KiB UTF-8 batch builder plus multibyte and oversized-singleton regressions. |
| 3 | medium | 9/10 | Unconditional trust in `X-Forwarded-For` allowed source-IP rate-limit spoofing. | Fixed with secure-empty `TRUSTED_PROXY_CIDRS`, right-to-left trusted-chain resolution, and centralized use by analytics and feedback. |

The second review passed without further findings. Full backend tests, 88 widget unit tests with coverage,
typecheck, production build, and all 20 Playwright tests pass.

### Slice 4.5 Analytics Workspace

**Artifact**: Bounded tenant analytics API, migration 000051, and Dispatch `/analytics` workspace
**Date**: 2026-07-22
**Rounds**: 2

#### Verdict: FIXED

| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | major | 10/10 | Rule rows used non-unique Vue keys. | Fixed with dimension-specific project, rule, and canonical-page identities. |
| 2 | major | 9/10 | Selected deleted rules and historical pages could disappear when the period changed. | Fixed with indexed selected-history lookups and retained selected options. |
| 3 | major | 10/10 | Page breakdowns/options were unbounded across API memory, JSON, and DOM rendering. | Fixed with deterministic 100/200 limits, totals, and truncation metadata shown in Dispatch. |
| 4 | major | 9/10 | Rule/page filters lacked selective aggregate indexes. | Fixed by migration 000051 and canonical page SHA-256 plus URL filtering. |
| 5 | major | 9/10 | Superseded Nuxt requests did not cancel their backend work. | Fixed by propagating the `useAsyncData` abort signal through `$apiFetch`. |
| 6 | major | 8/10 | Analytics queries had no execution deadline or useful operational diagnostics. | Fixed with a 10-second context deadline, per-tenant concurrency bound, and structured duration/cardinality logs. |
| 7 | major | 10/10 | Rule options scanned and grouped the complete retained history on every request. | Fixed by period-bounding normal history and using the rule index only for the selected out-of-period rule. |
| 8 | major | 10/10 | Ordered `array_agg` retained every row merely to choose the latest rule name. | Fixed with a narrow `DISTINCT ON` latest-name relation joined to count aggregates. |
| 9 | major | 9/10 | Page populations were grouped twice for breakdowns and options. | Fixed with one shared page-total aggregate derived from the range scope. |
| 10 | major | 10/10 | An arbitrary page URL was fabricated as a valid option and returned `200`. | Fixed with all-history tenant/project/rule/page validation and `404` for absent or mismatched filters. |
| 11 | minor | 9/10 | Accepted noncanonical UUID forms could reach PostgreSQL unchanged. | Fixed by normalizing parsed UUIDs before query construction. |

The final suite covers 7/30/90-day boundaries, zero denominators, deleted and dormant rules, historical pages,
cross-tenant and same-tenant filter mismatches, deterministic high-cardinality limits, timeout logging, desktop
and mobile loading/data/empty/error/retry states, and the complete responsive Dispatch journey. Backend tests,
web unit tests, typecheck, local production build, and focused Playwright runs pass. No deployment was performed.

### Slice 4.6 Reconciliation And Retention

**Artifact**: Worker reconciliation, bounded raw/daily retention, migration 000052, and replica-safe cadence
**Date**: 2026-07-22
**Rounds**: 2

#### Verdict: FIXED

| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | major | 10/10 | Retention deleted only one 1,000-row batch per hour, allowing normal event volume to grow the backlog. | Fixed with independently bounded multi-batch drains and explicit budget-exhaustion telemetry. |
| 2 | major | 9/10 | Reconciliation and cleanup had no execution deadline and a raw cleanup failure prevented daily cleanup. | Fixed with separate two-minute phase deadlines, independent cleanup stages, joined errors, and continuation tests. |
| 3 | major | 10/10 | Multiple worker replicas could reconcile concurrently and overwrite inline aggregate updates. | Fixed with a global advisory lease, project-row serialization shared with both ingestion paths, and deterministic concurrent PostgreSQL coverage. |
| 4 | major | 10/10 | A staggered replica could repeat the full cadence immediately after the first replica released its advisory lock. | Fixed with a persisted admin-only singleton cadence marker in migration 000052. |
| 5 | major | 9/10 | Global candidate discovery and long project/day transactions could scan broadly and block foreground ingestion. | Fixed by enumerating project/day scopes, reconciling indexed page chunks, and enforcing a five-second chunk deadline. |
| 6 | major | 10/10 | Session time zones could shift daily retention boundaries. | Fixed by passing explicit UTC calendar dates, verified with a non-UTC PostgreSQL session and exact-boundary tests. |
| 7 | minor | 9/10 | Rollback cleanup could outlive a cancelled request, and partial cleanup logs could claim completion. | Fixed with bounded rollback/connection disposal and accurate partial/budget log fields. |

Correctness and security reviewers approved the second round. The performance reviewer identified the final
candidate-scan, lock-duration, persisted-cadence, rollback, and observability gaps; these were resolved within
the two-round cap and covered by the final test run. Full backend tests and the widget analytics race suite pass.
No deployment was performed.

### Task 7 Web SSR And API Proxy Hardening

**Artifact**: Nuxt SSR authentication fallback, API proxy failure handling, and trusted target construction
**Date**: 2026-07-22
**Rounds**: 2

#### Verdict: FIXED

| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | critical | 10/10 | An HTTP absolute-form request target could replace the configured upstream while the proxy still forwarded its internal authentication secret. | Fixed by rebuilding the target solely from the validated configured HTTP(S) origin plus the parsed request pathname and query, with a regression covering an attacker-controlled absolute URL. |
| 2 | major | 10/10 | A malformed truthy upstream was parsed outside failure handling and could enter Nitro's SSR error renderer. | Fixed with validated target construction inside the structured `503` boundary. |
| 3 | major | 9/10 | Missing or unreachable API configuration during browser SSR could repeatedly enter the renderer and exhaust the Node process. | Fixed with structured proxy responses and a server-side authentication redirect when no upstream is configured. |

Both missing and unreachable upstream cases were exercised through a real browser against the production build
with a 128 MB heap; each redirected to login and left the process healthy. Web unit tests, typecheck, build, and
the full applicable desktop/mobile Playwright suite pass. The two-round cap was reached; the final absolute-form
finding was resolved with a focused regression and no production deployment was performed.
