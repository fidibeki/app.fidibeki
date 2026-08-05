# Session Replay Observability and Recovery

**Date:** 2026-08-01
**Scope:** Replay verification failures, processing recovery, tenant-facing failure states, and
privacy-safe operational telemetry
**Decision:** Approved failure-aware retry with immediate deletion of rejected replay data

## Problem

Production replay sessions can reach a terminal `failed` state without providing either operators
or tenants enough information to understand the failure. The incident that prompted this design was
caused by a contract mismatch: rrweb `2.1.1` emitted media event fields named `playbackRate` and
`loop`, while the backend media-event validator rejected those fields. Six batches were accepted and
two were rejected, leaving the session with 112 verified events but no complete replay.

The failure was difficult to diagnose for four independent reasons:

1. replay verification collapsed every event-schema failure into `invalid_event`;
2. the verifier marked the batch rejected, deleted the raw object, and returned `nil`, so Asynq
   considered the task successful;
3. the media worker had no replay-specific Sentry error reporter, while the global Sentry scrubber
   removed all tags, context, and error messages; and
4. the tenant UI displayed the processing message even after the session had become terminally
   failed.

This is not only an error-copy issue. Retryability depends on which inputs survive. A transient merge
failure is recoverable because all verified batch objects remain. A rejected or expired batch is not
recoverable because some source data has been deleted. A universal Retry button would therefore be
misleading and could produce an incomplete replay.

## Goals

- Accept the complete safe rrweb `2.1.1` media-event contract, including `playbackRate` and `loop`.
- Distinguish contract defects, unsafe payloads, upload expiry, processing exhaustion, retention
  expiry, and genuinely empty recordings.
- Report unexpected replay defects to Sentry with enough bounded metadata to identify the broken
  contract, without sending replay content or tenant identity.
- Preserve the existing automatic retry behavior for transient processing failures.
- Let an authorized tenant user manually restart terminal processing when all verified inputs remain.
- Never offer retry when source data is incomplete, rejected, expired, or retention-deleted.
- Give tenants accurate, actionable failure messages instead of internal errors or perpetual
  processing copy.
- Preserve fail-closed deletion of rejected replay objects.

## Non-goals

- Recovering the batches already deleted during the production incident.
- Retaining rejected raw replay payloads, enabling object versioning for rejected data, or weakening
  the anonymous replay privacy contract.
- Allowing tenants to bypass or override replay safety validation.
- Merging only the surviving batches of an incomplete session.
- Exposing stack traces, storage keys, raw validator errors, event content, tenant IDs, project IDs,
  session IDs, page URLs, or arbitrary field names through Sentry or the tenant API.
- Redesigning the replay player controls or the broader feedback-detail layout.
- Deploying to production without a separately authorized release step.

## Considered approaches

### 1. Failure-aware recovery with immediate unsafe-data deletion

Classify replay failures, retain the present fail-closed deletion behavior, report unexpected
contract defects through privacy-safe telemetry, and expose manual retry only for exhausted merge
processing where verified inputs remain.

This is the selected approach. It restores useful recovery without changing the privacy boundary or
pretending that deleted recordings can be reconstructed.

### 2. Universal retry backed by a short rejected-data quarantine

Keep rejected raw batches in private storage for a fixed grace period and re-run validation after a
deployment. This would make some validator defects recoverable, but it would also retain payloads the
backend could not prove safe. It contradicts the current replay threat model and is rejected.

### 3. Observability and copy changes without manual recovery

Fix the contract, improve Sentry reporting, and explain terminal states, but leave retry entirely to
Asynq. This is smaller, but it leaves tenants dependent on support after automatic processing is
exhausted even though the verified inputs still exist. It is rejected.

## Failure model

Sessions gain a nullable, bounded `failure_code`. The supported codes are:

