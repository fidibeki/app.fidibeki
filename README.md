# Fidibeki

Fidibeki is a multi-tenant feedback platform. The current foundation provides a Go API,
PostgreSQL row-level tenant isolation, PgBouncer, Redis, S3-compatible local storage, Caddy,
an embeddable feedback widget, transactional feedback ingestion, queued work, and real-container tests.

The authoritative implementation plan is [plans/fidibeki.plan.md](plans/fidibeki.plan.md).

## Prerequisites

- Docker with Compose v2
- Go 1.25.12 when running the API or tests outside Docker
- Node.js 24 and pnpm 11.15.1 for widget development

## Run locally

Start the full stack:

```sh
docker compose -f infra/docker-compose.yml up -d --build
```

The migration container runs before the API starts. Services are available at:

| Service | URL |
| --- | --- |
| API through Caddy | http://localhost:8088 |
| Widget API/assets | http://widget.localhost:8088 |
| API directly | http://localhost:8080 |
| MinIO console | http://localhost:9001 |
| PostgreSQL | localhost:5433 |
| PgBouncer | localhost:6432 |
| Redis | localhost:6380 |

Check the stack:

```sh
curl -i http://localhost:8088/healthz
curl -i http://localhost:8088/readyz
docker compose -f infra/docker-compose.yml ps
```

Use `api.localhost:8088` for platform traffic and `<tenant>.localhost:8088` for tenant
traffic. Health and readiness endpoints are available on every host. The development passwords
in Compose and `.env.example` are local-only credentials.

Local authentication emails are captured as private JSON files in the `email-capture` Docker
volume. Inspect the newest capture with:

```sh
docker compose -f infra/docker-compose.yml exec worker sh -lc 'ls -t /var/lib/fidibeki-emails | head'
```

Create or promote the first platform administrator before using the admin API:

```sh
docker compose -f infra/docker-compose.yml run --rm api admin bootstrap --email admin@example.com
```

Platform authentication is served from `api.localhost:8088` under `/admin/auth/*`. Tenant-member
authentication is served from `<tenant>.localhost:8088` under `/auth/*`. Both flows return a
10-minute audience-bound access token and set a hashed, rotating 30-day refresh-token cookie.
Concurrent refreshes from the same user-agent and IP have a 10-second rotation grace period so
multiple browser tabs remain signed in. Retries return one HMAC-derived successor instead of creating
additional sessions. Reuse outside that window, from a different fingerprint, or after the successor
has rotated revokes that administrator or member's active session family.

Request a platform magic link with `POST /admin/auth/login`, then open the captured or delivered
link and confirm the sign-in. The confirmation sets separate short-lived, HTTP-only cookies scoped
to the Asynq monitor at `http://api.localhost:8088/admin/queues/` and the read-only
privacy-incident workspace at `http://api.localhost:8088/admin/operations/privacy-incidents`;
merely previewing the email link does not consume its one-time token.

Organizations can authenticate members through their own SAML 2.0 identity provider. SSO is
per organization and disabled by default. Administrators with `tenant:update` configure it under
`/v1/sso/saml` and in **Settings → Single sign-on**; the identity provider uses
`https://<tenant>.<base-domain>/auth/sso/saml/metadata` and `.../acs`, both derived from
`WEB_PUBLIC_BASE_URL` (default `PUBLIC_BASE_URL`) and `BASE_DOMAIN`. Required mode is refused
until a test sign-in has succeeded and a signing certificate is currently valid, the workspace
owner keeps an audited break-glass link, and SAML sessions are marked `authentication_method =
saml` so they never receive a Fidibeki TOTP challenge. Required mode also disables the workspace
two-factor control without deleting any existing enrolment. See
[docs/saml-sso.md](docs/saml-sso.md) for setup, certificate rotation, and outage recovery.

Authenticated platform administrators manage tenants under `/admin/tenants`. Authenticated tenant
members can invite and manage members under `/auth/invite` and `/v1/members`; every route enforces
its explicit permission and the tenant ID embedded in the JWT.

Each organization sets a Fidibeki-managed two-factor policy of `optional` (the default) or
`required` at `/v1/security/two-factor/policy`. Members enrol a TOTP authenticator at
`/v1/security/two-factor/enroll` and `/v1/security/two-factor/confirm`, which returns ten one-time
recovery codes exactly once; secrets are stored AES-256-GCM encrypted under
`TWO_FACTOR_ENCRYPTION_KEY` and recovery codes only as SHA-256 hashes. An enrolled member's
`/auth/verify` and `/auth/refresh` return `two_factor.status` instead of an access token until
`POST /auth/2fa/challenge` accepts a code from the same refresh cookie. Under a required policy an
unenrolled member is refused every workspace route with `403 two_factor_enrollment_required` — there
is no grace period — while the enrolment routes stay reachable. Requiring the policy is refused
unless the acting administrator is enrolled and still holds unused recovery codes, so the workspace
always keeps one recoverable administrator. Administrators see enrolment status at
`/v1/security/two-factor/compliance` and can reset a member at
`/v1/members/{memberID}/two-factor/reset`, which revokes that member's sessions and forces
re-enrolment; they can never read a secret or a recovery code. SAML sessions record
`authentication_method = 'saml'` and are never challenged, because their assurance is delegated to
the identity provider. Policy changes, enrolment, removal, recovery-code use, failed challenges, and
administrative resets are recorded in `tenant_access_events`.

