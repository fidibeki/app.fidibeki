# Integration Downstream Action Checklist Design

## Status And Scope

This design completes the local product work remaining in GitHub issue #66. It extends the
integration disclosure ledger introduced by migration 99 and reuses the privacy-case and recipient
action workflows introduced for #60.

The scope is staging-only product implementation. It does not deploy services, change production
configuration, claim legal compliance, or pretend that incoming Slack, Teams, or generic webhooks
can update, delete, or recall a delivered message.

## Decision

Create a dedicated, durable downstream-action checklist instead of overloading
`privacy_recipient_actions`.

Recipient actions are transactional email instructions sent to a named external contact. A
checklist entry must exist even when no contact email is known, when the integration was deleted,
or when the destination exposes no mutation API. Keeping these concepts separate preserves the
existing delivery/retry contract while allowing a checklist entry to reference a recipient action
later.

A read-only derived checklist was rejected because it could not record ownership, completion
evidence, or the fact that a new disclosure reopened an already completed action.

## Data Model

Migration 100 adds `privacy_integration_actions`, tenant-scoped with forced RLS.

Each row represents one action kind and one stable destination key for a privacy case:

- privacy case and tenant;
- optional current integration ID plus the stable `integration:{id}` delivery key;
- destination kind and display name snapshot;
- action kind: `rectification` or `erasure`;
- handling mode: `manual` or `api`;
- status: `manual_required`, `api_pending`, `completed`, or `failed`;
- destination capability snapshot;
- disclosed category names only, never payload content;
- disclosure count plus first and last successful disclosure timestamps;
- completion member, time, note, idempotency key, and request fingerprint.

The unique key is `(tenant_id, privacy_case_id, destination_key, action_kind)`.

All currently supported destinations are incoming webhooks and therefore generate
`handling_mode=manual`, `status=manual_required`. The schema reserves API states for a future
authenticated destination adapter, but this slice does not generate an API action.

`privacy_recipient_actions` gains an optional checklist reference. This links a sent correction or
deletion instruction to the affected destination without treating successful email delivery as
proof that the destination completed the requested action.

## Checklist Discovery

Refreshing a checklist considers successful integration delivery logs only.

For a verified reporter case, eligible feedback and issues are those owned by the case email digest
and created no later than the case receipt time, matching the existing reporter rectification
scope. Issue ownership also includes linked reporter feedback.

For a verified member case, only issue deliveries whose matching issue activity was performed by
the subject member and whose disclosed categories include `workflow_context` are eligible. This
avoids claiming that a member was disclosed merely because an unrelated issue was delivered.

Rows are grouped by stable destination key. Refresh is idempotent:

- new destinations create checklist rows;
- repeated refresh updates category/timestamp/count evidence without duplicating rows;
- a successful disclosure newer than a completed action reopens it as `manual_required` and clears
  stale completion evidence;
- deleted integrations remain represented by the delivery-key and kind snapshots;
- failed, suppressed, pending, or bounced attempts do not count as external disclosure.

## API And Workflow

Two writable tenant-admin endpoints are added:

- `POST /v1/privacy/cases/{caseID}/integration-actions/refresh`
- `POST /v1/privacy/cases/{caseID}/integration-actions/{actionID}/complete`

Both require an active human member, `tenant:update`, a writable workspace, tenant RLS, and a
verified non-terminal rectification or erasure case.

Completion is allowed only for `manual_required` rows and requires a concise evidence note plus an
`Idempotency-Key`. Repeating the same completion returns the existing result; changing the payload
under the same key returns the existing idempotency-conflict contract. API-mode rows cannot be
manually completed through this endpoint.

The existing recipient-action create endpoint accepts an optional checklist ID. It verifies that
the row belongs to the same case and destination before linking the email instruction.

Every create, reopen, link, and completion writes the existing privacy-case timeline plus the
tenant privacy audit stream with identifiers and category names only.

## Backoffice Experience

Rectification and erasure case details gain a "Destination checklist" section before recipient
notifications.

The section provides:

- an explicit refresh command;
- destination name/kind, disclosure count, latest disclosure time, and category names;
- a capability label that states `Manual action required` for current webhook destinations;
- any linked recipient-notification delivery status;
- a completion note and command for manual rows;
- a clear empty state when no successful external disclosure exists.

The UI never offers an "automatic delete" command for current Slack, Teams, or webhook
destinations. It explains that completion records operator evidence rather than remote deletion
guaranteed by Fidibeki.

## Failure And Safety Behavior

- Unknown, cross-tenant, ineligible, or deleted case/action identifiers return the standard
  not-found or unsupported response without leaking another tenant's resources.
- Checklist refresh remains retryable and transactional.
- Completion cannot erase or rewrite disclosure evidence.
- Restriction continues to block future provider requests before delivery.
- No recipient email, credentials, page query/hash, payload content, or provider secret is copied
  into checklist or audit rows.

## Verification

Backend integration coverage must prove:

- reporter and member scope discovery;
- successful-only delivery selection;
- grouping and category union;
- idempotent refresh and reopen after a newer disclosure;
- deleted integration preservation;
- completion evidence and conflict behavior;
- manual/API capability boundaries;
- recipient-action linking;
- permission and tenant isolation.

Web coverage must prove:

- checklist refresh, empty/manual/completed/reopened states;
- completion note validation;
- recipient notification linkage;
- English and Portuguese copy;
- desktop and mobile layout without overlap.

The phase ends with migration `up/down/up`, full backend/web regression checks, and an authenticated
staging journey only after deployment work is explicitly authorized again.