| Code | Meaning | Retryable | Sentry |
| --- | --- | --- | --- |
| `contract_violation` | The current recorder emitted a structurally recognized event that the backend contract did not support | No; rejected data is deleted | Yes |
| `unsafe_payload` | The uploaded event violated an anonymity, size, URL, or structural safety rule | No | No; keep the existing abuse diagnostic |
| `upload_expired` | At least one declared batch was not completed and verified before expiry | No | No unless expiry processing itself errors |
| `processing_failed` | Merge or verified-object processing exhausted automatic retries while all required verified batches remain | Yes | Yes |
| `unavailable` | No verified events exist or the manifest cannot form a replay for a non-transient reason | No | Only when caused by an unexpected internal error |

Retention remains a higher-level `expired` session status, not a retryable failure code. A
retention-deleted session is never retryable regardless of any older processing failure.

The validator must return a typed result rather than only `unsafeReplayBatch{reason:
"invalid_event"}`. That result contains only bounded enums:

- failure class: contract or unsafe;
- event type;
- incremental source when applicable; and
- a server-defined violation code such as `media_fields_not_supported`.

It must not carry event JSON, field values, arbitrary property names, page URLs, or identifiers into
telemetry. Unknown attacker-controlled field names map to a generic bounded code.

The distinction between `contract_violation` and `unsafe_payload` is server-defined and closed. A
schema-only addition inside a recognized rrweb core event or incremental source is a contract defect
when the base event otherwise passes its privacy checks. It maps to a bounded violation code rather
than preserving the arbitrary field name. Malformed data, unknown custom sources, unsafe URLs,
unmasked text, unsafe node attributes, and other privacy-breaking additions remain `unsafe_payload`.
Each contract-violation branch requires a test built from the corresponding rrweb/client event
shape.

When concurrent or successive batch outcomes could assign different session failures, precedence is
deterministic: `contract_violation`, then `unsafe_payload`, then `upload_expired`, then
`unavailable`. `processing_failed` is assigned only when every batch is safe, so it never competes
with an invalid-batch failure.

## State and persistence

The existing external statuses remain `recording`, `processing`, `ready`, `failed`, and `expired`.
No additional public status is required.

Status precedence is explicit: retention deletion produces `expired`; a merged session is `ready`;
an ended session with a terminal `failure_code` is `failed`; another ended or finalize-requested
session is `processing`; and every other session is `recording`. Persisting a batch failure while
capture is still winding down does not expose `failed` until finalization stamps `ended_at`.

The session manifest gains:

- `failure_code`, nullable and constrained to the supported codes; and
- `merge_generation`, a positive integer with an initial value of `1`.

`merge_error` remains an internal bounded diagnostic for operators and retry mechanics. Tenant APIs
must not expose it directly. They expose a safe failure object derived from `failure_code` and current
state:

```json
{
  "failure": {
    "code": "processing_failed",
    "message": "Replay processing failed after several attempts. The recorded data is still available.",
    "retryable": true
  }
}
```

`retryable` is computed, not trusted from a stored boolean. It is true only when all of these are
true:

- `failure_code = 'processing_failed'`;
- the session is not merged or retention-deleted;
- automatic merge attempts are terminal;
- at least one safe batch exists; and
- no batch is pending, quarantined, rejected, or expired.

The database migration backfills terminal historical rows conservatively:

- sessions with rejected batches become `unsafe_payload` because the old generic reason cannot prove
  a contract defect;
- sessions with expired batches become `upload_expired`;
- terminal sessions with a merge request and only safe batches become `processing_failed`; and
- remaining terminal failures become `unavailable`.

The production incident therefore receives an accurate unrecoverable message but does not gain a
Retry button.

## Verification and finalization flow

The media validator is extended to accept and independently validate rrweb media fields
`playbackRate` and `loop`. `playbackRate` uses the existing finite replay-number check used for media
numeric fields, and `loop` must be a boolean. Both remain optional for compatibility with older
recordings. Existing restrictions on media URLs and other media data remain unchanged.

When batch verification rejects data:

1. the typed validation result is persisted on the batch using a bounded reason;
2. the session receives `contract_violation` or `unsafe_payload` without storing raw event data;
3. the raw object is deleted immediately, as it is today;
4. the existing abuse diagnostic is recorded for unsafe uploads;
5. contract violations are reported explicitly to the replay telemetry reporter; and
6. the handler returns `nil` after durable rejection and deletion, preserving idempotent safety
   semantics.

