# Sentry Error Telemetry Scrubbing Design

**Issue:** #62
**Status:** Approved under the user's standing authorization for recommended decisions
**Environment boundary:** Local only; no production, deployment, or provider configuration changes

## Problem

`docs/privacy-data-map.md` and the #63 provider governance registry already assert that Sentry
error telemetry is scrubbed before it leaves the application: "default PII capture disabled" on the
backend and "scrubbed context; user, request body, credentials, query, and sensitive context
removed" on the widget. Auditing the actual code shows that claim is only partly true:

- **Backend** (`backend/internal/observability/sentry.go`) sets `SendDefaultPII: false` and
  `AttachStacktrace: true` but registers no `BeforeSend` hook at all. `middleware.Recovery` recovers
  a panic and calls `sentry-go`'s `RecoverWithContext`, which turns the recovered value into
  `Event.Message` (string panic) or `Event.Exception[].Value` (error panic) using its raw
  `Error()`/`fmt.Sprintf` text verbatim. Any panic value built from request-scoped data (a
  `fmt.Errorf` that happened to interpolate an email, id, or token) is sent to Sentry unredacted.
  There is no code path today that sets `Request`, `User`, `Contexts`, or `Tags`, but nothing
  prevents a future call site from doing so and having it forwarded untouched — the existing
  `logging.isSensitiveKey` key-name filter (`backend/internal/logging/logging.go`) only applies to
  `slog.Attr` values written to stdout; it has no relationship to `sentry.Event` and does not run on
  the Sentry path at all.
- **Widget** (`widget/src/modules/error-reporting.ts`) does scrub, but by mutate-and-delete: it
  removes `user` and known `request` keys, keeps a query-stripped `request.url` (a raw page path,
  which "may contain personal data" per this issue's brief), and filters `extra`/`breadcrumbs` by a
  regex over key names (`/email|token|authorization|cookie|text|html|value/i`). A field whose name
  doesn't match the regex — or a thrown `Error`'s own `message`/`stack` value, which the current code
  never touches — passes through unchanged. The widget's own reporter/attachment error paths
  (`AttachmentPipelineError`, `PublicAPIResponseError` in `widget/src/submission-errors.ts`) build
  `Error` messages from bounded, non-personal fields today, but nothing stops an arbitrary thrown
  error elsewhere (a DOM API exception, a future code path) from embedding page or form content in
  its `message`, and the current scrubber would forward that verbatim.

Both gaps are exactly what #62 scopes for "Sentry/error telemetry retention and scrubbing" in
`plans/fidibeki.plan.md`. This slice closes the **scrubbing** half only. The provider-side retention
period, region, legal entity, and contract status remain `external_control`/`decision_required` —
no founder/legal evidence exists for those yet, and this slice does not claim otherwise.

## Options Considered

1. **Extend the existing key-name blocklist on both sides (regex/`isSensitiveKey`-style).**
   Cheapest change, but the audit above is the direct counter-example: a blocklist only catches
   fields whose *name* looks sensitive. `Exception.Value`/`message` carry arbitrary free text under
   an innocuous key name (`value`, `message`) and a blocklist can't generalize to "any string that
   might contain interpolated request data." The issue brief explicitly asks for the alternative.
2. **Deterministic allowlist reconstruction (recommended).** `BeforeSend`/`beforeSend` builds a new
   event from a fixed set of known-safe fields instead of deleting known-bad ones from the original.
   A field that isn't explicitly copied is absent by construction — including any field a future
   `sentry-go`/`@sentry/browser` upgrade adds to the event shape, or any field a future call site
   populates (`Request`, `User`, `Contexts`, `Tags`, arbitrary `extra`). Free-text fields
   (`Exception.Value`/`.value`, `Message`/`message`) are replaced with a fixed placeholder rather than
   passed through, because there is no reliable way to prove a given error string never interpolated
   request-scoped data. This is more restrictive than the current widget behavior (it also drops the
   query-stripped `request.url`, which the current code keeps) but matches the issue's explicit
   requirement: "no request URL at all (paths may contain personal data)".
3. **Disable default SDK integrations (`integrations: []`) instead of/in addition to `beforeSend`.**
   Considered for the widget, since `@sentry/browser`'s default integrations are what populate
   `request`/breadcrumbs from host-page state in the first place. Rejected as the *primary* control:
   it depends on the exact integration list for the pinned `@sentry/browser` version and would need
   re-verification on every SDK bump, whereas `beforeSend` sees the fully-assembled event regardless
   of which integration populated a field and is the one contract both SDKs commit to across
   versions. `beforeSend` alone is sufficient to satisfy every category in the issue's acceptance
   list, so the widget change is scoped to `beforeSend` plus the existing lazy-load/optional-DSN
   behavior, not a second integration-level control.

Option 2 is selected on both sides.

## Event Field Contract

The scrubber is a pure reconstruction: copy an explicit allowlist of fields onto a fresh event,
genericize the two free-text fields, and let everything else default to empty. This is the
"telemetry contract" required by #62 — it is code-owned (doc comment plus the fields the tests
pin down), not a separate config file, because the contract only has meaning as the literal set of
fields `ScrubEvent`/`scrubEvent` copies.

