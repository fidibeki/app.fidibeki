# Fidibeki Data Rights Case Management Design

**Date:** 2026-07-24
**Status:** Approved by the founder's standing approval for recommended product decisions
**Target:** Staging-only product development

## Outcome

Fidibeki must manage data-subject requests as operational cases instead of treating export and
anonymization jobs as the complete rights workflow. The implementation covers reporter/end-user
and workspace-member subjects, documents Fidibeki's controller role, tracks statutory operations,
and enforces restrictions before non-essential processing.

This work is additive. Existing `reporter_privacy_requests`, verification links, exports,
anonymization jobs, and review decisions remain valid and continue to work.

## Domain Model

`PrivacyCase` is the canonical operational aggregate:

- subject type: `reporter` or `member`;
- right type: access, rectification, erasure, restriction, objection, portability assessment, or
  consent withdrawal;
- controller role: controller or customer/controller assistance;
- identity evidence and verification time;
- received time, response deadline, owner, status, pause/clarification periods, and completion;
- requested scope, decision summary, legal-review notes, exception/hold rationale, and reporter
  communication state;
- optional link to a legacy reporter request or privacy job.

Raw reporter email is retained only while needed for verification and communication. Matching uses
the normalized email digest. Member cases use a tenant-scoped member id. A case cannot mix subject
types or silently broaden its verified subject.

`PrivacyCaseEvent` is an append-only audit stream. Each command writes one event with an
idempotency key, actor, previous/new status, structured changes, and occurrence time. Events cannot
be updated or deleted.

`PrivacyRecipientAction` records external recipients/subprocessors that need correction or erasure
notification, delivery state, attempts, and completion evidence. It never implies successful
notification before a recorded delivery result.

Recipient corrective/erasure notices are explicit operator-created rights-compliance actions, not
reporter update subscriptions. They therefore do not reuse the reporter marketing/progress consent
gate or get silently suppressed as a generic integration. Creation still requires verified subject
scope and completed correction/erasure evidence. The first delivery format is deliberately minimal:
workspace, public case reference, action kind, and the approved instruction only; it excludes raw
feedback, subject/reporter email, and internal legal-review notes.

`PrivacyProcessingRestriction` is a separately queryable projection owned by a verified case. It
lists blocked purposes and becomes active before the case is reported as in progress. Deactivation
requires an explicit terminal decision event.

## State Machine And Deadlines

Cases use:

`received -> pending_verification -> in_progress`

From `in_progress`, an operator can move to `awaiting_clarification`, `paused`, or
`awaiting_legal_review`, and then resume. Terminal states are `completed`,
`partially_fulfilled`, `refused`, and `cancelled`.

The response deadline is stored, not inferred by the UI. Pauses record start/end timestamps and
accumulated paused duration. Every refusal, partial fulfillment, exception, and legal hold requires
a reason. The product presents operational state and evidence; it does not make an automatic legal
conclusion.

## Identity Verification

- Reporter cases use an opaque random verification token whose hash is stored. Verification links
  expire and are tenant/case scoped. Public responses never disclose the email or tenant data.
- Member cases reference an existing member in the same tenant. Authenticated self-service
  requests are verified from the active session. Operator-entered cases retain the requesting
  actor and the membership evidence used, without claiming the operator is the subject.
- A verified identity cannot be replaced. A new subject requires a new case.

## Restriction Enforcement

Before a non-essential operation, the server resolves the resource to the verified subject and
checks active restrictions. The protected purposes are:

- `ai`;
- `integration`;
- `reporter_email`;
- `replay`;
- `analytics`;
- `background_processing`.

The check is repeated both when work is scheduled and immediately before side effects. A database
error fails closed. Restricted work is not sent to providers and is recorded as suppressed rather
than retried indefinitely.

Anonymous analytics that cannot be linked to the verified subject are not falsely attributed to
that subject. Once a replay or event is linked to restricted feedback, downstream processing is
blocked. Storage required to preserve the request, legal evidence, security, or an explicit hold
remains possible and is documented on the case.

## Rights Execution

- Access/export reuses the existing export job and expands discovery to AI outputs, authentication
  and profile data, consent evidence, delivery evidence, replay, analytics, and relevant
  observability metadata.
- Rectification stores field-level approved changes, applies them transactionally to the canonical
  record, invalidates derived AI data when necessary, and creates recipient notification actions.
- Erasure is a distinct operation. It deletes data when allowed, records retained exceptions or
  legal holds item by item, and never labels anonymization as erasure.
- Restriction and objection activate the processing projection first, then assess the requested
  scope.
- Portability remains an assessment until an operator records that the applicable data set and
  format are approved.
- Consent withdrawal routes to the existing consent records and disables future communication or
  processing for the relevant purpose.

## API And Backoffice

Tenant APIs provide list, detail, create, assign, transition, restriction, execution, recipient,
retry, and communication commands. Mutating commands require an idempotency key and privacy
management permission.

