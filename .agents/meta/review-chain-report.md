---
skill: review-chain
version: 1
date: 2026-07-21
status: final
---

# Review Chain Report

**Artifact**: Fidibeki public signup, first-project activation, widget connection signal, and staging runtime configuration
**Date**: 2026-07-21
**Rounds**: 2

## Verdict: FIXED

## Issues Found
| # | Severity | Confidence | Location | Problem | Status |
|---|----------|------------|----------|---------|--------|
| 1 | medium | 10/10 | `backend/internal/widgetapi/http.go` | Any authorized configuration fetch could report a connection without a matching rule. | Fixed |
| 2 | medium | 9/10 | `web/components/TurnstileWidget.client.vue` | A failed Turnstile script load cached a rejected promise and could not recover. | Fixed |
| 3 | medium | 9/10 | `web/pages/auth/verify.vue`, `web/middleware/auth.global.ts` | Activation routing omitted the widget-rule permission and allowed an incomplete flow. | Fixed |
| 4 | high | 10/10 | `widget/src/loader.ts`, `widget/src/config.ts` | Sending the full page URL for connection detection could expose sensitive query or fragment data. | Fixed |
| 5 | high | 10/10 | `web/nuxt.config.ts` | The public base domain could not be overridden by Railway at runtime. | Fixed |
| 6 | medium | 10/10 | `backend/internal/widgetapi/http.go`, `widget/src/config.ts` | Go and JavaScript regular-expression engines could disagree during duplicated rule matching. | Fixed |
| 7 | medium | 9/10 | `backend/internal/widgetapi/http.go` | A telemetry write failure could prevent an otherwise valid widget configuration from loading. | Fixed |
| 8 | medium | 10/10 | `web/pages/signup.vue` | Server-side plan loading could prevent the signup document from rendering when the API upstream was unavailable. | Fixed |
| 9 | medium | 10/10 | `web/playwright.config.ts` | CI started Nuxt dev over production build artifacts, causing unresolved manifest imports and random navigation stalls. | Fixed |

## Input Quality Assessment
| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Rich | Approved activation design, Dispatch UI direction, existing tenant/auth/widget contracts, and staging topology were available. |
| Requirements clarity | Precise | The approved design specifies routing, permissions, recovery, privacy, polling, and test acceptance criteria. |
| Upstream artifacts | Fresh | The activation design and implementation were both created and verified on 2026-07-21. |

## Simplifications Applied
Connection detection now uses one client-side rule resolver followed by a minimal, origin-authorized rule-ID acknowledgement. The backend validates that the acknowledged rule is in the enabled public configuration, eliminating duplicated regex evaluation and page-URL transmission. Activation permission checks are centralized in one helper.

## Changes Made
Added recoverable Turnstile loading, complete activation permission enforcement, privacy-safe best-effort widget activity reporting, separate private/public Nuxt base-domain variables, and client-side plan loading with an explicit retry state. Added focused backend, widget, web unit, and browser coverage for each correction.

## Reviewer's Summary
The platform proxy allowlist, plan catalog, migration, polling limits, authorized-origin ordering, and activation UI were sound. Two review rounds and CI identified nine concrete issues; all were resolved and the final backend, web, widget, browser, production-build, and database-backed checks pass.

## Resolver's Notes
No findings were declined. Telemetry failures are logged but intentionally do not affect widget availability. Production deployment remains deferred by user direction.

---

# AI Failure Diagnostics Review

**Artifact**: `backend/internal/ai/processor.go` failure diagnostics and tests
**Date**: 2026-07-21
**Rounds**: 2

## Verdict: FIXED

## Issues Found
| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | high | 9/10 | Raw provider errors could expose customer text and create unbounded logs. | Fixed |
| 2 | medium | 9/10 | Retry exhaustion did not represent terminal `SkipRetry` failures. | Fixed |
| 3 | low | 10/10 | Tests did not cover retry disposition, malformed payloads, or terminal severity. | Fixed |

## Changes Made
Replaced raw error logging with allowlisted failure classes and numeric provider status codes. Added
explicit retry disposition, terminal severity, safe identifier logging, and tests proving task payload
and provider message text never appear in logs.

## Reviewer's Summary
The second review passed with no remaining privacy, correctness, retry, or finalization issues.

---

# Stripe Return Authentication Review

**Artifact**: Tenant refresh-cookie policy for Stripe Checkout return navigation
**Date**: 2026-07-21
**Rounds**: 1

## Verdict: PASS

