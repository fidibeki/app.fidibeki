# Fidibeki Mobile Feedback Sheet Redesign

**Date:** 2026-07-30
**Status:** Implemented and validated
**Scope:** `@fidibeki/mobile` and the mobile example application
**Environment:** Staging only

## Context

The first mobile SDK slice established the correct trust and product boundaries: the host
application owns the feedback trigger, Fidibeki renders a native sheet, and the configured Widget
Definition supplies response blocks, localization, recurrence, privacy, and supported appearance
values.

The current `FeedbackSheet` is functionally complete but visually behaves like a generic full-page
form. It gives every field excessive height and spacing, renders rating as numeric buttons instead
of the stars used by the web widget, exposes optional sections at the same visual priority as
required questions, and lets the submit action move with the scroll content. On a normal phone this
produces a long, sparse form with weak hierarchy.

The current dismiss behavior is also incorrect for a host-owned mobile trigger. Closing the sheet
records `dismissed` in the recurrence engine, sets the SDK to ineligible, and disables the host
trigger. A person who closes without submitting cannot reopen the form.

## Goals

- Replace the current mobile UI with the approved **Compact Progressive** direction.
- Open as a native-feeling bottom sheet occupying 75% of the safe viewport.
- Match web rating semantics with five cumulative stars while preserving numeric `1..5` values.
- Use the configured/resolved accent color for every selected state, including stars.
- Keep configured response blocks dynamic and preserve all existing submission contracts.
- Reduce optional-section prominence without hiding customer-configured response blocks.
- Keep header and primary action stable while the form content scrolls.
- Allow immediate reopen after closing without submission and preserve the in-memory draft.
- Preserve accessibility, keyboard safety, reduced motion, dark mode, localization, privacy, and
  screenshot boundaries.

## Non-Goals

- No injected launcher, floating button, tab, or automatic presentation.
- No backend, capture-session, Widget Definition, or submission-payload changes.
- No new response-block types or mobile-only configuration schema.
- No multi-step form, gesture-driven detents, third-party bottom-sheet dependency, or WebView.
- No production deployment, registry publication, infrastructure, or provider work.
- No change to recurrence after a successful submission.

## Approved Direction

### Sheet Frame

The provider continues to mount one modal surface, but it renders a custom bottom-aligned sheet
instead of an operating-system page sheet that visually fills the screen.

- A neutral scrim separates the form from the host application.
- The sheet opens at 75% of the safe viewport with stable top corners and a visible grabber.
- The header contains the localized prompt and a 44-point close control.
- The form body is the only scrolling region.
- The submit footer remains fixed to the bottom of the sheet.
- When the keyboard opens, the sheet uses the full safe height available above it so the focused
  control and validation message cannot be occluded.
- Rotation, safe-area changes, and long localized copy recompute the available layout without
  overlap.
- Reduced-motion mode removes nonessential transitions while retaining the same final geometry.

The implementation must use React Native primitives already available to the package. It must not
add a bottom-sheet library or native module solely for this redesign.

### Visual System

The existing appearance precedence remains authoritative:

`host theme override -> Widget Definition appearance -> light/dark fallback`

The resolved accent drives:

- filled rating stars;
- selected reaction/single-choice chips;
- focus treatment;
- links and compact action affordances;
- the primary submit button.

There is no fixed amber rating color. Unselected stars and controls use neutral surface, border, and
muted-text tokens. Validation errors continue to use the resolved danger token rather than the
accent. `accent_foreground` remains the submit label color. Light and dark themes must meet readable
contrast without inventing a second brand palette.

Typography stays native and compact. The sheet uses a clear prompt, small field labels, normal body
copy, and no hero-sized text. Required fields use a visual asterisk plus accessible required state;
the UI does not append a large `(Required)` string to every label.

### Response Blocks

Every configured response block remains visible in definition order. Optional response blocks are
not collapsed merely because they are optional; they represent questions intentionally selected by
the customer.

- `rating` renders five equal 44-point star targets. Selecting value `n` fills stars `1...n` with
  the resolved accent. Each target remains a native radio with a localized label such as
  `4 out of 5`; the submitted value remains the number `4`.
- `reaction` and `single_choice` render compact wrapping chips with a clear selected state.
- `free_text` renders a bounded multiline input with a compact initial height and visible focus
  treatment.
- Labels and option copy continue to come from the resolved localization, with current fallbacks.
- Field-level validation remains localized and appears directly below the affected control.

## Progressive Disclosure

Progressive disclosure applies only to optional SDK sections around the configured response
blocks:

- `allow_comment` starts as an **Add more context** row. Activating it expands the localized comment
  input in place and preserves its value if collapsed and reopened.
- Contact collection starts as a **Get updates** row only when both `collect_contact` and a valid
  controller notice are present. Activating it reveals the email input, consent control, controller
  notice, and privacy-policy link for the rest of the current draft. It does not silently collapse
  an enabled consent section.
- Screenshot support remains a compact **Add screenshot** row and invokes only the host-owned
  `screenshotProvider`. A selected screenshot replaces the row with an attached state and a remove
  action.