Self-service signup is `POST http://api.localhost:8088/v1/signup`. It creates a 14-day trial and
the first tenant admin, then queues a fragment-based magic link. Production and staging require
Cloudflare Turnstile plus `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, and one Stripe Price ID in
each of `STRIPE_PRICE_STARTER`, `STRIPE_PRICE_GROWTH`, and `STRIPE_PRICE_SCALE`, plus their yearly
counterparts `STRIPE_PRICE_STARTER_YEARLY`, `STRIPE_PRICE_GROWTH_YEARLY`, and
`STRIPE_PRICE_SCALE_YEARLY`. Configure the
Stripe endpoint at `https://api.fidibeki.com/v1/billing/webhooks/stripe` for these six events:
`checkout.session.completed`, `customer.subscription.created`, `customer.subscription.updated`,
`customer.subscription.deleted`, `invoice.paid`, and `invoice.payment_failed`.

Tenant billing admins use `/v1/billing/checkout` and `/v1/billing/portal`. Account deletion is
scheduled through `/v1/billing/delete-account` after typing the tenant slug and can be cancelled at
`/v1/billing/cancel-deletion` during the seven-day read-only grace period. The worker sends trial
dunning mail daily, suspends expired unpaid trials, removes unverified signups after 24 hours, and
executes due deletions only after Stripe cancellation and S3 prefix cleanup succeed. Platform
administrator force-deletion uses the same audited executor without the grace period.

Tenant issue workflows are available under `/v1/issues`. Members with the relevant issue
permissions can list and inspect issues, change status with an optional internal comment, merge an
issue into another issue in the same project, and undo a merge. Status changes append immutable
`issue_updates` history and enqueue opted-in reporter mail without holding the API transaction open.

Reporter email uses `updates@fidibeki.com`, includes a 90-day audience-bound unsubscribe link, and
records every provider attempt in `delivery_logs`. In production, set `EMAIL_DRIVER=resend`,
`RESEND_API_KEY`, and `RESEND_WEBHOOK_SECRET`; point the Resend webhook at
`https://api.fidibeki.com/v1/webhooks/resend` for bounce and complaint handling.

Members route their own optional notifications at `/settings/notifications`, backed by
`GET`/`PATCH /v1/notification-preferences`. Assignments, mentions, comments, important Issue changes,
and new-feedback summaries are each delivered immediately, in a daily or weekly digest, or not at
all; defaults are low-noise and new feedback is summary-only, so no setting produces one email per
submission. Digests use the member's timezone and chosen delivery window, are claimed per window in
`member_digest_deliveries` so retries cannot duplicate or omit events, and are never sent empty.
Every digest carries a signed one-click opt-out at `/v1/member-notifications/unsubscribe`.
Authentication, security, account-deletion, and billing mail is separate and cannot be disabled
there. See [docs/transactional-email.md](docs/transactional-email.md).

Scale and Enterprise tenants can manage Slack, Teams, and generic webhook destinations at
`/v1/integrations`. Each destination selects from `feedback.created`, `issue.created`,
`issue.updated`, and `issue.resolved`; the outbox creates one deterministic
`integration.deliver` task per subscribed destination. Generic webhook bodies include the stable
delivery ID and are signed as `X-Fidibeki-Signature: sha256=<hex HMAC-SHA256>`. Webhook secrets are
shown once, while secrets and custom headers are stored with AES-256-GCM under
`INTEGRATION_ENCRYPTION_KEY`. Slack uses Block Kit and Teams uses MessageCard payloads. Every HTTP
attempt is isolated, retried by Asynq, and recorded in `delivery_logs` without blocking feedback.

Project and widget configuration is available to authenticated tenant members under
`/v1/projects` and `/v1/projects/{projectID}/widget-rules`. Project creation enforces the current
plan's project limit. Allowed domains accept exact hosts and leading wildcards such as
`*.preview.example.com`; widget rules return field-level `422` errors for invalid matcher,
trigger, appearance, replay, or plan-gated settings.

Embed the widget with the project public key:

