---
skill: task-breakdown
version: 1
date: 2026-07-27
status: approved
scope: LOCKED
---

# Platform backoffice delivery plan

## Shared context

- Dedicated Nuxt application: `backoffice/`; no tenant-web routes or UI are
  changed.
- Existing `/admin/tenants*` mutation routes are reused unchanged. New routes
  are confined to `/admin/backoffice/*`.
- Platform JWT audience and explicit permissions remain the authorization
  boundary. Cookies remain path-scoped, HttpOnly, and SameSite Strict.
- The Nuxt server proxy sets the trusted API host and proxy secret itself. It
  never accepts them from browser headers.
- No impersonation, secret exposure, or tenant isolation bypass is in scope.

## Prerequisites

- Deployment config supplies `PLATFORM_BACKOFFICE_URL` (exact, absolute admin
  origin) and the backoffice process receives the same internal proxy secret as
  the API.
- Production root bootstrap is an explicit operational command after deployment;
  local integration verification uses the Compose database only.

## Task 1: Backoffice magic-link origin

**Depends on:** None

**Outcome:** A platform administrator can request a generic accepted response
whose magic link lands at the configured backoffice origin; existing admin login
behavior is unchanged.

**Why:** Proves the highest-risk cross-origin authentication boundary first.

**Acceptance:** A Go HTTP test requests `/admin/backoffice/auth/login`, asserts
an unknown address is indistinguishable from a known one, and verifies a known
admin's queued link has the exact configured origin and no caller-selected
redirect.

**Autonomy:** AFK

## Task 2: Backoffice aggregate read model

**Depends on:** Task 1

**Outcome:** Authorized administrators can retrieve bounded, non-PII overview
totals; unauthorized/wrong-audience requests are rejected.

**Why:** Establishes the backend read-model interface and authorization pattern.

**Acceptance:** A Go HTTP test proves `GET /admin/backoffice/overview` returns
only aggregate values for `platform:tenant:read` and returns 401/403 otherwise.

**Autonomy:** AFK

## Task 3: Paginated workspace directory

**Depends on:** Task 2

**Outcome:** Authorized administrators can search and filter workspaces with a
validated opaque cursor and stable result ordering.

**Why:** Provides the primary operating workflow without unbounded database reads.

**Acceptance:** A Go HTTP test proves invalid filters/cursors are rejected and a
two-page status/plan-filtered query has no duplicate or skipped workspace.

**Autonomy:** AFK

## Task 4: Workspace detail and audit ledger

**Depends on:** Task 3

**Outcome:** Workspace details include only operational metadata and a bounded
platform access ledger, never credentials, token material, raw attachments, or
replay content.

**Why:** Enables support decisions with a redaction-safe backend contract.

**Acceptance:** A Go HTTP test asserts the detail/audit responses include their
documented records and exclude a seeded sensitive column.

**Autonomy:** AFK

## Task 5: Dedicated Nuxt shell and hardened proxy

**Depends on:** Task 1

**Outcome:** `backoffice/` builds independently and proxies only platform admin
API/operations paths with an overwritten trusted host and secret.

**Why:** Keeps the new browser surface isolated from tenant routing.

**Acceptance:** A Vitest test proves the proxy maps an allowed path to the API
origin with `api.<base-domain>` and rejects a tenant/public path or caller-set
proxy header.

**Autonomy:** AFK

## Task 6: Browser passwordless session flow

**Depends on:** Tasks 1, 5

**Outcome:** Admins can request a link, verify it, refresh using only an
HttpOnly cookie, logout, and are guarded away from protected pages when unauthenticated.

**Why:** Delivers secure entry before exposing operational data.

**Acceptance:** A Vitest test proves refresh retries one protected request and
clears in-memory access state on a failed refresh.

**Autonomy:** AFK

## Task 7: Dashboard and accessible navigation

**Depends on:** Tasks 2, 6

**Outcome:** A responsive, keyboard-accessible dashboard presents overview
signals and links to Workspaces, Queues, and Privacy operations.

**Why:** Makes the platform's existing operations discoverable.

**Acceptance:** A Playwright test signs in with mocked API data and verifies the
overview totals and navigation landmarks are visible.

**Autonomy:** AFK

## Task 8: Workspace directory and detail user flows

**Depends on:** Tasks 3, 4, 6, 7

**Outcome:** Operators can search/filter the directory, paginate results, and
inspect workspace metadata/audit activity.

**Why:** Delivers the main manage-and-investigate workflow.

**Acceptance:** A Playwright test filters by plan/status, opens a result, and
verifies the expected detail and audit rows.

**Autonomy:** AFK

## Task 9: Safe lifecycle mutations

**Depends on:** Task 8

**Outcome:** Operators can create/edit/suspend/reactivate/recover ownership and
force-delete through existing endpoints, with permission-aware UI and typed
confirmation for destructive actions.

**Why:** Completes the supported workspace-management lifecycle.

**Acceptance:** A Playwright test proves a force-delete request is impossible
until the exact workspace slug is confirmed, then sends the existing DELETE API
request only once.

**Autonomy:** AFK

## Task 10: Operational proxy paths and runbook

**Depends on:** Tasks 5, 6

**Outcome:** Existing queue and privacy-operations pages work under the
backoffice origin with their scoped cookies, and deployment/bootstrap steps are
documented.

**Why:** Avoids duplicating mature operational UIs and makes setup repeatable.

**Acceptance:** A Nuxt server test proves an `/admin/operations/*` request is
proxied while preserving the path-scoped cookie and denying unrelated paths.

**Autonomy:** AFK

## Task 11: Root bootstrap and local stack proof

**Depends on:** Tasks 1-10

**Outcome:** The named root account is provisioned idempotently without a
password, and the runbook distinguishes local verification from production.

**Why:** Ensures the authorized operator can enter the delivered console.

**Acceptance:** An integration test runs the bootstrap command twice against
the local database and proves one admin has the full platform permission set.

**Autonomy:** AFK

## Task 12: Quality gate, visual evidence, and PR

**Depends on:** Tasks 1-11

**Outcome:** Full verification, security review, screenshots, and one pull
request show the complete deliverable is ready for merge.

**Why:** Prevents an unreviewed privileged interface from shipping.

**Acceptance:** CI reports all required checks green and the PR contains the
screenshots plus test evidence.

**Autonomy:** AFK

## Dependency order

`1 -> 2 -> 3 -> 4 -> 8 -> 9 -> 11 -> 12` and
`1 -> 5 -> 6 -> 7 -> 8`, with `5 + 6 -> 10 -> 11`.

## Plan self-review

- Every task has one observable acceptance test.
- Authentication/proxy risk is first; UI follows stable contracts.
- All dependencies and external configuration are explicit.
- Each task is a vertical, independently verifiable slice.
