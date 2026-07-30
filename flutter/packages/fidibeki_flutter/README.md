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
);

MaterialApp(
  builder: (context, child) => FidibekiFeedbackHost(
    controller: feedback,
    child: child ?? const SizedBox.shrink(),
  ),
  home: ProductScreen(feedback: feedback),
);
```

The package is under active development and is not published.
