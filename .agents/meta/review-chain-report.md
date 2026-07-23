---
skill: review-chain
version: 1
date: 2026-07-23
status: final
---

# Review Chain Report

**Artifact**: Automatic feedback context privacy (#54)
**Date**: 2026-07-23
**Rounds**: 2 independent Claude Sonnet reviews

## Verdict: FIXED

## Issues Found

| # | Severity | Confidence | Location | Problem | Status |
|---|----------|------------|----------|---------|--------|
| 1 | major | 9/10 | migration `77` | The historical-origin regex could retain part of credentials containing multiple `@` characters as a hostname. | Fixed with a last-`@` boundary and a destructive sentinel migration fixture. |
| 2 | minor | 9/10 | feedback/logging tests | DOM context omission, depth-only bounds, and `dom_snapshot_html` log redaction lacked direct coverage. | Fixed with unit and HTTP integration coverage. |
| 3 | minor | 9/10 | `feedback/validation.go` | A referrer validation branch was unreachable after automatic metadata minimisation. | Fixed by removing the dead branch. |
| 4 | major | 9/10 | `web/pages/feedback/[id].vue` | Origin-only context remained a clickable page link in the backoffice. | Fixed: origin-only context is plain text; only a specific safe path is linked. |

## Input Quality Assessment

| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Rich | Approved roadmap, issue contract, privacy data map, and replay threat model were available. |
| Requirements clarity | Precise | The safe-location projection, prohibited fields, downstream boundaries, and migration behavior were explicit. |
| Upstream artifacts | Fresh | The approved issue #54 design and related privacy documents were written on 2026-07-23. |

## Simplifications Applied

The widget, API, replay verifier, integrations, and web UI reuse one rule-safe location model.
The backend rejects or omits unsafe context instead of trying to repair identity-bearing payloads,
and historical data is reduced to origin rather than guessed from old rules.

## Changes Made

- Pinned rrweb metadata and navigation events to the server-validated session location at capture,
  verification, and merge.
- Remediated historical feedback, funnel, replay, and export data in migration `77`.
- Added structural DOM limits, log redaction, HTTP boundary tests, migration collision coverage,
  and positive coverage for every permitted incremental rrweb event.
- Raised the branch back above the backend CI coverage gate without lowering thresholds.
- Removed clickable links for origin-only context in notifications and the backoffice.

## Reviewer's Summary

After the fixes, no remaining correctness, privacy, migration, or boundary finding survived
verification at confidence 8/10 or higher. Widget and Go projections stay aligned through their
shared contract, and replay URLs are pinned independently at each trust boundary.

## Resolver's Notes

The existing string-prefix targeting semantics were not changed because both runtime
implementations and the approved product behavior already define `url_prefix` that way. Migration
rollback intentionally does not restore deleted personal context.
