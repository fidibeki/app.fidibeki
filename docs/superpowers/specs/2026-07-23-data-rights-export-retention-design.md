# Fidibeki Data Rights, Export, And Retention Design

**Date:** 2026-07-23  
**Issue:** #25  
**Status:** Approved through the founder's explicit standing approval for recommended design decisions

## Goal

Give a workspace administrator an operational, tenant-isolated way to export workspace data and
handle a reporter's verified access or anonymization request. Make every effective retention period
visible and reconcile database state with private object storage without inventing legal promises.

This design extends the existing tenant transaction, append-only access audit, email queue, Asynq
worker, replay cleanup, analytics maintenance, attachment storage, and Settings surfaces.

## Audit Findings

- Workspace deletion already removes the tenant storage prefix before deleting database rows and is
  retried by the billing maintenance loop.
- Raw widget analytics are retained for 90 days and daily aggregates for 730 days. Their maintenance
  is leased, bounded, retryable, and reconciles recent aggregates before deleting raw events.
- Replay objects are removed according to plan storage limits of 30, 90, or 365 days, but the cleaner
  deletes the session row and therefore loses useful evidence that a replay once existed.
- Completed feedback attachments have no plan-retention cleanup. Only abandoned, incomplete uploads
  are marked failed. The old plan's claim that attachment retention was complete is inaccurate.
- `notify_reporter` currently combines consent history and current subscription state. Unsubscribe
  removes the current preference but does not preserve when consent was given or withdrawn.
- Reporter email is stored on feedback and issues. DOM snapshots, media, transcripts, replays, free
  text, responses, comments, and clarification responses can also contain personal information.
- No data export, reporter privacy request, privacy audit, or export-expiry model exists.

## Approaches Considered

### 1. Synchronous database dump

Generate one response directly from the authenticated HTTP request. This is initially small, but it
blocks requests, is hard to retry, cannot reconcile object storage, and encourages exporting secrets
through generic table serialization. Rejected.

### 2. Separate export, reporter, and retention systems

Create one queue and data model per workflow. This isolates code at first but duplicates job state,
auditing, object cleanup, signed access, and Settings UI. Rejected.

### 3. One data-rights domain with explicit job kinds (recommended)

Use a small `privacy` domain that owns export and reporter-request state, safe allowlisted archive
builders, signed download access, anonymization, and retention orchestration. It delegates analytics
and replay details to their existing stores and does not replace workspace deletion. Selected.

## Authorization Boundary

- Workspace exports and reporter requests require `tenant:update`, an active human member, and a
  tenant JWT. API keys cannot perform these human privacy operations.
- Listing retention information requires `tenant:read`.
- Export download authorization is rechecked when the short-lived URL is requested. A completed job
  is never exposed merely because its UUID is known.
- Reporter requests require control of the exact normalized email address. A request is inert until
  its one-time opaque email token is verified.
- Public verification and reporter-status responses are neutral, rate limited, `no-store`, and do
  not disclose whether an email matched records in the workspace.
- Every request, verification, download grant, anonymization result, failure, and expiry is written
  to the append-only tenant access audit without storing the reporter email in audit data. The audit
  stores a one-way email digest and record counts instead.

## Data Model

### `privacy_jobs`

One tenant-scoped job table supports these kinds:

- `workspace_export`
- `reporter_export`
- `reporter_anonymize`

Jobs carry lifecycle state (`pending`, `processing`, `completed`, `failed`, `expired`), actor member,
optional reporter-request relation, enqueue/attempt metadata, private object key, byte size,
completion/expiry timestamps, a bounded error, and created/updated timestamps. A generation counter
creates deterministic queue task IDs while allowing an explicit retry to schedule a new attempt.

Workspace exports expire seven days after completion. Reporter exports expire seven days after
completion. The private object is removed before a job is marked expired.

### `reporter_privacy_requests`