**Backend (`sentry.Event`, package `github.com/getsentry/sentry-go` v0.48.0 — this version has no
`Extra` field at all; the Go SDK's arbitrary-data surfaces are `Contexts`/`Tags`):**

| Kept as-is | Genericized | Always dropped |
|---|---|---|
| `EventID`, `Level`, `Platform`, `Timestamp`, `Release`, `Environment`, `Type` | `Message` → placeholder (if set) | `Request` (URL/method/data/query/cookies/headers/env) |
| `Exception[].Type`, `.Module`, `.ThreadID`; reconstructed stack frames limited to function/symbol/module/filename/line/column/in-app/package/platform/stack-start | `Exception[].Value` → placeholder (if set) | `User` (id/email/ip/username/name/data) |
| `Exception[].Mechanism.Type/Handled/ExceptionID/ParentID/IsExceptionGroup` | | Stack-frame absolute paths, source context, locals, and address metadata; mechanism description/help-link/source/data |
| `Threads[].ID`, reconstructed stack trace, `.Crashed`, `.Current` | | Thread names, `ServerName`, `Sdk`, `Modules`, and `Breadcrumbs` |
| | | `Contexts`, `Tags` (both fully arbitrary; nothing in this codebase sets them today) |

Stack traces are themselves reconstructed. This is necessary even though the current Go SDK does
not normally populate `Frame.Vars` or source context: custom events and future SDK behavior must not
be able to bypass the allowlist by placing customer data inside a frame.

**Widget (`@sentry/browser` `ErrorEvent`):**

| Kept as-is | Genericized | Always dropped |
|---|---|---|
| `event_id`, `timestamp`, `platform`, `level`, `release`, `environment`, `type` | `message` → placeholder (if set) | `request` (no URL at all, incl. path/query/fragment) |
| `exception.values[].type`; reconstructed stack frames limited to filename/function/module/platform/line/column/in-app/instruction-address/address-mode/debug-id | `exception.values[].value` → placeholder (if set) | `user`, `contexts`, `tags`, `sdk` |
| `exception.values[].mechanism.type/handled` | | Stack-frame absolute paths, source context, locals and module metadata; mechanism data; breadcrumbs |
| `extra.phase` from the nine code-owned phases; `project_id`/`widget_rule_id` only when they match `[A-Za-z0-9_-]{1,128}` | | every other `extra` key or invalid/oversized allowed-key value |

The three `extra` keys are the widget's existing bounded, code-owned operational context (see
`widget/src/loader.ts:184` and `widget/src/widget.ts:604-607`, both unchanged by this slice).
`phase` must be one of `analytics`, `targeting`, `mount`, `configuration`, `replay`,
`replay_consent_prompt`, `analytics_consent_prompt`, `attachments`, or `submission`; project and
rule IDs must match the bounded identifier pattern above. The allowlist drops any other key and any
invalid allowed-key value, so future call sites cannot place free text under an apparently safe key.

Both sides use the same fixed placeholder string for genericized values
(`"[fidibeki] value removed before Sentry send; see exception type and stacktrace"`), so a Sentry
issue still reads as "an exception happened, here is its Go/JS type and structural stack trace," which is
the evidence engineers actually triage on — the free-text message is what could carry customer data,
and Go/JS error messages are unstructured strings with no reliable way to prove one never
interpolated a request value, so it is always replaced rather than conditionally inspected.

## Scrubbing Flow

- **Backend:** `observability.ScrubEvent` (new file `backend/internal/observability/scrub.go`) is
  registered as `sentry.ClientOptions.BeforeSend` inside `InitSentry`. It runs for every event any
  hub sends through the client `InitSentry` configures, including the `sentry.CurrentHub().Clone()`
  hub `SentryPanicReporter` builds when `middleware.Recovery` calls it — no change to
  `recovery.go`/`SentryPanicReporter` is needed because `BeforeSend` is a client-level option, not
  something the panic-reporting call site has to opt into per call.
