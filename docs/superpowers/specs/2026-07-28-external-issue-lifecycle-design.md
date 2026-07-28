# External Issue Lifecycle Authorization Design

**Date:** 2026-07-28
**Issue:** [#159](https://github.com/fidibeki/app.fidibeki/issues/159)
**Status:** Approved through the standing approval for recommended security plans

## Problem

Automatic Jira and Linear creation is authorized only by connection
configuration. `OutboxResolver` does not evaluate the tenant that owns a
connection, and `AutoCreateHandler` does not re-evaluate that tenant before
creating an external link, decrypting credentials, and sending issue content to
the provider.

An `issue.created` task queued while a workspace is active can therefore send
its title, description, and priority after the workspace is suspended,
cancelled, or past its trial end. A pending workspace deletion also leaves an
otherwise active tenant eligible today.

The existing Slack and webhook path already checks active and trial status at
both destination resolution and worker execution. Its SQL is package-private,
however, and it does not explicitly reject an active tenant with a pending
deletion request.

## Lifecycle Policy

Outbound delivery is eligible at an evaluation time only when both conditions
hold:

1. `deletion_requested_at IS NULL`; and
2. the tenant is `active`, or is `trial` with `trial_ends_at` strictly after the
   evaluation time.

Suspended, cancelled, expired-trial, trial-without-end, and deletion-requested
tenants are ineligible. The exact trial boundary is ineligible.

Deletion grace remains reachable for the existing read-only product workflows,
but it does not authorize sending new tenant data to Slack, webhooks, Jira, or
Linear.

## Considered Approaches

### 1. Guard only the automatic worker

This prevents a queued task from reaching Jira or Linear after a lifecycle
change, but inactive workspaces still produce queue destinations. It does not
satisfy the resolver acceptance criteria and creates avoidable queue traffic.

### 2. Add a PostgreSQL lifecycle function

A database function would centralize the predicate, but it adds a migration and
schema lifecycle for a compact policy already consumed only by Go-owned SQL.
That is unnecessary scope for this issue.

### 3. Share a tenant-owned SQL predicate and guard both boundaries

The tenant package will expose a parameterized SQL predicate for the canonical
outbound policy. The existing Slack/webhook resolver and worker query, plus the
Jira/Linear resolver and worker eligibility query, will all use it.

This is the selected approach. It centralizes the database policy without a
migration and preserves each delivery pipeline's existing transaction and
error-handling model.

## Data Flow

### Destination resolution

`issuetracker.OutboxResolver` will join `issue_tracker_connections` to
`tenants` and apply the shared predicate with its injected clock. Unsupported
events remain ignored before database access. Eligible active and unexpired
trial tenants continue to produce one destination per configured automatic
provider; ineligible tenants produce none.

The integration resolver will replace its private status expression with the
same shared predicate. This adds the explicit deletion-requested denial to
Slack and webhook fanout.

### Queued automatic creation

`AutoCreateHandler.create` will evaluate the owning tenant with the shared
predicate before loading the connection or issue, creating an external-link
row, decrypting provider credentials, constructing a provider client, or
making HTTP requests.

An ineligible tenant is a terminal suppression. The handler records a system
denial in `tenant_access_events` with:

- kind `issue_tracker.auto_create_suppressed`;
- resource type `issue_tracker` and the Fidibeki issue ID;
- action `automate` and outcome `denied`; and
- only the provider and reason `tenant_lifecycle_inactive`.

The event stores no issue content, credential material, endpoint, tenant
lifecycle value, or provider response. If the audit write fails, the handler
fails closed and remains retryable without accessing credentials or the
provider. A successful suppression returns nil so Asynq does not retry it.

The existing connection, issue, privacy-restriction, idempotency, provider, and
activity behavior remains unchanged for eligible tenants.

### Slack and webhook delivery

The integration worker's existing pre-decryption eligibility result will use
the shared predicate. Existing failed `delivery_logs` evidence remains its
suppression audit mechanism; this issue does not change that ledger or delivery
handler flow.

## Concurrency

The eligibility read is immediately before automatic creation work. A
lifecycle transition committed before the worker's read is always observed and
suppressed.

The implementation will not hold a tenant row lock across a provider HTTP
request. Such a lock could delay suspension or deletion for the provider
timeout and consume a database connection for every in-flight delivery. A
transition racing after the authorization read retains the same narrow
check-to-send interval as the existing Slack/webhook path.

## Error Handling

- Ineligible tenant: record the denial, acknowledge the task, and send nothing.
- Missing tenant: acknowledge the stale task and send nothing.
- Eligibility query or audit failure: return an error and fail closed before
  credentials, links, clients, or HTTP.
- Disconnected, disabled, or unconfigured provider: preserve the current
  terminal no-op behavior.
- Privacy restriction: preserve the current terminal suppression.
- Provider or link failure for an eligible tenant: preserve the current
  retryability and activity behavior.

## Test Strategy

A PostgreSQL integration test with a fixed clock and local fake Jira and Linear
providers will prove:

- active Jira automatic creation still succeeds;
- an unexpired-trial Linear automatic creation still succeeds;
- suspended, cancelled, expired-trial, and deletion-requested tenants resolve
  no automatic destinations;
- a task resolved while active but executed after suspension returns
  successfully without credential decryption, provider HTTP, or external-link
  creation;
- the suppression writes the minimal system denial with the expected provider
  and reason; and
- the exact trial boundary is ineligible.

The no-decryption assertion will replace the queued provider credential with
invalid ciphertext before executing the inactive task. Returning successfully
demonstrates the lifecycle guard ran before credential decryption. Provider
request counters and link counts independently prove no outbound call or link
creation occurred.

The existing integration-delivery test will add an active
deletion-requested case, proving Slack/webhook resolution and worker execution
use the same policy.

Focused package tests, the full backend suite, formatting, vet, coverage,
race-enabled focused tests, and `git diff --check` will be run as feasible. No
provider account, customer data, staging, or production environment is used.
