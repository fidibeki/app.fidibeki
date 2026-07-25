---
skill: review-chain
version: 1
date: 2026-07-25
status: final
---

# Review Chain Report

**Artifact**: Yearly plan subscription support (branch `feat/yearly-plans`, ~25 files: Go billing backend, migration 000110, Nuxt billing page, marketing pricing data)
**Date**: 2026-07-25
**Rounds**: 1 (specialist dispatch: security + correctness + consistency/perf, run in parallel)

## Verdict: FIXED

## Issues Found
| # | Reviewer | Severity | Confidence | Location | Problem | Status |
|---|----------|----------|------------|----------|---------|--------|
| 1 | correctness | major | 7/10 | backend/internal/billing/store.go (ApplyEvent) | `billing_interval` was written from the incoming price on every event, while a downgrade defers the plan via `pending_plan` — a combined downgrade+interval switch would expose a plan/interval pair Stripe isn't actually charging | Fixed — interval now updates only in the branches where `plan` itself updates; NULL-safe via `intervalValue()` |
| 2 | correctness | minor | 7/10 | infra/migrations/000110 up | No backfill: pre-existing subscribers would show `billing_interval: null` until their next webhook | Fixed — `UPDATE tenants SET billing_interval='monthly' WHERE stripe_subscription_id IS NOT NULL` (all pre-migration subscriptions are monthly by construction) |
| 3 | consistency | minor | 6/10 (uncertain) | web/pages/billing.vue checkout() | With the yearly toggle active, checkout sent `yearly` even for a plan without a configured yearly price (latent partial-rollout footgun; card already displays the monthly fallback) | Fixed — checkout clamps to `monthly` when the plan has no `yearly_price_cents`, matching the displayed price |
| 4 | consistency | nit | 5/10 (uncertain) | backend/internal/config/plans_test.go | Self-service plan test did not assert `YearlyPriceCents != nil` | Fixed — assertion added |

Security reviewer: PASS (no findings; independently flagged issue #1 as a non-security observation, corroborating the correctness reviewer).

## Input Quality Assessment
| Input | Rating | Evidence |
|-------|--------|----------|
| Product/domain context | Rich | Existing monthly billing pipeline, marketing pricing data, and plan catalog fully specified the intent |
| Requirements clarity | Precise | Audit performed first; yearly discounts (7%/14%) already advertised by backend catalog and marketing site |
| Upstream artifacts | Fresh | Live codebase, tests, and migrations current as of implementation |

## Changes Made
Interval update in `ApplyEvent` now travels with the plan state machine instead of leading it; migration backfills existing subscribers; web checkout clamps interval to what the card displays; config test hardened. Full backend short suite, web unit tests, web typecheck, marketing tests/typecheck, and the billing Playwright e2e (desktop) re-run green after fixes.

## Reviewer's Summary
Core interval-aware plumbing (catalog, checkout validation, webhook mapping, migration NULL handling, per-month arithmetic) judged correct and well covered by tests; trust boundaries unchanged — price IDs are resolved exclusively server-side, webhook plan/interval derives from Stripe's own subscription data, and access control middleware is untouched.

## Resolver's Notes
No findings declined. Integration tests (Docker-based) were updated for the new semantics but could not be executed locally (no Docker daemon); they will run in CI.
