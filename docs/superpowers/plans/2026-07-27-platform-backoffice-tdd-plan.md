---
date: 2026-07-27
status: approved
scope: locked
source: docs/superpowers/specs/2026-07-27-platform-backoffice-design.md
---

# Platform backoffice TDD implementation plan

## Shared constraints

- Create a standalone Nuxt app under `backoffice/`; leave `web/` routes and UI
  untouched.
- Existing `/admin/tenants*` mutation routes remain the authority. Every added
  backend route is under `/admin/backoffice/*`.
- Use platform-audience JWTs, explicit permissions, same-origin proxying, and
  path-scoped HttpOnly cookies. Never add impersonation or cross-tenant bypass.
- Add `PLATFORM_BACKOFFICE_URL` as the exact allowed magic-link origin, plus
  matching Nuxt proxy configuration and deployment documentation.

## Vertical slices

### 1. Magic-link origin contract

Write a failing platform-admin HTTP test for `POST /admin/backoffice/auth/login`:
known and unknown emails both return accepted, and a known admin receives a
configured-origin URL with no caller-controlled redirect. Add only the service
and config seam needed to pass it.

### 2. Aggregate dashboard contract

Write a failing HTTP test for a permission-protected
`GET /admin/backoffice/overview`, including wrong-audience and missing-permission
cases. Implement a bounded, non-PII store projection and handler.

### 3. Workspace directory contract

Write a failing HTTP/store test for plan/status/query filters, stable descending
ordering, opaque cursors, input limits, and no duplicate across adjacent pages.
Implement the paginated projection and route.

### 4. Workspace detail and audit contract

Write failing tests that prove a detail projection and its access ledger expose
only operational data; invalid IDs and token/session/secret fields never leak.
Implement the read model, indexes/migration only if query evidence requires it,
and routes.

### 5. Hardened standalone proxy

Scaffold `backoffice/` with Nuxt/Vitest/Playwright. Start with a failing proxy
unit test: only admin API and existing operations paths are allowed; callers
cannot inject the trusted proxy host/secret. Implement the narrow middleware.

### 6. Passwordless browser session

Write a failing frontend test for refresh/retry/logout memory state and route
guard denial. Add login and verify screens, in-memory access state, and response
cookie forwarding. Refresh credentials must never enter local/session storage.

### 7. Dashboard UX

Write a Playwright test for a signed-in user seeing accessible dashboard totals
and navigation landmarks. Add the application layout, overview cards, loading,
empty, and error states.

### 8. Directory and detail UX

Write a Playwright test for filtering/paginating workspaces and opening a detail
with audit rows. Add query synchronization, validation/error rendering, and
keyboard-accessible data table/card views.

### 9. Safe lifecycle UX

Write a Playwright test proving force deletion cannot dispatch until an exact
slug confirmation is entered and only one existing DELETE call follows. Extend
the same pattern to create/edit/suspend/reactivate and ownership recovery.

### 10. Existing operations proxy and runbook

Write a server test for forwarding `/admin/queues/**` and
`/admin/operations/**` with their scoped cookies while rejecting unrelated
paths. Add deployment, local launch, root bootstrap, and rollback documentation.

### 11. Root bootstrap proof

Add an integration test that runs the existing `admin bootstrap` command twice
against local PostgreSQL and proves one passwordless account at
`acidiney.dias@fidibeki.com` retains the complete platform-permission set.

### 12. Release evidence

Run the full Go/Node quality gates, browser E2E, vulnerability checks, and an
independent security review. Capture the authenticated dashboard and workspace
detail screenshots, attach them to the single PR, and wait for every Actions
check to pass.

## Order

`1 -> 2 -> 3 -> 4 -> 8 -> 9 -> 11 -> 12` and
`1 -> 5 -> 6 -> 7 -> 8`, with `5 + 6 -> 10 -> 11`.

## Plan review

- Each slice begins with exactly one behavior-level failing test.
- Auth/proxy risks are resolved before UI work.
- New data is bounded, explicitly authorized, and redaction-tested.
- No task changes an existing route contract.
