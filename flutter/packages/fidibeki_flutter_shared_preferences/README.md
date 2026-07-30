# `fidibeki_flutter_shared_preferences`

Optional recurrence-storage adapter for `fidibeki_flutter`.

```dart
final storage = SharedPreferencesFidibekiStorage(
  namespace: '@my-company/fidibeki',
);
```

Only anonymous recurrence values are stored through this adapter. Capture tokens, form drafts,
reporter contact, and screenshot bytes remain memory-only. The adapter uses the asynchronous
`shared_preferences` API so its reads do not rely on an isolate-local cache.
