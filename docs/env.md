# Environment reference

Fidibeki has four code services: `api`, `worker`, `web`, and `widget`. Keep secrets in the
deployment platform or CI secret manager. `.env.example` contains local-safe placeholders only.

## API and worker

The API and worker use the same validated Go configuration. Values marked required must be set
for both services because configuration is validated before the command starts.

| Variable | Required | Purpose |
| --- | --- | --- |
| `APP_ENV` | yes | `local`, `test`, `staging`, or `production`. |
| `APP_VERSION` | no | Release identifier; set to the deployed Git SHA. |
| `LOG_LEVEL` | no | Go slog level; defaults to `info`. |
| `PORT` | managed | Platform port. Used as `:$PORT` when `HTTP_ADDRESS` is unset. |
| `HTTP_ADDRESS` | no | Explicit listen address; overrides `PORT`. |
| `BASE_DOMAIN` | yes | Root host without scheme, for example `fidibeki.com`. |
| `PUBLIC_BASE_URL` | yes | Public API origin, for example `https://api.fidibeki.com`. |
| `READINESS_TIMEOUT` | no | Per-request readiness budget; default `2s`. |
| `SHUTDOWN_TIMEOUT` | no | Graceful shutdown budget; default `10s`. |
| `DATABASE_URL_APP` | yes | Runtime URL with the `fidibeki_app` username and password. `migrate up` applies these credentials to the RLS-constrained role. |
| `DATABASE_URL_ADMIN` | yes | Runtime URL with the `fidibeki_admin` username and password. `migrate up` applies these credentials to the bypass-RLS role. |
| `DATABASE_URL_DIRECT` | yes | Direct owner-level Postgres URL used by migrations and runtime-role provisioning. |
| `DATABASE_APP_MAX_CONNS` | no | App pool maximum; default `10`. |
| `DATABASE_ADMIN_MAX_CONNS` | no | Admin pool maximum; default `5`. |
| `MIGRATIONS_PATH` | no | Migration directory; container default `/app/migrations`. |
| `REDIS_URL` | yes | Redis URL for queues, rate limits, and cache. |
| `S3_ENDPOINT` | yes | S3-compatible private endpoint. |
| `S3_PUBLIC_ENDPOINT` | no | Browser-reachable endpoint used for presigned uploads. |
| `S3_REGION` | no | Bucket region; default `us-east-1`. |
| `S3_BUCKET` | yes | Private object bucket. |
| `S3_ACCESS_KEY` | yes | S3 access key. |
| `S3_SECRET_KEY` | yes | S3 secret key. |
| `S3_VIRTUAL_HOST_STYLE` | no | Use bucket-as-subdomain URLs for providers such as Railway Buckets; default `false`. |
| `S3_AUTO_CREATE_BUCKET` | no | Local/test convenience only; keep `false` in production. |
| `JWT_SECRET` | yes | At least 32 random characters. Rotating it revokes all JWTs. |
| `INTERNAL_PROXY_SECRET` | staging/prod | Shared secret used to authenticate tenant host forwarding from Nuxt. At least 32 random characters. |
| `TRUSTED_PROXY_CIDRS` | no | Comma-separated ingress proxy networks allowed to supply `X-Forwarded-For`; empty trusts none. |
| `INTEGRATION_ENCRYPTION_KEY` | yes | Base64 encoding of exactly 32 random bytes. |
| `AUTH_ACCESS_TOKEN_TTL` | no | Access-token lifetime; default `10m`. |
| `AUTH_MAGIC_LINK_TTL` | no | Magic-link lifetime; default `10m`. |
| `AUTH_REFRESH_TOKEN_TTL` | no | Refresh lifetime; default `720h`. |
| `AUTH_REFRESH_RETRY_GRACE` | no | Idempotent same-client retry window; default `0s` for two-phase rollout, maximum `30s`. |
| `REPORTER_UNSUBSCRIBE_TTL` | no | Reporter unsubscribe JWT lifetime; default `2160h`. |
| `EMAIL_DRIVER` | yes | `capture` locally or `resend` in production. |
| `EMAIL_CAPTURE_DIR` | capture | Local captured-email directory. |
| `EMAIL_QUEUE_CONCURRENCY` | no | Email worker concurrency; default `20`. |
| `RESEND_API_KEY` | resend | Resend API token. |
| `RESEND_AUTH_FROM` | resend | Auth sender, including display name. |
| `RESEND_WELCOME_FROM` | resend | Founder-welcome sender; verify this address/domain with Resend before enabling staging delivery. |
| `RESEND_WELCOME_REPLY_TO` | resend | Monitored reply address for the one-time founder welcome. |
| `RESEND_WEBHOOK_SECRET` | production | Svix/Resend signing secret. |
| `STRIPE_SECRET_KEY` | staging/prod | Stripe secret key. |
| `STRIPE_WEBHOOK_SECRET` | staging/prod | Stripe endpoint signing secret. |
| `STRIPE_PRICE_STARTER` | staging/prod | Starter recurring Price ID. |
| `STRIPE_PRICE_GROWTH` | staging/prod | Growth recurring Price ID. |
| `STRIPE_PRICE_SCALE` | staging/prod | Scale recurring Price ID. |
| `TURNSTILE_SECRET_KEY` | staging/prod | Cloudflare Turnstile server key. |
| `TURNSTILE_SITE_KEY` | staging/prod | Cloudflare Turnstile public key. |
| `GEMINI_API_KEY` | production | Gemini API key. Empty is allowed outside production. |
| `GEMINI_MODEL` | no | Model name; default `gemini-3.1-flash-lite`. |
| `GEMINI_RPM` | no | Provider request limit; default `15`. |
| `AI_QUEUE_CONCURRENCY` | no | AI worker concurrency; default `3`. |
| `AI_DAILY_REQUEST_LIMIT` | no | Tenant-independent safety ceiling; default `5000`. |
| `AI_RECONCILE_INTERVAL` | no | Stalled-task reconciliation interval; default `5m`. |
| `SENTRY_DSN` | no | Backend Sentry DSN. Empty disables reporting. |

