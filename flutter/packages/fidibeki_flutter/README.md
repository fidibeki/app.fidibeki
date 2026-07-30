# `fidibeki_flutter`

Native iOS and Android feedback capture for Flutter hosts.

The host application owns the trigger and obtains short-lived capture sessions from its trusted
backend. A Fidibeki tenant API key must never be placed in the application bundle.

```dart
final feedback = FidibekiFeedbackController(
  apiBaseUrl: Uri.parse('https://widget.fidibeki.com'),
  appVersion: '1.2.3',
  locale: const Locale('en'),
  osVersion: Platform.operatingSystemVersion,
  screen: '/checkout/review',
  sessionProvider: hostBackend.createCaptureSession,
  storage: appStorage,
  screenshotProvider: hostScreenshots.capture,
  linkOpener: hostLinks.open,
  theme: const FidibekiTheme(
    accent: Color(0xff087f6b),
  ),
);

MaterialApp(
  builder: (context, child) => FidibekiFeedbackHost(
    controller: feedback,
    child: child ?? const SizedBox.shrink(),
  ),
  home: ProductScreen(feedback: feedback),
);
```

The capture session controls response blocks, customer-facing copy, recurrence, appearance, contact
consent, and applicable AI disclosures. The sheet supports rating, reaction, single-choice, and
free-text responses in definition order, with progressive comment and reporter-contact sections.
System copy is available in English and Portuguese and falls back to English for other locales.

`refreshConfiguration()` preserves only answers that remain compatible with the new definition and
always clears the previous consent grant. A `configuration_changed` response performs this refresh
automatically. Draft content, reporter email, capture tokens, and responses remain in memory; only
anonymous recurrence state crosses the storage boundary.

For session recurrence, controllers created in the same process share an anonymous in-memory app
session. A cold launch or 30 continuous minutes in the background starts a new generated session.
Hosts that already own this lifecycle can pass `appSessionId`; the SDK then keeps that value stable
until the host creates a controller with a new one. Query strings, fragments, repeated separators,
and trailing slashes do not create distinct recurrence keys.

The host observes application lifecycle changes and rechecks cooldown eligibility on foreground.
While the app remains foregrounded, an ineligible controller also wakes at the exact cooldown
boundary; the timer is suspended in background and recalculated on resume.
Expired or denied capture sessions are discarded before another request can be sent. Public errors
use typed, sanitized messages and never expose capture-provider messages, bearer tokens, reporter
data, response content, server-provided validation copy, or screenshot bytes. Validation field keys
are accepted only when the active capture session declares them and are mapped to SDK-owned
localized messages; a rejection without a visible field displays one safe generic submit failure.
The stable error codes are `validation`, `network`, `sessionExpired`,
`configurationChanged`, `accessDenied`, `attachment`, `invalidResponse`, and
`retryPayloadChanged`.

The host renders the same Compact Progressive sheet under `MaterialApp` and `CupertinoApp`. It uses
75% of the viewport, expands above the keyboard, keeps its header and submit action fixed, and
scrolls only the form body. System back and the scrim close an idle draft but cannot dismiss an
active submission. Required, busy, error, and success states are announced; invalid submission
scrolls to and focuses the first invalid input where possible. Light/dark mode, orientation,
reduced motion, and large text use the resolved tenant accent without changing the numeric values
sent by rating stars.

Screenshot capture is also host-owned and runs only after the user selects the sheet action. The
provider returns a declared PNG, JPEG, or WebP size plus a bounded byte reader and optional cleanup:

```dart
Future<FidibekiScreenshotAsset?> capture() async {
  final image = await hostPicker.pickScreenshot();
  if (image == null) return null;
  return FidibekiScreenshotAsset(
    contentType: image.contentType,
    sizeBytes: image.sizeBytes,
    read: (maximumBytes) => image.readBytes(maximumBytes),
    dispose: image.dispose,
  );
}
```

The SDK accepts no local URI, limits screenshots to 10 MiB, verifies the declared PNG, JPEG, or WebP
signature, uploads only to the returned safe grant, and completes through the existing mobile
endpoint. A later upload/completion failure keeps the Feedback accepted and exposes only a scoped
attachment warning.

The package is under active development and is not published.
