---
skill: review-chain
version: 1
date: 2026-07-23
status: pending-ci
---

# Review Chain Report

**Artifact**: Data rights, exports, anonymization, and retention (#25)
**Date**: 2026-07-23
**Rounds**: 3 local review passes

## Verdict: FIXED LOCALLY

## Issues Found

| # | Severity | Confidence | Location | Problem | Status |
|---|----------|------------|----------|---------|--------|
| 1 | major | 10/10 | `web/utils/download.ts` | A signed download URL returned by the API was used directly, allowing a compromised response to supply a non-HTTP executable scheme. | Fixed with a shared parser that permits only credential-free HTTP(S) URLs, plus unit coverage. |
| 2 | major | 9/10 | `web/nuxt.config.ts` | The public token page added no-referrer/noindex only after client hydration, leaving the initial document response without explicit token-safe headers. | Fixed with route-level no-store, no-referrer, and X-Robots-Tag headers; client meta remains defense in depth. |
| 3 | minor | 10/10 | `web/components/settings/DataPrivacy.vue` | Mobile retention rows initially hid the effective duration, removing the core decision information from small screens. | Fixed by retaining category and duration while hiding only the policy source. |
| 4 | minor | 10/10 | `web/pages/settings/index.vue` | The first component reference used the directory-prefixed auto-import name even though this Nuxt project disables component path prefixes. | Fixed by using the generated `DataPrivacy` component name and exercising the route in E2E. |

## Input Quality Assessment

| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Rich | Issue #25 and the approved design define workspace portability, verified reporter rights, retention, expiry, audit, and legal boundaries. |
| Requirements clarity | Precise | The design specifies exact permissions, token neutrality, allowlisted exports, destructive behavior, retention windows, and responsive states. |
| Upstream artifacts | Fresh | Tenant deletion, analytics retention, storage, queueing, audit, email, permissions, Settings, and public reporter patterns were audited and reused. |

## Simplifications Applied

The implementation reuses the existing storage, transaction, queue, permission, email, Settings, and public
reporter foundations. Reporter exports and anonymization share one privacy job state machine, while workspace
exports use the same reconciler without exposing cross-kind downloads. No parallel identity, storage, or
production-only path was added.

## Changes Made

Workspace administrators can generate, retry, inspect, and download durable allowlisted exports. Verified
reporters can request a scoped export or irreversible anonymization; structured identity, media, transcripts,
DOM evidence, progress tokens, and notification routing are removed while operational history remains.
Retention now preserves non-downloadable manifests, expires archives and request access, exposes the effective
plan matrix, and records bounded retryable cleanup. The Dispatch Settings UI and token-safe public status page
cover all resulting states.

## Reviewer's Summary

The review traced tenant isolation, human-only permissions, hash-only verification tokens, neutral public
responses, rate limiting, object-before-manifest deletion, snapshot consistency, retry idempotency, consent
history, signed-download safety, manual-review disclosure, and responsive UI behavior. No unresolved local
correctness, PII exposure, authorization, data-mutation ordering, or tenant-isolation finding remains.

## Resolver's Notes

Backend migration replay, all Go packages, the 80% coverage gate, `go vet`, lint, Security CI, Release
Artifacts, and Backend CI pass. Web typecheck, 76 unit tests, Nuxt build, 124 full E2E passes with 44
intentional matrix skips, and focused privacy journeys pass locally. Web CI and the staging smoke are still
required before #25 can be closed.
