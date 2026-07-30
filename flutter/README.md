# Fidibeki Flutter workspace

This workspace contains the platform-neutral `fidibeki_flutter` capture SDK, its optional
`shared_preferences` recurrence adapter, and a host-owned Android/iOS example.

Run the package gate from this directory with Flutter 3.44 or newer:

```sh
dart format --output=none --set-exit-if-changed .
flutter analyze --fatal-infos --fatal-warnings
flutter test packages/fidibeki_flutter packages/fidibeki_flutter_shared_preferences \
  --coverage --coverage-path=coverage/lcov.info
dart run tool/check_coverage.dart coverage/lcov.info 85
(cd packages/fidibeki_flutter && dart doc)
(cd packages/fidibeki_flutter_shared_preferences && dart doc)
(cd packages/fidibeki_flutter && dart pub publish --dry-run)
(cd packages/fidibeki_flutter_shared_preferences && dart pub publish --dry-run)
```

Validate both current and lowest compatible dependency resolutions by running the test suite after
`dart pub upgrade` and after `dart pub downgrade`. Restore the committed lockfile with
`dart pub upgrade` when the lowest-resolution check is complete.

The packages are intentionally marked `publish_to: none`. A publish dry-run validates their
archives but does not upload them.

## Native smoke

Start the local fixture from the repository root:

```sh
node examples/mobile/smoke-server.mjs
```

Launch an Android emulator or iOS simulator, then run the opt-in lifecycle smoke from this
directory:

```sh
flutter test example/integration_test/local_runtime_test.dart \
  -d DEVICE_ID \
  --dart-define=FIDIBEKI_NATIVE_SMOKE=true
```

The smoke uses only local endpoints. It opens and reopens the host-triggered sheet, preserves its
in-memory draft, selects the host-provided screenshot, uploads it, and verifies accepted feedback
plus recurrence. No API key is present in the application.
