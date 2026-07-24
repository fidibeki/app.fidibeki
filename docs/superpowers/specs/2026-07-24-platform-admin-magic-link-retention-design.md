# Platform Admin Magic-Link Retention Design

**Issue:** #62
**Status:** Approved under the user's standing authorization for recommended decisions
**Environment boundary:** Local and staging only; no production or deployment work

## Problem

`platform_admin_magic_links` has an authoritative `expires_at` boundary and the authentication
store rejects links at or after that boundary, but expired rows are never removed. The rows retain
an email address and credential hash after they have lost all authentication function.

The tenant equivalent is already enforced by `tenantmember.Maintainer`: expired links are deleted
in bounded, concurrent-safe batches and a minimized audit event is written in the same statement.
Platform-admin links cannot use `tenant_access_events` because they have no tenant scope. There is
no existing global platform authentication event sink.

This slice removes only expired platform-admin magic links. It does not choose a retention period
for platform-admin sessions, raw IP/user-agent data, administrators, or security audit records.

## Options Considered

1. **Global minimized security-event ledger and atomic cleanup (recommended).**
   Delete expired links and insert non-content evidence in one PostgreSQL statement. This preserves
   demonstrable cleanup without retaining the email, token hash, or administrator identity.
2. **Structured logs only.**
   This is smaller, but logs are not a reliable application-owned proof of individual cleanup and
   their retention is provider/configuration dependent.
3. **Do nothing until all security-retention periods are approved.**
   This needlessly preserves dead credentials. The link's functional lifetime is already explicit
   in `expires_at`; cleaning it does not invent a legal retention period.

Option 1 is selected.

## Data Model

Migration `102` adds:

- `idx_platform_admin_magic_links_expires_at` for ordered expiry selection.
- `platform_security_events`, a global, append-only application ledger with:
  - server-issued UUID `id`;
  - bounded dotted `kind`;
  - bounded `resource_type`;
  - opaque UUID `resource_id`;
  - JSON object `data`;
  - server-authoritative `created_at`;
  - uniqueness on `(kind, resource_type, resource_id)`.

The runtime admin role receives only `SELECT` and `INSERT` on the event table. The tenant runtime
role receives no privileges. The cleanup event is:

- `kind`: `auth.platform_magic_link_expired`;
- `resource_type`: `platform_admin_magic_link`;
- `resource_id`: the deleted link ID;
- `data`: `{ "was_consumed": true|false }`.

The event deliberately excludes the administrator ID, email, token hash, expiry value, IP address,
and user agent. After deletion, its resource ID is no longer resolvable to the original account.

## Cleanup Flow

`platformadmin.MaintenanceStore.ExpiredMagicLinks` receives a UTC cutoff and batch limit. One SQL
statement:

1. selects `expires_at <= cutoff` rows ordered by `(expires_at, id)`;
2. locks candidates with `FOR UPDATE SKIP LOCKED`;
3. deletes the selected links;
4. inserts one minimized event per deleted row;
5. returns the deletion count.

Deletion and evidence are atomic. An event insertion failure rolls back the deletion. A retry sees
the original link and remains safe. Re-running after success finds no link and creates no duplicate
event.

The exact-boundary rule matches `ConsumeMagicLink`, which only accepts `expires_at > now`.
Consumed and unconsumed expired links are both dead credentials and are both removed.

## Worker

`platformadmin.Maintainer` follows the existing tenant magic-link maintainer:

- default batch size 200;
- maximum 20 batches per run;
- one-minute phase timeout;
- hourly execution;
- immediate run on worker startup;
- UTC injectable clock;
- aggregate counts and failure/budget signals only, with no personal data in logs.

It is wired to the admin pool beside `tenantmember.Maintainer`. Cancellation is clean and store
errors remain retryable on the next scheduled run.

## Retention Registry And Product Truth

The existing `auth_magic_links` category becomes the enforced policy for both tenant and
platform-admin magic links. `platform_admin_magic_links` moves out of the unresolved
`authentication_security` category. `platform_security_events` remains in
`authentication_security` with `decision_required` status because this slice does not invent a
maximum period for security evidence.

The executable registry continues to have 27 categories and expands from 67 to 68 PostgreSQL
tables. The technical data map states that both magic-link families are deleted within one hour of
their configured expiry and that only minimized evidence remains.

## Migration And Compatibility

Existing rows keep their original `expires_at`; migration does not reset clocks or delete data.
The worker classifies legacy rows naturally against that existing timestamp on its first run.
Migration down removes the event table and expiry index. Up/down/up replay must pass.

No endpoint, response shape, UI, provider, credential, or production configuration changes.

## Verification

Automated coverage must prove:

- exact-boundary, past, and future behavior;
- consumed and unconsumed expiry;
- absence of email, token hash, and administrator ID from evidence;
- atomic rollback when evidence insertion fails;
- idempotent reruns;
- batch limits;
- concurrent sweeps without double deletion or lost rows;
- maintainer drain, budget, cancellation, timeout, logging, and error behavior;
- admin-only table privileges and append-only runtime grants;
- migration up/down/up;
- all 68 tables covered exactly once by the retention registry;
- focused, uncached package tests, full backend tests, `go vet`, and formatting.

## Non-Goals

- Platform-admin session, IP, user-agent, administrator, or event-retention periods.
- General legal/customer hold semantics for security records.
- Backoffice cleanup status or alerts.
- Staging deployment or authenticated staging validation.
- Production work of any kind.
