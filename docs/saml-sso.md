# Organization SAML SSO

Fidibeki authenticates workspace members through an organization's own SAML 2.0
identity provider. Configuration is per organization and **disabled by default**:
a workspace that never sets it up keeps the existing magic-link sign-in
unchanged.

SAML authenticates *identity only*. Fidibeki permissions remain the
authorization source — no assertion attribute grants a role, and there is no
group-to-role mapping in this version.

## Endpoints

Every URL is derived from the running environment (`WEB_PUBLIC_BASE_URL` or
`PUBLIC_BASE_URL`, plus `BASE_DOMAIN` and the workspace slug). Nothing is
hardcoded to a staging or production host.

| Purpose | URL |
| --- | --- |
| SP metadata / entity ID | `https://<slug>.<base-domain>/auth/sso/saml/metadata` |
| Assertion consumer service (ACS) | `https://<slug>.<base-domain>/auth/sso/saml/acs` |
| SP-initiated start | `https://<slug>.<base-domain>/auth/sso/saml/start` |
| Policy discovery (public) | `https://<slug>.<base-domain>/auth/sso/saml/status` |

The settings UI shows the exact values for the deployment you are looking at;
copy them from there rather than from this table.

## Administrator setup

Requires the `tenant:update` permission (the workspace-administrator tier that
already manages members, billing, and privacy settings).

1. Open **Settings → Single sign-on**.
2. Copy the entity ID and ACS URL into your identity provider's SAML
   application. Fidibeki requests the `emailAddress` NameID format and reads a
   verified email claim; every other asserted attribute is ignored and never
   stored.
3. Point Fidibeki at the identity provider using one of:
   - **Metadata URL** — Fidibeki fetches and parses it. The fetch is HTTPS-only
     outside local development, refuses private, loopback, and link-local
     destinations, does not follow redirects, and is size- and time-bounded.
   - **Metadata XML** — paste the document.
   - **Manual** — entity ID, HTTP-Redirect single sign-on URL, and a signing
     certificate in PEM or base64.
4. Optionally list **verified email domains**. When set, only those domains may
   be asserted. Workspace membership is always required regardless.
5. Run a **test sign-in**. This uses the same flow members will use and creates
   no session.
6. Choose an enforcement mode.

## Enforcement modes

| Mode | Behaviour |
| --- | --- |
| **Disabled** | Existing Fidibeki authentication applies. |
| **Optional** | Members may use SAML or a Fidibeki sign-in link. |
| **Required** | Normal member access must use the identity provider. |

**Required cannot be selected** until a test sign-in has succeeded against the
current identity provider *and* at least one signing certificate is currently
valid. Changing the identity provider's entity ID or single sign-on URL clears
the successful-test marker and drops the workspace back to Optional, so Required
never applies to an untested configuration.

## Multi-factor authentication

A SAML-authenticated session delegates multi-factor assurance to the identity
provider. Fidibeki does not add a second challenge to those sessions, and the
settings page says so explicitly while Required mode is active.

Fidibeki has no local TOTP factor today; member sign-in is magic-link based.
Every session now records the credential path that produced it
(`authentication_method` is `local` or `saml`, alongside the asserting identity
provider and subject), which is the enforcement point a future Fidibeki-managed
second factor will read. Enabling SSO deletes no credentials, so restoring local
authentication restores whatever local policy applied before.

## Certificate rotation

Several signing certificates can be trusted at once. Rotate without an
authentication outage:

1. Add the identity provider's **next** certificate under **Settings → Single
   sign-on → Signing certificates**. Both are now accepted, so no in-flight
   sign-in fails.
2. Switch the identity provider over to the new certificate.
3. Remove the retired certificate.

Guards that make this safe:

- The last remaining certificate cannot be removed while SSO is enabled.
- Removing the last *currently valid* certificate drops the workspace out of
  Required mode rather than leaving it enforcing an integration that can no
  longer verify a signature.
- The settings page warns when a certificate expires within 30 days, and warns
  when none is currently valid.

Refreshing from a metadata URL or XML document replaces the trusted set;
adding a certificate through the rotation control adds to it.

## Recovery

### Workspace owner break-glass

Under Required mode the **workspace owner** keeps local magic-link sign-in. It is
restricted to that single member, surfaced on the login page behind a
disclosure, and every use writes an `sso.break_glass_login_requested` event to
the access audit log.

Use it when:

- the identity provider's signing certificate has expired,
- the identity provider is unreachable,
- a configuration change broke assertion validation.

Once signed in, the owner can fix the configuration, re-run the test sign-in, or
switch the mode back to Optional.

### Certificate expiry

The symptom is `sso.login_failed` audit events with reason `assertion_invalid`.
Sign in as the workspace owner, add the identity provider's current signing
certificate, and re-run the test sign-in.

### Identity provider outage

Set the mode to **Optional** as the owner. Members can then sign in with
Fidibeki links until the provider is back, at which point Required can be
restored — the successful-test marker survives a mode change.

### Total lockout

If the owner account itself is unavailable, workspace ownership recovery is a
platform-administrator operation (see the existing ownership recovery flow in
[runbook.md](runbook.md)).

## Assertion validation

Every response is checked for:

- XML-DSig signature against the organization's own trusted certificates;
- issuer matching the configured identity provider entity ID;
- audience matching this workspace's entity ID;
- `Recipient` and `Destination` matching this workspace's ACS URL;
- `Conditions` `NotBefore` / `NotOnOrAfter` and issue instant, within bounded
  clock skew;
- `InResponseTo` matching an AuthnRequest this service provider issued and has
  not yet consumed;
- assertion-ID replay against a per-organization ledger;
- a present, normalized, verified email claim.

An assertion is only ever linked to an existing active member of *that*
organization, or to an outstanding invitation for it. A matching email address
never links an identity across organizations.

**IdP-initiated SSO** is off by default and can be enabled per organization. It
widens exactly one thing: a response carrying no `InResponseTo` is accepted. A
response quoting an `InResponseTo` that matches no live request is still
rejected, enabled or not.

## Audit

Every configuration, test, enablement, disablement, certificate rotation,
break-glass use, and sign-in outcome is written to the tenant access-event
ledger with `resource_type = sso_configuration`, visible under **Settings →
Access audit**. Assertions, IdP-asserted attributes, private keys, and raw
tokens are never logged.

Event kinds: `sso.configured`, `sso.mode_changed`, `sso.disabled`,
`sso.certificate_added`, `sso.certificate_removed`, `sso.test_started`,
`sso.test_succeeded`, `sso.login_succeeded`, `sso.login_failed`,
`sso.break_glass_login_requested`.

## Data retention

Correlation rows (`tenant_saml_auth_requests`) and the replay ledger
(`tenant_saml_assertions`) are hard-deleted hourly once their authoritative
expiry has passed — the same boundary the validator already enforces, so a swept
row never had remaining function. The sweep reports to the bounded
retention-health ledger as `saml_auth_artifacts`.

The service-provider private key is sealed with AES-256-GCM under
`INTEGRATION_ENCRYPTION_KEY`, scoped to its tenant, and never leaves the API.

## Out of scope

SCIM provisioning and deprovisioning, automatic role mapping from IdP groups,
social OAuth login, and a Fidibeki-hosted identity provider.
