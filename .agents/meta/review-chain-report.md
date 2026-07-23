---
skill: review-chain
version: 1
date: 2026-07-23
status: final
---

# Review Chain Report

**Artifact**: Widget controlled-appearance hierarchy and migration 000058
**Date**: 2026-07-23
**Rounds**: 2 local review passes

## Verdict: FIXED

## Issues Found

| # | Severity | Confidence | Location | Problem | Status |
|---|----------|------------|----------|---------|--------|
| 1 | major | 10/10 | `backend/internal/project/store.go` | A legacy client update replaced the complete definition appearance object, silently dropping new surface, text, and logo overrides. | Fixed by merging legacy position/theme/accent tokens while explicit new-contract updates still replace the layer. Covered by a PostgreSQL HTTP regression. |

## Input Quality Assessment

| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Rich | The approved three-pillar roadmap defines the workspace/project/definition hierarchy and controlled-token boundary. |
| Requirements clarity | Precise | The slice requires inheritance, safe tokens, preview parity, compatibility, migrations, and backend/web/widget tests. |
| Upstream artifacts | Fresh | Roadmap, current implementation plan, lifecycle slice, and staging-only decision are all dated 2026-07-22. |

## Simplifications Applied

The existing widget-rule table remains the definition store. Three JSONB override objects provide sparse layers,
and one pure resolver is used per runtime rather than introducing a theme document or arbitrary CSS model.

## Changes Made

Added workspace, project, and definition appearance layers; accessible color validation for the new contract;
HTTPS logo validation; effective public configuration; runtime rendering; backoffice editors and preview; migration
backfill; legacy-update preservation; and focused PostgreSQL, unit, browser, and responsive visual coverage.

## Reviewer's Summary

Authorization remains behind the existing writable tenant/project permissions and RLS boundaries. Inputs are
restricted to enums, six-digit colors, paired accessible surface/text colors, and escaped HTTPS logo URLs. The
backend never fetches the logo, so the URL does not create SSRF. Explicit updates can reset a layer, while legacy
updates preserve fields their clients cannot represent. No unresolved blocking issue remains.

## Resolver's Notes

Legacy accent colors retain their historical hex-only validation to avoid breaking existing clients. The stricter
contrast rule applies to the new controlled appearance contract and all new backoffice writes.