A request stores tenant, action (`export` or `anonymize`), normalized reporter email while processing,
its SHA-256 digest, a random token hash, verification expiry, verification time, related job, actor,
and lifecycle timestamps. Tokens are stored only as hashes. Verification links expire after 48 hours;
verified request/status access expires after 30 days. Completed anonymization clears the stored email
while preserving the digest and non-personal audit result.

### Existing records

- Feedback and issues gain separate contact-consent granted/withdrawn timestamps. Existing subscribed
  records are backfilled as granted at record creation. Unsubscribe and complaint flows set the
  withdrawal timestamp as well as disabling delivery.
- Attachments and replay sessions gain `retention_deleted_at`. Their database records remain as a
  manifest after objects are removed. Replay consistency explicitly permits a merged replay with no
  storage key only when retention deletion is recorded.
- No secrets, token hashes, encrypted integration values, API keys, sessions, magic links, billing
  provider identifiers, or internal queue payloads are included in an export.

All new tenant tables have forced RLS and tenant-scoped foreign keys. Maintenance uses the admin pool
but every candidate and mutation carries and rechecks `tenant_id`.

## Workspace Export

1. The tenant request inserts a pending job and returns `202` immediately.
2. The worker reconciler finds pending/stale jobs and enqueues deterministic tasks on the existing
   domain queue. A crash between insert and enqueue cannot lose the request.
3. The worker claims one job, takes a consistent read-only tenant snapshot, and builds a ZIP using
   explicit field allowlists rather than dumping arbitrary rows.
4. The archive is written to a random, tenant-prefixed key in the existing private bucket.
5. The job records object size, completion, and expiry. Failed attempts return to a retryable state
   with a bounded error; terminal failure remains visible and can be retried by an administrator.
6. The download endpoint creates a ten-minute presigned GET with attachment disposition. The bucket
   remains private, and the public object URL is never persisted in the database.

The archive contains:

- `manifest.json`: schema version, scope, generation time, counts, retention notes, and file index;
- `workspace.json`: safe tenant identity, plan, status, reporter-access settings, and effective limits;
- JSONL for projects, widget definitions/rules/targeting, feedback, feedback tags, issues, issue tags,
  issue status updates, issue activities, issue comments, tags, and safe integration metadata;
- `attachments.jsonl`: kind, content type, declared size, completion/processing state, and whether the
  object is available, retention-deleted, or omitted from the archive;
- `replays.jsonl`: capture timing, event/batch counts, rrweb version, processing state, and whether the
  replay object is available, retention-deleted, or omitted;
- `widget-analytics-daily.jsonl`: anonymous daily aggregates still inside retention.

Binary attachments and replay event streams are not copied into the archive in this phase. The
manifest states this explicitly and provides IDs and availability for a controlled follow-up. This
keeps exports bounded and avoids creating an additional long-lived copy of sensitive recordings.

## Reporter Requests

An administrator starts a request with an email and action. The scheduler sends a branded verification
message using the existing transactional email queue and stable idempotency key. The reporter opens a
tenant-scoped page backed by the opaque token.

Verification atomically marks the request verified and creates its job. Repeated token use is
idempotent and returns only lifecycle status. A reporter export uses the same archive format filtered
to exact, case-insensitive email matches and directly related issues/history/manifests.

Anonymization removes supported direct identifiers while preserving non-personal workflow history:

- clear reporter email and disable contact on matching feedback and issues;
- record contact-consent withdrawal and revoke reporter progress tokens;
- clear DOM snapshots and attachment transcriptions for matching feedback;
- remove matching attachment and replay objects, then retain manifest rows with deletion timestamps;
- leave ratings, status, title, description, triage, tags, issue history, and anonymous aggregates.

Free text, structured answers, comments, page URLs, and clarification responses can contain identifiers
that cannot be removed reliably without corrupting evidence. The result therefore records a manual
review count and the product explains this limitation to the administrator. It does not claim that
automatic anonymization is a substitute for legal review.

Object deletion occurs before the database removes identifiers. Partial storage failure leaves the
job retryable and personal database fields intact; already removed objects are safe to remove again.
The final database mutation is one tenant-scoped transaction.

