# Fidibeki Unrated Feedback Implementation Plan

**Design:** `docs/superpowers/specs/2026-07-22-fidibeki-unrated-feedback-design.md`

## Goal

Make the Dispatch dashboard represent missing ratings as “Not rated” instead of the invalid value
`0 out of 5`, while preserving the existing rated presentation.

## Tasks

1. **Add the failing dashboard contract**
   - Extend `web/tests/e2e/dispatch-platform.spec.ts` with an open feedback fixture whose rating field
     is absent.
   - Assert the dashboard row exposes `Not rated`, visibly renders `—`, and contains no
     `0 out of 5` text.
   - Retain an assertion that a populated rating exposes its numeric `<value> out of 5` label.
   - Run the focused desktop Playwright case and confirm the unrated assertion fails before the fix.

2. **Implement the shared null state**
   - Update `web/components/design/RatingMark.vue` so `rating` is optional and nullable.
   - Derive the rated state explicitly; keep current classes and text for numeric values.
   - Render compact `—` or full `Not rated` for `null`/`undefined`, with a matching title and exactly
     one screen-reader label.
   - Keep fixed compact dimensions and avoid assigning a `rating-0` class.
   - Update `web/pages/index.vue` to pass `item.rating` without a zero fallback.

3. **Verify behavior and layout**
   - Run the focused dashboard test until it passes on desktop and mobile.
   - Run web unit tests, typecheck, production build, and the complete Playwright suite using the
     repository's pinned pnpm version.
   - Capture desktop and mobile dashboard screenshots and inspect row alignment, overflow, and
     overlap.
   - Commit as `fix(web): represent unrated feedback accurately`.

4. **Publish and verify staging**
   - Push after the security baseline is green, so the new Security CI also evaluates this commit.
   - Require Web CI, Security CI, and Release Artifacts to pass.
   - Verify a real unrated staging feedback row exposes `Not rated` and never `0 out of 5`.
   - Do not deploy or promote production.
