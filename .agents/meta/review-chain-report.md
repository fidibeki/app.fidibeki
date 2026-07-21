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

## Input Quality Assessment
| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Rich | Approved activation design, Dispatch UI direction, existing tenant/auth/widget contracts, and staging topology were available. |
| Requirements clarity | Precise | The approved design specifies routing, permissions, recovery, privacy, polling, and test acceptance criteria. |
| Upstream artifacts | Fresh | The activation design and implementation were both created and verified on 2026-07-21. |

## Simplifications Applied
Connection detection now uses one client-side rule resolver followed by a minimal, origin-authorized rule-ID acknowledgement. The backend validates that the acknowledged rule is in the enabled public configuration, eliminating duplicated regex evaluation and page-URL transmission. Activation permission checks are centralized in one helper.

## Changes Made
Added recoverable Turnstile loading, complete activation permission enforcement, privacy-safe best-effort widget activity reporting, and separate private/public Nuxt base-domain variables. Added focused backend, widget, web unit, and browser coverage for each correction.

## Reviewer's Summary
The platform proxy allowlist, plan catalog, migration, polling limits, authorized-origin ordering, and activation UI were sound. Two rounds identified seven concrete issues; all were resolved and the final backend, web, widget, browser, production-build, and database-backed checks pass.

## Resolver's Notes
No findings were declined. Telemetry failures are logged but intentionally do not affect widget availability. Production deployment remains deferred by user direction.
