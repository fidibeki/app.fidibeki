---
skill: task-breakdown
version: 1
date: 2026-07-23
status: active
scope: locked
source: docs/superpowers/specs/2026-07-23-public-submission-abuse-protection-design.md
---

# Public Submission Abuse Protection Tasks

## Shared Context

- Extend existing public-key/origin authorization, Redis GCRA, tenant transactions, Asynq queues,
  storage, moderation, and Settings patterns.
- Keep the current API error envelope compatible; new reason and retry fields are additive.
- Use HMAC-SHA-256 for short-lived limiter identities. Persist no raw network, origin, visitor, page,
  or digest data in diagnostics.
- Keep feedback text when optional upload, replay, diagnostics, or AI work fails.
- Keep AI `processing_status` separate from attachment/replay `safety_status`.
- Staging only. No production deployment or infrastructure changes.

## Task 1: Stable Public Decisions

**Depends on:** None
**Status:** Complete

**Outcome:** The API can serialize a stable abuse reason and retry delay without changing existing
error consumers.

**Why:** Every protected public endpoint and the widget need one compatible failure contract.

**Acceptance:** `go test ./internal/api/...` proves old envelopes remain unchanged when optional
fields are absent and new fields serialize when present.

**Autonomy:** AFK

## Task 2: Privacy-Preserving Policy Evaluator

**Depends on:** Task 1
**Status:** Complete

**Outcome:** One abuse service derives rotating keyed buckets and evaluates named multi-scope
policies using the existing limiter.

**Why:** Feedback, media, replay, and AI must not invent divergent identities or decisions.

**Acceptance:** `go test ./internal/abuse/...` proves deterministic/current-previous key handling,
scope composition, denial precedence, retry calculation, and limiter failure classification.

**Autonomy:** AFK

## Task 3: Aggregate Diagnostic Ledger

**Depends on:** Task 2
**Status:** Complete

**Outcome:** Tenant/project hourly abuse categories are stored and retained without visitor
identifiers.

**Why:** Operators need pressure and failure visibility without product analytics fingerprinting.

**Acceptance:** The database migration and `internal/abuse` integration suite prove RLS isolation,
atomic concurrent aggregation, bounded queries, and 90-day cleanup.

**Autonomy:** AFK

## Task 4: Feedback Bounds And Coordinated Limits

**Depends on:** Tasks 2 and 3
**Status:** Complete

**Outcome:** Feedback submission enforces project/origin/network/visitor policies and bounded
attachment declarations while preserving honeypot, idempotency, and text intake.

**Why:** The highest-volume public path must reject abuse before database, storage, or AI cost.

**Acceptance:** Feedback HTTP integration tests prove authorization order, four-file/125-MiB bounds,
duplicate rejection, stable retry responses, distributed decisions, and text acceptance during
limiter failure.

**Autonomy:** AFK

## Task 5: Tenant Diagnostics API

**Depends on:** Task 3
**Status:** Complete

**Outcome:** Authorized members can read bounded submission-health totals and hourly series.

**Why:** The aggregate ledger is useful only through a tenant-isolated operational contract.

**Acceptance:** Abuse HTTP integration tests prove `tenant:read`, date/project bounds, zero-state,
aggregation, and cross-tenant denial.

**Autonomy:** AFK

## Task 6: Attachment Safety Schema

**Depends on:** Task 4
**Status:** Complete

**Outcome:** Attachments have an explicit declaration expiry and independent safety lifecycle, with
completed legacy objects preserved as safe.

**Why:** Upload state must be distinct from optional AI processing before quarantine is introduced.

**Acceptance:** Migration `up/down/up` tests prove enum/check constraints, legacy backfill, RLS, and
lossless rollback.

**Autonomy:** AFK

## Task 7: Quarantined Attachment Verifier

**Depends on:** Task 6
**Status:** Complete

**Outcome:** Completion consumes an unexpired declaration into quarantine and a deterministic worker
marks only signature-valid bounded objects safe.

**Why:** Storage metadata alone cannot establish media type or safety.

**Acceptance:** Verifier integration tests prove supported signatures, forged/truncated/oversized
rejection, object cleanup, idempotent reconciliation, and no silent safe state on scanner failure.

**Autonomy:** AFK

## Task 8: Safe-Only Attachment Consumers

**Depends on:** Task 7
**Status:** Complete

**Outcome:** Streaming, previews, exports, and attachment AI can consume only safe objects.

**Why:** Quarantine is ineffective if any downstream path bypasses it.

**Acceptance:** Cross-domain integration tests prove quarantined/rejected/expired objects cannot be
downloaded, exported as available, or claimed by AI while safe objects retain current behavior.

**Autonomy:** AFK

## Task 9: Replay Protection And Verification

**Depends on:** Tasks 2, 3, and 7
**Status:** Complete

**Outcome:** Replay grants are rate/byte bounded, expire, quarantine uploaded batches, and accept only
bounded structurally valid rrweb arrays.

**Why:** Replay is a separate high-volume storage path with the same abuse boundary.

**Acceptance:** Replay integration tests prove coordinated limits, aggregate caps, expiry, token
consumption, malformed/trailing/event-count rejection, cleanup, and safe-only finalization.

**Autonomy:** AFK

## Task 10: Idempotent AI Cost Budget

**Depends on:** Tasks 3 and 8
**Status:** Complete

**Outcome:** Text and safe-attachment analysis reserve tenant/project daily units once per work
generation and defer without losing feedback when unavailable.

**Why:** The provider RPM limit controls bursts but not tenant-attributed cost.

**Acceptance:** AI integration and concurrency tests prove one reservation across retries, budget
isolation, no provider call after denial/failure, visible defer state, and explicit retry recovery.

**Autonomy:** AFK

## Task 11: Widget Retry Experience

**Depends on:** Tasks 1 and 4
**Status:** Complete

**Outcome:** The widget maps stable reasons to inline states, honors retry delay, and retains all
entered content and local attachments.

**Why:** Abuse protection must not turn recoverable failures into lost feedback.

**Acceptance:** Widget unit/browser tests prove reason-specific text, countdown/retry, preserved
form/client ID/media, and accepted-feedback attachment-failure behavior.

**Autonomy:** AFK

## Task 12: Submission Health UI

**Depends on:** Task 5

**Outcome:** Settings displays a compact responsive breakdown of rate, spam, quota, upload, AI, and
provider pressure.

**Why:** Administrators need to distinguish operational causes without inspecting logs.

**Acceptance:** Playwright desktop/mobile tests prove period/project filtering, category states,
empty/loading/error states, permission behavior, and no horizontal overflow.

**Autonomy:** AFK

## Task 13: End-To-End Quality Gate

**Depends on:** Tasks 9, 10, 11, and 12

**Outcome:** The complete public protection flow is documented, tested, and ready in staging.

**Why:** Issue #26 closes only when every layer works together under real deployment behavior.

**Acceptance:** Migration replay, full backend/widget/web suites, static checks, responsive
screenshots, CI, and one staging journey all pass for the same commit.

**Autonomy:** AFK
