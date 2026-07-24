# Mobile Feedback Filters Implementation Plan

> **Scope mode:** LOCKED
>
> **Design:** `docs/superpowers/specs/2026-07-24-mobile-feedback-filters-design.md`
>
> **Issue:** #72
>
> **Boundary:** local product work only. No deployment, production, backend, provider, DNS, or
> infrastructure changes.

## Task 1: Prove The Responsive Gap And Preserve Desktop

Extend `web/tests/e2e/dispatch-platform.spec.ts` with focused queue-filter coverage. At desktop
width, assert that the current project, status, rating, evidence, and advanced controls remain
directly available and keep exact URL/result behavior. At mobile width, prove one explicit Filters
entry point exposes the complete structured filter set without horizontal document overflow.

**Acceptance:** The focused Playwright cases fail against the current horizontal-scroll
implementation for the mobile contract while the desktop compatibility assertions pass.

**Status:** Done. Added `desktop feedback toolbar keeps every structured filter directly
available`, `mobile Filters disclosure exposes every structured filter and restores focus`, and
`mobile filter panel stays within the viewport and scrolls internally on short screens` to
`web/tests/e2e/dispatch-platform.spec.ts`. Confirmed the mobile case failed against the
pre-implementation horizontal-scroll markup (no `Filters` disclosure existed) before Task 2 landed,
while the desktop assertions passed unchanged throughout.

## Task 2: Add One Complete Mobile Filter Surface

Update `web/pages/feedback/index.vue` so widths at or below `700px` retain the common search field
and replace the horizontally scrolling select row with a native mobile disclosure. Bind project,
status, rating, evidence, moderation, Issue relationship, AI state, sentiment, and date range to
the existing refs. Add a computed active structured-filter count plus a Done helper that closes the
disclosure and restores focus to its summary. Reuse the existing clear operation and immediate URL
synchronization.

Add concise English and Portuguese strings in `web/i18n/messages/feedback.en.ts` and
`web/i18n/messages/feedback.pt.ts`. Keep all existing labels and desktop controls.

**Acceptance:** The focused mobile Playwright case can select primary and advanced filters, observe
the active count and exact URL/result changes, clear them, close the disclosure, and observe focus
on the summary.

**Status:** Done. `web/pages/feedback/index.vue` adds a desktop-only wrapper
(`.feedback-filter-bar-desktop`, `display: contents`) around the existing controls, an
`activeFilterCount` computed covering project/status/rating/evidence/moderation/issue/AI
status/sentiment/from/to, and a mobile-only `<details class="feedback-mobile-filters">` bound to
the same refs with a `closeMobileFilters` helper that closes the disclosure and refocuses its
summary. `feedback.en.ts`/`feedback.pt.ts` add `filters.mobileTrigger`,
`filters.mobileTriggerActive` (pluralized), and `filters.done`.

## Task 3: Constrain Mobile Geometry And Accessibility

Remove mobile horizontal filter-bar scrolling. Style the search/disclosure row and its bounded panel
using existing Dispatch tokens. The panel uses two columns at normal mobile widths, one column on
very narrow screens, and internal vertical scrolling on short viewports. Preserve native label,
select, input, summary, and button semantics plus visible focus.

Add stable geometry checks at `390x844` and a short mobile viewport. Assert no control or panel
exceeds the viewport and the document has no horizontal overflow.

**Acceptance:** The focused responsive Playwright geometry case passes at both viewports and proves
the panel itself scrolls when its content exceeds the available height.

**Status:** Done. The mobile toolbar no longer scrolls horizontally
(`.feedback-filter-bar { overflow-x: visible }` under the `700px` breakpoint); the panel is a
`position: fixed` overlay (`top/right/bottom/left`, `overflow-y: auto`, `align-content: start`)
using a two-column grid that collapses to one column at `360px` and below. The geometry test
confirms containment and internal scrolling at `390x844`, a short `390x380` viewport, and a narrow
`340x844` viewport.

## Task 4: Run Complete Gates And Reconcile Records

Run web unit tests, Nuxt typecheck, Nuxt build, focused Playwright, and the complete desktop/mobile
Playwright suite. Inspect desktop/mobile screenshots for the closed toolbar, open filter panel,
long Portuguese labels, active count, and short-viewport containment. Record #72 in
`plans/fidibeki.plan.md` and comment on the GitHub issue with the exact commit and evidence.

**Acceptance:** All local gates pass, the delivery worktree is clean with a tested commit and
handoff, and neither the code nor the record claims deployment or staging validation.

**Status:** Done. Web unit tests (133 passed), Nuxt typecheck, and Nuxt production build all pass.
The complete Playwright suite passed 222/280 (58 intentionally viewport/project-skipped, 0 failed)
across two consecutive full runs; two unrelated `activation-flow.spec.ts` magic-link tests flaked
under full-suite worker contention on one run and passed cleanly both in isolation and on the
following clean full run, consistent with a pre-existing timing-sensitive flake unrelated to this
slice. Desktop, mobile, short-viewport, narrow-column, and Portuguese screenshots were generated and
visually inspected (see `plans/fidibeki.plan.md` #72 entry). No deployment or staging work was
performed, per the boundary.
