# Tenant Passwordless Login Abuse Guard Design

**Issue:** [#155](https://github.com/fidibeki/app.fidibeki/issues/155)  
**Scope:** Tenant-local `POST /auth/login` only  
**Rollout:** Staging only until production work is explicitly authorized

## Context

The tenant login route is public and receives the shared Redis-backed limiter,
but it does not invoke that limiter. Every request for an active member can
therefore create a distinct magic-link row and enqueue a distinct email task.
The token hash is part of the task idempotency key, so queue idempotency does
not collapse repeated requests.

The repository already has the required primitives:

- `httpx.ClientIPFromRequest` derives a client address only through configured
  trusted proxies;
- `ratelimit.RedisLimiter` provides atomic GCRA decisions in Redis;
- `abuse.Evaluator` composes fail-closed policies and derives keyed-HMAC bucket
  names using the rotating abuse secrets; and
- tenant transaction middleware registers the email enqueue only after the
  magic-link transaction commits.

The change must protect both known and unknown syntactically valid addresses
without making the response reveal membership.

## Decision

Add one domain-level `LoginGuard` dependency to `tenantmember.HTTPOptions`. The
HTTP handler calls it exactly once after tenant resolution and email
normalization, but before `BreakGlassLogin`, `RequestMagicLink`, audit writes,
magic-link inserts, or email hook registration.

The production implementation composes two existing `abuse.Evaluator`
policies:

1. a one-minute subject cooldown; then
2. the wider subject, network, and tenant budgets.

The cooldown runs first. A repeated request for the same address is suppressed
without consuming the wider IP or tenant budgets. This prevents one repeatedly
targeted address from becoming a cheap way to deny login to the whole tenant.
If the cooldown is granted but a later budget fails or denies the request, the
cooldown remains consumed. That conservative outcome sends no email and avoids
trying to roll back an external Redis decision from a database transaction.

The handler-facing interface returns one aggregate decision:

```go
type LoginAttempt struct {
    TenantID string
    Email    string
    Network  string
}

type LoginGuard interface {
    Allow(context.Context, LoginAttempt) (LoginDecision, error)
}
```

Tests can replace this interface with a single-call stub. The production guard
keeps the concrete bucket composition out of the HTTP handler.

## Policy

The initial policy is fixed in code and versioned with the abuse-key namespace:

| Scope | Limit | Period | Purpose |
| --- | ---: | ---: | --- |
| tenant + normalized email cooldown | 1 | 60 seconds | Collapse concurrent and immediate duplicate requests |
| tenant + normalized email | 3 | 15 minutes | Bound inbox delivery for one member |
| tenant + trusted client network | 10 | 15 minutes | Bound one caller across address guesses |
| tenant | 60 | 1 hour | Bound distributed abuse against one workspace |

These are security ceilings, not plan entitlements. They are deliberately not
tenant-configurable in this slice.

`abuse.Identity` gains a subject identity used for the normalized email. The
rate-limit keys contain only keyed HMAC digests. Raw email addresses, IP
addresses, tokens, and user agents are not written into Redis keys or logs.
The current and previous abuse secrets continue to enforce the same decision
during secret rotation.

The network scope uses the existing trusted-proxy client-IP resolver. A forged
`X-Forwarded-For` value from an untrusted direct peer cannot select a different
bucket.

## Response And Failure Semantics

The route keeps its existing validation behavior for malformed JSON and invalid
email syntax.

For every syntactically valid request:

- an allowed attempt follows the existing break-glass, magic-link, audit, and
  after-commit email flow;
- a cooldown or budget denial skips all those operations and returns the same
  `202 {"status":"accepted"}` response as an allowed or unknown-member request;
- a missing guard, missing limiter, invalid guard configuration, or Redis error
  fails closed with the existing generic `503 service_unavailable` response;
  and
- no rate-limit detail or `Retry-After` header is exposed on a suppressed
  enumeration-safe `202`.

The `503` is not membership-dependent: it is returned before member lookup.
It tells a legitimate caller that delivery could not safely be attempted
instead of claiming that an email was sent.

## Concurrency

The subject cooldown is a Redis GCRA bucket with a burst of one. Its decision
is atomic across API replicas. Concurrent requests for the same tenant and
normalized address can therefore produce at most one allowed guard decision
per cooldown period. Only that request can reach magic-link creation and email
hook registration.

The broader policy evaluates every configured scope for a newly admitted
subject request. The calls are sequential, so a later denial or dependency
failure can leave an earlier budget consumed. This fails conservatively and
never creates additional work. A new multi-key Lua algorithm is intentionally
not introduced: the security invariant is enforced by the atomic cooldown,
while the existing tested limiter remains authoritative for all budgets.

## Observability

Suppressed attempts produce one structured log event containing:

- tenant ID;
- `passwordless_login_cooldown` or
  `passwordless_login_rate_limited`; and
- no raw email, network address, token, or request body.

Limiter absence or failure produces an error log with tenant ID and the
dependency error, followed by a generic `503`. These events distinguish
healthy suppression from fail-closed dependency outages without adding a new
database write to the public attack path.

## Compatibility

- Existing successful login responses and frontend behavior do not change.
- Unknown members remain `202` and consume the same guard as known members.
- Invitations, SAML assertion handoffs, platform-admin login, refresh, and 2FA
  keep their existing controls.
- Existing `HTTPOptions.Limiter` remains the 2FA limiter. `LoginGuard` is a
  separate explicit dependency so a future refactor cannot silently remove
  login protection while leaving 2FA green.
- There is no schema migration.

## Regression Coverage

1. Guard unit tests pin all four windows, policy order, fail-closed behavior,
   keyed bucket privacy, email normalization, and current/previous key rotation.
2. A real-Redis concurrency test sends simultaneous equivalent attempts and
   proves exactly one guard decision is allowed.
3. HTTP tests prove the handler calls the guard once with tenant, normalized
   email, and trusted client network before any authentication service work.
4. HTTP/integration tests prove cooldown, subject, network, and tenant denials
   all return the generic `202` and create no extra magic-link row or email job.
5. Failure tests cover a missing guard and Redis/guard error, asserting `503`,
   zero new links, zero email jobs, and a PII-free failure log.
6. Existing tenant authentication, lifecycle, SSO break-glass, 2FA, and web
   login tests remain green.

## Rollout And Closure

1. Merge only after focused tests, the full backend coverage gate, lint,
   vulnerability scan, and security CI pass.
2. Deploy the API only to staging. The worker does not change.
3. Verify API readiness and clean error logs.
4. Exercise a controlled staging request without sending repeated real email,
   and rely on the real-Redis integration test for threshold and concurrency
   proof.
5. Record commits, CI, staging deployment, and test evidence on #155, then close
   the issue when all acceptance criteria are proven.

No production service or infrastructure is changed in this slice.
