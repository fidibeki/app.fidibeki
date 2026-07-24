# Breach Documentation Exports Design

## Status And Scope

Approved on 2026-07-24 under the founder's standing approval for recommended product decisions.
This is the next bounded vertical for issue #64.

The slice adds restricted, downloadable documentation for facts the breach register already
records: the frozen initial/update/closure controller-notification snapshots (slice 4) and a
deterministic current post-incident report assembled from incident, chronology, assessment,
action, and notification facts (slices 1-4). It does not send anything, mutate any incident,
invent a legal conclusion, add a migration, or expose this data to a tenant-scoped role.

## Audit Result

The register already exposes every fact this slice needs through `Store.GetIncident`, which loads
the incident, chronology, assessments, current/history action events, and controller notifications
with their delivery attempts in one `REPEATABLE READ READ ONLY` transaction (`store.go`). Nothing
in that read path performs N+1 queries, and nothing needs a new column: the frozen notification
snapshot (`ControllerNotification`), its append-only delivery attempts
(`ControllerNotificationAttempt`), the append-only chronology, the append-only assessments, and the
append-only action-event ledger (`actions_store.go`) already carry exactly the bounded, minimized
facts this slice may disclose -- none of them has ever carried evidence bodies, credentials,
reporter content, raw provider payloads, IP/user-agent data, query strings, or signed object URLs
(see the existing minimization comments in `notifications.go` and `notification_delivery.go`).

All existing routes sit under `RegisterPlatformRoutes` behind `RequirePlatformAdmin` with either
`platform:breach:read` (list/get) or `platform:breach:append` (every mutating command). There is no
existing export/download route in this package. Two permission-scoped read patterns already exist
elsewhere in the codebase to imitate: `accessaudit`'s bounded CSV stream and `privacy`'s
platform-support ZIP stream, both of which set `Content-Type`, `Content-Disposition`,
`Cache-Control: private, no-store`, and `X-Content-Type-Options: nosniff` by hand rather than
through a shared helper.

## Decision

Add two read-only GET routes under the existing incident resource, both gated by the existing
`platform:breach:read` permission (this is a read of already-staged/recorded facts, not a new
class of action, so no new permission is introduced):

- `GET /admin/breach-incidents/{incidentID}/controller-notifications/{notificationID}/export`
  downloads one frozen notification snapshot (any kind: initial, update, closure) plus its
  append-only delivery attempts, exactly as staged/sent.
- `GET /admin/breach-incidents/{incidentID}/post-incident-report` downloads a deterministic,
  point-in-time rendering of the whole incident: incident facts, full chronology, the latest
  assessment (explicitly `null` when none exists yet -- never inferred), current action state plus
  a derived history summary, and the full staged-notification/delivery ledger.

Both accept `?format=json` or `?format=markdown` (required, validated, `422` otherwise) and both
register a mirrored `HEAD` route against the same handler, which renders the body first, sets an
explicit `Content-Length` from the exact rendered byte length, and only then skips writing the body
for `HEAD` -- so `Content-Type`, `Content-Disposition`, and `Content-Length` are byte-for-byte
identical between `GET` and `HEAD`, and a `HEAD` caller learns the real download size without a body.

This deviates from the single generic `/exports/{kind}` shape suggested as a starting point: the
two documents take different path parameters (one is scoped to a specific notification, the other
to the whole incident) and overloading one route with a `kind` query value would either force an
optional `notificationID` query parameter (weaker not-found semantics than a path segment) or a
route that sometimes ignores part of its own path. Two resource-shaped routes under the same
incident keep the existing not-found/permission conventions intact and let each handler call
`GetIncident` once, exactly like `getIncident` already does.

Both handlers call `BreachStore.GetIncident` -- the same call `getIncident` already makes -- and
render from its return values. No new store method, query, or migration is added; this satisfies
"reuse current detail loading" exactly, since the existing call already avoids N+1 access.

## Document Contracts

Both documents share a wrapper: `schema_version` (starts at `1`), `generated_at` (server clock,
`HTTPOptions.Now`, defaulting to `time.Now`), and `incident_public_id`. Every ID in the body is the
existing stable resource ID (row `id`, never a synthetic export ID).

### Notification export

Serializes the matched `ControllerNotification` (which already carries `delivery_attempts`)
unmodified under a `notification` key -- this is the exact frozen content/version/kind that was
staged, plus the bounded delivery evidence, with nothing added or summarized. If `notificationID`
does not belong to `incidentID`, the response is the existing non-disclosing `404`.

### Post-incident report

- `incident`: the full `Incident` record (facts only; the 72-hour deadline is server-computed and
  already present, never recomputed here).
