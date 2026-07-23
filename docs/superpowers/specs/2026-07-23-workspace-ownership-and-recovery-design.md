# Workspace Ownership and Recovery Design

**Date:** 2026-07-23
**Issue:** #24
**Environment boundary:** staging only; no production or infrastructure work

## Objective

Give every usable workspace one explicit owner and prevent permission or member lifecycle changes
from making the workspace unmanageable. Ownership, administrative access, current sessions, and
support recovery must be one coherent security model rather than a UI permission preset.

## Existing-System Audit

- Members currently have only an email, optional name, permission array, bounce state, and login
  timestamp. There is no owner, lifecycle status, removal endpoint, or tenant access audit stream.
- Signup creates the first member with every tenant permission, but ownership is inferred from
  insertion order. Platform-admin tenant creation can create an empty workspace.
- Permission edits write the new array directly. They do not protect an administrator, revoke
  sessions, or invalidate already-issued ten-minute access tokens.
- Refresh-token reuse revokes a member's refresh sessions, providing an existing revocation pattern.
- Tenant authentication already runs inside the RLS transaction and can check current member state
  without adding Redis, a denylist service, or another infrastructure dependency.
- Settings lists members and edits permission presets but has no member status, owner marker,
  transfer, deactivate, or remove action.
- Issue activity is resource-specific and cannot serve as an organization access ledger.

## Approaches Considered

### 1. Tenant owner foreign key plus member lifecycle (recommended)

Store `owner_member_id` on the tenant and lock that tenant row for transfer. This gives one canonical
owner reference and makes concurrent transfer serializable. Member status and authorization version
remain on the member, where login and session checks already operate.

### 2. `is_owner` flag on members

A partial unique index can prevent two owners but cannot naturally prevent zero owners during
separate mutations. Queries and safeguards must repeatedly rediscover the owner from the member set.

### 3. Separate versioned ownership table

This provides ownership history but duplicates the append-only access ledger and adds another
aggregate for every authentication-sensitive operation. It is unnecessary for the current product.

The first approach is selected. History belongs in the audit ledger, while the tenant row holds the
current invariant.

## Data Model

Migration `000060` adds:

- `member_status`: `active`, `deactivated`, `removed`.
- `tenant_members.status`, defaulting to `active`.
- `tenant_members.authorization_version`, starting at `0` for compatibility with current JWTs.
- `tenants.owner_member_id`, with a tenant-scoped foreign key to `tenant_members`.
- `tenant_access_events`, an append-only RLS table containing event kind, tenant, optional member or
  platform-admin actor, optional subject member, structured data, and timestamp.

Existing workspaces with members use the earliest created member as owner and ensure that member has
the complete tenant permission set. Truly empty legacy workspaces remain unclaimed for explicit
support recovery; they have no member who could safely be invented. Product signup and platform
tenant creation must always create and assign an owner, so no new unclaimed workspace is produced.

Removed members remain as inert records so issue/activity foreign keys and attribution survive. They
have no permissions or sessions. PII minimization and later pseudonymization belong to #25.

## Current-Authority Enforcement

Tenant JWTs add `authorization_version`. Existing tokens decode as version `0`, matching migrated
members. On every member-token request, tenant authentication reads the tenant-scoped member and
requires:

- status `active`;
- token version equal to current member version;
- tenant ID and member ID match.

The middleware returns permissions from the current member row after the version check. API keys
continue through their existing independent branch.

Permission reduction, deactivation, removal, transfer elevation, and recovery increment the affected
member version and revoke every refresh session in the same transaction. Existing access tokens fail
their next request immediately; refresh tokens cannot mint replacements.

## Tenant Operations

All operations lock the tenant before member rows, preserving one lock order.

### Ownership transfer

`POST /v1/ownership/transfer` requires tenant/member update permissions, the current owner as actor,
authentication by magic link within the last ten minutes, the target member ID, and explicit workspace
slug confirmation. The target must be active and have completed a login. The transaction:

