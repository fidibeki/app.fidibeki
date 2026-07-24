# Mobile Feedback Filters Design

**Issue:** #72
**Status:** Approved under the founder's standing authorization for recommended decisions
**Environment boundary:** Local product work only; no deployment, production, or infrastructure
work

## Problem

The Feedback queue currently turns its complete desktop filter toolbar into a horizontally
scrolling row below `1100px`. At a `390px` viewport, search, project, and status are visible, while
rating, evidence, and the advanced-filter trigger begin outside the viewport. The controls still
exist, but there is no visible affordance that the row can be swiped. A mobile operator can
therefore miss material triage dimensions even though the backend and URL contract already support
them.

This slice improves discovery and operation of the existing filters. It does not add a filter,
change query semantics, or introduce another data-loading path.

## Approaches

### 1. Mobile filter panel with one explicit entry point (selected)

Keep search immediately available and replace the horizontally scrolling select row with one
compact **Filters** disclosure on mobile. The disclosure exposes every existing structured filter,
shows how many are active, and reuses the current reactive state, URL synchronization, and clear
action.

This preserves the dense Dispatch layout while making the complete filter set discoverable and
testable.

### 2. Add a fade and scroll hint to the current row

This is the smallest visual change, but rating and evidence remain off-screen and the primary action
still depends on a gesture that is easy to miss. It treats the symptom rather than the workflow.

### 3. Wrap every filter into a permanent mobile grid

All controls would be visible, but the queue would begin below several rows of configuration on
every visit. That is a poor trade-off for repeated triage, especially on short viewports.

## Responsive Contract

### Desktop and tablet

At widths above `700px`, retain the existing toolbar order and geometry:

1. search;
2. project;
3. status;
4. rating;
5. evidence;
6. advanced filters.

No desktop behavior, label, query parameter, or selection semantics change.

### Mobile

At `700px` and below:

- search remains directly visible and can shrink within the available row;
- one native disclosure summary shows a filter icon, localized **Filters** label, and an active
  count when the count is greater than zero;
- the horizontal toolbar no longer scrolls;
- the disclosed panel contains project, status, rating, evidence, moderation, Issue relationship,
  AI state, sentiment, from date, and to date;
- the panel is constrained to the viewport, uses internal vertical scrolling on short screens, and
  never creates document-level horizontal overflow;
- **Clear filters** uses the existing reset operation;
- **Done** closes the disclosure and restores focus to its summary.

The panel is non-modal. Operators may close it through the summary or Done action, and the queue
updates immediately as each native control changes.

## State And URL Behavior

The desktop controls and mobile panel bind to the same existing refs. Only one responsive set is
rendered visually at a time; CSS-hidden controls are absent from layout and accessibility
navigation.

The active count includes non-empty values for:

- project;
- status;
- rating;
- evidence;
- moderation;
- Issue relationship;
- AI state;
- sentiment;
- from date;
- to date.

Search and sort are excluded because each already has a persistent, directly visible control.

Existing watchers remain authoritative for URL updates and reloads. There is no draft/apply state:
selecting a value updates the same query parameters and result set as desktop. Browser history,
queue links, clear behavior, and the empty state remain unchanged.

## Accessibility And Localization

- Use native `details`/`summary`, `label`, `select`, `input`, and `button` semantics.
- Preserve every current accessible filter name.
- The visible active-count badge is included in the summary's accessible name.
- Closing through Done returns focus to the summary so focus is never left inside hidden content.
- Focus rings use the existing application tokens.
- Add concise English and Portuguese strings for Filters and Done; reuse the current filter labels
  and Clear filters copy.
- The layout supports long Portuguese labels at `390px` and falls back to one column on very narrow
  viewports.

## Implementation Boundary

Keep the change inside the Feedback page and its message catalog:

- add a computed active structured-filter count;
- preserve the current desktop controls in a desktop-only wrapper;
- add a mobile-only disclosure bound to the same state;
- add the close-and-refocus helper;
- replace mobile horizontal overflow with a stable search/disclosure row and a bounded filter panel.

No backend, migration, API type, cache, permission, analytics, or provider change is required.

## Error Handling

Filtering continues to use the current list request and existing loading, empty, and retry states.
Changing a mobile control may therefore produce the same recoverable request error as desktop. The
panel itself does not manufacture a separate error surface or retain unapplied values.

## Verification

- A focused desktop Playwright case proves the existing rating, evidence, and advanced controls
  remain in their current toolbar and update the URL/results.
- A focused mobile case opens the disclosure, reaches all ten structured filters without horizontal
  document overflow, applies representative primary and advanced filters, observes active-count and
  URL synchronization, clears them, closes the panel, and verifies focus restoration.
- Geometry assertions cover `390x844` and a short mobile viewport, including internal panel
  scrolling and containment.
- English and Portuguese labels are exercised through the existing localization suites.
- Run web unit tests, Nuxt typecheck/build, focused E2E, and the complete responsive Playwright
  suite.

## Non-Goals

- New filter dimensions or saved filter presets.
- A draft/apply transaction, backend query endpoint, or filter analytics.
- Changing the desktop toolbar or queue routing model.
- Redesigning Feedback rows, bulk actions, or the inspector.
- Deployment, staging changes, production, or infrastructure work.