- `chronology`: every `ChronologyEntry`, in existing sequence order.
- `latest_assessment`: the most recent `Assessment` by sequence, or explicit JSON `null` if none
  exists. Older assessments remain retrievable from `GET .../{incidentID}` if a full history is
  needed; the report exists to document current understanding, not to duplicate the assessment
  ledger.
- `actions`: the current revision of every action (same rows `getIncident` already returns as
  `actions`).
- `action_history_summary`: one row per action derived from the full action-event ledger --
  `action_id`, `action_kind`, `containment_area`, `title`, `current_status`, `current_revision`,
  `revision_count`, `first_occurred_at`, `last_occurred_at`. This is a count/timestamp rollup of
  existing rows, not a new judgment -- it never states or implies whether containment is
  "complete."
- `controller_notifications`: every staged notification (all kinds/sequences) with its delivery
  attempts, i.e. the same list `getIncident` already returns.

Nothing in the report computes or asserts notifiability, high-risk status, closure, or compliance.
Pending/unset values (`assessment_pending`, a `null` `latest_assessment`, a `null` `next_update`,
etc.) are always rendered explicitly rather than omitted, so an empty report cannot be misread as
"nothing pending."

### Markdown rendering

Both documents also render as Markdown, at full field parity with the JSON above: every stable ID
(incident, chronology entry, assessment, action-ledger event/action, notification, delivery
attempt), every actor/owner/tenant ID, every lifecycle timestamp, and every optional field --
rendered as an explicit `(pending)`/`(none)` marker rather than omitted when unset. Rendering is
pure and deterministic -- it walks the same ordered slices/structs the JSON encoder walks, never a
map, so byte-identical input always produces byte-identical output in either format. The report's
per-notification section reuses the exact same renderer as the standalone notification export, so
the two documents stay in parity by construction rather than by convention.

Every value that did not come from this package's own fixed enum vocabulary (operator narrative
text -- summaries, reasoning, outcomes -- and customer/operator-controlled short strings such as a
workspace name, recipient email, title, provider message ID, or corrective reference) is
Markdown-injection-safe by construction, via one of two primitives:

- `mdEscapeInline` for short, single-line-context values (a bullet value or table cell): it doubles
  literal backslashes, converts embedded newlines/control characters into visible `\n`/`\r`/`\xHH`
  markers instead of real line breaks, and backslash-escapes remaining Markdown-significant
  punctuation (`| \` * _ # [ ] < > ~ !`). A value can therefore never open a heading, list item,
  table row, emphasis, link, or code span of its own -- the content survives, just inertly.
- `mdSafeBlock` for long narrative free text: it renders the (CRLF-normalized) text inside a fenced
  code block whose fence is chosen one backtick longer than the longest backtick run already present
  in the text, so CommonMark can never parse a heading/list/table inside the block, and no line
  inside the text can be mistaken for (and prematurely close) the fence.

The Markdown report ends with the same non-conclusory footer line already used in the notification
email ("does not determine whether an authority or affected-person notification is required") so
the exported document cannot be read as a compliance determination on its own.

## HTTP Behavior

- `format` query parameter: `json` or `markdown` only (a new `ParseExportFormat`, following the
  existing `Parse*` domain-validation convention); anything else is `ErrInvalidRequest` -> `422`.
- `incidentID` / `notificationID`: validated with `uuid.Parse` exactly like every existing route;
  a malformed ID or one the store can't find both resolve to the existing non-disclosing `404`
  (matches `getIncident`'s existing behavior for a non-UUID path segment).
- Headers: `Content-Type` (`application/json` or `text/markdown; charset=utf-8`),
  `Content-Disposition: attachment; filename="..."` (built only from the server-generated,
  already-safe incident public ID, sequence, and kind -- never from user input), an explicit
  `Content-Length` set from the exact rendered body byte length, `Cache-Control: private, no-store`,
  `X-Content-Type-Options: nosniff`, and `Referrer-Policy: no-referrer` (matching
  `privacy`/`platformadmin`'s existing restricted-download and platform-admin-surface headers).
- `HEAD` mirrors `GET`'s status and headers (including `Content-Length`) with an empty body.
- Authorization: identical to every other route in this package --
  `RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachRead)`. A tenant-audience token is
  rejected before the permission check ever runs; a platform token missing `platform:breach:read`
  gets `403`.

## Follow-Up Boundary

Later #64 work still includes an authenticated staging capture exercise for controller
notifications, subprocessor incident intake, staging-only tabletop scenarios, and an internal
platform-admin UI (which would be the first real consumer of these two export routes) when that
product surface exists. This slice does not build that UI, perform any staging call, or claim #64
complete.