1. locks tenant and current/target members;
2. grants the target the full owner permission set;
3. increments the target authorization version and revokes target sessions;
4. changes `owner_member_id` once;
5. appends an `ownership.transferred` event.

The former owner remains an active administrator until deliberately changed. Concurrent transfers
serialize on the tenant row; a request from the former owner fails after the first commits.

### Permission and lifecycle changes

- The owner's permissions cannot be reduced below the complete owner set.
- `PATCH /v1/members/{id}/status` supports active/deactivated transitions for non-owner members.
- `DELETE /v1/members/{id}` marks a non-owner as removed, clears permissions, consumes pending magic
  links, increments authorization version, and revokes sessions.
- Reactivation requires member-update permission and restores no permissions implicitly.
- A protection failure appends a failed-check audit event. The handler commits only that audit row;
  no protected mutation is applied.
- The owner always satisfies the recoverable-administrator invariant. No operation may remove,
  deactivate, or demote that final protected administrator before transfer.

## Support-Assisted Recovery

`POST /v1/platform/tenants/{tenantID}/ownership/recover` requires platform tenant-update permission,
a platform-admin login within the last ten minutes, workspace slug confirmation, an active verified
target member, a customer-verification reference, and a human reason.

Recovery atomically assigns and elevates the target, deactivates the inaccessible former owner when
present, revokes both members' sessions, and appends `ownership.recovered` with the platform-admin
actor and verification reference. It is not exposed in tenant Settings and is never an unaudited DB
shortcut. A runbook requires support to verify control of the workspace's configured billing email
and one independent organization proof before executing the endpoint.

## Settings Experience

The Members table exposes owner and lifecycle status as compact labels. The owner row cannot show
deactivate/remove controls. An owner with recent authentication can open a Transfer ownership dialog,
select an active verified member, enter the workspace slug, and confirm. Other members expose edit,
deactivate/reactivate, and remove icon actions according to RBAC.

Dialogs explain the consequence at the action point. Errors remain adjacent to the relevant control.
Desktop and mobile layouts retain the existing dense Settings table rather than adding nested cards.

## Error Contract

Domain conflicts return additive, stable error codes for owner protection, last administrator,
recent authentication, invalid confirmation, inactive/unverified target, and already removed member.
Authentication-version mismatch or inactive membership returns the existing unauthenticated response,
so revoked credentials reveal no extra account state.

## Delivery Slices

1. **Ownership invariant and current authority:** migration/backfill, versioned JWT enforcement,
   atomic owner transfer, failed protection audit, and owner-visible Settings transfer.
2. **Member lifecycle:** deactivate/reactivate/remove, permission protections, session revocation,
   audit coverage, and responsive actions.
3. **Recovery and provisioning:** owner-backed platform creation, support recovery endpoint/runbook,
   unclaimed legacy handling, and full concurrency/isolation validation.

## Verification

- Migration `up/down/up`, RLS, backfill, and tenant-member foreign-key tests.
- Unit and PostgreSQL integration tests for transfer races, old-token rejection, refresh revocation,
  owner/last-admin guards, lifecycle transitions, audit persistence on denied operations, and tenant
  isolation.
- Platform tests for owner-backed provisioning, recent-auth recovery, verification requirements,
  and complete audit attribution.
- Web unit/type/build coverage plus Playwright desktop/mobile Settings workflows and visual
  inspection for long identities and narrow dialogs.
- One real staging journey: invite/activate target, transfer ownership, prove old owner cannot transfer
  again, prove an invalidated target token fails, inspect audit rows, and restore the test workspace.

## Self-Review

The design has no placeholders. Ownership has one source of truth; JWT invalidation and refresh-session
revocation are consistent; empty legacy tenants are not assigned fabricated identities; support recovery
has explicit verification and audit requirements; and no production or new infrastructure work is included.