Explicit finalization with rejected or expired batches must finish the session as failed instead of
returning a generic incomplete-batches conflict and leaving it to abandoned-session recovery. The
public capture endpoint still returns an accepted terminal status without revealing validation
details to the untrusted caller. The tenant feedback API provides the safe explanation.

Abandoned-session recovery uses batch failure codes to select the same terminal failure code. It no
longer assigns “No replay events were uploaded” when verified events exist alongside an invalid
batch.

Each merge failure increments `merge_attempts`. When the terminal attempt is recorded and every
batch is still safe, the store assigns `processing_failed`; a successful merge clears both
`failure_code` and `merge_error`.

## Privacy-safe Sentry reporting

Replay reporting is isolated behind a small `FailureReporter` interface injected into verification
and worker processing. Tests use a recorder stub; production uses a Sentry-backed implementation.

The global Sentry scrubber gains an explicit allowlist for these replay tags only:

- `subsystem`, fixed to `replay`;
- `operation`, one of `verify_batch` or `merge`;
- `failure_code`, from the closed failure-code set;
- `violation_code`, from the closed server-defined violation set;
- `rrweb_version`, validated as a bounded semantic version;
- `event_type` and `incremental_source`, validated numeric enums; and
- `retry_exhausted`, `true` or `false`.

All other tags remain dropped. Contexts, breadcrumbs, request data, user data, arbitrary messages,
identifiers, URLs, storage keys, event bodies, and field values remain removed. The scrubber tests
must prove both that approved tags survive and that unapproved or malformed values do not.

Contract rejection is captured explicitly before the verifier returns `nil`. Expected unsafe
payloads are not sent to Sentry because they are policy outcomes and possible hostile traffic.
Unexpected merge/storage/code errors are reported by a replay-specific Asynq error handler; the
terminal attempt is tagged `retry_exhausted=true`. Sentry grouping uses the bounded operation,
failure, and violation codes so one contract defect produces one actionable issue instead of one
issue per tenant session.

Normal structured application logs may retain operational IDs under the existing logging policy,
but Sentry does not receive them.

## Manual retry API and queue safety

The tenant API adds:

```text
POST /v1/sessions/{sessionID}/retry
```

The route requires an active writable tenant membership plus both `session:read` and
`feedback:update`. This matches the feedback evidence workflow while preventing read-only members
from restarting processing.

The retry transaction locks the tenant-scoped session and re-evaluates the complete retry predicate.
For an eligible session it:

1. increments `merge_generation`;
2. resets `merge_attempts` to zero;
3. clears `merge_error` and `failure_code`;
4. keeps the original `merge_requested_at` and verified batch records; and
5. clears `merge_enqueued_at`.

After commit, the scheduler enqueues the current generation. The queue task ID becomes
`replay:merge:<session-id>:<generation>`, and the generation is included in the task payload. The
merge handler claims work only when the payload generation equals the current session generation.
Older retained or delayed tasks exit successfully without changing state. This avoids the current
24-hour fixed-task-ID conflict and prevents stale tasks from overwriting a newer retry.

If enqueueing fails after the transaction commits, the reconciler sees the cleared enqueue marker
and schedules the current generation. The API can therefore return `202` after the state transition
without losing the retry.

Responses are:

- `202 Accepted` with `{"status":"processing"}` after a successful restart;
- `202 Accepted` when the same retry is repeated after processing has already restarted, making
  client/network repetition idempotent; this case is identified by `merge_generation > 1`, because
  only a manual retry increments the generation;
- `404 Not Found` for a missing or cross-tenant session; and
- `409 Conflict` with a bounded reason for recording, ready, expired, or unrecoverable sessions.

Automatic Asynq retries remain unchanged within one merge generation.

## Tenant experience

The feedback detail response replaces direct use of `merge_error` with the safe `failure` object.
The replay panel renders messages by failure code:

- `processing_failed`: “Replay processing failed after several attempts. The recorded data is still
  available.”
- `contract_violation` or `unsafe_payload`: “Part of this recording could not be processed and was
  securely removed. This replay cannot be recovered.”
