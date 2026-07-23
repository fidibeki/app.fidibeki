---
skill: review-chain
version: 1
date: 2026-07-23
status: final
---

# Review Chain Report

**Artifact**: Moderated-content propagation (#6)
**Date**: 2026-07-23
**Rounds**: 2 local review passes

## Verdict: FIXED

## Issues Found

| # | Severity | Confidence | Location | Problem | Status |
|---|----------|------------|----------|---------|--------|
| 1 | major | 10/10 | `backend/internal/feedback/tenant_store.go` | The first projection replaced every flagged feedback title, including safe operator edits. | Fixed by replacing only titles derived from reporter content and retaining independent summaries. |
| 2 | major | 10/10 | `backend/internal/issue/store.go` | Exact replacement missed fallback titles that were only the first sentence or another substring of the original report. | Fixed with bidirectional derived-copy detection and regression coverage. |
| 3 | major | 10/10 | `backend/internal/issue/http.go`, `backend/internal/integration/delivery.go` | Historical activity `changes.from` values could expose an older raw title/body after the current Issue was edited to safe content. | Fixed by projecting title/description activity and integration changes against the linked moderated sources. |

## Input Quality Assessment

| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Adequate | The issue identifies the feedback, Issue, spam, and integration leak paths with concrete examples. |
| Requirements clarity | Good | `[MODERATED]` is the explicit public projection; preserving raw evidence at rest is an implementation decision. |
| Upstream artifacts | Fresh | Existing AI moderation flags, suggested title, outbox retry behavior, and nullable Issue linkage were reused. |

## Simplifications Applied

No extra AI call, schema migration, moderation service, or destructive rewrite was added. One shared redaction
helper and safe read/delivery projections contain the change while preserving the original evidence.

## Changes Made

Flagged and spam feedback now render safe text in the tenant API, including copied response/comment content and
flagged transcripts. Issue creation stores safe derived content; historical linked Issues and activity changes are
safely projected; external integrations wait for AI completion before constructing provider payloads.

## Reviewer's Summary

The review traced raw reporter content from submission storage through tenant reads, Issue creation/history, and
all integration provider formats. Safe operator-authored titles and descriptions remain intact. No unresolved
correctness, security, or tenant-isolation finding remains in the changed paths.

## Resolver's Notes

All backend packages pass, including PostgreSQL integration tests. Web typecheck, 72 unit tests, and the Nuxt
build pass. The full browser suite passes 115 tests with 43 intentional skips; the moderation journey passes on
desktop and mobile, and both screenshots were inspected for containment and absence of raw content. The remaining
gate is the standard CI and staging post-deploy run for this commit.
