# Fidibeki Public Submission Abuse Protection Design

**Date:** 2026-07-23
**Issue:** #26
**Status:** Approved through the founder's explicit standing approval for recommended design decisions

## Goal

Protect public feedback submission, attachment upload, replay ingestion, and optional AI processing
from automated abuse without fingerprinting visitors, losing valid feedback, or forcing every visitor
through a CAPTCHA.

This design extends the existing public-key/origin authorization, honeypot, request-size limits,
Redis GCRA limiter, plan quotas, attachment declarations, one-time completion tokens, replay
validation, moderation, Asynq workers, and operational Settings surfaces.

## Audit Findings

- Public widget requests already require a valid project public key and an allowed origin. Query and
  header keys must agree.
- Feedback submission already has a 1 MiB JSON body limit, a hidden honeypot, a ten-per-minute
  project/network limit, a one-hundred-per-hour project limit, plan quota enforcement, and an
  idempotent client feedback ID.
- Free text and comments are limited to 10,000 characters. DOM HTML and captured styles are bounded,
  and each declared attachment has a kind-specific content-type and size limit.
- The current attachment contract binds a presigned URL to a generated storage key and uses a
  one-time hashed completion token. Completion checks object size and storage metadata content type,
  but it does not inspect actual file bytes or enforce token age.
- Attachment count and aggregate declared bytes are not bounded. Storage metadata supplied by an
  uploader can therefore pass while the object payload has a different format.
- Attachment `processing_status` currently combines upload and AI lifecycle concerns. Completed
  objects can reach streaming and AI paths without a separate safety decision.
- Replay batches are individually bounded to 2 MiB and 50 events, and the merger performs bounded
  JSON decoding. Public replay endpoints do not share the feedback rate policies, completion does
  not enforce declaration expiry, and malformed data is rejected only after ingestion.
- The AI processor has one global provider RPM limiter and classifies provider failures. It does not
  have a tenant/project cost budget before work reaches the provider.
- The widget retains entered values when a request fails, but collapses all public API failures into
  a generic message and does not use `Retry-After`.
- Raw IP addresses are used transiently in Redis keys and are not product analytics identifiers.
  They should nevertheless be replaced with keyed, short-lived digests in limiter storage.
- Logs distinguish several failures, but administrators do not have a tenant-safe product view of
  rate limiting, spam rejection, plan quota, unsafe upload, or provider pressure.

## Approaches Considered

### 1. Add checks independently to each endpoint

This is the smallest initial patch, but feedback, attachments, replay, and AI would acquire different
keying, error, metrics, and failure behavior. It would duplicate the current partial controls and be
difficult to audit. Rejected.

### 2. Put all protection in an external gateway

A gateway is useful for coarse network traffic, but it cannot understand a Fidibeki project, declared
media cost, replay session state, accepted feedback, plan quota, or optional analysis. It would also
turn staging configuration into a product dependency. Rejected for this phase.

### 3. Add one shared abuse domain and keep domain-specific validation (recommended)

A small `abuse` domain owns privacy-preserving buckets, named policies, stable public decisions,
aggregate diagnostics, and metrics. Feedback, replay, upload verification, and AI remain responsible
for their own payload and lifecycle rules. Selected.

## Privacy-Preserving Identity

The limiter receives only the minimum request context needed for a policy:

- tenant and project IDs from the authorized public key;
- normalized allowed-origin host;
- network address from the existing trusted-proxy-aware resolver;
- the existing anonymous widget visitor ID when it is present and valid.

Origin, network, and visitor values are converted to fixed-length HMAC-SHA-256 digests before they are
placed in Redis keys or diagnostic dimensions. The HMAC secret is a dedicated application secret and
supports current and previous values during rotation. Digests never enter widget analytics, exports,
webhooks, or user-facing responses.

Limiter keys include a policy version and period bucket so they expire naturally. Diagnostics retain
only tenant, project, event category, reason, hour, and aggregate count. They never retain a raw
address, origin, visitor ID, user agent, page URL, payload, or digest.

## Coordinated Policies

The shared evaluator applies all relevant scopes and rejects when any one scope is exhausted:

| Operation | Scopes | Initial policy |
|---|---|---|
| Feedback submission | project, origin, network, visitor | existing project/network limits plus origin and visitor limits |
| Attachment declaration | project, origin, network, visitor | request count plus declared aggregate bytes |
| Attachment completion | project, network | bounded completion attempts per declaration and project |
| Replay session start | project, origin, network, visitor | bounded starts per minute/hour |
| Replay batch declaration/completion | project, network, session | bounded requests and declared bytes |
| AI scheduling | tenant, project | daily cost-bearing work budget in addition to provider RPM |

