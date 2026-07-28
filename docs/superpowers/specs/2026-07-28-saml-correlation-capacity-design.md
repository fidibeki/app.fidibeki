# SAML Correlation and Capacity Design

**Date:** 2026-07-28  
**Issue:** [#156](https://github.com/fidibeki/app.fidibeki/issues/156)  
**Scope:** Backend and staging validation only

## Problem

The public SAML start endpoint stores one pending authentication request per
call. Assertion validation currently loads only the 50 newest live request IDs
and gives that list to the SAML library. An attacker can therefore create 50
newer requests and make a legitimate, still-live request disappear from the
correlation set.

Increasing the list limit only changes the cost of the attack. Removing the
limit would preserve correctness but let public traffic grow both storage and
the assertion-validation working set without a bound.

## Decision

Use three independent controls:

1. Correlate a response against the exact `InResponseTo` candidate carried by
   its SAML Response envelope.
2. Rate-limit public SAML starts per tenant and trusted client network, and per
   tenant overall. Limiter failure is fail-closed.
3. Keep at most 128 live, unconsumed SAML requests per tenant. Reject a new
   request at capacity without deleting or invalidating an existing one.

These controls address different failure modes. Exact correlation removes the
newest-N eviction defect. The public rate limit controls normal abuse before a
database write. The database capacity limit remains the hard backstop for
concurrency, distributed traffic, authenticated test starts, and limiter
misconfiguration.

## Exact Correlation

Before signature validation, decode the SAML Response with `encoding/xml` and
read only the protocol Response root's `InResponseTo` attribute. Treat this
value as an untrusted lookup candidate, never as an authenticated identity.
Reject malformed base64, malformed XML, a wrong root namespace/name, or a
candidate longer than 128 bytes.

For a non-empty candidate, query whether that exact request ID is live,
unconsumed, and belongs to the current tenant:

- if live, pass only that ID to crewjam's `ParseXMLResponse`;
- if absent, expired, consumed, or owned by another tenant, pass no solicited
  IDs and let validation reject the response;
- if the candidate is empty, include the empty correlation value only when the
  tenant explicitly allows IdP-initiated SSO.

The envelope lookup does not authenticate the response. Crewjam still validates
the original response bytes, including signatures, issuer, audience,
destination, recipient, timestamps, and response/assertion correlation. After
validation, Fidibeki still atomically consumes the exact signed assertion
`InResponseTo` value and records the assertion replay hash.

The global `LiveAuthRequestIDs` query and its newest-50 window are removed.

## Public Start Guard

Add a small `StartGuard` contract to the SAML HTTP handler. Its production
implementation uses the existing abuse evaluator and HMAC keyring with two
sequential fail-closed policies:

| Scope | Limit | Window |
| --- | ---: | ---: |
| tenant + trusted client network | 10 | 15 minutes |
| tenant | 100 | 15 minutes |

The network key uses `httpx.ClientIPFromRequest`, so forwarding headers are
honored only for configured trusted proxies. A fixed internal project scope
keeps the existing network-bucket key contract intact. Raw tenant IDs and IP
addresses are HMAC-digested before they reach Redis.

The network policy runs first. A network-rejected request does not consume the
tenant-wide budget, so one noisy client cannot exhaust the allowance for every
other network merely by continuing to send already-rejected requests.

The guard runs after tenant resolution but before `StartLogin`, so a denial or
Redis failure creates no authentication-request row. Both cases redirect to the
workspace login page with the same coarse `sso_error=sso_unavailable` response.
Logs contain only the generic reason and underlying operational error, never
assertion contents, user identity, or the client IP. Protected administrator
test starts bypass the public limiter but remain subject to the database cap.

The handler fails closed when no guard is configured. Tests must explicitly
provide an allow guard, which prevents a future wiring omission from silently
removing the production control.

## Outstanding Request Capacity

`StartLogin` uses one timestamp for lifecycle eligibility, request expiry, and
capacity evaluation. `Store.CreateAuthRequest` will:

1. acquire a transaction-scoped PostgreSQL advisory lock derived from a fixed
   SAML namespace and the tenant ID;
2. remove expired request rows for that tenant as local hygiene;
3. count live, unconsumed requests;
4. return `ErrAuthRequestCapacity` when the count is already 128;
5. otherwise insert the new request.

The tenant transaction already wraps every SAML route. The advisory lock makes
the count-and-insert reservation atomic across concurrent starts without
locking the SAML configuration row or changing lock order with configuration
updates. A hash collision could serialize two unrelated tenants temporarily but
cannot weaken the cap.

At capacity, public starts use the same generic unavailable redirect. The
authenticated test endpoint returns a temporary service-unavailable error.
No issued request is evicted. Expired rows cease to count immediately even if
the scheduled retention job has not run.

No schema migration is required.

## Alternatives Rejected

### Increase or remove the newest-request limit

A larger fixed limit preserves the eviction vulnerability. No limit avoids
eviction but makes assertion processing depend on an attacker-controlled list.

### Rate limiting without a hard cap

Rate limiting reduces traffic but is not a storage invariant. Distributed
clients, concurrency at the boundary, or incorrect deployment configuration
could still create an unbounded live set.

### Correlation cookie

A signed, browser-bound cookie would add useful binding but also introduces
SameSite behavior, key rotation, multi-tab handling, and IdP redirect
compatibility. Exact server-side correlation already solves this issue without
that browser-state risk. Cookie binding can be considered separately as
defense-in-depth.

### Database slot column and unique index

Explicit slots would enforce the cap at schema level, but require migration,
backfill, slot reuse, and compatibility rules for existing live requests. A
tenant-scoped transaction lock provides the same reservation invariant within
the existing transaction boundary with less schema risk.

## Test Strategy

Tests are implemented red-green in focused layers:

1. Provider tests cover valid solicited and IdP-initiated envelopes, malformed
   base64/XML, wrong roots, and oversized candidates.
2. Store integration tests cover exact live lookup, wrong tenant, consumed and
   expired requests, capacity rejection, expiry freeing capacity, and
   concurrent starts that never exceed 128.
3. A full signed-response integration test starts one victim request, creates
   more than 50 later requests, and proves the victim response still succeeds.
4. Guard tests verify both budgets, HMAC-only Redis keys, denial, missing
   limiter, and limiter errors.
5. HTTP tests prove the guard runs once before storage, uses trusted client IP
   resolution, redirects generically on denial/unavailability, and creates no
   row.
6. Existing IdP-initiated, replay, lifecycle, and SAML HTTP suites remain green.

After focused tests, run backend formatting, lint, vet, vulnerability scanning,
and the repository coverage gate. A fresh security review is required before
integration. Push only after review and validate the API in staging; do not
deploy production.
