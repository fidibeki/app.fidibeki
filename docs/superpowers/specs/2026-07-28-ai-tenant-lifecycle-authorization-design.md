# AI Tenant Lifecycle Authorization Design

**Date:** 2026-07-28
**Issue:** [#158](https://github.com/fidibeki/app.fidibeki/issues/158)
**Status:** Approved through the standing approval for recommended security plans

## Problem

AI work is authorized when feedback enters the queue, but tenant lifecycle can
change before the worker sends data to Gemini. The current scheduler,
reconciler, text claim, and attachment claim do not consistently require an
active tenant. Once a claim succeeds, the processor calls the external provider
without checking lifecycle again.

This allows queued feedback text or attachment content to leave Fidibeki after:

- a workspace is suspended or cancelled;
- a trial expires; or
- workspace deletion is requested.

The fix must also prevent inactive work from being rediscovered indefinitely,
record a terminal safe state, preserve budget correctness, and continue deleting
files already held by Gemini.

## Lifecycle Policy

AI analysis is eligible at an evaluation time only when both conditions hold:

1. `deletion_requested_at IS NULL`; and
2. the tenant is `active`, or is `trial` with `trial_ends_at` strictly after the
   evaluation time.

Suspended, cancelled, expired-trial, and deletion-requested tenants are
ineligible. Deletion grace may remain reachable for read-only product access,
but it never authorizes new external AI processing.

Migration `000129` will define
`fidibeki_ai_tenant_processing_eligible(...)` as the single SQL expression for
this policy. Scheduler, claims, reconciliation, and just-in-time authorization
must all use it.

## Considered Approaches

### 1. Scheduler and claim predicates only

This is the smallest change, but a lifecycle transition after claim can still
occur before the provider call. It does not satisfy the claim-to-send acceptance
case.

### 2. Layered authorization

Apply the same policy at scheduling/reconciliation, atomically at claim, and
again immediately before the external call. This closes all material queue and
worker windows while keeping database transactions short.

This is the selected approach.

### 3. Hold a database row lock during the provider call

This gives strict serialization between lifecycle updates and provider calls,
but an external request can hold a database connection and block suspension or
deletion for the task timeout. The configured AI concurrency would make this an
operational availability risk. It is not selected.

## Data Flow

### Scheduling

`PrepareFeedback` locks the feedback and evaluates the tenant lifecycle before
reserving budget or preparing tasks.

If the tenant is ineligible, existing pending or processing text and attachment
work for that feedback is cancelled:

- processing status becomes `skipped`;
- enqueue and budget-deferral fields are cleared;
- matching budget reservations are refunded;
- open processing evidence is marked `discarded` with
  `tenant_lifecycle_inactive`; and
- completion is recalculated.

`PendingFeedbackIDs` applies the lifecycle predicate so ineligible feedback is
never discovered as pending.

### Reconciliation

At the start of each reconciliation cycle, a bounded sweep cancels ineligible
text and attachment work. The sweep uses row locking with `SKIP LOCKED` so
parallel workers cannot process the same feedback and one busy row does not
stall the batch.

`InFlightTasks` also applies the lifecycle predicate to text and attachment
branches. The Gemini file-deletion branch remains lifecycle-independent because
provider cleanup must continue after suspension or deletion request.

### Claim

Text and attachment claims evaluate lifecycle in the same transaction that
changes processing state and creates processing evidence. An ineligible claim
is terminal no-work:

- cancel and refund the exact generation or attempt;
- discard matching processing evidence if it exists;
- commit the safe state; and
- return `ErrNoWork`, which the worker acknowledges without retry.

Database errors fail closed and remain retryable. No external provider or object
storage call occurs.

### Just-In-Time Authorization

After claim and rate-limit evaluation, but immediately before data access or the
provider call, the processor re-evaluates lifecycle.

For text, authorization precedes `AnalyzeText`.

For attachments, authorization precedes both `OpenObject` and
`AnalyzeAttachment`.

If lifecycle changed after claim, the exact work item is cancelled and the
handler returns success without retry. If authorization cannot be evaluated
because the database is unavailable, the handler returns an error and calls
neither storage nor Gemini.

The remaining interval between the committed authorization read and the
function call is intentionally minimal. Holding a database lock throughout a
remote call was rejected because it could delay lifecycle operations for the
full provider timeout.

## Cancellation State

The existing `processing_status` value `skipped` is the terminal public state.
No enum expansion or frontend compatibility migration is needed.

For claimed work, `ai_processing_runs` records:

- `status = 'discarded'`;
- `failure_reason = 'tenant_lifecycle_inactive'`; and
- `completed_at` set to the cancellation time.

For attachment rows, `error_message` is cleared. Lifecycle state and tenant
identifiers are not copied into error text or provider metadata.

Budget reservations are refunded only when cancellation changes an eligible
pending/processing work item. Idempotent repeated cancellation therefore cannot
decrement usage more than once.

## Gemini File Cleanup

Lifecycle guards apply only to analysis work. Existing `ai_gemini_files`
deletion preparation, claims, retries, and in-flight reconciliation continue for
inactive and deletion-requested tenants. This ensures prior provider uploads
still follow their deletion/retention workflow.

## Error Handling

- Lifecycle mismatch: terminal `skipped`, acknowledge task, no retry.
- Database or authorization failure: fail closed, return retryable error, no
  storage/provider call.
- Stale generation or attempt: existing `ErrNoWork` behavior remains.
- Provider failure after authorization: existing transient/permanent handling
  remains.
- Cancellation/refund failure: roll back the transaction and retry; never call
  the provider with uncertain authorization.

## Test Strategy

PostgreSQL integration tests will prove:

- active and unexpired-trial tenants remain eligible;
- suspended, cancelled, expired-trial, and deletion-requested tenants are
  rejected;
- a lifecycle change between enqueue and claim produces no analyzer call and a
  terminal safe state;
- a lifecycle change between claim and provider authorization produces no
  analyzer call;
- attachment rejection opens neither object storage nor the analyzer;
- authorization database failure is fail-closed;
- pending and in-flight reconciliation excludes inactive work;
- the cancellation sweep terminates and refunds inactive backlog;
- repeated cancellation is idempotent; and
- Gemini file-deletion work is still reconciled for inactive tenants.

Focused package tests, race tests, repository formatting/linting, coverage,
vulnerability checks, and an independent security review are required before
integration. Delivery is staging-only; production is not part of this issue.
