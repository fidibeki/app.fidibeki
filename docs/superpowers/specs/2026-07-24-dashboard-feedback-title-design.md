# Dashboard Feedback Title Design

**Issue:** #73
**Status:** Approved under the founder's standing authorization for recommended decisions
**Environment boundary:** Local product work only; no deployment, production, backend, or
infrastructure work

## Problem

Fidibeki separates reporter-authored `description` from the short, editable operational `title`.
The Feedback inbox follows that model, but the dashboard attention queue still renders
`text_cleaned || text` as its headline and ignores `title`. This can put a long original submission
back into a dense work queue and hides an operator-edited or AI-suggested summary.

The current browser fixture does not expose the bug because its title, description, text, and
cleaned text are identical.

## Approaches

### 1. Use the existing title with compatibility fallbacks (selected)

Resolve dashboard copy as:

```text
title -> text_cleaned -> text -> localized no-detail copy
```

This matches the product domain, requires no contract change, and keeps older records usable.

### 2. Add a backend `display_title`

This would duplicate a deterministic projection in every API response and require backend/type
changes without adding policy or data. The existing title already is the intended public
operational summary.

### 3. Continue using cleaned reporter text

Moderation safety is important, but it does not justify ignoring the title. The API already returns
the safe projected title for moderated Feedback; the UI should consume that field.

## Behavior

`web/pages/index.vue` uses `item.title` first for each “Work requiring attention” row. If an older or
incomplete response has an empty title, the current cleaned/raw/localized fallback remains.

The row metadata, rating, attachments, Issue link, relative time, destination, and responsive
geometry do not change. The original description remains available on the Feedback detail page and
is not added as a second dashboard line.

## Safety And Compatibility

- Do not reconstruct or clean a title in the browser.
- Treat the API title as the authoritative moderated operational projection.
- Preserve `text_cleaned` before `text` in the fallback order.
- Treat an empty string as absent, matching the current truthy fallback behavior.
- Make no backend, database, API, localization, permission, or analytics change.

## Verification

- Modify a dashboard-only E2E fixture so title differs from description/text.
- Assert the dashboard work row contains the curated title and does not contain the reporter
  description.
- Exercise the same assertion in desktop and mobile projects.
- Add a missing-title case that proves `text_cleaned` remains the first compatibility fallback.
- Run web unit tests, Nuxt typecheck/build, focused responsive Playwright, and inspect the resulting
  dashboard screenshots.

## Non-Goals

- Editing titles from the dashboard.
- Changing Feedback detail, inbox, moderation, or AI behavior.
- Adding a second line of reporter text to the worklist.
- Deployment, staging changes, production, or infrastructure work.
