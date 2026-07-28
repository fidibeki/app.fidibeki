# Tenant Authentication Lifecycle Enforcement

## Context

Issue #152 identifies a lifecycle gap in every public tenant authentication
transition. A suspended, cancelled, or expired-trial workspace can currently
request and consume local magic links, start or complete SAML, rotate refresh
sessions, and finish a pending two-factor challenge.

The existing `RequireActive` middleware protects most authenticated routes, but
authentication routes intentionally run before an access token exists. Checking
only the tenant object resolved before the transaction is also insufficient: the
workspace can change state after resolution and before the session mutation.

## Decision

Use one domain policy and enforce it against the current database row inside the
same transaction as each authentication mutation.

### Lifecycle policy

Add `tenant.Tenant.IsActiveAt(now)` as the authoritative policy:

- `active` is eligible;
- `trial` is eligible only when `trial_ends_at` exists and is strictly after
  `now`;
- `suspended`, `cancelled`, a missing trial end, and an expired trial are
  ineligible;
- a workspace with `deletion_requested_at` remains eligible during the existing
  deletion-grace period, regardless of its status. `RequireWritable` continues
  to make that workspace read-only.

`RequireActive` will delegate to this policy so authenticated route behavior and
authentication transitions cannot drift.

### Transactional lifecycle guard

Add a tenant-package guard that reloads `status`, `trial_ends_at`, and
`deletion_requested_at` by tenant ID with `FOR SHARE`, then evaluates
`IsActiveAt(now)`.

The row lock gives lifecycle changes and authentication transitions a defined
order:

- a lifecycle change committed first makes the authentication transition fail;
- an authentication transaction that obtains the lock first completes before a
  concurrent lifecycle update can commit.

Trial expiry is time-based rather than a row update, so long paths re-evaluate
the policy immediately before the final session or handoff mutation.

### Enforcement points

Local authentication:

- magic-link requests check before member lookup and return the existing generic
  `202 Accepted` without creating a link, email job, or break-glass audit when
  ineligible;
- the concrete store checks again before inserting a link;
- magic-link consumption and refresh rotation check while locking the tenant
  authentication policy, before consuming a link or rotating a session.

SAML:

- login and test initiation check before loading provider material or creating
  an AuthnRequest;
- assertion consumption checks before parsing and again immediately before
  resolving the member and creating the single-use handoff;
- the concrete SAML magic-link store checks once more before insertion;
- public failures use the existing generic redirect contract, with inactive
  lifecycle classified as `sso_unavailable`; the ACS forces transaction
  rollback before returning that redirect so no partial assertion state can
  commit.

Two-factor:

- loading a pending challenge checks lifecycle before reading or locking the
  member session;
- successful possession is checked again before changing the session from
  `pending` to `satisfied`;
- an ineligible response is `403 tenant_suspended` and does not consume a
  recovery code, promote the session, or issue an access token.

Logout remains available so an inactive workspace can discard a browser
credential. Public SAML metadata/status and privacy obligations are not session
promotion transitions and remain outside this change.

## Error and Privacy Contract

- `POST /auth/login` remains enumeration-safe: every syntactically valid request
  receives the same accepted response.
- Verify, refresh, two-factor challenge, and protected SAML test requests return
  the existing `tenant_suspended` error for an inactive workspace.
- Refresh and two-factor failures clear or withhold browser credentials as they
  do for other failed authentication transitions.
- SAML browser navigation returns a coarse login redirect and never exposes the
  underlying status, assertion, member identity, or token.
- No inactive login attempt creates a misleading successful break-glass audit.

## Test Strategy

Use a fixed clock where trial boundaries matter and real PostgreSQL integration
tests for state-changing behavior.

1. Domain table tests cover active, unexpired trial, exact trial boundary,
   expired trial, suspended, cancelled, and deletion grace over inactive and
   expired-trial states.
2. Middleware tests prove it delegates to the same policy.
3. Local HTTP integration tests cover, for suspended, cancelled, and expired
   trial:
   - no email or magic-link row on login request;
   - a link created while active cannot create a session after the transition;
   - a refresh session cannot create a successor after the transition.
4. Two-factor integration tests create a pending challenge while active, change
   lifecycle, and prove the session remains pending with no access token.
5. SAML integration tests prove inactive initiation creates no correlation row,
   and a request started while active creates no handoff after the lifecycle
   transition.
6. Active, unexpired-trial, and deletion-grace paths retain working local and
   SAML authentication.
7. Focused package tests, full backend coverage/lint/format gates, and staging
   health/log checks are required before closing #152.

## Alternatives Considered

### Route middleware only

Applying `RequireActive` to public auth routes is small, but it gives login
requests a different public response and does not protect service/store callers
or a lifecycle change between middleware and mutation.

### Service checks against the resolved tenant

This preserves handler behavior and is easy to test, but the resolved object was
read before the tenant transaction. It leaves a time-of-check/time-of-use gap and
does not protect direct store callers.

### Domain policy plus transactional guards

This is the selected approach. It adds one small database read/row lock to each
authentication transition, centralizes policy, protects non-HTTP callers, and
provides deterministic behavior under concurrent lifecycle changes.

## Non-goals

- Rate limits and correlation-window storage for SAML remain in #156 and #157.
- Passwordless-login budgets and cooldowns remain in #155.
- Queued AI and issue-tracker lifecycle enforcement remain in #158 and #159.
- No production deployment or production infrastructure change is part of this
  work.
