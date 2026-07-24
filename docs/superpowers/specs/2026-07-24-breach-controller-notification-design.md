# Breach Controller Notification Design

## Status And Scope

Approved on 2026-07-24 under the founder's standing approval for recommended product decisions.
This is the next bounded vertical for issue #64.

The slice adds staged processor-to-controller email notifications to the restricted breach
register. It does not notify an authority or affected person, infer a legal conclusion, expose the
register to tenant members, add production monitoring, deploy, or send a test message outside an
explicit operator action.

## Audit Result

The existing breach register already provides platform-admin-only incidents, affected tenant
references, immutable chronology, reasoned assessments, a server-authoritative 72-hour deadline,
and an append-only containment/corrective-action ledger. It has no controller contact snapshot,
notification content/version, delivery state, provider evidence, retry path, or worker.

Migration `103` added governed workspace privacy and rights contacts. The privacy contact is the
appropriate processor-notification destination; the rights contact remains specific to
data-subject requests.

## Decision

Use an explicit two-step workflow:

1. A platform administrator stages a versioned notification for one affected tenant.
2. A separate command queues the immutable snapshot for delivery.

Staging never sends. The store resolves the tenant's current privacy contact and workspace name,
then freezes both with the operator-approved, structured content. If the incident is not recorded
with the processor role, the tenant is not affected, or the privacy contact is absent, staging
fails closed. Content is never edited after staging; changed facts create an update or closure
notification with a new sequence.

This is preferable to sending directly from incident creation because processor notification must
be deliberate, previewable, and based on a governed contact. It is preferable to mutable drafts
because the delivery evidence must identify the exact content and recipient that were approved.

## Notification Contract

Notification kinds are `initial`, `update`, and `closure`. Each notification stores:

- incident and affected tenant IDs, incident public ID, kind, sequence, and template version;
- snapshotted workspace name, privacy contact email, and profile version;
- a bounded operator-approved summary, measures taken, and next-update text;
- incident awareness time and minimized category/impact facts copied from the incident;
- `draft`, `pending`, `processing`, `sent`, or `failed` operational state;
- creator, queueing actor, server timestamps, idempotency keys, and request fingerprints;
- bounded failure state and provider message ID after an attempt.

The email includes the Fidibeki incident reference, awareness timestamp, affected category labels,
impact dimensions, approved summary/measures/next update, and the Fidibeki privacy contact. It
never includes reporter content, evidence bodies, tokens, credentials, raw payloads, IP addresses,
user agents, query strings, signed object URLs, or internal assessment reasoning.

An `initial` notification is unique per incident and tenant. Updates and closure notices receive a
monotonic tenant-local sequence. Closure remains an operational update, not a claim that authority
or affected-person notification was unnecessary.

## Delivery And Evidence

Queueing writes a minimal event containing only opaque IDs to a dedicated breach-notification
outbox. That table is restricted to `fidibeki_admin`: the general `outbox_events` table is
tenant-scoped and granted to `fidibeki_app`, so using it would disclose that a breach workflow
exists even if its payload were minimized. A second dispatcher instance reuses the existing
deterministic Asynq enqueuer without weakening the database boundary. The worker:

- acquires a five-minute processing lease so concurrent tasks do not double-send;
- uses a stable provider idempotency key per notification;
- appends one attempt row before calling the provider;
- marks the attempt and notification `sent` only after a provider message ID is returned;
- records a bounded generic failure without provider response content;
- permits explicit retry only from `failed`.

The content snapshot is immutable. Delivery attempts are append-only. Only operational status,
lease, failure, provider ID, and delivery timestamps are mutable. The restricted tables grant
access to `fidibeki_admin` only and remain unavailable to `fidibeki_app`.

## API

All routes remain under the platform-admin audience:

- `POST /admin/breach-incidents/{incidentID}/controller-notifications` stages a snapshot;
- `POST /admin/breach-incidents/{incidentID}/controller-notifications/{notificationID}/queue`
  queues a draft;
- `POST /admin/breach-incidents/{incidentID}/controller-notifications/{notificationID}/retry`
  queues a failed delivery;
- incident detail additively returns `controller_notifications` with attempts.

Stage, queue, and retry reuse `platform:breach:append`; reading remains
`platform:breach:read`. Every command requires an idempotency key.

## Failure And Safety Behavior

- Unknown incident, tenant, or notification uses the existing non-disclosing not-found response.
- Missing/invalid controller contact, controller-role incidents, and malformed content are rejected
  without creating outbox or delivery rows.
- Reusing an idempotency key with changed input conflicts; exact retries return prior state.
- Queue/retry and the restricted outbox writes are transactional.
- A send failure remains retryable and never rewrites the frozen content.
- A notification already sent is terminal. A later correction is a new `update`.
- The staging exercise uses the capture sender or a controlled test double; this slice performs no
  staging deployment and no real customer notification.

## Verification

Tests cover domain validation, affected-tenant/contact enforcement, contact snapshots,
idempotency, stage/queue/retry state transitions, transaction rollback, worker leases,
provider-idempotent delivery, generic failures, append-only attempts, restricted-outbox grants,
migration up/down/up, HTTP audience/permission boundaries, additive incident detail, outbox
dispatch, email sentinel exclusion, retention-registry coverage, and repository-wide Go checks.

## Follow-Up Boundary

Later #64 work still includes subprocessor incident intake, a platform-admin UI when that surface
exists, notification and closure document exports, and staging-only tabletop scenarios. Provider
entities, DPA terms, notification deadlines, and approved retention periods remain governed by
#63 and #62; this slice does not invent them.
