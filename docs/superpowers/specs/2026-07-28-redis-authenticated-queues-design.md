# Authenticated Redis and Signed Email Tasks

## Scope

Close security issue #148 without changing the local Docker workflow or touching production.
Outside local and test, every Redis client must authenticate as a named ACL user over TLS.
Raw `email:send` payloads must no longer be executable merely because a caller can write to
Redis.

## Redis identity

`REDIS_URL` remains the single Redis connection setting used by a process. The API and worker
services receive different values in Railway:

- API: `rediss://fidibeki_app:<secret>@redis.railway.internal:6380/0`
- worker: `rediss://fidibeki_worker:<secret>@redis.railway.internal:6380/0`

This preserves the existing application interface while giving each workload an independently
rotatable identity. In staging and production, configuration rejects a missing username, the
Redis `default` user, or a missing/empty password. Local and test continue to accept the current
unauthenticated `redis://` URL.

Redis ACLs allow the application data-plane commands required by go-redis and Asynq while denying
the `@admin` and `@dangerous` command categories. Both identities can access the Fidibeki instance
keyspace because API cache/rate-limit state and Asynq queues already share that instance. Redis
remains private and TLS-only.

## Email task integrity

`email:send` uses a versioned JSON envelope:

```json
{
  "version": 1,
  "payload": {},
  "signature": "base64url-hmac-sha256"
}
```

The signature covers the task type, envelope version, and exact JSON payload. The producer signs
with a 32-byte base64-encoded `EMAIL_TASK_SIGNING_KEY`. The worker verifies in constant time before
decoding or sending the message. Unsigned legacy payloads, malformed envelopes, and invalid
signatures return `asynq.SkipRetry` and never invoke the sender.

`EMAIL_TASK_PREVIOUS_SIGNING_KEY` is optional and verification-only. It supports rotation without
accepting arbitrary historical keys. Both key settings are redacted from configuration output.

## Rotation

Redis credentials rotate one service identity at a time. A staging rollout temporarily keeps the
legacy Redis user enabled, deploys API and worker with their named identities, verifies readiness,
and then disables the legacy user.

Email signing keys rotate worker-first:

1. Deploy the worker with current key A and previous key B.
2. Deploy the API with current key B and previous key A.
3. Promote the worker to current key B and previous key A.
4. Remove A after the maximum task retry/retention window.

If Redis credentials or queued messages may have been exposed, disable the affected ACL user,
rotate Redis and email signing secrets, inspect or purge untrusted queues, rotate bearer links
present in queued messages, and review email-provider/audit logs.

## Verification

- Configuration matrix for local, test, staging, and production with anonymous, password-only,
  default-user, named-user, and empty-password Redis URLs.
- Redis option tests prove username/password propagation to go-redis and Asynq.
- A real Redis TLS integration test starts named ACL users, denies administrative commands,
  enqueues as the API identity, and consumes as the worker identity.
- Email tests prove signed delivery succeeds and raw, tampered, malformed, and previous-key cases
  behave as specified.
- Staging validation proves API/worker readiness with distinct ACL users and proves the legacy
  Redis identity no longer authenticates.