Initial numeric values live in typed backend policy configuration with conservative defaults and can
be changed without altering endpoint code. Public keys are represented by their authorized project,
not persisted as an additional identifier.

Redis is the distributed source of truth for short windows. A limiter failure does not discard an
otherwise valid text submission: the request is accepted, the failure is diagnosed, and optional
cost-bearing work is deferred. A limiter failure on a new attachment or replay upload grant denies
that grant with retry guidance because issuing it can create unbounded storage cost.

No mandatory CAPTCHA is added. A future adaptive challenge may consume the same decision contract
only after diagnostics show that the lighter layers are insufficient.

## Stable Public Error Contract

The existing API error envelope remains compatible and gains optional additive fields:

```json
{
  "error": {
    "code": "rate_limit_exceeded",
    "message": "Too many attempts. Try again shortly.",
    "reason": "submission_rate_limited",
    "retry_after_seconds": 42
  }
}
```

Stable reasons cover:

- `submission_rate_limited`;
- `upload_rate_limited`;
- `replay_rate_limited`;
- `spam_rejected`;
- `plan_quota_exhausted`;
- `upload_expired`;
- `upload_invalid`;
- `upload_unsafe`;
- `provider_temporarily_unavailable`.

Rate responses use HTTP 429 and `Retry-After`. Plan quota keeps its existing status/code for
compatibility. Validation keeps field errors. Unsafe payloads use a neutral response that does not
reveal scanner internals.

The widget parses this envelope, preserves text, rating, reporter contact, client feedback ID, and
pending local attachments, and shows concise inline guidance. For retryable failures it disables only
submission until the server-provided retry time and then offers retry. A feedback record accepted
before optional media or AI failure remains accepted and visible.

## Payload And Declaration Bounds

Validation occurs before database writes, presigning, object reads, or AI scheduling:

- at most four declared feedback attachments, matching the supported screenshot, audio, video, and
  replay capture slots;
- existing per-kind content-type and size limits remain authoritative;
- aggregate declared feedback attachment bytes are capped at 125 MiB;
- duplicate client attachment IDs and duplicate single-slot attachment kinds are rejected;
- replay retains its 2 MiB batch and 50-event limits and additionally caps declared bytes and batch
  count across the session;
- completion attempts are bounded and must occur within the original ten-minute declaration TTL;
- size, type, key, tenant, project, feedback/session, and one-time token must all match the locked
  declaration.

Presigned keys remain server-generated and tenant/project/resource scoped. A refresh rotates the
completion token, records a new expiry, and never changes the declared size, kind, type, or object
key.

## Attachment Safety Lifecycle

`processing_status` continues to represent AI processing. A separate `safety_status` represents:

- `pending`: declared but not completed;
- `quarantined`: uploaded and awaiting required verification;
- `safe`: verified and eligible for preview or AI;
- `rejected`: malformed, type-mismatched, or unsafe;
- `expired`: declaration elapsed without a valid completion.

Existing completed attachments are backfilled to `safe`; incomplete legacy uploads are `pending`.
The attachment also stores declaration expiry, verification time, bounded failure reason, and detected
media type. No database row or object URL is exposed as downloadable merely because storage metadata
claims a supported type.

Completion first locks and validates the declaration, checks expiry, consumes the token, verifies
exact size/type metadata, and commits `quarantined`. A deterministic Asynq verifier then opens the
fixed object key through private storage with a strict byte limit and checks:

- PNG, JPEG, WebP, WebM, Ogg, MP3, WAV, MP4, JSON, and gzip signatures against the declared type;
- structural container markers required to distinguish lookalike formats;
- the declared kind/type combination;
- configured safety scanners through a small interface.

The initial required scanner performs signature and bounded structural checks locally. The scanner
interface permits a malware provider later without changing the public contract. A missing scanner
does not silently mark content safe.

Safe verification records the detected type and schedules existing attachment AI. Rejected,
malformed, oversized, or unreadable objects are removed, retain a non-downloadable manifest, never
enter preview/AI, and produce an aggregate diagnostic. Queue reconciliation repairs a crash between
quarantine and scheduling. Repeated tasks and object deletion are idempotent.

Tenant attachment streaming, signed-link creation, thumbnail/preview paths, exports, and AI claims
all require `safety_status = 'safe'` in addition to their current authorization and processing rules.

## Replay Safety

Replay declarations use the shared policies and store an explicit upload expiry. Completion consumes
the token once, verifies exact storage metadata, and marks the batch quarantined. The existing bounded
decoder becomes the required verifier: it reads no more than the declaration plus one byte, requires
one valid JSON array, validates event count and rrweb event shape, and rejects trailing data.

