# Organization two-factor authentication policy (issue #22)

Fidibeki-managed TOTP two-factor authentication with an organization policy of `optional`
(default, backwards compatible) or `required`.

## Decisions

- **Authentication method**: TOTP only (RFC 6238, HMAC-SHA1, 6 digits, 30s period, ±1 step skew).
  No SMS, push, or WebAuthn.
- **No new Go dependency**: TOTP, the `otpauth://` provisioning URI, the QR code encoder, and the
  AES-256-GCM secret cipher are implemented on the standard library inside `internal/twofactor`.
  A new direct dependency would also fail `make governance-check`.
- **Grace period for existing sessions**: none. When an administrator switches the policy to
  `required`, every already-issued member access token is rejected on its next request with
  `403 two_factor_enrollment_required` until the member enrols. This is the "restricted enrollment
  state" branch of the ticket, chosen because the alternative (bounded grace) leaves workspace data
  reachable for up to one access-token TTL. Enforced in `middleware.RequireTenantMember`, so it
  covers every tenant route by construction.
- **Challenge transport**: the TOTP challenge after primary authentication is carried by the
  existing HTTP-only refresh cookie, not by a second token type. `/auth/verify` creates the session
  in the `pending` two-factor state and returns no access token; `/auth/2fa/challenge` promotes the
  same session to `satisfied` and returns the normal token payload.
- **SAML/SSO**: SAML is not implemented yet. Sessions carry `authentication_method`
  (`local` | `saml`); a `saml` session is never moved into the `pending` state and never receives a
  Fidibeki challenge. The policy controls are non-applicable for such sessions, and the store-level
  behaviour is covered by tests so a future SAML integration inherits the exemption.
- **Lockout prevention**: an administrator may only switch the policy to `required` when they are
  themselves enrolled *and* have confirmed possession of their recovery codes. That guarantees at
  least one recoverable administrator survives the switch.
- **Secrets**: TOTP secrets are stored AES-256-GCM encrypted under `TWO_FACTOR_ENCRYPTION_KEY`
  (AAD binds tenant + member), never returned after enrolment. Recovery codes are stored as SHA-256
  hashes, displayed exactly once, and single-use. No secret, QR payload, code, or recovery code is
  ever logged or written to an audit row.

## Data model — migration `000111_add_two_factor_authentication`

- `tenants.two_factor_policy text NOT NULL DEFAULT 'optional'` (`optional` | `required`),
  `two_factor_policy_updated_at`, `two_factor_policy_updated_by`.
- `tenant_member_totp` — one row per member: encrypted secret, `confirmed_at`, `last_used_step`
  (replay protection), unique `(tenant_id, member_id)`, RLS + grants like the other tenant tables.
- `tenant_member_recovery_codes` — `code_hash` (SHA-256 hex), `used_at`, unique per member.
- `tenant_member_sessions` gains `authentication_method` (`local` | `saml`), `two_factor_state`
  (`not_required` | `pending` | `satisfied`), `two_factor_verified_at`.

## Backend

`internal/twofactor` (new): `totp.go`, `otpauth.go`, `recovery.go`, `crypto.go`, `qr.go`
(byte-mode QR encoder rendering an inline SVG, cross-checked against golden vectors).

`internal/tenantmember`:

| Route | Purpose |
| --- | --- |
| `POST /auth/2fa/challenge` | TOTP or recovery code after primary authentication (refresh cookie) |
| `GET /v1/security/two-factor` | Own enrolment status |
| `POST /v1/security/two-factor/enroll` | Start enrolment — secret, `otpauth://` URI, QR SVG |
| `POST /v1/security/two-factor/confirm` | Confirm with a code; returns recovery codes once |
| `POST /v1/security/two-factor/recovery-codes` | Regenerate (recent auth + valid code) |
| `DELETE /v1/security/two-factor` | Remove (recent auth + valid code; blocked when required) |
| `GET/PATCH /v1/security/two-factor/policy` | Organization policy (`tenant:read` / `tenant:update`) |
| `GET /v1/security/two-factor/compliance` | Per-member enrolment status, never secrets |
| `POST /v1/members/{memberID}/two-factor/reset` | Administrator-assisted recovery |

`/auth/verify` and `/auth/refresh` return `two_factor.status` = `challenge_required` or
`enrollment_required` instead of tokens when the session is not compliant.

Rate limiting: Redis-backed windows per session and per IP on challenge, confirm, and removal.
Auditing: `tenant_access_events` rows for policy change, enrolment, confirmation, removal,
recovery-code use, failed challenges, and administrative resets.

## Frontend

- `/auth/two-factor` — challenge page (authenticator code or recovery code).
- `/auth/two-factor-setup` — mandatory enrolment flow (QR + manual secret + recovery codes).
- Profile → security card: enrol, regenerate recovery codes, remove.
- Settings → new **Security** tab: policy selector plus the compliance table.
- Global handling of `403 two_factor_enrollment_required` → redirect into the setup flow.
- English and Portuguese catalog entries (the i18n parity and source-coverage tests are enforced).

## Tests

- `internal/twofactor`: RFC 6238 vectors, secret/recovery generation, cipher round-trip and AAD
  rejection, QR golden vectors.
- `internal/middleware`: the required-policy gate, API keys exempt, enrolment routes reachable.
- `internal/tenantmember`: HTTP error mapping unit tests plus a PostgreSQL integration test that
  covers optional enrolment, challenge, recovery-code single use, required-policy enforcement,
  removal rules, administrator reset, lockout prevention, and the SAML-exempt session.
- Web: unit tests for the two-factor client helpers; Playwright coverage of the challenge and
  forced-enrolment routes.