- **Widget:** `scrubEvent` becomes an allowlist reconstruction instead of a mutate-and-delete
  pass and is still wired as `Sentry.init({ dsn, beforeSend: scrubEvent })` inside
  `captureWidgetError`. The lazy-load-on-first-error behavior, the `initializedDSN` guard, and the
  `try/catch` around the dynamic `import()` in `loader.ts`/`widget.ts` (error reporting must never
  interfere with widget use) are unchanged.

## Retention Registry And Provider Governance

No status changes. `sentry_error_telemetry` stays `pendingProvider`/`RetentionExternalControl` in
`backend/internal/privacy/governance_registry.go` and `retention_registry.go` — this slice makes the
*scrubbing* claim in that row true, it does not touch or resolve the provider's retention period,
region, legal entity, or contract status, all of which remain `external_control`/`decision_required`
pending founder/legal evidence per #63. The row's `PersonalDataCategories` text is tightened to
describe the actual allowlist (type/stacktrace/timestamp/release/environment plus the widget's three
context keys; message/value/request/user/breadcrumbs/contexts/tags removed) instead of the looser
"message and stack trace" / "scrubbed context" phrasing that predates this slice.
`docs/privacy-data-map.md`'s "Audit and observability" row and Configurable Providers bullet get the
same precision. The #62 plan subsection gains a bullet describing this vertical; #62 stays
unchecked, since retention periods, IP/user-agent minimization, and the other listed remaining items
are untouched.

## Migration And Compatibility

No migration, no schema change, no HTTP contract change, no widget config field change. Both
`InitSentry(SentryConfig)` and `captureWidgetError(error, options)` keep their existing signatures;
only their internal `BeforeSend`/`beforeSend` behavior changes. An empty/absent DSN keeps both SDKs
inactive exactly as before (`InitSentry` returns early on `config.DSN == ""`; `captureWidgetError`
returns early on `!options.dsn`).

## Verification

- Backend: sentinel tests proving a raw `Request` (URL with path/query/fragment, headers, cookies,
  body), `User` (id/email/IP/username), `Breadcrumbs` (message and data), arbitrary `Contexts`/`Tags`,
  frame absolute path/source context/locals, mechanism free text/data, and a panic value/wrapped
  error containing an email or token string are all absent from the scrubbed event, while
  `Exception.Type`, structural stack frames, `Timestamp`, `Release`, `Environment`, and `EventID`
  survive unchanged. One test exercises the real
  `sentry.Init`-with-`BeforeSend` → `RecoverWithContext` → transport path (not just the pure
  function) to prove the wiring, not just the function in isolation.
- Widget: sentinel tests proving `request`/`user`/`contexts`/`tags`/`breadcrumbs` are absent
  regardless of what the raw event contained, a thrown string and a thrown `Error` whose
  `message`/`stack` embeds an email/token/raw URL never reach the scrubbed event's `message` or
  `exception.values[].value`; stack-frame source context, locals, absolute paths and module metadata
  are removed; and `extra` survives only for a known phase and bounded project/rule identifiers,
  even when caller-supplied context carries extra keys or invalid values.
- `gofmt`, `go vet ./...`, `go build ./...`, focused `observability`/`logging` package tests, full
  `go test ./...`, `make governance-check`, widget `typecheck`, `test` (coverage), and `build`.
  `git diff --check` for whitespace/EOL hygiene.

## Non-Goals

- Any Sentry retention period, deletion guarantee, region, legal entity, or contract status —
  these stay `external_control`/`decision_required` pending founder/legal/provider evidence.
- Backend/widget `release`/`environment` wiring beyond what already exists (the widget path does not
  currently plumb a release identifier into `Sentry.init`; this slice does not add that plumbing,
  since it is unrelated to scrubbing and would need its own config/versioning decision).
- Disabling or reconfiguring `@sentry/browser` default integrations (see Options Considered #3).
- Any change to `logging.isSensitiveKey`/slog redaction, `backend/internal/ai/redaction.go`'s
  free-text pattern redaction, or any other existing redaction path — they solve a different problem
  (unstructured free text vs. a structured, versioned event protocol) and are not duplicated here.
- Production, deployment, or provider credential/configuration changes of any kind.