## Retention And Reconciliation

Settings displays these effective defaults:

| Category | Effective retention |
|---|---|
| Feedback and issue workflow metadata | Workspace lifetime, until explicit deletion/anonymization |
| Attachments | Plan storage limit: 30/90/365 days |
| Session replay objects | Plan storage limit: 30/90/365 days |
| Raw widget funnel events | 90 days |
| Daily widget funnel aggregates | 730 days |
| Export archives | 7 days after completion |
| Unverified reporter privacy requests | 48 hours |
| Verified reporter request/status access | 30 days |

The privacy maintainer runs bounded batches with `FOR UPDATE SKIP LOCKED` candidates and a per-phase
time budget. For storage-backed categories it removes all known objects first and then marks metadata
deleted. Failed objects stay candidates for the next run. Export expiry follows the same rule.

The existing analytics maintainer remains the owner of analytics retention. The replay cleaner is
adapted to preserve session manifests. Workspace deletion remains the only whole-tenant deletion path.

## Product Surface

Settings gains a `Data & privacy` tab in the existing restrained backoffice style:

- an effective-retention table with category, period, and current policy source;
- a workspace export action and compact history with state, expiry, retry, and download commands;
- a reporter request form with email, export/anonymize choice, explicit confirmation for anonymize,
  and a status list that never displays a raw email after anonymization;
- a concise manual-review notice for unstructured content and media.

The public reporter page shows verification, processing, completed, expired, and failed states. It
does not reveal record counts before verification. On completion, an export action obtains a fresh
short-lived link; anonymization reports completion without exposing internal data.

All new controls have stable responsive dimensions and are validated at desktop and mobile widths.

## Error Handling

- Duplicate workspace export requests while one is pending or processing return the existing job.
- Queue loss is repaired by reconciliation; deterministic task IDs prevent duplicate concurrent work.
- Worker leases become stale after a bounded interval and can be reclaimed.
- Archive upload or database completion failures are retryable and overwrite the same random job key.
- A missing already-expired object is treated as successfully removed.
- Download URLs are unavailable for incomplete, failed, expired, foreign-tenant, or reporter-mismatched
  jobs.
- Reporter verification is idempotent; expired or malformed tokens return one neutral unavailable state.
- Retention never removes a database row before every referenced object removal succeeds.

## Documentation Boundary

`docs/privacy-data-map.md` documents technical data categories, purposes, effective technical
retention, configured providers/subprocessors, and the customer's responsibility to configure and
lawfully use session replay. It is explicitly a technical inventory for founder/legal review, not a
published privacy policy, DPA, residency promise, or legal guarantee.

## Test Strategy

- Migration `up/down/up`, constraints, grants, RLS, ownership, and legacy consent backfill.
- HTTP authorization, active-human enforcement, idempotent requests, tenant isolation, invalid input,
  reporter verification, neutral public errors, retry, and download eligibility.
- Worker success, stale reclaim, deterministic enqueue, archive allowlists, JSONL validity, upload
  failure, partial object deletion, database retry, expiry, and missing-object idempotency.
- Anonymization of feedback/issues/contact state/DOM/transcripts/tokens with preservation of workflow
  history and anonymous analytics.
- Retention boundary dates and bounded batches for starter, growth, and scale workspaces.
- Frontend type/unit coverage plus Playwright desktop/mobile journeys for workspace export, reporter
  request, destructive confirmation, status, retry, and responsive overflow.
- Staging-only verification after CI. No production deployment or production infrastructure work.

## Delivery Slices

1. **Durable exports:** migration, job API, reconciled worker, allowlisted archive, private upload,
   short-lived download, audit, and tests.
2. **Verified reporter rights:** consent history, verification email/token, filtered export,
   anonymization, media/replay cleanup, public status, and tests.
3. **Retention truth:** attachment and replay manifest-preserving cleanup, export expiry, retention API,
   technical privacy inventory, and partial-failure tests.
4. **Product UI:** Settings data/privacy controls, reporter status page, responsive E2E, CI, and staging
   validation.
