# Fidibeki Widget Session State Design

**Date:** 2026-07-22  
**Status:** Approved  
**Scope:** Widget state and recurrence vertical slice within product Slice 3

## Problem

The widget currently writes a boolean suppression flag to `sessionStorage` when a trigger opens.
That means merely seeing the form prevents it from returning after refresh. Its page identity also
includes the query string, so `/chart?asset=BAI` and `/chart?asset=BIC` are treated as different
pages even though they are the same product surface.

Submission and attachment upload are also handled as one success boundary. If the feedback API
accepts the feedback but an attachment upload fails afterward, the widget reports that the feedback
could not be sent even though the feedback record already exists.

## Decisions

### Page identity

Widget state is scoped by project, matched rule, and page pathname. Query parameters and fragments
are excluded:

```text
https://example.com/chart?asset=BAI#price -> /chart
https://example.com/chart?asset=BIC       -> /chart
https://example.com/portfolio             -> /portfolio
```

Browser origin remains an implicit boundary because Web Storage is origin-scoped. Trailing slashes
remain meaningful: `/chart` and `/chart/` are not normalized into one route.

This normalization applies only to widget state. Existing exact, prefix, and regular-expression
targeting continues to evaluate the full current URL so teams can intentionally target query values.

### Storage model

State is stored as versioned JSON in `localStorage`, keyed by project, rule, and normalized page.
A random session identifier in `sessionStorage` distinguishes the current top-level browsing
session. The record stores when the transition occurred; the current rule configuration determines
whether that record still suppresses the widget.

```json
{
  "version": 1,
  "state": "submitted",
  "session_id": "anonymous-random-id",
  "occurred_at": "2026-07-22T12:00:00.000Z"
}
```

The state vocabulary is `new`, `submitted`, `minimized`, `dismissed`, and `snoozed`. This delivery
actively writes `submitted` and `dismissed`; `new` is represented by no record. The remaining states
are reserved for the compact launcher and snooze work in the rest of Slice 3.

Malformed, unavailable, or blocked storage fails open: the widget may appear, but it must never
break the host page. State IDs are anonymous and contain no reporter or device identity.

### Recurrence configuration

Each widget rule exposes a `recurrence_mode` and optional `recurrence_days`:

- `cooldown` is the default and suppresses a submitted page across sessions for the configured
  number of days. Existing and new rules default to 30 days.
- `session` suppresses only during the top-level browsing session that submitted the feedback.
- `never` keeps the submitted page suppressed until storage is cleared or the rule identity changes.

`recurrence_days` is required only for `cooldown` and accepts 1 through 3650 days. The project rule
editor presents these choices as **After a period**, **Next browser session**, and **Never show
again**, with a numeric days input for the first choice.

The browser stores `occurred_at`, not a fixed expiry. On every evaluation the widget applies the
latest rule configuration. Changing a rule from 30 to 7 days, or from `never` to `session`, therefore
affects existing browser records as soon as the refreshed configuration is loaded. An expired record
is removed and the page becomes eligible again. The next mount creates a fresh
`client_feedback_id`, so a changed opinion is a new feedback record rather than a retry.

### State transitions

- Matching a rule, arming a trigger, or mounting the form does not write suppression state.
- Closing the form explicitly writes `dismissed` for the current session and page.
- A non-success feedback response leaves the state unchanged and keeps retry available.
- The first successful feedback response writes `submitted` immediately, before attachment uploads.
- Refreshing or changing only query/hash on that page in the same session does not mount the widget.
- A different pathname remains eligible for its own matching rule and state.
- Under the default policy, a new tab or later browser session remains suppressed until 30 days
  after submission, then becomes eligible again.
- Under `session`, a new top-level tab is eligible; under `never`, no time-based expiry applies.
- Explicit dismissal suppresses only the current session; submission recurrence does not apply to a
  user who closed the form without sending feedback.

The loader owns persistence and passes explicit `onSubmitted` and `onDismissed` callbacks into the
widget UI. This keeps the form independent from storage details and makes state transitions directly
testable.

### Partial attachment failure

Once the feedback API returns success, the feedback is submitted. Attachment uploads continue, but
a later attachment failure is a partial success rather than a failed feedback submission. The form
shows that the feedback was received and that an attachment could not be uploaded, reports the
technical error, and does not offer a second feedback submission from the same mounted form.

This prevents duplicate operator work and matches the backend acceptance boundary used by the
future `submitted` funnel event.

## Compatibility

A compatible migration adds recurrence fields to `widget_rules`. Project CRUD and public widget
configuration expose the fields additively, while existing rules receive the 30-day default. The
backoffice editor can change the policy with the existing widget-rule update permission. Existing
targeting behavior and stored feedback are unchanged.

Existing storage booleans use a different key and are ignored. The new key includes a schema
version, allowing later state fields to be added without guessing old value shapes.

Production remains untouched. Staging deployment is permitted only after local widget verification.

## Verification

Unit tests cover:

- page identity ignores query and hash while preserving pathname and trailing slash;
- state isolation by project, rule, and pathname;
- `session`, `cooldown`, and `never` recurrence semantics at time boundaries;
- current-session suppression for `dismissed`;
- expired record cleanup;
- no suppression for missing, malformed, or unavailable storage records, or for stale-session
  records when the rule uses `session` recurrence.

Backend and backoffice tests cover:

- migration defaults and up/down/up compatibility;
- recurrence validation and additive project/public configuration responses;
- editing all three recurrence modes and the conditional days input.

Widget browser tests cover:

- opening the form without submitting, then refreshing, shows it again;
- a failed feedback request remains retryable and does not suppress;
- `/chart?asset=BAI` submission followed by refresh/navigation to `/chart?asset=BIC` does not show
  the widget again and creates only one feedback request;
- the default 30-day cooldown suppresses a new tab, then allows a new submission after expiry;
- session recurrence allows the same page in a new top-level tab;
- a different pathname remains eligible;
- accepted feedback followed by attachment failure shows partial success and remains suppressed.

Typecheck, unit coverage, build, and Playwright must pass before a widget-only staging deployment and
a controlled staging smoke. No production service or configuration is changed.
