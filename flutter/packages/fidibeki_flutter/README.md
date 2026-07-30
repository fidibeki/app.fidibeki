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

The SDK accepts no local URI, limits screenshots to 10 MiB, uploads only to the returned safe grant,
and completes through the existing mobile endpoint. A later upload/completion failure keeps the
Feedback accepted and exposes only a scoped attachment warning.

The package is under active development and is not published.
