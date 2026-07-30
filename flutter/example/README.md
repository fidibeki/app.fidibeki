# Fidibeki Flutter Example

Start the existing local mobile smoke server from the repository root:

```sh
node examples/mobile/smoke-server.mjs
```

Then launch the host-owned example from `flutter/example`:

```sh
flutter run
```

The app places its own feedback trigger and calls a local host session endpoint. It contains no
tenant API key and does not open the sheet automatically. Its screenshot provider fetches a local
fixture only after the user selects **Add screenshot**.

Override either local endpoint when needed:

```sh
flutter run \
  --dart-define=FIDIBEKI_WIDGET_BASE_URL=http://127.0.0.1:43136 \
  --dart-define=FIDIBEKI_SESSION_URL=http://127.0.0.1:43136/session
```

The opt-in local contract smoke can be repeated while the server is running:

```sh
flutter test --dart-define=FIDIBEKI_LOCAL_SMOKE=true test/local_smoke_test.dart
```

The Android/iOS runtime smoke is documented in the workspace
[`README.md`](../README.md#native-smoke).
