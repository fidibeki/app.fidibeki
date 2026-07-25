# Organization SAML SSO with IdP-managed MFA

Implements [issue #23](https://github.com/acidiney/app.fidibeki/issues/23).

## Interaction with Fidibeki two-factor authentication

Fidibeki-managed TOTP landed on main in #98, and its session model already
anticipates SAML: `SessionSecurity.AuthenticationMethod` exists and
`Requirement()` exempts a SAML session from the challenge. This work feeds that
model rather than building a parallel one — the SAML handoff stamps the session
as `saml`, so the existing state machine does the exempting.

On top of that:

- Required SSO disables the organization's Fidibeki two-factor control and says
  why, without rewriting the stored policy.
- Enabling SSO never deletes a TOTP enrolment or its recovery codes.

## Architecture

Backend is Go + chi, one package per feature under `backend/internal`. Tenant traffic is
resolved by subdomain and runs inside an RLS-scoped app-pool transaction. The Nuxt web app at
`<slug>.<baseDomain>` proxies `/auth/*` and `/v1/*` to the API, so SAML endpoints live under
`/auth/sso/saml/*` and are reachable from the browser and the IdP on the tenant's own origin.

New package: `backend/internal/samlsso`.

Dependency: `github.com/crewjam/saml v0.5.1` (with `russellhaering/goxmldsig`) for XML-DSig
verification, metadata parsing, and AuthnRequest construction. Hand-rolling XML signature
validation invites signature-wrapping bugs; this is the mature, widely deployed Go SP.

## URL derivation

All SP identifiers derive from the active environment, never hardcoded:

| Value | Template |
| --- | --- |
| SP entity ID | `<web-origin-scheme>://<slug>.<BASE_DOMAIN>[:port]/auth/sso/saml/metadata` |
| ACS URL | `…/auth/sso/saml/acs` |
| Start URL | `…/auth/sso/saml/start` |
| Post-login | `…/auth/verify#token=…` (existing magic-link landing page) |

New optional env `WEB_PUBLIC_BASE_URL` (defaults to `PUBLIC_BASE_URL`) supplies the
browser-facing scheme/port. Production leaves it unset; local dev sets
`http://api.localhost:3000` so the Nuxt dev server terminates the flow.

## Data model (migration `000112`)

- `tenant_sso_mode` enum: `disabled` | `optional` | `required`.
- `tenant_saml_configurations` — one row per tenant. IdP entity ID, SSO URL, optional metadata
  URL, verified email domains, `allow_idp_initiated`, SP key pair (private key AES-256-GCM
  encrypted under `INTEGRATION_ENCRYPTION_KEY`), `last_tested_at`, `last_tested_subject`,
  `last_successful_login_at`, `mode`, audit columns.
- `tenant_saml_certificates` — N signing certificates per configuration, so rotation overlaps
  old and new without an outage. Unique on `(tenant_id, fingerprint)`.
- `tenant_saml_auth_requests` — SP-initiated request correlation (`InResponseTo`), single-use,
  short TTL, carries purpose (`login` | `test`), relay state, and the initiating member for
  test logins.
- `tenant_saml_assertions` — assertion-ID replay ledger keyed by `(tenant_id, assertion_hash)`.
- `tenant_magic_links` gains `authentication_method`, `saml_idp_entity_id`, and
  `saml_subject_id`; `tenant_member_sessions` gains the two identity columns
  (migration `000111` already added `authentication_method` there for two-factor).

All tenant-scoped tables get the standard RLS isolation policy.

## Authentication flow

SP-initiated:

1. `GET /auth/sso/saml/start` — resolves the tenant, refuses when mode is `disabled`, records
   an auth-request row, 302s to the IdP via HTTP-Redirect binding.
2. IdP authenticates (including MFA) and POSTs to the ACS.
3. `POST /auth/sso/saml/acs` — validates signature, issuer, audience, recipient, destination,
   timestamps, `InResponseTo` against the stored request, and assertion-ID replay. Normalises
   and requires a verified email claim. Resolves an existing active member or an outstanding
   invitation *for this tenant only* — never cross-organization by email.
4. On success the ACS mints a 2-minute single-use magic link marked `authentication_method =
   'saml'` and 302s to `/auth/verify#token=…`. The existing verify page exchanges it for a
   session, so session rotation, reuse detection, and the SPA flow are reused unchanged.

IdP-initiated is accepted only when the tenant opted in *and* the response carries no
`InResponseTo`; a non-empty `InResponseTo` that matches no stored request is always rejected
(this is stricter than crewjam's `AllowIDPInitiated`, which would accept any value).

Test logins use the same ACS with `purpose = 'test'`: they never create a session, they stamp
`last_tested_at`, and they redirect back to the settings page with a pass/fail result.

## Policy enforcement

- `disabled` — magic-link login unchanged.
- `optional` — both paths available.
- `required` — `POST /auth/login` returns `403 sso_required` for ordinary members. The
  workspace owner (`tenants.owner_member_id`) keeps local login as the documented break-glass
  path: single member, always audited as `sso.break_glass`, and surfaced in the settings UI.
- `required` cannot be selected until a test assertion has succeeded *and* at least one
  currently-valid signing certificate exists.
- Removing the last valid certificate, or changing the IdP entity ID/SSO URL, clears
  `last_tested_at` and drops the tenant out of `required`, with a warning in the response.

`GET /auth/sso/saml/status` is unauthenticated and returns only organization-level policy
(`mode`, `start_url`, `local_login_enabled`) — never membership.

## Audit

Every configuration, test, enable, disable, certificate rotation, break-glass, and login event
writes to the existing `tenant_access_events` ledger via `accessaudit.Record` with
`resource_type = 'sso_configuration'`. Assertions, attributes, private keys, and raw tokens are
never logged.

## API surface

Unauthenticated (tenant host):

- `GET /auth/sso/saml/metadata` — SP metadata XML
- `GET /auth/sso/saml/status` — policy discovery
- `GET /auth/sso/saml/start` — SP-initiated redirect
- `POST /auth/sso/saml/acs` — assertion consumer

Authenticated, `tenant:update`:

- `GET /v1/sso/saml`
- `PUT /v1/sso/saml` — configure by metadata URL, metadata XML, or manual fields
- `PATCH /v1/sso/saml/mode`
- `DELETE /v1/sso/saml`
- `POST /v1/sso/saml/certificates`, `DELETE /v1/sso/saml/certificates/{id}`
- `POST /v1/sso/saml/test` — returns the test start URL

Metadata-by-URL fetches are SSRF-guarded: HTTPS only outside local/test, no private, loopback,
or link-local destinations, bounded body size and timeout, no redirects to private space.

## Frontend

- `web/pages/login.vue` — queries SSO status; renders "Continue with single sign-on" for
  `optional`, and replaces the email form entirely for `required` (owner break-glass link
  behind a disclosure).
- `web/pages/settings/index.vue` — new **Single sign-on** tab for `tenant:update` holders:
  SP metadata/entity ID/ACS URL with copy buttons, IdP configuration form, certificate list
  with rotation, test-login button, mode selector gated on a passing test, and the Required-mode
  "MFA is managed by your identity provider" notice.
- i18n strings in `en` and `pt` message catalogues.

## Documentation

- `docs/saml-sso.md` — administrator setup, certificate rotation, IdP outage and lockout
  recovery, break-glass procedure.
- README section wiring it into the existing auth documentation.

## Tests

- `samlsso` unit tests: mode transitions, lockout guards, email normalisation, member linking
  rules, SSRF guard, metadata parsing, URL derivation.
- Assertion validation tests against a locally generated IdP (crewjam's `saml` IdP side signs
  real assertions): valid, bad signature, wrong issuer, wrong audience, expired, unsolicited,
  replayed, and cross-tenant assertions.
- Integration tests over real PostgreSQL for RLS isolation, replay persistence, and
  session/`authentication_method` marking across two tenants.
- Web Vitest coverage for the login-page policy rendering.
