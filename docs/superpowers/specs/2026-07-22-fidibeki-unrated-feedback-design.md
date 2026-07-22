# Fidibeki Unrated Feedback Design

**Date:** 2026-07-22
**Status:** Approved
**Target:** Dispatch backoffice

## Problem

Feedback can be submitted without a rating. The feedback queue represents that state as `—`, but the
dashboard currently coerces an absent rating to zero before rendering `RatingMark`. The result says
`0 out of 5`, which is a different and misleading value.

## Goals

- Represent an absent rating as absent, never as zero.
- Show a compact `—` in the dashboard worklist, consistent with the feedback queue.
- Expose the accessible name and tooltip `Not rated` for the compact marker.
- Preserve the current presentation and accessible text for ratings 1 through 5.

## Non-goals

- No changes to satisfaction aggregates, feedback capture, API schemas, or stored data.
- No redesign of the dashboard or feedback queue.
- No new rating value, filter, or sorting behavior.
- No changes to the interface-study components, whose fixture ratings are always populated.

## Component Contract

`RatingMark` makes its rating prop optional and accepts `number | null`. This covers both the omitted
frontend field and an explicit API null. It derives a rated state without truthiness coercion:

- For values 1 through 5, it keeps the current number, `/ 5` label in non-compact mode, color class,
  tooltip, and screen-reader text.
- For `null` or `undefined`, compact mode renders a stable-width `—` with `title="Not rated"` and
  screen-reader text `Not rated`.
- For `null` or `undefined`, non-compact mode renders `Not rated` in the same component frame. This
  defines a safe shared behavior even though the dashboard uses compact mode.

The component does not accept zero as an alias for absence. Type and runtime behavior remain explicit
so an invalid numeric rating is not silently relabeled.

## Dashboard Data Flow

The dashboard passes `item.rating` directly to `RatingMark`; it removes `item.rating || 0`. No API,
query, or response transformation changes. The marker keeps its existing fixed compact dimensions so
switching between rated and unrated rows does not move adjacent content.

Other production views may retain their current conditional wording (`—` or `No rating`). This change
only centralizes a valid null presentation for consumers that use `RatingMark` directly.

## Accessibility

The visible dash is decorative to assistive technology. The component exposes exactly one readable
label, `Not rated`, and the wrapper tooltip uses the same wording. Rated values continue to expose
`<value> out of 5`. Color remains supplemental and is never the sole rating signal.

## Testing And Verification

- Extend the existing Dispatch dashboard browser coverage with rated and unrated feedback items.
- For the unrated row, assert visible `—`, accessible `Not rated`, and absence of `0 out of 5`; for
  the rated row, retain the existing numeric and accessible-value assertions.
- Run web unit tests, typecheck, production build, and the Dispatch Playwright suite.
- Check desktop and mobile screenshots for row alignment, overflow, and text overlap.
- Verify the same behavior against an unrated feedback item on staging after deployment.

No production deployment is part of this change.