## Changes Reviewed
The tenant refresh cookie now uses `SameSite=Lax` when issued and cleared. The focused regression
test covers both lifecycle paths while preserving `HttpOnly`, configurable `Secure`, root path,
expiry, and deletion semantics. Platform-admin cookies remain `SameSite=Strict`.

## Reviewer's Summary
The change correctly permits top-level cross-site GET returns from Stripe without enabling the
cookie on cross-site POST requests. No correctness, security, consistency, or test issues remain.

---

# Magic-Link Retry Review

**Artifact**: `web/pages/auth/verify.vue` hash-only retry, cancellation, and token-history handling
**Date**: 2026-07-22
**Rounds**: 2

## Verdict: FIXED

## Issues Found
| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | high | 10/10 | A superseded project lookup could navigate over a newer verification attempt. | Fixed |
| 2 | medium | 9/10 | Post-auth navigation failures could be presented as invalid-token failures. | Fixed |
| 3 | medium | 9/10 | Direct history replacement discarded router history state. | Fixed |
| 4 | medium | 9/10 | The sequential test did not cover overlapping verification work. | Fixed |
| 5 | high | 10/10 | The optional project lookup could start an uncancellable refresh that overwrote a fresh session. | Fixed |
| 6 | medium | 9/10 | Vue Router retained the exchanged fragment internally, allowing Back to restore the token. | Fixed |

## Changes Made
Verification attempts now use per-attempt abort controllers and identity guards after asynchronous
work. Token exchange errors are handled separately from post-auth routing. The fragment is removed
with a forced Vue Router replace before exchange, preserving history state while sanitizing router
metadata. The optional project lookup cannot refresh the session.

## Verification
Browser regressions cover same-page retry, overlapping attempts, refresh suppression, history Back,
and normal activation on desktop and mobile. The final full run passed 39 E2E tests with 9 expected
skips; 16 unit tests and Nuxt typecheck also passed. The review chain stopped after its required
maximum of two rounds; all six reported findings were resolved and exercised by the final test run.

---

# Feedback RBAC Review

**Artifact**: Feedback queue and detail mutation controls for read-only tenant members
**Date**: 2026-07-22
**Rounds**: 2

## Verdict: FIXED

## Issues Found
| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | major | 10/10 | Removing the selection cell shifted every later feedback-row grid item for read-only members. | Fixed |

## Changes Made
Feedback status, AI reprocessing, attachment retry controls, and their client handlers now require
`feedback:update`. Read-only members retain feedback, issue, evidence, and attachment inspection.
An inert grid placeholder preserves row placement without exposing a selection control.

## Verification
Rendered-position coverage exercises read-only rows on desktop and mobile. The final verification
passed 16 unit tests, Nuxt typecheck, production build, and 49 Playwright tests with 15 expected
skips. The second independent review found no remaining authorization or layout issues.

---

# Deletion Grace Read-Only Review

**Artifact**: Tenant deletion grace enforcement across backoffice, billing, widget, streams, and API-key authentication
**Date**: 2026-07-22
**Rounds**: 2

## Verdict: FIXED

## Issues Found
| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | critical | 10/10 | Billing checkout, portal, and deletion scheduling bypassed the writable-tenant middleware. | Fixed |
| 2 | major | 9/10 | A member with only `billing:manage` could not reach billing to cancel deletion. | Fixed |
| 3 | major | 8/10 | Suspended and expired-trial tenants lost read access during the deletion grace period. | Fixed |
| 4 | major | 10/10 | Frontend-only tests could not detect direct backend mutation bypasses. | Fixed |
| 5 | major | 10/10 | Attachment and replay stream handlers duplicated the old active-tenant check. | Fixed |
| 6 | major | 10/10 | API-key authentication updated `last_used_at` during the read-only period. | Fixed |
| 7 | major | 10/10 | Widget connection telemetry still updated projects during the read-only period. | Fixed |

## Changes Made
Workspace mutation controls and handlers now share `canWrite`, while the backend remains the security
boundary. Billing mutators require writable state; cancellation is the sole exception. Pending-deletion
tenants retain reads across normal routes, public widget configuration, attachments, and replay streams.
Widget activity and API-key usage telemetry are suppressed during the grace period.

## Verification
Route and PostgreSQL integration tests assert blocked billing mutations have no provider or store side
effects, manage-only cancellation works, API-key reads do not update usage, and public or streamed reads
remain available for suspended and expired-trial tenants. The final independent security review passed.

