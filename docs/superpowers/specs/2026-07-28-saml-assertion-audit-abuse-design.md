# SAML Assertion Audit Abuse Protection Design

**Date:** 2026-07-28
**Issue:** [#157](https://github.com/fidibeki/app.fidibeki/issues/157)
**Scope:** Backend and staging validation only

## Problem

The SAML assertion consumer is necessarily public because an identity provider
posts there before the browser has a Fidibeki session. The current handler
resolves the tenant, parses up to 1 MiB of form data, validates the response,
and records every `ConsumeAssertion` error as a tenant-visible
`sso.login_failed` access event.

Those failure events have no deduplication window and the endpoint has no
request guard. A caller can therefore submit a short invalid base64 value
repeatedly and cause one database write and one tenant-visible security event
per request. The entries use a system actor even though the input has not been
authenticated by the configured identity provider.

## Decision

Use three independent controls:

1. Apply fail-closed network and tenant budgets before reading or parsing the
   posted form.
2. Distinguish anonymous input from an assertion whose IdP signature has
   already been validated.
3. Deduplicate equivalent failure signals for 15 minutes using a finite,
   server-owned action key derived from provenance and the existing coarse
   rejection reason.

The request guard bounds parser and validator work. Audit deduplication is a
separate database-write invariant if requests are distributed across networks,
arrive at a limiter boundary, or bypass the normal request volume assumptions.
Verified assertion provenance preserves useful operational evidence without
trusting arbitrary request content.

## Public Assertion Guard

Add an `AssertionGuard` contract to `samlsso.HTTPOptions`. Its production
implementation uses the existing abuse evaluator, Redis limiter, and HMAC
keyring with two sequential fail-closed policies:

| Scope | Limit | Window |
| --- | ---: | ---: |
| tenant + trusted client network | 20 | 15 minutes |
| tenant | 200 | 15 minutes |

The assertion allowance is twice the public SAML start allowance so one browser
retry does not consume the entire response budget. The tenant-wide cap still
bounds IdP-initiated traffic and distributed callers.

The network policy runs first. Requests denied by that policy do not consume the
tenant-wide allowance. Forwarding headers are accepted only through
`httpx.ClientIPFromRequest` and the configured trusted proxy CIDRs. Redis keys
contain HMAC digests rather than raw tenant IDs or IP addresses.

The handler checks the guard immediately after tenant resolution and before
`http.MaxBytesReader`, `ParseForm`, `ResponseInResponseTo`, XML parsing,
signature validation, correlation mutation, or audit insertion. A missing
guard, a limiter error, or an unavailable decision fails closed and redirects
to the workspace login page with the same coarse
`sso_error=sso_unavailable` result. A normal rate-limit denial emits no warning
per request and creates no audit row.

## Verified Assertion Provenance

An assertion becomes IdP-verified only after crewjam successfully validates the
signature, issuer, audience, destination, recipient, timestamps, and SAML
conditions. Errors before that boundary, including invalid base64, invalid XML,
unknown correlation, and a bad signature, remain anonymous.

After that boundary, wrap later failures with a small internal provenance value:

- configured IdP entity ID;
- whether the signed assertion was solicited or IdP-initiated;
- the SAML purpose when correlation has resolved it.

The wrapper continues to unwrap to the existing sentinel error, so public
redirect behavior and callers using `errors.Is` remain compatible. It never
contains subject IDs, email addresses, assertion IDs, raw XML, signatures, IP
addresses, or correlation IDs.

Provider-side checks that happen after cryptographic validation, such as a
missing required email claim, also use verified provenance. Signature failures
do not.

## Bounded Audit Signal

Keep the existing event kind and resource:

- kind: `sso.login_failed`;
- resource type: `sso_configuration`;
- resource ID: the tenant ID;
- actor: system;
- outcome: denied.

Set a 15-minute `DedupWindow`. The current access-audit unique key does not
include event data, so the action becomes a finite server-owned dimension:

`login_failed_{anonymous|idp_verified}_{coarse_reason}`

This retains separate bounded signals for, for example, anonymous invalid
assertions and verified ineligible members without adding attacker-controlled
cardinality or a schema migration. Event data repeats the coarse reason and
provenance. Verified events may additionally include the configured IdP entity
ID, correlation mode, and known purpose.

All reason values come from `assertionFailureReason`; no exception string or
request field is used in the dedup key. Within one 15-minute bucket, equivalent
failures collapse to one row under the existing concurrent-safe unique index.

Malformed forms and missing responses are rejected before assertion validation
and create no tenant audit event. They remain bounded by the public assertion
guard.

## Alternatives Rejected

### Deduplication without a request guard

This bounds ledger writes but still lets public traffic spend CPU on form, XML,
and signature parsing. It also depends on the database to absorb every
conflicting insert.

### Request guard without audit deduplication

A distributed caller can still consume the tenant-wide allowance as distinct
database writes. Boundary windows also permit bursts on both sides of a reset.

### Treat every SAML error as verified

The endpoint is public. Invalid base64 and invalid signatures do not establish
IdP provenance and must not appear as authoritative identity-provider events.

### Store client IPs, response hashes, or assertion identifiers

Those values add personal or attacker-controlled data to an append-only tenant
ledger without being required for the bounded signal. The existing abuse
diagnostics and HMAC limiter keys are the appropriate volume-control layer.

### Add a new audit deduplication column

A dedicated dimension would be more general, but this issue has a finite,
server-owned classification that fits the existing action key. A migration and
shared access-audit API expansion would add risk without improving this
security invariant.

## Test Strategy

Implement the fix in red-green vertical slices:

1. HTTP/PostgreSQL regression: repeated invalid base64 responses create one
   anonymous event in the 15-minute window.
2. Guard tests: both budgets, sequential short-circuiting, HMAC-only keys,
   missing limiter, and limiter outage.
3. HTTP ordering tests: denial, missing guard, and limiter outage occur before
   form parsing and create no audit row; trusted proxy resolution supplies the
   network.
4. Signature tests: a tampered response is anonymous, while a genuinely signed
   ineligible-member response records verified IdP provenance.
5. Malformed and oversized forms and a missing response create no assertion
   audit event.
6. Redis integration: concurrent requests enforce the network cap atomically
   and rejected network traffic does not exhaust the tenant budget.
7. Existing SAML correlation, replay, lifecycle, and success flows remain
   green.

After focused tests, run formatting, lint, race detection, vulnerability
scanning, and the repository coverage gate. Require a fresh security review
before integration. Push only after review and validate the API in staging; do
not deploy production.
