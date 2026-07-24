---
skill: review-chain
version: 1
date: 2026-07-24
status: final
---

# Review Chain Report

**Artifact**: Breach containment and corrective-action ledger (#64)
**Date**: 2026-07-24
**Rounds**: 2 independent fresh-eyes reviews

## Verdict: FIXED

## Issues Found

| # | Severity | Confidence | Location | Problem | Status |
|---|----------|------------|----------|---------|--------|
| 1 | major | 9/10 | `actions_store.go` | Temporal due-date validation ran before idempotency lookup, so a retry after its deadline failed instead of returning the original action. | Fixed by separating stable normalization from creation-time validation and testing post-deadline replay. |
| 2 | major | 9/10 | migration `101` | Default privileges left `UPDATE` and `DELETE` granted to `fidibeki_admin`, despite the intended SELECT/INSERT-only boundary. | Fixed with an explicit revoke/regrant and exact admin/app/PUBLIC privilege tests. |
| 3 | major | 9/10 | `Store.GetIncident` | Separate READ COMMITTED statements could return different action revisions in current state and history. | Fixed with one read-only REPEATABLE READ transaction and a coordinated concurrent reader/writer test. |
| 4 | major | 9/10 | action validation | Evidence entries respected individual bounds but could exceed the database's 4,000-character aggregate bound and surface as a 503. | Fixed in shared normalization with aggregate-bound coverage. |
| 5 | major | 9/10 | migration `101` | Non-seed rows could omit idempotency evidence because both key and fingerprint were nullable. | Fixed by requiring both fields on every event and assigning deterministic keys/fingerprints to internal seeds. |
| 6 | major | 9/10 | migration `101` | `ARRAY[NULL]::text[]` could satisfy the database completion-evidence check. | Fixed by forbidding NULL array elements and adding a direct-insert migration test. |

## Input Quality Assessment

| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Rich | The approved three-pillar roadmap, issue #64 audit, existing breach register, and executable privacy registry were available. |
| Requirements clarity | Precise | The approved design fixes the action model, transitions, API, permissions, retention boundary, and explicit non-goals. |
| Upstream artifacts | Fresh | Design and implementation plan were created and approved on 2026-07-24. |

## Simplifications Applied

The ledger keeps one append-only event table and derives current state from the highest revision.
Internal checklist seeds now use the same mandatory idempotency shape as operator-created events,
avoiding a nullable special case.

## Changes Made

- Added migration `101`, nine legacy-safe containment seeds, immutable revision guards, exact grants,
  and decision-required retention coverage.
- Added transactional post-incident action creation, optimistic revisions, durable idempotency,
  complete action history, and additive platform-admin HTTP routes.
- Added domain, HTTP, PostgreSQL, migration rollback/reapply, concurrency, snapshot, privilege, and
  migration-chain coverage.
- Updated the technical privacy map and product plan without deploying or changing a cleaner.

## Reviewer's Summary

All first-round findings were resolved. The second round identified one additional SQL NULL-array
edge case; the resolver added an explicit database constraint and destructive migration test.
No further review round was run because the review-chain protocol caps verification at two rounds.