Only verified batches count toward a session's accepted events and bytes. Finalization cannot merge a
session with pending, expired, or rejected batches. The merged object remains private and becomes
tenant-streamable only after the current merge validation succeeds. Invalid batch objects are removed
and cannot be retried under the consumed declaration; the client must declare the batch again.

## AI Cost Protection

The current global provider RPM limiter remains the final provider-pressure control. Before enqueue,
a project/day policy reserves one text or safe-attachment analysis unit. Reservation is idempotent by
resource and generation/attempt so retries do not consume the budget repeatedly.

When the budget is exhausted or the limiter is unavailable:

- accepted feedback remains stored;
- text/attachment analysis moves to a visible deferred or skipped state with a safe reason;
- no provider call occurs;
- an aggregate `ai_budget` or `provider` diagnostic is recorded;
- an administrator can explicitly retry after capacity returns.

Provider 429, authentication, timeout, and unavailable classifications feed diagnostics from the
existing worker error handler. Provider messages and submitted content are never copied into the
diagnostic aggregate.

## Operational Diagnostics

A tenant-scoped hourly aggregate stores:

- tenant and optional project;
- event category: `rate_limit`, `spam`, `plan_quota`, `upload_safety`, `ai_budget`, or `provider`;
- stable reason;
- count and first/last occurrence.

The write is an atomic upsert and is best effort for public paths. A diagnostics API requires
`tenant:read`, accepts a bounded period and optional project, and returns totals and hourly series.
The existing Settings area gains a compact `Submission health` section showing:

- blocked submission/replay/upload requests;
- honeypot/spam rejections;
- plan quota exhaustion;
- rejected/expired uploads;
- deferred AI and provider failures.

The view explains operational cause, not visitor identity. Empty state means no recorded pressure,
not proof that no abuse occurred. Aggregates are retained for 90 days by a bounded maintainer.

## Failure Handling

- Feedback and public-origin authorization run before detailed abuse responses, preventing project or
  policy enumeration.
- Honeypot requests preserve the current neutral fake-accepted response and record only an aggregate
  spam count.
- Duplicate idempotent feedback requests return the existing feedback and do not double-consume
  attachment declarations or AI reservations.
- Redis failure permits valid text intake but denies new upload/replay grants and defers optional AI.
- Diagnostic-write failure is logged and never changes a public decision.
- Quarantined objects are unavailable until verification succeeds.
- Expired, rejected, and unsafe objects are deleted before their manifest becomes terminal.
- Verifier/queue crashes are reclaimed and reconciled; deterministic jobs prevent concurrent duplicate
  verification.
- Optional AI failure never rolls back or hides accepted feedback.

## Test Strategy

- Unit tests for HMAC bucket stability/rotation, policy composition, retry calculation, stable error
  serialization, attachment counts/aggregate sizes, declaration expiry, and every supported signature.
- Redis integration and concurrency tests proving project/origin/network/visitor coordination across
  handler instances and idempotent AI reservations.
- Feedback HTTP integration tests for origin authorization order, honeypot, stable 429/Retry-After,
  fail-open text intake, fail-closed upload grants, declaration reuse, expiry, and tenant isolation.
- Upload worker tests for forged metadata, polyglot/lookalike headers, truncation, size mismatch,
  scanner failure, safe transition, object removal, crash reconciliation, and prevention of preview/AI.
- Replay tests for rate limits, session aggregate bounds, expiry, malformed arrays, event-count
  mismatch, trailing data, consumed tokens, and finalization with quarantined/rejected batches.
- AI tests for idempotent daily reservation, deferred state, provider classification, and preservation
  of accepted feedback.
- Widget unit and browser tests for reason-specific messages, countdown/retry, retained inputs and
  attachments, and accepted-feedback/media-failure behavior.
- Responsive Playwright validation of Submission health and widget errors at desktop and mobile
  widths, followed by CI and a real staging-only journey.

## Delivery Slices

1. **Shared decisions and bounds:** HMAC identities, named coordinated policies, stable error details,
   aggregate diagnostics model/API, attachment count/aggregate validation, and distributed tests.
2. **Quarantined uploads:** migration, explicit expiry, signature verifier worker, safe-only
   preview/AI/export access, reconciliation, object cleanup, and failure tests.
3. **Replay and AI budgets:** shared replay policies, verified batch lifecycle and session aggregates,
   idempotent AI cost reservation/defer behavior, and concurrency tests.
4. **Product experience:** widget retry states, Submission health Settings view, responsive E2E, CI,
   and staging validation.

Production deployment, production configuration, mandatory CAPTCHA, browser fingerprinting, and an
external malware-provider integration are outside this issue.