---

# Feedback Route Synchronization Review

**Artifact**: Feedback queue route/filter synchronization and issue-count copy
**Date**: 2026-07-22
**Rounds**: 2

## Verdict: FIXED

## Issues Found
| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | major | 10/10 | Missing `status` was ambiguous between default Open navigation and the local All-status choice. | Fixed |
| 2 | minor | 9/10 | The first queue regression test did not prove navigation remained client-side. | Fixed |

## Changes Made
Filter-driven query destinations are now marked locally so the route watcher only imports state from
external queue, search, and history navigation. All status, Clear filters, and row selection retain
their state and clean URLs. Issue detail counts use singular copy for one linked feedback record.

## Verification
Mixed-status E2E coverage exercises queue links, All status, Clear filters, row selection, filtered
records, URL state, and the absence of document reloads. The final independent review passed with no
remaining findings.

---

# Abandoned Attachment Upload Review

**Artifact**: Expired attachment reconciliation, recovery paths, migration, and feedback-detail UI
**Date**: 2026-07-22
**Rounds**: 2

## Verdict: FIXED

## Issues Found
| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | major | 9/10 | Concurrent reconcilers could expire different attachments for one feedback without either completing its AI state. | Fixed |
| 2 | minor | 9/10 | The activity index retained terminal incomplete uploads and would degrade reconciliation over time. | Fixed |

## Changes Made
Incomplete uploads expire after a 15-minute grace period and no longer block feedback completion.
Affected feedback rows are locked in stable order before completion is recalculated, preventing the
multi-reconciler race. Idempotent submission and late completion recover an expired upload, while the
backoffice only offers AI retry for uploads that actually completed. The partial index now contains only
pending or processing incomplete uploads.

## Verification
A deterministic PostgreSQL concurrency regression holds a feedback lock while two reconcilers expire
different attachments, then proves the final feedback completion is recorded. Backend tests, vet,
coverage enforcement, migration tests, web unit tests, typecheck, production build, and the full browser
suite pass. The second review's focused index finding was fixed and revalidated without starting a third
review round.

---

# Abandoned Replay Recovery Review

**Artifact**: Terminal replay recovery, merge retry bounds, migration, and feedback-detail UI
**Date**: 2026-07-22
**Rounds**: 2

## Verdict: FIXED

## Issues Found
| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | major | 10/10 | Transient storage failures were treated as missing objects and could permanently discard recoverable replay data. | Fixed |
| 2 | major | 8/10 | Redeclaring an incomplete batch did not refresh recovery activity, so a valid renewed upload could be terminalized. | Fixed |

## Changes Made
Abandoned zero-batch sessions now become explicitly unavailable, while incomplete uploads receive one
storage recovery pass before terminalization. Typed object absence is distinguished from retryable
storage failures, which increment bounded merge attempts. Batch declarations refresh upload activity,
preventing a renewed upload URL from being expired during its validity window. Terminal sessions are
excluded from reconciliation and exposed consistently through the API and feedback UI.

## Verification
PostgreSQL regressions cover zero-batch and incomplete-only abandonment, renewed upload activity,
transient storage errors, bounded terminal state, and late completion rejection. The MinIO integration
test verifies typed absence. Backend tests, vet, coverage enforcement, web unit tests, typecheck,
production build, and the full browser suite pass. The final independent review passed.

---

# Replay Configuration Cap Review

**Artifact**: Replay duration and event cap alignment across API, database, widget rules, and backoffice
**Date**: 2026-07-22
**Rounds**: 2

## Verdict: FIXED

## Issues Found
| # | Severity | Confidence | Problem | Status |
|---|----------|------------|---------|--------|
| 1 | major | 9/10 | Migration 43 left a live-write window between clamping existing rows and installing the narrower constraints. | Fixed |
| 2 | minor | 10/10 | Migration coverage did not prove legacy values were clamped or that rollback restored the wider limits. | Fixed |

## Changes Made
Replay rules now share the product limits of 30 minutes and 50,000 events across backend validation,
session creation, database constraints, and the project settings UI. The migration installs narrow
`NOT VALID` constraints before normalizing existing rows, validates them, and replaces the old
constraint names without accepting concurrent out-of-range writes.

## Verification
The PostgreSQL migration test exercises version 42, upgrade, rollback, and a second upgrade while
asserting legacy values are accepted before migration and clamped afterward. Backend tests and vet,
web unit tests, typecheck, production build, the full browser suite, and the final independent review pass.
