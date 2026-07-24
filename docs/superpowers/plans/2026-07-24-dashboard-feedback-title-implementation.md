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

**Status:** Pending.

## Task 2: Use The Curated Title With Compatibility Fallbacks

Update `feedbackCopy()` in `web/pages/index.vue` to resolve `title`, then `text_cleaned`, then `text`,
then the existing localized no-detail copy. Add a missing-title assertion proving old records still
prefer cleaned text and do not require a backend or localization change.

**Acceptance:** The focused responsive case passes for both the distinct-title and missing-title
records without changing dashboard row geometry.

**Status:** Pending.

## Task 3: Validate And Reconcile

Run web unit tests, Nuxt typecheck/build, the focused dashboard Playwright case, and inspect
desktop/mobile screenshots. Record the completed behavior in `plans/fidibeki.plan.md`, commit the
slice, and comment on #73 with exact evidence.

**Acceptance:** All local gates pass, the worktree is clean, and the issue record makes no
deployment or staging claim.

**Status:** Pending.
