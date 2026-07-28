# Platform backoffice design

## Decision

Create `backoffice/`, an independently deployed Nuxt application for Fidibeki
platform administrators. It shares no route or application shell with the
tenant-facing `web/` app. It is served at a dedicated `admin` origin and uses a
same-origin server proxy to reach the existing Go API. The proxy always supplies
the trusted internal host header for the platform API, so a browser cannot select
another tenant or API host.

The delivery is deliberately additive: existing API paths keep their contracts.
The backoffice calls the existing `/admin/tenants` mutation endpoints and new,
namespaced `/admin/backoffice/*` read/auth endpoints are added where a robust UI
needs capabilities the existing API does not expose.

## User-facing scope

The backoffice provides:

1. Passwordless platform-admin sign-in and logout, with recovery through the
   existing rotating, HTTP-only session cookie. The login link is issued by a new
   backoffice-specific endpoint and lands on the backoffice verification page;
   it never puts a refresh token, magic-link token, or administrator email in
   browser storage or a redirect URL.
2. An operations dashboard with tenant totals by status and plan, trial-expiry
   and recent-signup signals, and direct links to operational tools.
3. A searchable, cursor-paginated workspace directory, filters for plan/status,
   and a workspace detail page with billing/contact data, current owner, project
   and member counts, usage signals, and the immutable platform access ledger.
4. Safe workspace actions: create, edit, suspend/reactivate, recover ownership,
   and force deletion. Destructive operations require an explicit typed
   confirmation and state the impact before sending the existing API request.
5. Proxied access to the existing queue monitor and privacy-incident operations
   workspace, preserving their narrow path-scoped cookies. No second copy of
   those operational UIs is created.

The initial release does not invent mutation endpoints for billing, feedback,
or tenant members when the platform API has no corresponding audited authority.
It exposes the available support and operations surface rather than creating an
unsafe impersonation mechanism. New admin actions require a separately reviewed
backend contract and audit trail.

## Architecture

```
Browser (admin.<domain>)
  -> Nuxt backoffice server proxy (same origin, forwards cookies/bearer token)
  -> Go API (x-fidibeki-proxy-host = api.<domain>)
  -> PostgreSQL admin pool
```

`backoffice/` owns presentation, route guards, client session state, API error
rendering, and local visual tests. `backend/internal/platformadmin` owns
authorization, all database reads/writes, validation, audit events, and magic
link issuance. The new read model lives behind a small `BackofficeStore`
interface so HTTP handlers depend on a testable contract instead of SQL.

### New additive API

| Endpoint | Authority | Purpose |
| --- | --- | --- |
| `POST /admin/backoffice/auth/login` | none | Issue a platform magic link whose verified browser origin is the configured backoffice origin. Generic accepted response prevents account enumeration. |
| `GET /admin/backoffice/overview` | `platform:tenant:read` | Aggregated, non-PII operational totals. |
| `GET /admin/backoffice/tenants` | `platform:tenant:read` | Validated query/filter/cursor-paginated workspace directory. |
| `GET /admin/backoffice/tenants/{tenantID}` | `platform:tenant:read` | Workspace detail and safe operational summary. |
| `GET /admin/backoffice/tenants/{tenantID}/recovery-candidates` | `platform:tenant:update` | Bounded, cursor-paginated active members with a recorded login; excludes the current owner. |
| `GET /admin/backoffice/tenants/{tenantID}/access-events` | `platform:tenant:read` | Bounded, cursor-paginated platform audit ledger. |

Existing `POST/PATCH/DELETE /admin/tenants*` remain unchanged and are the only
workspace mutation paths. The new read endpoints return no secrets, raw session
tokens, magic links, attachment content, replay payloads, or tenant-member
credentials.

## Security model

- Every new read endpoint uses the existing platform-audience JWT and explicit
  `platform:tenant:read` permission; mutations retain their current narrower
  permissions.
- The backoffice proxy accepts only expected admin API paths and overwrites,
  rather than forwards, the trusted internal proxy host and secret headers.
- The backoffice login limiter uses a browser address asserted by that
  authenticated proxy; a direct request cannot supply the limiter address.
- The login endpoint derives its verify URL from an exact configured backoffice
  origin. It does not accept a caller-provided redirect URL.
- Refresh credentials remain `HttpOnly`, `Secure` outside local/test,
  `SameSite=Strict`, and path-scoped. Access tokens live only in in-memory Nuxt
  state and are cleared on logout/failed refresh.
- Browser pages use no-store and no-referrer headers. The application includes a
  restrictive Content Security Policy and all user-supplied values are rendered
  with Vue escaping, never `v-html`.
- Force delete and ownership recovery demand explicit confirmations; backend
  validation and existing audit writes remain the authority.

## Testing strategy

TDD is applied as vertical slices:

1. A failing Go HTTP test for each new authorization, validation, pagination,
   and data-redaction behavior; then the minimal handler/store code to pass it.
2. A failing Nuxt unit test for proxy host isolation, auth refresh/logout,
   route guard behavior, API contract parsing, and destructive-confirmation
   gating; then minimal code to pass it.
3. Playwright end-to-end coverage using the local stack for passwordless login,
   dashboard/directory navigation, filtered search, safe mutation flows, and
   an unauthorized-user denial.
4. Full backend, frontend, type, build, lint/format, security/vulnerability,
   and integration suites before the PR. Screenshots are captured from the
   browser E2E flow and attached to the PR.

## Operations and root administrator

The root account is created idempotently with the existing audited bootstrap
command, using `acidiney.dias@fidibeki.com` and the full platform-permission
set. It is passwordless: the account can only request a magic link. The precise
production invocation is documented but is not run against a production
database from a development workstation; local-stack verification runs it
against the local Compose database.

## Non-goals and follow-up boundary

This feature gives platform staff a secure operating console over the platform
capabilities that already exist, plus safe aggregate/read capabilities necessary
to operate them. It does not bypass tenant isolation, add account impersonation,
or silently broaden platform authority. Future modules such as billing
reconciliation or support tooling must begin with their own additive API,
permission, audit, and retention design.
