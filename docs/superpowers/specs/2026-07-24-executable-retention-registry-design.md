# Executable Retention Registry Design

## Status And Scope

Approved on 2026-07-24 under the founder's standing approval for recommended product decisions.
This is the next bounded vertical for issue #62.

The slice replaces the current incomplete, flat retention schedule with an executable inventory.
It does not invent legal periods, delete a new category, add production infrastructure, or claim
that a pending policy is enforced.

## Audit Result

`privacy.EffectiveRetentionForPlan` currently returns nine customer-visible rows. Six describe
implemented cleaners or authoritative expiry clocks; the workflow row still says "Workspace
lifetime". The response has no typed enforcement state, resource inventory, deletion action, hold
behavior, cadence, or evidence that every store is represented.

The repository currently contains:

- 66 PostgreSQL tables;
- private object prefixes for attachments, replay batches/merges, identity assets, and exports;
- Asynq queues for domain fan-out, email, attachment verification, replay, privacy exports, and AI;
- widget browser keys for recurrence, visit policy, consent, analytics identity, and retry outbox;
- configured external boundaries for storage, email, AI, billing, telemetry, and customer
  integrations.

Only attachments/replay objects, temporary privacy archives/request access, widget analytics, and
tenant magic links have bounded automated retention. The remaining categories must not be presented
as though a vague technical lifetime were an approved policy.

## Decision

Create a typed, code-owned retention registry and derive the tenant-visible schedule from it.

Each policy declares:

- stable category and localized product label key;
- purpose summary and controller/processor role;
- lifecycle status: `enforced`, `configurable`, `external_control`, or `decision_required`;
- trigger, duration when approved, lifecycle action, and cleanup cadence;
- hold behavior: `case_scoped`, `supported`, `not_applicable`, or `decision_required`;
- the PostgreSQL tables, object prefixes, queue names, browser keys, and providers it covers;
- the implementation/control identifier that makes an enforced claim traceable to code.

`decision_required` is a first-class state, not a custom duration. It means Fidibeki retains the
data today but no approved maximum period and cleaner exist. `external_control` means the code can
identify the boundary but the provider-side configuration and contract must be verified outside
the application. `configurable` is reserved for a real current configuration such as plan-based
attachment/replay storage, never for a future setting.

The rejected alternatives were:

1. Expanding the existing string list. This would remain impossible to validate against the
   actual stores and would keep policy state implicit.
2. Adding a mutable retention-policy editor and new cleaners now. That would let an operator select
   unsupported or legally unapproved periods and could imply deletion guarantees the product
   cannot yet meet.

## Coverage Contract

The registry is the single source for the `/v1/privacy/retention` response. Internal resource names
are returned as grouped counts and safe resource-kind labels; raw credentials, URLs, object keys,
queue payloads, and tenant data are never included.

Automated tests parse all `CREATE TABLE` statements in `infra/migrations/*.up.sql` and fail when a
table is not assigned to exactly one lifecycle category. Tests also compare a maintained set of
object-prefix families, queue names, browser-key families, and provider boundaries to the registry.
This turns future storage additions into an explicit retention decision during development.

A registry entry may cover multiple related tables when they share one purpose and lifecycle.
Join/evidence tables follow their owning category unless they have a materially different retention
or legal-hold requirement.

## API

The existing authenticated `GET /v1/privacy/retention` route remains additive and backward
compatible:

- `plan` and `policies` remain;
- existing `category`, `label`, `duration`, `duration_days`, and `source` remain where applicable;
- each policy gains typed status, trigger, action, cadence, hold behavior, implementation/control
  reference, and resource-kind counts;
- the envelope gains a registry version and summary counts by lifecycle status.

The endpoint remains readable with `tenant:read`. It contains platform policy metadata only, so it
does not expose another tenant's records.

No migration is required in this slice because no mutable policy or runtime reconciliation state is
introduced. A later slice will add durable maintenance outcomes only when it can report real last
success/failure/backlog evidence from every cleaner.

## Backoffice

The Data & privacy retention section becomes a compact operational registry:

- summary counters distinguish enforced/configurable coverage from decisions and external controls;
- every row shows category, effective duration or explicit decision state, action, and status;
- expandable/details text shows trigger, cadence, hold behavior, control reference, and resource
  coverage counts;
- the old "Workspace lifetime" presentation is removed;
- decision-required rows use warning semantics without claiming non-compliance or making a legal
  conclusion.

The existing visual language remains: dense table, restrained status markers, no marketing cards.
Mobile collapses secondary columns and keeps status/duration readable without horizontal overflow.

## Failure And Safety Behavior

- Registry initialization fails tests and returns an internal error if a category is malformed,
  duplicated, or claims `enforced` without a duration, action, cadence, and implementation.
- A resource cannot belong to zero or multiple categories.
- Unknown plans continue to fail closed.
- The registry changes no cleaner behavior and cannot trigger deletion.
- Legal holds are reported only where code currently enforces them; all other categories state the
  missing decision/support explicitly.
- Provider-side retention remains `external_control` until configuration and contractual evidence
  are verified.

## Verification

Backend tests cover:

- registry validation and status invariants;
- exact PostgreSQL table coverage against all migrations;
- known object, queue, browser, and provider boundary coverage;
- plan-derived durations for attachments/replay;
- backward-compatible JSON plus summary counts;
- unknown-plan failure.

Web tests cover:

- typed API contract and English/Portuguese labels;
- enforced, configurable, decision-required, and external-control rows;
- compact desktop and mobile layout;
- no fallback to "Workspace lifetime".

The phase ends with full backend unit/integration/static checks, web unit/typecheck/build, focused
desktop/mobile E2E, plan/data-map updates, and a local commit. Authenticated staging validation stays
deferred; no deployment is performed.

## Follow-Up Boundary

The next #62 slices can consume this registry one category at a time:

1. durable cleaner health/backlog evidence;
2. approved authentication-session and platform-admin magic-link lifecycles;
3. delivery/outbox/notification cleanup;
4. tenant-configurable shorter periods and general hold scopes;
5. workflow, security/audit, billing, consent, AI, and telemetry decisions after founder/legal
   approval.
