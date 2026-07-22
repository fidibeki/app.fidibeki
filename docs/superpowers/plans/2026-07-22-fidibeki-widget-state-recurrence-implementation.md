# Fidibeki Widget State And Recurrence Implementation Plan

**Design:** `docs/superpowers/specs/2026-07-22-fidibeki-widget-session-state-design.md`  
**Scope:** Widget state and recurrence vertical slice within product Slice 3  
**Deployment:** Staging only; production remains untouched

## Shared Context

- Page state is scoped by project, rule, and pathname. Query parameters and fragments never create
  a second state identity, while targeting continues to evaluate the full URL.
- A successful feedback API response is the `submitted` boundary. Attachment upload is subsequent
  evidence delivery and cannot turn accepted feedback back into an unsent submission.
- Rules support `cooldown`, `session`, and `never`; `cooldown` defaults to 30 days and accepts 1-3650.
- Stored state contains an anonymous session ID and occurrence time. The latest rule configuration,
  rather than a frozen client expiry, decides whether state is still active.
- Dismissal suppresses only the current top-level browsing session. Merely opening the widget never
  suppresses it.
- Existing feedback, targeting, project, and widget-rule contracts remain compatible through
  additive fields and defaults.
- Each task uses a focused failing test before implementation and production is never touched.

## Task 1: Persist Rule Recurrence Safely

**Depends on:** None

**Outcome:** Migration 47 adds a constrained recurrence mode and conditional days value to every
widget rule, backfilling existing rules to a 30-day cooldown without modifying any other rule data.

**Acceptance:** Migration up/down/up passes and database tests prove defaults, valid modes, the
1-3650 day boundary, and rejection of inconsistent mode/day pairs.

## Task 2: Expose And Validate Recurrence End To End

**Depends on:** Task 1

**Outcome:** Tenant widget-rule CRUD and public widget configuration return the additive recurrence
fields; create/update validation normalizes unused days to null and defaults omitted values to a
30-day cooldown.

**Acceptance:** Focused Go unit and HTTP integration tests create, read, update, list, and publicly
fetch each mode, reject invalid combinations, and confirm legacy request bodies receive the default.

## Task 3: Configure Recurrence In Dispatch

**Depends on:** Task 2

**Outcome:** The existing project rule editor offers After a period, Next browser session, and Never
show again, with a constrained days input only when applicable and the 30-day default for new rules.

**Acceptance:** Web unit/type tests cover input normalization and one desktop/mobile Playwright test
edits all modes, verifies the request bodies, and confirms the controls remain compact and readable.

## Task 4: Replace Boolean Suppression With Versioned State

**Depends on:** Task 2

**Outcome:** The widget stores versioned state in `localStorage`, uses an anonymous top-level session
ID, ignores query/hash in page identity, and evaluates submitted/dismissed records against current
rule recurrence without throwing when storage is unavailable.

**Acceptance:** Widget unit tests prove page isolation, all recurrence modes, exact time boundaries,
expired cleanup, malformed data recovery, storage failure behavior, and fresh eligibility after
expiry.

## Task 5: Record Only Meaningful Widget Transitions

**Depends on:** Task 4

**Outcome:** The loader stops suppressing on trigger open. The form reports explicit submitted and
dismissed callbacks, which the loader persists for the current project/rule/page evaluation.

**Acceptance:** A browser test proves open+refresh shows again, close+refresh stays dismissed for the
session, failed submit remains retryable, and accepted submit suppresses refresh and query-only
navigation.

## Task 6: Separate Feedback Success From Attachment Failure

**Depends on:** Task 5

**Outcome:** After feedback acceptance, attachment failure produces a partial-success message,
reports the technical error, prevents duplicate submission, and retains submitted suppression.

**Acceptance:** A browser test accepts feedback, fails an attachment PUT or completion request,
observes the partial-success UI and one feedback POST, then reloads and confirms the widget remains
suppressed.

## Task 7: Prove Recurrence Across Pages, Tabs, And Time

**Depends on:** Tasks 3-6

**Outcome:** The public widget and Dispatch configuration agree on session, cooldown, and never
behavior across realistic navigation.

**Acceptance:** Playwright covers `/chart?asset=BAI` to `/chart?asset=BIC`, a different pathname, a
new top-level tab, 30-day expiry with a fresh client feedback ID, and never mode without relying on
private implementation details.

## Task 8: Regress And Validate In Staging

**Depends on:** Tasks 1-7

**Outcome:** Backend, web, and widget remain compatible and the state/recurrence journey works on the
deployed staging services.

**Acceptance:** Backend tests and migration up/down/up, web unit/type/build/Playwright, widget
unit/coverage/type/build/Playwright, desktop/mobile visual checks, and one controlled staging smoke
all pass. Only staging API, web, and widget services are deployed; production remains untouched.