### Browser upload CORS

The widget uploads attachments and replay batches directly to presigned S3 `PUT` URLs. Configure
this policy once on every environment's bucket before enabling those features:

- allowed origins: `*`
- allowed methods: `PUT`
- allowed headers: `*`
- exposed headers: `ETag`
- max age: `3600` seconds

The wildcard origin is intentional because customer widgets run on tenant-configured domains.
Upload authorization remains constrained by the short-lived, object-specific presigned URL; bucket
objects stay private and backoffice downloads continue through the authenticated API. Railway
creates an isolated bucket for each environment, so configuring staging does not configure the
future production bucket.

## Web and widget edge

| Variable | Service | Purpose |
| --- | --- | --- |
| `PORT` | web | Nitro listen port; injected by Railway. |
| `HOST` | web | Listen host; image defaults to `0.0.0.0`. |
| `NUXT_API_UPSTREAM` | web | Private API origin used by the same-origin server proxy. |
| `NUXT_INTERNAL_PROXY_SECRET` | web | Must equal the API service's `INTERNAL_PROXY_SECRET`; never expose it through public runtime config. |
| `NUXT_BASE_DOMAIN` | web | Server-only trusted base domain used when proxying public platform endpoints. |
| `NUXT_PUBLIC_BASE_DOMAIN` | web | Browser-visible base domain used to construct workspace and environment-specific widget URLs. |
| `NUXT_PUBLIC_TURNSTILE_SITE_KEY` | web | Public Turnstile site key rendered by the signup form. |
| `WIDGET_API_UPSTREAM` | widget | Private API upstream for `widget.fidibeki.com`. |
| `API_UPSTREAM` | widget | Optional fallback upstream for non-widget edge traffic. |
| `WIDGET_HOST` | widget | Additional widget hostname, used for environment-specific Railway domains. |

## CI and deployment

Store `SENTRY_AUTH_TOKEN` and `RAILWAY_PRODUCTION_TOKEN` as GitHub secrets. Store the remaining
non-secret identifiers and URLs as GitHub environment or repository variables:

- `SENTRY_ORG`, `SENTRY_API_PROJECT`, `SENTRY_WEB_PROJECT`, `SENTRY_WIDGET_PROJECT`
- `STAGING_API_URL`, `STAGING_TENANT_URL`, `PRODUCTION_API_URL`
- `RAILWAY_PROJECT_ID`, `RAILWAY_API_SERVICE`, `RAILWAY_WORKER_SERVICE`,
  `RAILWAY_WEB_SERVICE`, `RAILWAY_WIDGET_SERVICE`

Use `/railway.json` for the API service, `/railway.worker.json` for the worker,
`/railway.web.json` for Nuxt, and `/railway.widget.json` for Caddy. Enable Railway GitHub
autodeploy on `main` only in staging and enable **Wait for CI**. Production deployment is the
manually dispatched GitHub `Promote Production` workflow and requires typing `fidibeki.com`.
Add a required reviewer to the `production` GitHub environment when the repository plan supports it.