- An applicable AI-processing disclosure and privacy-policy link remain visible before submission.
  They are not hidden behind optional interaction.

Collapsed optional data follows the current submission rules. Hidden contact values must never be
submitted after contact collection becomes unavailable. Screenshot cancellation or provider
failure does not disturb the rest of the draft.

## Dismiss And Reopen

Closing an explicitly opened mobile sheet is not a recurrence event.

`dismiss()` must:

1. set the sheet to not visible;
2. return the SDK status to `ready` while the capture session is valid;
3. keep `eligible=true`;
4. retain the current in-memory form and prepared screenshot;
5. avoid writing `dismissed` to recurrence storage.

Calling `open()` again reuses the same valid capture session and draft without a network request.
Only a successful feedback acceptance records `submitted` and applies the configured
`session`, `cooldown`, or `never` recurrence rule.

Existing SDK versions may already have stored `dismissed` records under the current recurrence key.
On the first eligibility read after upgrade, the recurrence engine must treat `dismissed` as a
legacy invalid state, remove that record, and return eligible. The parser may retain
`dismissed` only for this migration path; new code must never write it.

Session expiry, terminal access denial, an incompatible definition refresh, or a new eligible
submission retains the existing reset/reconciliation behavior. This redesign does not introduce a
public discard-draft API.

## Submission And States

- Submitting disables the primary action without changing its dimensions and presents a compact
  loading state.
- Client validation scrolls to and focuses the first invalid field when feasible.
- Retryable submission errors keep every response, expanded optional section, contact value,
  consent state, and prepared screenshot.
- Configuration refresh continues to reconcile compatible response blocks and clear consent when
  its notice changes.
- Accepted feedback replaces the form with a compact success state.
- A later attachment failure keeps the accepted-feedback success boundary and displays the existing
  attachment warning.

## Component Boundaries

The public API remains unchanged:

- `FidibekiProvider` mounts the sheet and owns session/recurrence state.
- `useFidibekiFeedback()` remains the host-controlled `open`, `close`, status, error, and eligibility
  interface.

Internally, the redesign may extract response-field and sheet-frame presentation from
`mobile/src/sheet.tsx` to keep state orchestration readable. Those components receive resolved
colors, localized copy, values, errors, and callbacks; they do not fetch data or own submission
state. The existing form-state, screenshot, client, and recurrence modules remain the behavioral
sources of truth.

No new dependency is required for icons. The familiar star glyph and simple native text/icon
controls avoid adding `react-native-svg` or an icon package to the SDK's peer surface.

## Accessibility

- Close, rating, chips, disclosure rows, screenshot actions, and submit use at least 44-point touch
  targets.
- Rating remains one accessible radiogroup with five radio controls and cumulative visual fill.
- Selected, disabled, expanded, invalid, and busy states are exposed through native accessibility
  properties.
- Error and success messages retain the appropriate live-region behavior.
- Focus order follows definition order and continues through expanded optional sections.
- Dynamic Type and long localized labels may wrap but cannot overlap adjacent controls.
- Color is never the only selected/error indicator; border, fill, accessibility state, and copy
  carry the same meaning.

## Validation

### SDK Tests

- iOS and Android render the 75% sheet frame, fixed header/footer, and scrollable body.
- Keyboard events expand to the safe available height and preserve the focused field.
- Rating renders stars, fills cumulatively, exposes `n out of 5`, and submits the same numeric value.
- Theme/appearance precedence applies the resolved accent to every selected state in light and dark
  mode.
- Reaction, single-choice, free-text, localization fallback, long copy, and validation remain
  functional.
- Comment disclosure expands/collapses without losing its value; contact disclosure remains open
  after activation and follows explicit consent.
- Contact removal/configuration refresh cannot submit hidden contact data.
- Screenshot add/cancel/error/replace/remove flows retain the host-owned permission boundary.
- Close followed by open preserves eligibility, draft, prepared screenshot, and capture session and
  performs no recurrence storage write.
- A legacy stored `dismissed` record is removed and cannot suppress session, cooldown, or never
  recurrence after the upgrade.
- Successful submission still records recurrence and suppresses according to
  `session/cooldown/never`.
- Reduced motion, safe areas, rotation, loading, success, retry, and attachment-warning states pass.

### Runtime And Visual Validation

- Mobile package tests, typecheck, build, and dry-run package checks pass.
- The Expo example compiles for iOS and Android without adding a launcher.
- Simulator/emulator screenshots cover a compact phone and a standard phone in light and dark mode.
- Runtime evidence proves host-trigger open, close, reopen with preserved draft, submit, screenshot
  completion, and post-submit recurrence.
- No control overlaps, no text escapes its container, and the primary action remains reachable with
  the keyboard open.

## Acceptance Criteria

The redesign is complete when the current sparse full-page UI is replaced by the approved compact
75% sheet, web-consistent accent-colored stars are functional and accessible, every configured
response block still submits unchanged data, optional SDK sections use the approved disclosure
pattern, closing without submission can always reopen with the draft intact, and the full mobile
test/runtime gate passes in staging.
