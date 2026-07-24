# Breach Controller Notification Implementation Plan

> **Scope mode:** LOCKED
>
> **Design:** `docs/superpowers/specs/2026-07-24-breach-controller-notification-design.md`
>
> **Boundary:** staging-only product work. No deployment, production change, authority message,
> affected-person message, or automatic customer send.

## Task 1: Add Restricted Notification Storage

Add migration `104` for versioned controller notifications, append-only delivery attempts, and a
dedicated `fidibeki_admin`-only notification outbox. Add grants, indexes, immutable-column
protection, and retention-registry coverage. Add focused migration up/down/up, grant, constraint,
and registry coverage.

## Task 2: Encode Domain And Email Contracts

Add notification kinds/states, structured inputs, normalization, fingerprints, and minimized email
composition. Reject non-processor incidents, non-affected tenants, incomplete privacy contacts,
unsafe bounds, and content sentinels. Unit-test validation and rendered content.

## Task 3: Implement Transactional Stage, Queue, And Retry

Resolve and snapshot the governed controller contact; create immutable content versions; queue
minimal restricted-outbox events; enforce idempotency and legal state transitions; load
notifications and attempts into incident detail. PostgreSQL tests cover isolation, conflicts,
rollback, sequence, snapshots, and append-only evidence.

## Task 4: Add Platform API And Delivery Worker

Expose stage, queue, and retry routes under `platform:breach:append`. Register the outbox resolver
and email worker handler. The worker uses a processing lease, one provider idempotency key, bounded
generic failures, explicit retry, and provider-confirmed success evidence. HTTP and worker tests
cover audience/permission denial, malformed IDs, concurrent delivery, replay, and failure recovery.

## Task 5: Reconcile Product Records And Run Gates

Update the privacy data map and canonical Fidibeki plan without claiming #64 complete. Run focused
breach/outbox/worker/database tests, `go test ./...`, `go vet ./...`, `go build ./...`,
`make format-check`, and `git diff --check`. Commit the design separately before implementation,
then commit the verified vertical slice. Do not deploy or push.
