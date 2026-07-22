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