- `upload_expired`: “Part of this recording did not finish uploading. This replay cannot be
  recovered.”
- `unavailable`: “This recording does not contain enough verified data to build a replay.”
- expired status: “This replay reached its retention limit and was deleted.”

Only a failed session whose failure object has `retryable: true` shows **Retry processing**. The
button uses the existing feedback mutation pending/error/notice behavior, disables immediately, and
refreshes the feedback after `202`. Processing continues to show the processing message and spinner;
ready mounts the existing player.

A failed state never displays “The replay will appear after its batches are merged.” Retrying the
player's download after a ready replay fails remains a separate existing action and does not invoke
session reprocessing.

## Testing

### Validator and classification tests

- Accept rrweb `2.1.1` media events containing valid `playbackRate` and `loop`.
- Reject invalid types, non-finite playback rates, and unsafe media data.
- Classify only the closed recorder-contract branches as `contract_violation`.
- Classify malformed, unknown, privacy-breaking, and attacker-controlled payloads as
  `unsafe_payload`.
- Prove rejected raw objects are still deleted immediately.

### Store and state-transition tests

- Backfill each historical terminal shape to the conservative failure code.
- Derive retryability only for terminal merge failures with exclusively safe retained batches.
- Finalize rejected/expired sessions promptly with accurate failure codes.
- Recover abandoned sessions with the same classification.
- Reset an eligible session atomically and increment its merge generation.
- Refuse retry for recording, processing without a prior manual restart, ready, retention-deleted,
  rejected, expired, empty, and cross-tenant sessions.
- Ignore stale merge generations and accept only the current generation.

### API and authorization tests

- Require active, writable membership, `session:read`, and `feedback:update`.
- Return `202`, `404`, and bounded `409` responses as designed.
- Prove a repeated request after restart is idempotent.
- Verify the feedback response exposes the safe failure object and never the raw merge error.

### Observability tests

- Capture contract violations even though the verification task returns `nil`.
- Capture unexpected processing failures and identify terminal retry exhaustion.
- Do not capture expected unsafe payloads.
- Preserve only the approved replay tags through `ScrubEvent`.
- Drop malformed tag values and all replay content, IDs, URLs, request data, user data, arbitrary
  context, and arbitrary messages.
- Group equivalent contract violations independently of tenant and session.

### Web tests

- Render accurate copy for processing, retryable failure, unrecoverable failure, and expiry.
- Show Retry only with `retryable: true` and the required permission.
- Disable Retry while the request is pending, display API errors, and refresh after acceptance.
- Never show processing copy for terminal failure.
- Provide equivalent English and Portuguese translations for every new tenant-facing message.

### Regression and release proof

- Run backend replay unit and integration tests, observability tests, formatting, vet, and the full
  backend suite.
- Run web unit/typecheck/build tests and the feedback-detail Playwright coverage.
- Run widget unit/typecheck/build tests because the accepted rrweb event shape originates there.
- Capture a real homepage session containing the looping demo video in a non-production environment,
  scroll it in and out of view, finalize it, and verify the replay reaches `ready` with the media
  events accepted.
- Verify a forced transient merge failure becomes retryable and reaches `ready` after manual retry.
- Inspect the emitted Sentry test event to prove useful grouping metadata survives without replay or
  tenant data.

## Rollout and compatibility

Deploy the database/API/worker contract before any widget change that can emit a new replay shape.
The immediate incident fix does not require a widget change because the widget already emits the
rrweb `2.1.1` media fields.

For rolling compatibility, a merge task payload created before this change has no generation. The
updated worker interprets an omitted generation as `1`; new producers always include it. Deploy the
generation-aware worker before or together with the producer change so queued pre-deployment tasks
continue safely.

After deployment, create and play a new homepage replay before considering the incident resolved.
Monitor replay readiness and failure-code counts by source origin through existing internal
diagnostics, and confirm the contract-violation Sentry group remains at zero for new recordings.

Existing ready recordings remain unchanged. Existing terminal failures are classified conservatively
and receive better copy, but rejected/expired recordings remain unrecoverable. No retry operation can
restore deleted source data.
