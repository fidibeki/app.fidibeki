# Breach Containment And Corrective Action Ledger Design

## Status And Scope

Approved on 2026-07-24 under the founder's standing approval for recommended product decisions.
This is the next bounded vertical for issue #64.

The slice adds an operational containment checklist and post-incident action tracking to the
existing restricted breach register. It does not send customer, authority, or data-subject
notifications; infer legal decisions; add a tenant-facing route; deploy; or change production
infrastructure.

## Audit Result

The breach register already provides:

- platform-admin-only access enforced by platform JWT audience, permissions, and missing
  `fidibeki_app` database grants;
- immutable incident facts and chronology;
- a server-authoritative awareness timestamp and 72-hour authority deadline;
- append-only, reasoned authority, high-risk, and affected-person communication assessments;
- idempotency, validation, and PostgreSQL integration coverage.

Containment progress currently exists only as prose in the initial summary or chronology. There is
no queryable checklist, owner, due date, current status, completion evidence, corrective reference,
or durable post-incident action.

## Decision

Use one append-only action-event table. Each row is a complete revision of one logical action.
Current state is the highest revision; history is every revision ordered by the global sequence.

This preserves the breach register's existing immutability contract without maintaining a mutable
projection that could drift from its audit history.

The rejected alternatives were:

1. Continue using chronology text. It cannot prove checklist coverage, enforce owners/deadlines, or
   answer which work remains open.
2. Store mutable actions plus a separate audit table. This is easier to query, but direct state
   updates can diverge from or bypass the evidence ledger.

## Action Model

Every action revision contains:

- incident ID, stable action ID, monotonic revision, and action kind;
- a fixed containment control area when the kind is `containment`;
- immutable title;
- status: `pending`, `in_progress`, `completed`, or `not_applicable`;
- optional owner platform-admin ID and due date;
- optional bounded outcome summary and corrective reference;
- bounded opaque evidence references;
- actor platform-admin ID, server timestamp, idempotency key, and request fingerprint.

The nine code-owned containment controls are:

1. API keys;
2. sessions;
3. integrations;
4. object storage;
5. database access;
6. email;
7. AI;
8. analytics;
9. replay.

They are inserted atomically when a breach incident is created. Migration `101` backfills the same
initial pending actions for existing incidents using each incident's original creator and creation
time, so deployment does not imply that containment work started later.

Post-incident actions are explicitly created with an owner and due date. They may carry an opaque
corrective reference such as an internal ticket or issue reference, but never a credential, token,
raw payload, or evidence body.

## State Rules

- Initial containment actions start `pending`.
- New post-incident actions start `pending` and require an existing platform-admin owner plus a
  server-validated due date.
- Moving to `in_progress` requires an owner.
- Moving to `completed` requires an owner, a bounded outcome summary, and at least one evidence
  reference.
- Moving to `not_applicable` requires an owner and a bounded reason in the outcome summary.
- A completed or not-applicable action may be reopened through a new `in_progress` revision; prior
  evidence remains immutable.
- `expected_revision` is mandatory for updates. A stale value returns a conflict instead of
  silently overwriting newer work.
- Kind, containment area, and title cannot change after revision 1.
- Every insert is idempotency-key/fingerprint protected within the incident.

## API

Existing additive platform-admin routes gain:

- `POST /admin/breach-incidents/{incidentID}/actions` to create a post-incident action;
- `POST /admin/breach-incidents/{incidentID}/actions/{actionID}/revisions` to append a state
  revision.

Both reuse `platform:breach:append`. Incident detail remains under `platform:breach:read` and gains:

- `actions`: the latest revision for each action;
- `action_events`: the complete append-only revision history.

No endpoint accepts an occurrence timestamp. `occurred_at` and `created_at` come from one server
clock read, matching chronology and assessment evidence.

## Storage And Authorization

Migration `101` adds enums for action kind, status, and containment area plus
`breach_action_events`.

- The table references `breach_incidents` and `platform_admins`.
- `fidibeki_admin` receives only `SELECT` and `INSERT`.
- `fidibeki_app` and `PUBLIC` receive no grant.
- The existing `breach_register_append_only()` trigger rejects `UPDATE` and `DELETE`.
- Constraints enforce revision shape, bounded evidence, status requirements, and
  containment/post-incident consistency.
- An index supports latest-state and ordered-history reads.

The executable retention registry adds this table to the existing `breach_register`
`decision_required` category. This slice does not introduce a retention period or cleaner.

## Failure And Safety Behavior

- Unknown incident, action, owner, or malformed UUID returns the existing non-disclosing error
  shape.
- Idempotent retries return the original revision; changed input with the same key conflicts.
- Concurrent updates serialize on the current action revision. Exactly one caller can advance an
  expected revision.
- Database insertion and latest-revision validation occur in one transaction. Action revisions are
  not duplicated into the general chronology.
- API responses never include request fingerprints or idempotency keys.
- No action causes a credential rotation, session revocation, provider call, notification, or
  deletion. Operators record work that happened through separately authorized systems.

## Verification

Tests cover:

- enum, bounds, transition, completion-evidence, due-date, and reference validation;
- automatic checklist creation for new incidents and migration backfill for legacy incidents;
- idempotent create/update, stale-revision conflict, concurrent update behavior, and not-found
  boundaries;
- append-only trigger and `fidibeki_app`/`fidibeki_admin` grants;
- platform JWT audience and read/append permissions;
- additive incident-detail JSON;
- migration `101` up/down/up and full migration-chain version;
- executable retention-registry coverage after the new table;
- package, full backend, static-analysis, and build regression.

## Follow-Up Boundary

Later #64 slices still need:

1. processor-to-controller notification contacts, templates, delivery evidence, and retries after
   #63's governance registry is integrated;
2. subprocessor incident intake mapped to governed providers and affected tenants;
3. staging-only tabletop scenarios and a no-send notification exercise;
4. an internal platform-admin UI when that product surface exists.
