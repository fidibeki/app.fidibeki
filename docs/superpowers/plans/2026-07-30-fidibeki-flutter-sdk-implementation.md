# Fidibeki Flutter SDK Implementation Plan

> **Scope mode:** LOCKED
>
> **Design:** `docs/superpowers/specs/2026-07-30-fidibeki-flutter-sdk-design.md`
>
> **Issue:** [#162](https://github.com/fidibeki/app.fidibeki/issues/162)
>
> **Boundary:** Flutter iOS/Android client work only. Reuse the deployed mobile capture contract.
> No backend, production, infrastructure, provider, registry publication, Swift SDK, Kotlin SDK,
> Flutter Web, desktop, or unrelated React Native changes.

## Durable Decisions

- Flutter is a pure, independent runtime. It never embeds React Native or JavaScript.
- The public integration is `FidibekiFeedbackController` plus `FidibekiFeedbackHost`; the customer
  owns the trigger and capture-session backend.
- The core package has no persistence, picker, permission, analytics, or logging plugin. The
  shared-preferences adapter is a separate optional package.
- The existing `fmc_` session, feedback, exact-retry, attachment-completion, consent, and
  recurrence contracts remain unchanged.
- React Native and Flutter share sanitized wire-contract fixtures, not implementation internals.
- Development and CI pin Flutter 3.44.7/Dart 3.12.2. Packages remain unpublished.
- Each task ends with focused tests and a commit. Native runtime proof is required before closure.

## Task 1: Establish The Flutter Contract Tracer

Install the pinned Flutter toolchain outside the repository and create the Dart pub workspace,
core package, optional shared-preferences adapter package, and actual host example. Add the first
sanitized mobile-capture fixtures for a valid session, malformed schema, screen normalization, and
recurrence key. Make both Flutter and the existing React Native tests consume the fixture boundary.

Implement strict session/configuration models, the host session-provider callback, controller
initialization, and a minimal `FidibekiFeedbackHost` frame. The example must become ready from the
local session endpoint, open a 75% sheet only from its own trigger, close, and reopen without a
second session request.

**Acceptance:** Format/analyze/unit/widget tests pass; React Native fixture compatibility remains
green; the example compiles far enough to prove the package integration; one focused test proves
host trigger -> session -> open -> close -> reopen with stable session and eligibility.

## Task 2: Submit One Complete Feedback

Extend the shared fixtures with canonical request/response and error cases. Implement form state,
rating and free-text blocks, localized required validation, one generated client feedback ID,
strict JSON serialization, the mobile feedback client, accepted success, and exact retry. Add the
first recurrence behavior: only accepted feedback writes `submitted`; opening/closing never does.

Exercise the vertical path against the local smoke server from the example:

`host trigger -> rating/text -> submit -> accepted Feedback -> success -> recurrence suppression`

Concurrent submit attempts must share one request. Ambiguous retries must reuse the exact ID and
body. Public errors must be typed and redact request content and bearer credentials.

**Acceptance:** Contract/unit/widget tests prove numeric rating compatibility, validation, one
accepted Feedback, exact retry, concurrent coalescing, safe errors, and post-submit suppression.
The local smoke server observes one feedback request and no Issue creation behavior changes.

## Task 3: Complete Dynamic Forms And Consent

Render every configured response block in definition order: cumulative rating stars, reaction and
single-choice chips, and bounded free text. Add Compact Progressive comment and reporter-contact
disclosures, complete versioned consent assertions, AI disclosure, privacy links, English/
Portuguese system copy, customer-copy locale fallback, and host/definition/light-dark theme
precedence.

Implement configuration refresh and draft reconciliation. Compatible answers survive; removed or
changed blocks do not. Any controller-notice or locale change clears reporter consent and requires
an explicit new grant. Hidden or unavailable contact values never enter a request.

**Acceptance:** Focused form/widget/contract tests prove every response type, definition order,
localization, theme resolution, disclosure persistence, consent provenance, hidden-data omission,
configuration reconciliation, and numeric/wire compatibility with React Native.

## Task 4: Complete The Screenshot Boundary

Implement the explicit host-owned screenshot provider with cancellation, PNG/JPEG/WebP allowlist,
10-MiB bound, byte/declaration consistency, disposal, and no local-URI transmission. Add screenshot
declaration to the frozen feedback request, upload only to the returned presigned grant, and call
the existing attachment completion endpoint.

Preserve the accepted-feedback boundary: an upload or completion failure displays success plus an
attachment warning and never retries feedback as a new resource. Reject unsafe redirects and
non-local insecure endpoints. The SDK must request no camera, media, storage, or photo-library
permission.

**Acceptance:** Unit/widget/contract tests cover cancellation, provider failure, size/type/read
mismatch, declaration, upload, completion, cleanup, unsafe redirects, exact retry, accepted
feedback plus attachment warning, and absence of URI/permission leakage.

## Task 5: Harden Lifecycle, Recurrence, And Privacy

Complete session/cooldown/never recurrence, anonymous app-session handling, 30-minute continuous
background rotation, cold launch, cooldown wake-up, corrupt storage, and legacy dismissed-record
cleanup. Normalize query, fragment, repeated separator, and trailing-slash screen variants to one
key.

Harden expiry, access denial, configuration change, malformed terminal responses, controller
disposal, ancestor rebuilds, late async completion, session replacement, and retry races. Capture
tokens remain memory-only; storage contains recurrence only; error strings, debug output, and the
example contain no token, reporter content, screenshot bytes, real tenant data, analytics, or
identifier.

**Acceptance:** Focused lifecycle/security tests prove every recurrence mode and boundary, stale
session rejection, safe disposal/rebuild, exact request ownership, redaction, storage minimization,
and shared failure-fixture compatibility.

## Task 6: Reach Compact Progressive UI Parity

Complete the custom overlay for Material and Cupertino hosts: neutral scrim, safe-area-aware 75%
sheet, keyboard expansion, fixed grabber/header/footer, scroll-only body, accessible close and
submit actions, cumulative accent stars, compact chips, progressive sections, screenshot states,
loading, retry, success, and attachment warning.

Add focus/scroll to the first invalid field, system-back behavior, reduced motion, orientation
handling, large text, long customer copy, readable accent foreground, semantic selected/required/
expanded/invalid/busy/live states, and stable 44-point targets. Add golden coverage for compact and
standard phones in light/dark mode and for the documented locale fallback.

**Acceptance:** Widget and golden tests pass with no clipping or overlap. Inspected goldens match
the approved React Native hierarchy while retaining Flutter-native semantics on iOS and Android.

## Task 7: Prove The Packages And Native Example

Finish public documentation, API docs, changelogs, licenses, package metadata, and the local host
example. Add package gates for format, fatal analyze, at least 85% line coverage, `dart doc`, lowest
and current dependency resolution, and safe `dart pub publish --dry-run` for both packages without
uploading.

Build and run the example on an Android emulator and iOS simulator. Inspect light, dark, compact,
standard, keyboard, large-text, selected, screenshot, error, success, and recurrence states.
Exercise close/reopen with the same session/draft and a complete local submission/upload. Confirm
logs and manifests contain no forbidden permission or sensitive value.

**Acceptance:** Every package gate and both native builds pass. Runtime evidence proves the full
capture lifecycle and accepted-feedback boundary on Android and iOS without backend/deployment
changes.

## Task 8: Add Isolated CI And Close The Delivery Loop

Add a dedicated Flutter workflow with pinned toolchain, public caches, Linux Android/package jobs,
and a macOS iOS compile job. Keep React Native CI unchanged except for its shared-fixture
compatibility assertion. Do not connect Flutter work to production promotion, deployment, or
registry publication.

Run the complete local gate, review the final diff independently for correctness/security and
package API stability, push the implementation commits, and wait for Flutter, React Native, and
Security CI. Update the product TODO, Flutter documentation, and the dedicated GitHub issue with
exact test/runtime/CI evidence. Close the issue only after all acceptance criteria are demonstrated.

**Acceptance:** Local and remote checks are green, the worktree is clean, evidence is recorded, the
packages remain unpublished, and no production, deployment, backend, or infrastructure action
occurred.
