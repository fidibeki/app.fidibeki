# Fidibeki Flutter SDK

**Date:** 2026-07-30
**Issue:** [#162](https://github.com/fidibeki/app.fidibeki/issues/162)
**Status:** Approved under the founder's explicit pre-approval
**Scope:** Flutter applications on iOS and Android
**Rollout:** Local implementation and validation only; publish-ready but not published

## Context

Fidibeki currently has one mobile client SDK: `@fidibeki/mobile`, implemented with React Native and
validated on iOS and Android. The repository does not contain an independent Apple/Swift, Android/
Kotlin, Dart, or Flutter SDK. The original mobile design deliberately proved the cross-platform
capture contract in React Native before duplicating it in another runtime.

That contract is now stable. A host-owned trigger opens a native Compact Progressive sheet, a
trusted customer backend mints a short-lived capture session, accepted feedback remains independent
from Issues, and an explicitly selected screenshot follows the existing presign/upload/completion
pipeline. Flutter should become the second cross-platform client without changing that backend or
embedding the React Native runtime.

Issue #136 remains closed because its approved implementation scope was the React Native slice.
Flutter delivery must be tracked as a new product issue rather than rewriting the historical
acceptance evidence for #136.

## Outcome

Deliver a publish-ready Flutter package with complete functional and visual parity:

`host-owned Flutter trigger -> Fidibeki native sheet -> existing mobile API -> Feedback inbox`

The first Flutter version supports iOS and Android only. Flutter Web, macOS, Windows, Linux,
independent Swift/Kotlin SDKs, registry publication, production deployment, and backend or
infrastructure changes are outside this slice.

## Selected Approach

Build a pure Dart/Flutter implementation with an idiomatic public API and no React Native or
JavaScript runtime dependency. React Native and Flutter remain separate client implementations,
but both execute sanitized shared contract fixtures covering the public wire format and lifecycle
rules.

This is preferred over:

- generated networking as the primary architecture, because OpenAPI generation does not capture
  recurrence, exact retry, accepted-feedback attachment outcomes, or UI lifecycle;
- wrapping the React Native SDK, because that would force Flutter customers to ship two UI
  runtimes and make installation, debugging, binary size, and native lifecycle behavior worse;
- a headless-only package, because Fidibeki needs one accessible, tested feedback experience rather
  than requiring every customer to rebuild the form and privacy controls.

## Package Layout

Add a self-contained Dart pub workspace under `flutter/`:

```text
flutter/
  pubspec.yaml
  packages/
    fidibeki_flutter/
    fidibeki_flutter_shared_preferences/
  example/
```

`fidibeki_flutter` is the core package. It depends on Flutter primitives and a bounded HTTP client,
but no persistence, picker, camera, photo-library, analytics, logging, or native permission plugin.

`fidibeki_flutter_shared_preferences` is a small optional adapter package. Keeping it separate
allows hosts to use secure storage, a database, their existing preferences layer, or an in-memory
adapter without pulling a platform plugin into the core package.

The example application consumes both packages by workspace path. All packages contain complete
metadata, README, CHANGELOG, LICENSE linkage, public API documentation, and publish-safe manifests.
They remain `publish_to: none` until explicit publication authorization; the release gate uses a
temporary manifest or equivalent dry-run mechanism to prove publishability without uploading.

## Supported Runtime

Development and CI pin Flutter 3.44.7 and Dart 3.12.2, the current stable documentation baseline.
The package constraints are:

```yaml
environment:
  sdk: ">=3.12.0 <4.0.0"
  flutter: ">=3.44.0"
```

The v1 platform floor follows Flutter's current supported mobile matrix:

- Android API 24 through 37;
- iOS 13 through 26.

Implementation uses only APIs available at the declared lower bounds. CI tests the pinned current
stable version and the resolved lowest allowed dependency versions. Flutter tooling is installed
into a repository-external cache; generated SDK files and developer machine paths are not
committed.

References:

- <https://docs.flutter.dev/reference/supported-platforms>
- <https://dart.dev/tools/pub/pubspec#sdk-constraints>
- <https://dart.dev/tools/pub/workspaces>

## Public API

The primary integration is controller plus host widget:

```dart
final feedback = FidibekiFeedbackController(
  apiBaseUrl: Uri.parse('https://widget.fidibeki.com'),
  appVersion: '1.2.3',
  osVersion: Platform.operatingSystemVersion,
  screen: '/checkout/review',
  locale: const Locale('pt', 'PT'),
  sessionProvider: hostBackend.createCaptureSession,
  storage: appStorage,
  screenshotProvider: appScreenshotPicker,
);

MaterialApp(
  builder: (context, child) => FidibekiFeedbackHost(
    controller: feedback,
    child: child ?? const SizedBox.shrink(),
  ),
  home: ProductScreen(feedback: feedback),
);

IconButton(
  onPressed: feedback.eligible ? feedback.open : null,
  icon: const Icon(Icons.chat_bubble_outline),
);
```

### `FidibekiFeedbackController`

The controller is a `ChangeNotifier` with:

- immutable configuration for API base URL, screen, app/OS metadata, device locale, host callbacks,
  optional app-session ID, and optional theme overrides;
- observable `status`, `eligible`, `error`, and `nextEligibleAt`;
- `open()`, `close()`, configuration refresh, and `dispose()` lifecycle;
- one in-memory draft and prepared screenshot scoped to the active eligible capture session.

Public statuses are:

`initializing | ready | open | submitting | submitted | ineligible | error`

`open()` reuses the valid session and draft. `close()` only hides the sheet unless submission has
already crossed the accepted-feedback boundary. The controller never creates or places a launcher.

### `FidibekiFeedbackHost`

The host widget owns the overlay insertion point and renders the sheet in response to controller
state. It works under `MaterialApp` and `CupertinoApp` without requiring the customer to call a
Material-only bottom-sheet API. Replacing the host widget or rebuilding ancestors does not reset a
valid controller or duplicate a session request.

### Session Provider

`FidibekiSessionProvider` receives a typed request containing only platform, normalized screen, and
requested display locale. The customer callback calls its own authenticated backend and returns a
strictly parsed `FidibekiCaptureSession`. The tenant API key remains exclusively on that backend.

The parser rejects unknown schema versions, malformed definitions, invalid deadlines, invalid URLs,
unknown response block types, and missing localized controller notices before rendering.

### Storage

`FidibekiStorage` provides asynchronous `read`, `write`, and `delete` operations. The optional
shared-preferences adapter namespaces all keys and stores only anonymous functional recurrence
state. Capture tokens, draft content, reporter email, screenshots, and API responses never enter
persistent SDK storage.

### Screenshot Provider

The optional host callback runs only after an explicit Add screenshot action. Cancellation returns
`null`; a selection returns:

- declared PNG, JPEG, or WebP content type;
- declared byte size up to 10 MiB;
- an asynchronous bounded `Uint8List` reader;
- optional asynchronous disposal.

The SDK does not accept or transmit a local URI. It verifies declared type and size against the
bytes before submission. Camera/photo-library presentation and permission requests remain entirely
host-owned.

## Native Sheet

Flutter reproduces the approved Compact Progressive experience rather than introducing another
design:

- a neutral scrim and custom bottom-aligned sheet at 75% of the safe viewport;
- full safe height while the keyboard is visible;
- stable grabber/header and submit footer, with only the form body scrolling;
- five cumulative rating stars that submit the existing numeric `1..5` value;
- compact wrapping chips for reaction and single-choice blocks;
- bounded multiline fields for free-text blocks;
- progressive disclosure for optional comment and reporter contact;
- compact screenshot add, attached, busy, remove, and warning states;
- accepted-feedback success that remains successful if attachment upload later fails.

The visual precedence is:

`host theme override -> Widget Definition appearance -> light/dark fallback`

The resolved accent drives filled stars, selected chips, links, focus treatment, and the primary
button. Foreground contrast is validated rather than assuming that a dark theme implies a light or
dark label. No third-party bottom-sheet or icon dependency is required.

The sheet shares one Fidibeki visual identity across iOS and Android while adapting safe areas,
keyboard timing, system back navigation, and reduced-motion animation behavior. It supports text
scaling, long English/Portuguese/French/Spanish copy, orientation changes, semantics, focus order,
44-point targets, live error/success announcements, and color-independent selected/error states.

## Form And Consent

The resolved definition remains the source of truth for response order, required state, option
labels, localization, recurrence, screenshot permission, AI disclosure, and reporter contact.

- SDK-owned system messages provide the same English and Portuguese coverage as React Native;
  unsupported system locales fall back to English. Customer-authored definition copy remains
  locale-driven and can render any locale returned by the host-minted session.
- Every configured `rating`, `reaction`, `single_choice`, and `free_text` block renders in order.
- Comment starts collapsed and retains its value if collapsed and reopened.
- Contact starts behind explicit Get updates disclosure and stays open for the draft once selected.
- Reporter email is submitted only with the complete versioned notice, locale, explicit grant, and
  `notify_reporter=true`.
- A definition or controller-notice change reconciles compatible answers and clears old consent.
- Applicable AI text/screenshot disclosure and privacy link remain visible before submission.

Validation scrolls to and focuses the first invalid input where possible. Hidden or no-longer-valid
contact values and responses cannot leak into a request.

## Data Flow

1. The controller calls the host session provider and validates the capture response.
2. It normalizes screen by removing query, fragment, repeated separators, and trailing slash.
3. It checks recurrence under `project + definition + normalized screen`.
4. The host trigger calls `open()` only when `eligible`.
5. Submit validates locally, creates one UUID `client_feedback_id`, and freezes a canonical request.
6. Concurrent submit attempts share the same in-flight operation.
7. An ambiguous retry reuses the exact client ID and body.
8. Backend acceptance immediately records `submitted` recurrence.
9. Any declared screenshot uploads to the returned presigned URL and then completes through the
   existing mobile attachment endpoint.
10. The controller exposes success plus a scoped attachment warning if upload/completion fails.

The Flutter client sends no new fields. Project, definition, application ID, platform, screen, and
context remain server-bound through the host-minted capture session.

## Recurrence

Recurrence matches the corrected React Native behavior:

- opening and closing never suppress;
- only accepted feedback writes `submitted`;
- session recurrence compares an anonymous app-session ID;
- 30 continuous background minutes rotate a generated session ID;
- cooldown uses the definition's day interval;
- never persists until the host clears its configured storage;
- corrupt or legacy `dismissed` records are deleted and treated as eligible;
- query/hash/trailing slash variants of the same screen share a key.

A successful recurrence transition disables eligibility even while an attachment is still
finishing. Reopening after a non-submit close reuses the existing capture session and full draft
without another network request.

## Errors And Recovery

Public `FidibekiException` values contain a stable code, safe message, and `retryable` flag:

`validation | network | sessionExpired | configurationChanged | accessDenied | attachment | server`

Exceptions and `toString()` never include bearer tokens, request headers, reporter data, response
content, or screenshot bytes.

- Retryable transport/server errors preserve the exact request and complete draft.
- `configurationChanged` obtains a new session, reconciles compatible answers, and requires renewed
  consent.
- Expired or denied sessions are discarded and cannot authorize another request.
- Disposal prevents late async completions from mutating widget/controller state.
- Accepted feedback is never rolled back or duplicated because a later attachment operation fails.
- Malformed terminal responses fail safely and never fabricate a successful Feedback ID.

## Privacy And Security

The package collects no advertising, vendor, installation, hardware, contact-list, analytics,
replay, log, or fingerprinting identifier. It performs no automatic screenshot, camera, media,
clipboard, or arbitrary app-state access.

Persistent SDK state contains only anonymous recurrence data. Capture tokens remain memory-only.
The HTTP layer redacts authorization by construction, follows only the bound API/presigned URL
flow, validates HTTPS for non-local endpoints, rejects unsafe redirects for attachment uploads, and
does not expose internal response bodies through public errors.

The example uses only local fake credentials and fixture data. Shared contract fixtures contain no
real tenant keys, capture tokens, domains, reporter information, or production endpoints.

## Shared Contract Fixtures

Add sanitized, versioned JSON fixtures under `contracts/mobile-capture/fixtures/` for:

- valid and invalid capture sessions;
- every response block and localization fallback;
- canonical feedback requests and accepted responses;
- screenshot grants and completion;
- strict unknown-field/schema rejection;
- screen normalization and recurrence scenarios;
- configuration change, expiry, denial, retry, and attachment failure.

Both React Native and Flutter tests load these fixtures. The fixtures assert public compatibility;
they do not force both clients to share internal class names, storage encodings, or UI structure.
Any additive backend contract change updates the fixtures and both SDK test suites in one commit.

## Example Application

The Flutter example is an actual host application, not an SDK-owned demo launcher. It:

- owns an emoji/icon trigger;
- calls a local host session endpoint;
- implements the optional shared-preferences adapter;
- supplies an explicit in-memory screenshot fixture;
- exposes light/dark and English/Portuguese test configurations;
- shows controller status outside the sheet for runtime verification;
- never opens the sheet automatically.

The example must compile and run on an Android emulator and iOS simulator. It is also the source for
README integration snippets and visual/runtime evidence.

## Validation

### Unit And Contract Tests

- strict session/definition/response parsing and serialization;
- UUID/idempotency ownership, canonical exact retry, and concurrent submit coalescing;
- screen normalization and storage-key isolation;
- session/cooldown/never recurrence, app background boundary, cold launch, corrupt storage, and
  legacy dismissal cleanup;
- form reconciliation, validation, consent clearing, locale fallback, and hidden-data omission;
- screenshot cancellation, bounds, byte/type mismatch, upload, completion, disposal, and
  post-acceptance failure;
- token/header/error redaction and unsafe URL/redirect rejection;
- every shared fixture passes in both Flutter and React Native.

### Widget And Golden Tests

- 75% frame, keyboard expansion, fixed header/footer, and scroll behavior;
- rating stars, chips, free text, disclosures, contact, screenshots, loading, errors, success, and
  attachment warning;
- host rebuild, close/reopen draft preservation, focus restoration, system back, and disposal;
- light/dark, host/server appearance precedence, readable accent contrast, reduced motion,
  orientation, compact/standard screens, and large text;
- English and Portuguese system copy plus French/Spanish customer copy with the documented English
  system fallback, with no clipping or overlap;
- accessibility semantics for selected, expanded, required, invalid, busy, and live states.

### Package Gates

- `dart format --output=none --set-exit-if-changed`;
- `flutter analyze --fatal-infos --fatal-warnings`;
- `flutter test --coverage` with at least 85% line coverage and complete critical branch scenarios;
- `dart doc` without public API warnings;
- lowest and current dependency resolution tests;
- `dart pub publish --dry-run` for both publishable packages without uploading;
- release Android build and iOS simulator build of the example.

### Native Runtime

Inspected Android and iOS runs must prove:

- host-trigger open, close, and reopen with the same session and draft;
- rating/choice/text entry and keyboard safety;
- explicit screenshot selection and completed upload;
- accepted submission creates one Feedback and no Issue;
- post-submit recurrence disables the trigger;
- light, dark, compact, standard, and large-text layouts have no overlap;
- logs contain no bearer, reporter data, screenshot bytes, framework warning, or unhandled error.

The runtime may use the existing local smoke server. Existing staging can be used only when needed
to validate the already-deployed mobile contract and with disposable least-privilege credentials.
No service deployment, production action, infrastructure work, or registry publication belongs to
this SDK slice.

## CI

Add a dedicated Flutter CI workflow rather than overloading the React Native Mobile CI job. It pins
the Flutter/Dart toolchain, caches only public dependency/tool artifacts, and runs format, analyze,
tests, coverage, docs, and publish dry-runs. Native example compilation can use platform-specific
jobs so a Linux runner validates Android and a macOS runner validates iOS.

The React Native CI gains only the shared fixture compatibility test. Existing backend, web,
widget, release, deployment, and production workflows remain unchanged.

## Acceptance Criteria

The slice is complete when:

- a Flutter host can integrate through the documented controller/host API without embedding a
  Fidibeki secret or an SDK-owned launcher;
- the Flutter sheet matches the approved mobile behavior and visual hierarchy on iOS and Android;
- all response, consent, screenshot, retry, recurrence, privacy, and error contracts match React
  Native and the existing backend;
- shared contract fixtures pass in both clients;
- unit, widget, golden, package, Android, and iOS gates pass;
- inspected native runtime evidence covers the complete capture lifecycle;
- package manifests and docs are ready for future `pub.dev` publication, but nothing is published;
- no backend, deployment, production, or infrastructure change occurs.