The privacy settings view adds a case dashboard with overdue, due soon, manual/legal review,
external action, failed, and completed filters. A detail view exposes the case timeline, scope,
identity evidence, holds/exceptions, processing restriction, recipients, and retryable failures.
English and Portuguese copy must describe operational facts without presenting legal advice.

## Delivery Slices

1. **Case core and fail-closed restriction.** Schema, state machine, reporter/member identity,
   deadline dashboard, append-only events, and enforcement across AI, integrations, reporter
   email, replay, analytics, and linked background jobs.
2. **Rights execution.** Access/portability assessment, field-level rectification, true erasure,
   holds/exceptions, refusal/partial fulfillment, retryable jobs, and expanded discovery.
3. **Recipients and communication.** Recipient tracking, correction/erasure notices, case
   communications, failure recovery, and the complete bilingual backoffice workflow.

Each slice includes migration compatibility tests, tenant isolation, idempotency, backend and
frontend tests, and staging E2E validation. Production remains out of scope.

## Implementation Status

- Slice 1 is implemented through migration 86.
- Slice 2a is implemented through migration 87: a verified reporter access case can start the
  existing reporter export job and download its archive from the authenticated case; a verified
  reporter erasure case can start the existing structured deletion/anonymization primitive while
  retaining `erasure` as the case right and operational decision. Case-level legal holds are
  idempotent, tenant-isolated, reasoned, releasable, and checked both when the job is queued and
  immediately before the worker removes stored objects or database data.
- Starting or completing the underlying job does not automatically complete the case. Manual
  review findings and the operator's recorded legal decision remain distinct evidence.
- Slice 2b1 is implemented through migration 88. Reporter discovery now adds linked replay-consent,
  delivery, widget-submission, and privacy-request evidence to the existing feedback, issue,
  attachment, AI, and replay records. Anonymous analytics consent and aggregates are not falsely
  attributed to a reporter. A reporter portability case requires an append-only, reasoned
  assessment with an approved category set and `jsonl_zip` format before execution. Partial
  approvals filter unapproved files from the generated archive instead of treating the assessment
  as display-only metadata.
- The member-access portion of slice 2b2 is implemented through migrations 89 and 90. A verified
  member access case owns a retryable `member_export` job and authenticated download. Its
  repeatable-read archive includes the member profile, session and magic-link metadata without
  secrets, subject-authored activity, subject-addressed notifications, sanitized access events,
  and case history. The allowlist excludes other members' content, hashes/tokens, raw audit
  payloads, queue data, avatar binary, and internal legal-review notes. Start, completion, terminal
  failure, retry, tenant isolation, and omission behavior are audited and tested.
- Member profile rectification is implemented through migration 91. A verified member
  rectification case can atomically change an explicit subset of `name`, `locale`, and `timezone`.
  Each effective correction preserves append-only before/after values, reason, actor, case
  activity, subject-aware access audit, idempotency, tenant isolation, member-export evidence, and
  a bilingual desktop/mobile backoffice history.
- Reporter content rectification is implemented through migration 93. A verified reporter
  rectification case exposes only feedback owned by the verified subject and created within the
  request cutoff, then atomically corrects the canonical `description` plus the legacy `text`
  compatibility field. The feedback title and linked Issue remain independent. Each correction
  preserves append-only before/after evidence and a safe audit trail, invalidates only text-derived
  AI state using a generation fence, and queues the existing governed processing policy rather
  than introducing a separate provider call. Prior reporter export archives expire so a later
  download cannot serve stale content. Bilingual desktop/mobile backoffice coverage exercises the
  complete selection, correction, and history flow.
- Itemized erasure outcomes and first-class terminal decision evidence complete slice 2b2 without
  a new migration. Case execution summarizes the existing append-only review-item ledger and
  exposes only resource identifiers, affected fields, dispositions, reasons, actors, timestamps,
  and disclosed-destination counts, never the removed or retained raw content. Completion requires
  settled execution without retained exceptions or active holds; partial fulfilment requires a
  retained exception or active hold; refusal is limited to pre-execution cases. Partial/refused
  erasure outcomes require a decision summary, legal-review notes, and exception basis, and
  idempotency covers those values. The bilingual backoffice exposes both the itemized evidence and
  decision workflow with PostgreSQL integration and desktop/mobile E2E coverage.
- The first Slice 3 vertical is implemented through migration 95. Verified rectification cases
  with an applied correction, and terminal erasure cases with settled execution evidence, can
  create tenant-scoped recipient actions. Each action is idempotent, writes a transactional outbox
  event, and records append-oriented delivery attempts. Enqueueing remains `pending`; only provider
  acceptance records `sent`. Provider failures are stored as a bounded generic error and can be
  retried explicitly. A processing lease prevents concurrent automatic/manual attempts, while a
  stable action-scoped provider idempotency key protects ambiguous provider responses. The
  bilingual backoffice exposes evidence-gated creation, delivery state, attempts, and retry in the
  case. Subject-facing case communications and the remaining complete case-detail presentation
  are still pending before Slice 3 is complete.