```html
<script src="http://widget.localhost:8088/v1/loader.js"
        data-fidibeki-key="pk_live_..." async></script>
```

The public widget API requires the project key and an allowed browser `Origin`. Feedback submission
creates one independent Feedback record and its outbox event atomically, returns 10-minute presigned
upload URLs, and verifies each attachment's one-time completion token, byte size, and MIME type
through S3 HEAD. Redis applies both the 10/minute per-IP/project and 100/hour project ceilings and
fails open if Redis is unavailable.

Automatic definitions combine their compatible legacy trigger with a deterministic local objective
policy. The policy uses active time, scroll depth, interaction counts, anonymous per-page visits,
public document metadata, and optional boolean attributes on `<html>`:
`data-fidibeki-engaged`, `data-fidibeki-content`, and `data-fidibeki-friction`. Values `true` and `1`
activate a signal. The runtime never reads input values, private page text, network payloads, or a
personal fingerprint. Launcher and inline presentations remain immediately available; reaction and
prompt presentations require progressively stronger evidence.

Replay-enabled rules lazy-load pinned `rrweb@2.1.1` before the feedback trigger. Recording uses the
rule's session-level sampling and masking policy, continues across same-rule SPA navigation, and ends
after 30 minutes, 50,000 events, five minutes idle, or `pagehide`. The widget uploads sequential
50-event/~200 KB JSON batches through presigned S3 URLs. The domain worker merges completed batches
into `replay.json`, deletes the temporary objects, and a daily cleanup removes sessions and objects
past each plan's storage period. Tenant members with `session:read` fetch metadata at
`GET /v1/sessions/{id}` and stream merged events through the authenticated, RLS-scoped
`GET /v1/sessions/{id}/replay`; direct S3 playback URLs are never exposed.

Growth, Scale, and Enterprise tenants get asynchronous Gemini processing for written feedback,
screenshots, audio, and video. The worker streams completed media from object storage through the
Gemini File API, requests schema-constrained JSON, deletes the temporary Gemini file, and stores
cleaned text, transcription/OCR, sentiment, and flag-only moderation results. Session replays are
never sent to Gemini. Uploaded Gemini filenames are recorded before analysis; deletion is
retried inline, and the reconciler resumes unresolved cleanup from the database ledger. Configure
production workers with:

```sh
GEMINI_API_KEY=...
GEMINI_MODEL=gemini-2.5-flash
GEMINI_RPM=15
AI_QUEUE_CONCURRENCY=3
DOMAIN_QUEUE_CONCURRENCY=4
MEDIA_QUEUE_CONCURRENCY=2
PRIVACY_QUEUE_CONCURRENCY=1
AI_DAILY_REQUEST_LIMIT=5000
AI_PROJECT_DAILY_REQUEST_LIMIT=2000
```

Without `GEMINI_API_KEY`, local AI jobs remain in Asynq's retry flow while feedback submission and
uploads continue normally. Members with `feedback:update` can retry one completed attachment at
`POST /v1/attachments/{id}/retry` or reset all AI passes with
`POST /v1/feedbacks/{id}/reprocess`. A five-minute reconciler recovers completed uploads whose jobs
were not queued.

Stop the stack with `make down`. Add `-v` to the Compose command when you intentionally need to
delete local database, Redis, and object-storage data.

## Run outside Docker

Start the infrastructure, create the backend environment file, run migrations, then start the API:

```sh
docker compose -f infra/docker-compose.yml up -d postgres pgbouncer redis minio
cp .env.example backend/.env
make migrate-up
cd backend && go run ./cmd/fidibeki-api serve
```

Do not reuse the example credentials in a deployed environment.

## Verification

```sh
make test-short   # unit tests only; skips testcontainers
make test         # all unit and integration tests
make coverage     # all tests plus 80% total / 60% per-package gates
make format-check
make lint         # go vet
make widget-build
make widget-test  # typecheck plus Vitest coverage
make widget-e2e   # Playwright browser contract
```

Integration tests start disposable PostgreSQL 16, Redis 7.4, and MinIO containers. The PostgreSQL
suite verifies migration up/down/up behavior, app/admin database roles, and positive and negative
RLS isolation.

## Database access

Application code has two pools with deliberately separate capabilities:

- `fidibeki_app` is subject to forced RLS and serves tenant-scoped handlers.
- `fidibeki_admin` bypasses RLS for platform operations and tenant resolution.
- Migrations use `DATABASE_URL_DIRECT`, bypass PgBouncer, and provision both runtime roles from the
  credentials in `DATABASE_URL_APP` and `DATABASE_URL_ADMIN`.

Tenant HTTP requests run in an app-pool transaction. Middleware resolves the subdomain using the
admin pool, sets `app.tenant_id` transaction-locally, and only flushes the HTTP response after the
transaction commits.
