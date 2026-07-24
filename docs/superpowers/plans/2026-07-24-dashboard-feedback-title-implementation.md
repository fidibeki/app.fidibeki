# Dashboard Feedback Title Implementation Plan

> **Scope mode:** LOCKED
>
> **Design:** `docs/superpowers/specs/2026-07-24-dashboard-feedback-title-design.md`
>
> **Issue:** #73
>
> **Boundary:** local web product work only. No deployment, production, backend, migration, or
> infrastructure changes.

## Task 1: Expose The Hidden Contract Bug

Add focused responsive coverage in `web/tests/e2e/dispatch-platform.spec.ts` with a Feedback record
whose title differs from its description, raw text, and cleaned text. Assert the dashboard attention
row shows only the curated title while preserving its link and metadata.

**Acceptance:** The focused desktop/mobile Playwright case fails against the current dashboard copy
resolver because it renders cleaned reporter text.

**Status:** Complete. The responsive fixture now assigns distinct curated title, original
description, cleaned text, and raw text values. The focused desktop/mobile case failed against the
old resolver because neither viewport rendered the title.

## Task 2: Use The Curated Title With Compatibility Fallbacks

Update `feedbackCopy()` in `web/pages/index.vue` to resolve `title`, then `text_cleaned`, then `text`,
then the existing localized no-detail copy. Add a missing-title assertion proving old records still
prefer cleaned text and do not require a backend or localization change.

**Acceptance:** The focused responsive case passes for both the distinct-title and missing-title
records without changing dashboard row geometry.

**Status:** Complete. `feedbackCopy()` now resolves `title`, `text_cleaned`, `text`, then the
localized no-detail copy. The same responsive fixture proves a second record with an empty title
uses cleaned text and never exposes its raw fallback.

## Task 3: Validate And Reconcile

Run web unit tests, Nuxt typecheck/build, the focused dashboard Playwright case, and inspect
desktop/mobile screenshots. Record the completed behavior in `plans/fidibeki.plan.md`, commit the
slice, and comment on #73 with exact evidence.

**Acceptance:** All local gates pass, the worktree is clean, and the issue record makes no
deployment or staging claim.

**Status:** Complete. All 133 web unit tests, Nuxt typecheck, and the production build pass. The
focused Playwright case passes in both desktop and mobile projects, no document overflow is present,
and inspected screenshots show the curated title and legacy cleaned fallback without changing row
geometry. No deployment or staging validation was performed.
