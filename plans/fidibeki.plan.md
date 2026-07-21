# Plan: Fidibeki

> Source PRD: Conversation with founder — instant feedback capture SaaS for web apps.
> This plan was stress-tested via a 41-branch grilling interview. The product decisions are
> locked; implementation details may be tightened when verification exposes an unsafe or
> internally inconsistent contract.

## Architectural decisions

- **Runtime & backend**: Go 1.25 (latest supported patch in CI/images), `chi` router, `slog` for
  structured logging. The module toolchain directive pins the local/CI patch release.
- **Database**: PostgreSQL 16, **Postgres Row-Level Security (RLS) as defense-in-depth**
  (not just app-layer `WHERE tenant_id`). Every tenant-scoped table has RLS enabled with
  `FORCE ROW LEVEL SECURITY` and policies `USING (tenant_id = current_setting('app.tenant_id')::uuid)`.
- **Two DB roles, two pools**:
  - `fidibeki_app` — RLS-enforced. Used by tenant-scoped HTTP handlers.
  - `fidibeki_admin` — `BYPASSRLS`. Used by platform routes, migrations, Stripe webhooks, and async workers.
- **Per-request transaction with transaction-local tenant context**: every request on the app pool
  runs inside a `pgx.Tx`; the tenant middleware calls
  `SELECT set_config('app.tenant_id', $1, true)` with a bound UUID at the start. It buffers the handler
  response, rolls back on panic, and only flushes the response after a successful commit.
  The transaction-local setting dies with the transaction — no GUC leak across requests, panic
  commit, SQL interpolation, or false-success response.
  Handlers take a `Querier` interface (satisfied by both `pgx.Tx` and `pgxpool.Pool`).
- **pgbouncer in transaction mode from day 1**: multiplexes many app connections onto few
  Postgres connections. `SET LOCAL` works in transaction mode; `SET` (session-level) does not.
  Prepared statements disabled (`default_query_exec_mode=exec`) to avoid pgbouncer version gotchas.
  Migrator uses `DATABASE_URL_DIRECT` (bypasses pgbouncer).
- **Migrations**: `golang-migrate` with SQL files in `infra/migrations`, one file per concept
  (table/enum/index-set), each paired with a `.down.sql`. CI runs an up→down→up test.
- **Multi-tenancy routing**: Subdomains of `fidibeki.com` resolve to a tenant via a
  `TenantResolver` interface. MVP ships `SubdomainResolver` only; `CustomDomainResolver` is
  a typed-out stub for the enterprise custom-domain feature. `BASE_DOMAIN` env var.
  `api.fidibeki.com` is the platform host (excluded from tenant resolution).
- **Host-based router split**: `api.fidibeki.com` serves the platform router and a dedicated public
  widget router. Platform routes use the admin pool. Public widget routes resolve the project/public
  key on the admin pool, then open an app-pool transaction and set transaction-local tenant context;
  all business reads and writes therefore remain RLS-enforced. `*.fidibeki.com` serves tenant routes on the app pool.
  Pool selection is router-group-level, never handler-selected. `X-Forwarded-Host` is NOT trusted
  (Caddy/forwarded header trust is a deploy-time decision).
- **Tenant lookup**: no cache for MVP — indexed `SELECT id, status, plan, trial_ends_at
  FROM tenants WHERE slug = $1` per request on the admin pool. Status is not cached to
  preserve the "suspended tenant can't transact" guarantee.
- **Middleware classification by chi route group**: four middlewares mounted per group —
  `Resolve` (always: subdomain → tenant, start tx, set transaction-local tenant context),
  `RequireActive` (write gate: 403 if suspended/expired-trial),
  `AllowDegraded` (read gate: suspended OK),
  `AllowAlways` (auth/billing gate).
  Plus `RequirePlanLimit(feature)` as a separate downstream middleware for 402s.
  Classification is by mounting, not by URL sniffing.
- **Authentication backoffice**: Magic-link login. Shared `internal/auth/magiclink` library
  built in Phase 1 (platform consumer); Phase 2 adds the tenant consumer.
  JWT access tokens (HMAC-S256, 10-min, `aud=platform|tenant`, `permissions` snapshot in claims)
  + opaque rotating refresh tokens (hashed in `*_sessions` tables, 30d, theft-detection via
  reuse-of-revoked-token). No passwords stored.
- **Token transport**: Access token in memory + `Authorization: Bearer` header; refresh token
  in httpOnly `SameSite=Strict` cookie. Same-origin (Nuxt + Go behind one Caddy reverse proxy
  on `*.fidibeki.com`) — no CORS on backoffice calls, cookies flow naturally.
  Silent refresh on app boot; silent refresh on 401 via `apiFetch` plugin.
- **Widget authentication**: Project public key (`pk_live_<22 base62>`) in `X-Fidibeki-Public-Key`
  header + strict `Origin` validation against `projects.allowed_domains` (exact match or
  explicit `*.domain` wildcard entries). Dynamic CORS origin echo, `credentials: 'omit'`,
  no `Access-Control-Allow-Credentials`. CORS is convenience, not the security boundary.
- **File storage**: S3-compatible storage in production. MinIO is pinned to the final official
  container release for local tests only because its server repository is archived. A single shared
  bucket uses `tenants/{tenant_id}/...` prefixed
  keys. The server exclusively constructs keys and presigns one exact key; production uses scoped
  STS credentials where the provider supports them. A shared root credential is not described as
  storage-layer tenant isolation. No versioning;
  write-once keys; lifecycle policy for retention (per plan's `storage_days`); async
  `delete_prefix` worker for explicit deletes. `Content-Disposition: attachment` at presign.
  Server validates MIME + size at `complete` via S3 HEAD; bucket policy denies dangerous MIME
  types (`text/html`, `application/javascript`).
- **Feedback submission wire format**: JSON metadata first → presigned S3 uploads for files →
  completion callback. `POST /v1/feedback` returns `{ feedback_id, presigned_urls[], complete_tokens[] }`;
  widget PUTs directly to S3; widget calls `POST /v1/attachments/{id}/complete` with a
  single-use completion token; server verifies token + S3 HEAD (size + MIME).
  `client_feedback_id` + `client_attachment_id` UUIDs provide idempotency
  (`UNIQUE(project_id, client_feedback_id)`, `UNIQUE(feedback_id, client_attachment_id)`).
  Presigned URL TTL: 10 minutes. `sweep_attachments` cron deletes orphaned uploads after 1h.
- **AI processing**: Google Gemini `gemini-2.5-flash` for all tasks (transcription, sentiment,
  moderation, OCR). Stream MinIO → Gemini File API → `generateContent` → delete Gemini file.
  Persist each remote filename before analysis; bounded inline deletion retries fall back to the
  durable `ai_gemini_files` cleanup ledger and reconciler.
  Per-attachment + per-text asynq jobs; `processing_status` state machine
  (`pending|processing|completed|failed|skipped`) with CAS transitions; revert to `pending` on
  transient failure, `failed` + `asynq.SkipRetry` on permanent (content policy, malformed media).
  `redis_rate` limiter keyed `gemini` with `GEMINI_RPM` env (default 15). Per-tenant daily AI
  budget enforced at enqueue time. Plan-gated: non-AI plans get `processing_status='skipped'`.
  Video uses Gemini's native video input (no FFmpeg in the Docker image).
- **Email delivery**: Resend via asynq `email-pool`. Two sender identities on one domain:
  `auth@fidibeki.com` (transactional — magic links, billing receipts) and
  `updates@fidibeki.com` (reporter-facing issue updates). Isolated reputation per sender.
  Go `html/template` + plaintext fallback, no MJML/React-Email for MVP. Async only.
  `delivery_logs` is one row per attempt (with `attempt_number`). Resend bounce/complaint
  webhook auto-unsubscribes reporters (`feedbacks.notify_reporter=false`).
  Unsubscribe via JWT (`aud=reporter-unsubscribe`, 90d TTL, dedicated middleware).
- **Queue**: `hibiken/asynq` on Redis. Domain changes write a transactional outbox row; an outbox
  dispatcher creates deterministic, idempotent tasks per destination. This replaces resource-level
  `delivery_dispatched` booleans, which cannot represent partial fan-out. Four isolated worker pools in one Go binary:
  `email-pool` (concurrency 20), `ai-pool` (concurrency 3), `notification-pool` (10),
  `maintenance-pool` (2). Per-task backoff policies. Terminal failures update the relevant
  DB row (`delivery_logs.status='failed'`, `attachments.processing_status='failed'`).
  Best-effort enqueue + 5-min `reconcile_pending_jobs` cron (self-healing if Redis blips).
  Jobs carry IDs only; workers re-fetch fresh state. A native read-only queue operations UI is
  mounted on `api.fidibeki.com` behind platform-admin auth.
- **Payments**: Stripe Checkout (hosted) + Customer + Subscription + one Price per plan.
  Price IDs in env vars (`STRIPE_PRICE_*`). Hybrid trial: Stripe-native trial +
  Fidibeki `trial_ends_at` enforcement + daily cron + Resend dunning (3d/1d/0d before end).
  6 webhook events with `stripe_events` idempotency table; handlers load-from-Stripe
  (event is a signal, Stripe is source of truth). Downgrades allowed: restrict-new-creation
  (not read-only-existing) + `pending_plan` applied at next period. Customer Portal for
  self-service card/cancel. Enterprise is platform-admin-managed, no Stripe integration.
  Plan limits return `402 Payment Required` with `upgrade_url`; rate limits return `429`.
- **Rate limiting**: `redis_rate` (GCRA) on Redis. Widget endpoints keyed per-IP + per-project;
  backoffice keyed per-member; platform keyed per-IP. Fail-open on Redis outage.
  Rate limit (429, per-IP/member, Redis) and plan limit (402, per-tenant, DB count) are
  separate middlewares; rate limit runs first. Per-project ceiling + honeypot field for MVP;
  Cloudflare Turnstile on signup from day 1; Turnstile on widget deferred to Phase 12.
  `X-RateLimit-Limit` / `X-RateLimit-Remaining` / `Retry-After` headers on every rate-limited response.
  API keys get a configurable higher limit (`api_keys.rate_limit_per_min`, default 1000).
- **Backoffice UI**: Nuxt 3 on `*.fidibeki.com`, hybrid SSR (`/login` + `/` SSR for auth
  bootstrap, rest SPA). One Nuxt app reads `Host` → `tenantSlug` in SSR context.
  Same-origin via Caddy reverse proxy (path-prefix routing: `/api/*`, `/v1/*`, `/auth/*`,
  `/billing/*` → Go; everything else → Nuxt). `apiFetch` plugin with silent-refresh-on-401.
  `useState` for auth (no Pinia for MVP). Server middleware for SSR routes, page middleware
  for SPA routes. `can(perm)` helper for authz only (plan limits separate, surface as 402 on click).
  No per-tenant branding for MVP (tenant name in header only).
- **Widget embed**: Vanilla JS, zero-dep. Single ~5KB `loader.js` + lazy-loaded ES modules
  (`rating.js`, `audio.js`, `video.js`, `screenshot.js`, `replay.js`, `error-reporting.js`) — only modules the plan allows
  are fetched. Shadow DOM for CSS/JS isolation. One-line embed:
  `<script src="https://widget.fidibeki.com/v1/loader.js" data-fidibeki-key="pk_live_..." async>`.
  Config fetched once, checked on SPA navigation (history patch + custom event), refreshed
  every 5 min and on `visibilitychange`. Three triggers (delay/scroll/exit_intent) with
  `sessionStorage` suppression. Attach-element captures HTML + computed styles; screenshot
  capture is a separate lazy module using the browser's display-capture permission flow.
- **Session replay**: rrweb (pinned version) with PII masking (`maskAllInputs`, `maskTextSelector`).
  Session-level sampling (`widget_rules.replay_sampling_rate`) + caps (30 min, 50k events).
  Continuous recording across SPA navigations, ends on `pagehide` or 5-min idle.
  Batch upload to S3 presigned PUT (one object per ~50-event batch, ~200KB each);
  post-session `replay.merge` job concatenates batches into `replay.json` and deletes batches.
  Player is `rrweb-player` in a Vue component; replay streamed through Go API (auth + RLS).
  `sessions.rrweb_version` + `sessions.merged_at` columns. Both `sessions.feedback_id` and
  `feedbacks.session_id` for bidirectional links.
- **Deploy target**: Railway (Docker + Postgres + Redis + optional S3/MinIO + pgbouncer).
  Multi-stage alpine Dockerfiles for Go API and Nuxt app (separate images). Widget is static
  files served by Caddy/CDN. GitHub Actions: PR pipeline (fast + integration, <8 min) →
  merge to main → auto-deploy staging → E2E post-deploy → manual promote to prod.
  Migrations run as part of deploy (`migrate up` before app start). Expand-then-contract
  for destructive changes. Real secrets in Railway only; CI uses mocks + testcontainers.
- **Testing discipline**: TDD with ≥80% total coverage + no package below 60% (excluding `main`/`cmd`).
  4-layer pyramid: unit (pure functions, mocked services), integration (testcontainers Postgres
  + Redis + MinIO, transaction-per-test, RLS positive+negative+admin tests per table),
  E2E (Playwright for widget + backoffice + Stripe test mode), contract (Stripe/Resend/Gemini fixtures).
  Widget: Vitest + `happy-dom` unit + Playwright E2E. Nuxt: Vitest for composables + Playwright E2E
  (skip component unit tests for MVP). Workers: test handler functions directly with mocked deps
  + 1 integration test per job type via real Redis testcontainer.
- **Observability**: `slog` JSON → stdout (Railway-collected) with fields: `time, level, msg,
  request_id, tenant_id, tenant_slug, route, duration_ms, user_id, error`. PII redaction custom handler
  (scrubs `text`, `email`, `transcription`, `dom_snapshot_html`, `storage_key`).
  Sentry SaaS for Go + widget + Nuxt with source maps and PII scrubbing.
  Skip Prometheus for MVP (derive from logs + the queue operations UI). `/healthz` (liveness, always 200) +
  `/readyz` (readiness, checks DB+Redis+S3 with 2s timeouts). Widget lazy-loads Sentry SDK
  on first error (preserves 5KB loader). `docs/runbook.md` with 10 scenarios.
- **API contract**: Flat single resources / `{ data, meta: { has_next, next_cursor } }` collections.
  Error envelope `{ error: { code, message, details? } }` with 13-code catalog
  (`bad_request`, `unauthenticated`, `forbidden`, `tenant_suspended`, `not_found`,
  `unknown_tenant`, `conflict`, `validation_error`, `rate_limit_exceeded`,
  `plan_limit_exceeded`, `plan_feature_unavailable`, `internal_error`, `service_unavailable`).
  `402` for plan limits (distinct from `429` rate limits). `500` includes `sentry_event_id`.
  `402` responses include `upgrade_url`. `Location` header on `201`. `X-Request-ID` on every
  response. URL path versioning (`/v1/`). JSON only (`application/json; charset=utf-8`).
  Widget maps `error.code` → friendly user-facing messages (`widget/src/messages.ts`).
- **Config**: Env vars + `.env` for local dev via `godotenv`. Typed `Config` struct with
  `Validate()` at startup (exit with clear error if invalid). ~35 env vars grouped by concern.
  Secrets vs. non-secrets convention; `Config.Redacted()` for safe logging.
  `plans.go` is code (not env) — plan features/limits are product decisions; Stripe Price IDs
  are env (deploy-specific). `.env.example` committed and maintained.
- **Permissions**: 32 tenant permissions (`resource:action`, no wildcards) + 9 `platform:*` permissions.
  3 UI presets (admin/editor/viewer) stored as explicit `permissions[]` arrays. `RequirePerm`
  middleware per route. First member (founder) gets all permissions. API keys use the same model.
  Accept up to 10-min JWT staleness on permission changes (token refresh picks up new perms).
- **Tenant deletion**: Self-service with 7-day grace period + platform admin force-delete.
  `tenants.deletion_requested_at` column; middleware treats non-null as read-only.
  Daily `tenant_deletion_executor` cron: cancel Stripe sub, revoke sessions, delete S3 prefix,
  `DELETE FROM tenants CASCADE`, insert into `tenant_deletions` audit table. Soft during grace,
  hard after. No automatic refunds (manual via Stripe dashboard). Data export deferred to Phase 12+.
- **Self-service signup**: `POST api.fidibeki.com/v1/signup` (no auth, Turnstile-protected) creates
  a `trial` tenant + first admin member. Lazy Stripe customer creation (on first billing interaction).
  Trial gets the chosen plan's limits. Magic-link verify flow: the email opens Nuxt
  `GET /auth/verify#token=...`; client code reads and immediately clears the fragment, then sends the
  token to Go `POST /auth/verify`. Fragments are not sent to the web server or request logs. Delete
  unverified tenants after 24h. First-come-first-served slugs.

## Data model principles

All tenant data is stored in a single PostgreSQL database and isolated by `tenant_id` with RLS.
For the MVP we avoid JSONB entirely: every attribute that matters is a typed column.
Arrays and enums are allowed where the set of values is bounded and queryable.
Tables with a direct `tenant_id` use the following fail-closed policy. Root and indirectly-owned
tables use explicit equivalent policies documented with their migration; `tenants` compares `id`
to the tenant setting and grants `UPDATE(name, billing_email)` only, while tables such as
`widget_rules` and member sessions isolate through their owning project/member.
```sql
ALTER TABLE <t> ENABLE ROW LEVEL SECURITY;
ALTER TABLE <t> FORCE ROW LEVEL SECURITY;
CREATE POLICY <t>_tenant_isolation ON <t>
  FOR ALL TO fidibeki_app
  USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE, DELETE ON <t> TO fidibeki_app;
```

## Enums

```sql
CREATE TYPE tenant_plan AS ENUM ('starter', 'growth', 'scale', 'enterprise');
CREATE TYPE tenant_status AS ENUM ('active', 'suspended', 'trial', 'cancelled');
CREATE TYPE matcher_type AS ENUM ('all_pages', 'url_exact', 'url_prefix', 'url_regex');
CREATE TYPE trigger_type AS ENUM ('delay', 'scroll', 'exit_intent');
CREATE TYPE attachment_kind AS ENUM ('screenshot', 'audio', 'video', 'replay');
CREATE TYPE processing_status AS ENUM ('pending', 'processing', 'completed', 'failed', 'skipped');
CREATE TYPE feedback_status AS ENUM ('open', 'spam', 'archived');
CREATE TYPE issue_status AS ENUM ('open', 'in_progress', 'resolved', 'closed');
CREATE TYPE integration_kind AS ENUM ('slack', 'teams', 'webhook');
CREATE TYPE delivery_status AS ENUM ('pending', 'sent', 'failed', 'bounced');
CREATE TYPE event_type AS ENUM ('feedback.created', 'issue.created', 'issue.updated', 'issue.resolved');
CREATE TYPE widget_position AS ENUM ('bottom-left', 'bottom-right', 'top-left', 'top-right');
CREATE TYPE replay_mode AS ENUM ('none', 'sampling', 'all');
CREATE TYPE widget_theme AS ENUM ('light', 'dark', 'auto');
```

Note: `member_kind` was removed — API keys live in a separate `api_keys` table.
Note: `email` was removed from `integration_kind` — reporter emails are not integrations.

## Key tables

- `tenants` — root company accounts with typed plan, status, billing, trial, deletion columns.
- `tenant_members` — human users belonging to a tenant. Access via explicit `permissions` text array.
- `api_keys` — machine credentials with `token_hash`, `permissions`, `rate_limit_per_min`.
- `tenant_member_sessions` — hashed rotating refresh tokens, revocable.
- `tenant_magic_links` — single-use magic-link tokens (SHA-256 hashed, 10-min TTL).
- `platform_admins` + `platform_admin_sessions` + `platform_admin_magic_links` — platform-level auth.
- `projects` — a site or app monitored by a tenant. Embed key and allowed domains are typed columns.
- `widget_rules` — per-page widget configuration with typed matcher/trigger/form/replay/theme columns.
- `feedbacks` — a captured user response with typed rating, text, AI columns, metadata, DOM snapshot, status.
- `attachments` — media linked to a feedback with typed transcription, sentiment, moderation, processing state.
- `sessions` — session replay bundles with rrweb version, merge state, event counts.
- `issues` — grouping of feedbacks with typed public id, status workflow, soft-merge support.
- `issue_updates` — status change history for an issue.
- `issue_counters` — per-(tenant, year) atomic sequence for public id allocation.
- `integrations` — external service connections (Slack, Teams, webhook) with typed config + webhook secret.
- `delivery_logs` — per-attempt audit trail of every outgoing notification with status and error.
- `stripe_events` — webhook idempotency table (event_id PK).
- `tenant_deletions` — compliance audit log of deleted tenants (not tenant-scoped, no RLS).

## Detailed database schema

### `tenants`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| name | TEXT NOT NULL | Display name of the company |
| slug | TEXT NOT NULL UNIQUE | Subdomain identifier, 3-24 chars, `[a-z0-9-]`, not reserved |
| plan | tenant_plan NOT NULL DEFAULT 'starter' | Billing tier |
| pending_plan | tenant_plan NULL | Scheduled downgrade, applied at next period |
| status | tenant_status NOT NULL DEFAULT 'trial' | Account lifecycle state |
| billing_email | TEXT | Invoice/billing contact |
| email_bounced | BOOLEAN NOT NULL DEFAULT false | Set by bounce webhook on billing_email |
| stripe_customer_id | TEXT | Stripe Customer object id (lazy-created) |
| stripe_subscription_id | TEXT | Stripe Subscription object id |
| trial_ends_at | TIMESTAMPTZ | Null when not on trial |
| deletion_requested_at | TIMESTAMPTZ | Non-null = in 7-day deletion grace (read-only) |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Constraints**: `CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{1,22}[a-z0-9])?$' AND slug NOT IN
('api','www','admin','mail','widget','auth','billing','app','staging','dev','test','demo','support','help'))`

**Indexes**: `idx_tenants_slug`, `idx_tenants_status`, `idx_tenants_stripe_subscription_id`

**RLS**: self-read + self-update (name, billing_email only) via app role; all other writes via admin role.

### `tenant_members`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| email | TEXT NOT NULL | Login/contact email |
| name | TEXT | Display name |
| permissions | TEXT[] NOT NULL DEFAULT '{}' | Explicit permission strings (31 possible) |
| email_bounced | BOOLEAN NOT NULL DEFAULT false | Bounce webhook on this email |
| email_complained | BOOLEAN NOT NULL DEFAULT false | Complaint webhook on this email |
| last_login_at | TIMESTAMPTZ | Updated on magic-link verify |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Constraints**: `UNIQUE(tenant_id, email)`, `CHECK (email ~ '^[^@]+@[^@]+\.[^@]+$')`

**Indexes**: `idx_tenant_members_tenant_id`, `idx_tenant_members_email`

### `api_keys`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| name | TEXT NOT NULL | "Slack integration script" |
| token_hash | TEXT NOT NULL UNIQUE | SHA-256 of `fk_live_<32 base62>` |
| token_prefix | TEXT NOT NULL | First 8 chars for display: `fk_live_ab12...` |
| permissions | TEXT[] NOT NULL DEFAULT '{}' | Same permission model as members |
| rate_limit_per_min | INT NOT NULL DEFAULT 1000 | Per-key rate limit override |
| last_used_at | TIMESTAMPTZ | |
| expires_at | TIMESTAMPTZ | Null = no expiry |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_api_keys_tenant_id`, `idx_api_keys_token_hash` (unique)

### `tenant_member_sessions`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | Direct RLS ownership |
| member_id | UUID NOT NULL FK → tenant_members(id) ON DELETE CASCADE | |
| refresh_hash | TEXT NOT NULL | SHA-256 of the refresh token |
| issued_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| last_used_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| revoked_at | TIMESTAMPTZ | Null = active |
| user_agent | TEXT | |
| ip | INET | |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_tenant_member_sessions_member_id`, `idx_tenant_member_sessions_refresh_hash`

### `tenant_magic_links`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| member_id | UUID FK → tenant_members(id) ON DELETE CASCADE | Null if invitation (email not yet a member) |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| email | TEXT NOT NULL | The email the link was sent to |
| token_hash | TEXT NOT NULL | SHA-256 of the magic-link token |
| expires_at | TIMESTAMPTZ NOT NULL | 10 min from creation |
| consumed_at | TIMESTAMPTZ | Null = not yet consumed |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_tenant_magic_links_token_hash`, `idx_tenant_magic_links_email`

### `platform_admins` + `platform_admin_sessions` + `platform_admin_magic_links`

Same shape as the tenant equivalents, with `admin_id` instead of `member_id` and no `tenant_id`.
`platform_admins.email` is `UNIQUE`. `permissions` uses the `platform:*` namespace (9 permissions).

### `projects`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| name | TEXT NOT NULL | Human name, e.g. `Tauview Web` |
| public_key | TEXT NOT NULL UNIQUE | `pk_live_<22 base62>` |
| allowed_domains | TEXT[] NOT NULL DEFAULT '{}' | Exact or `*.domain` wildcard entries |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_projects_tenant_id`, `idx_projects_public_key`

### `widget_rules`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| project_id | UUID NOT NULL FK → projects(id) ON DELETE CASCADE | |
| name | TEXT NOT NULL | Label for this rule |
| priority | INT NOT NULL DEFAULT 0 | Higher wins on overlapping matches |
| enabled | BOOLEAN NOT NULL DEFAULT true | |
| matcher_type | matcher_type NOT NULL | `all_pages`, `url_exact`, `url_prefix`, `url_regex` |
| matcher_value | TEXT | Pattern/URL (null if `all_pages`) |
| trigger_type | trigger_type NOT NULL | `delay`, `scroll`, `exit_intent` |
| trigger_delay_seconds | INT | Used when `trigger_type='delay'` |
| trigger_scroll_percent | INT | Used when `trigger_type='scroll'` |
| can_rate | BOOLEAN NOT NULL DEFAULT true | |
| can_write_text | BOOLEAN NOT NULL DEFAULT true | |
| can_record_audio | BOOLEAN NOT NULL DEFAULT false | Plan-gated |
| can_record_video | BOOLEAN NOT NULL DEFAULT false | Plan-gated |
| can_attach_element | BOOLEAN NOT NULL DEFAULT false | HTML + computed styles |
| can_attach_screenshot | BOOLEAN NOT NULL DEFAULT false | Separate from element attach |
| greeting | TEXT | Custom message shown by the widget |
| position | widget_position NOT NULL DEFAULT 'bottom-left' | |
| theme | widget_theme NOT NULL DEFAULT 'light' | |
| accent_color | TEXT | Hex color `^#[0-9a-fA-F]{6}$`, empty = default |
| replay_mode | replay_mode NOT NULL DEFAULT 'none' | Plan-gated |
| replay_sampling_rate | INT NOT NULL DEFAULT 0 | 0-100, used when `replay_mode='sampling'` |
| replay_max_duration_seconds | INT NOT NULL DEFAULT 1800 | |
| replay_max_events | INT NOT NULL DEFAULT 50000 | |
| replay_mask_all_inputs | BOOLEAN NOT NULL DEFAULT true | rrweb `maskAllInputs` |
| replay_mask_text_selector | TEXT | CSS selector for extra masking |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_widget_rules_project_id`, `idx_widget_rules_enabled`, `idx_widget_rules_priority`

**Save-time validation**: trigger/matcher consistency, regex compiles, accent_color format,
plan-feature alignment (e.g. `can_record_audio=true` requires plan's `AudioRecording=true`).

### `feedbacks`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| project_id | UUID NOT NULL FK → projects(id) ON DELETE CASCADE | |
| widget_rule_id | UUID FK → widget_rules(id) ON DELETE SET NULL | |
| public_id | TEXT NOT NULL | `fb_<8 base62>`, `UNIQUE(tenant_id, public_id)` |
| issue_id | UUID FK → issues(id) ON DELETE SET NULL | Auto-created issue per feedback |
| session_id | UUID FK → sessions(id) ON DELETE SET NULL | Optional replay link |
| client_feedback_id | UUID NOT NULL | Widget-generated, idempotency key, `UNIQUE(project_id, client_feedback_id)` |
| rating | SMALLINT | 1-5 |
| text | TEXT | Free-form review text |
| text_cleaned | TEXT | AI-cleaned text (filler removed) |
| text_sentiment_score | DECIMAL(3,2) | -1.0 to 1.0 |
| text_moderation_flags | TEXT[] NOT NULL DEFAULT '{}' | AI moderation flags on text |
| text_process_status | processing_status NOT NULL DEFAULT 'pending' | AI pass state for text |
| text_processing_generation | BIGINT NOT NULL DEFAULT 0 | Invalidates stale retry tasks after reprocess |
| ai_processed_at | TIMESTAMPTZ | When all AI passes finished |
| ai_job_enqueued_at | TIMESTAMPTZ | For reconcile_pending_jobs |
| reporter_email | TEXT | Optional email for follow-ups |
| notify_reporter | BOOLEAN NOT NULL DEFAULT false | Opt-in for issue updates |
| page_url | TEXT NOT NULL | |
| referrer | TEXT | |
| user_agent | TEXT | |
| os | TEXT | Parsed |
| browser | TEXT | Parsed |
| device | TEXT | `desktop`, `tablet`, `mobile` |
| viewport_width | INT | |
| viewport_height | INT | |
| language | TEXT | |
| dom_snapshot_html | TEXT | HTML of attached element |
| dom_snapshot_styles | TEXT | Computed styles of attached element |
| status | feedback_status NOT NULL DEFAULT 'open' | `open`, `spam`, `archived` |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_feedbacks_tenant_id`, `idx_feedbacks_project_id`, `idx_feedbacks_issue_id`,
`idx_feedbacks_session_id`, `idx_feedbacks_public_id`, `idx_feedbacks_status`,
`idx_feedbacks_created_at`, `idx_feedbacks_client_feedback_id` (unique composite with project_id)

### `attachments`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| feedback_id | UUID NOT NULL FK → feedbacks(id) ON DELETE CASCADE | |
| kind | attachment_kind NOT NULL | `screenshot`, `audio`, `video`, `replay` |
| client_attachment_id | UUID NOT NULL | Widget-generated, `UNIQUE(feedback_id, client_attachment_id)` |
| storage_key | TEXT NOT NULL | `tenants/{tid}/feedbacks/{fid}/attachments/{aid}/{kind}.{ext}` |
| content_type | TEXT | MIME type |
| size_bytes | BIGINT | |
| duration_seconds | INT | For audio/video/replay |
| processing_status | processing_status NOT NULL DEFAULT 'pending' | Includes `skipped` |
| processing_attempts | INT NOT NULL DEFAULT 0 | |
| complete_token_hash | TEXT | Single-use completion token, cleared on complete |
| completed_at | TIMESTAMPTZ | |
| transcription | TEXT | AI-generated transcript |
| sentiment_score | DECIMAL(3,2) | -1.0 to 1.0 |
| moderation_flags | TEXT[] NOT NULL DEFAULT '{}' | e.g. `profanity`, `harassment` |
| error_message | TEXT | On failure |
| processed_at | TIMESTAMPTZ | When AI finished |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_attachments_tenant_id`, `idx_attachments_feedback_id`, `idx_attachments_kind`,
`idx_attachments_processing_status`, `idx_attachments_client_attachment_id` (unique composite)

### `ai_gemini_files`

Durable cleanup ledger for temporary Gemini File API uploads. Each row records the tenant,
attachment, remote filename, next deletion time, enqueue time, attempt count, last error, and
terminal `deleted_at` or `failed_at` timestamp. The reconciler claims due rows with
`FOR UPDATE SKIP LOCKED`; filenames remain recoverable across worker crashes and provider outages.

### `sessions`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| project_id | UUID NOT NULL FK → projects(id) ON DELETE CASCADE | |
| feedback_id | UUID FK → feedbacks(id) ON DELETE SET NULL | Primary display link |
| session_id | TEXT NOT NULL | Client-generated session uuid |
| page_url | TEXT | First page of the session |
| started_at | TIMESTAMPTZ NOT NULL | |
| ended_at | TIMESTAMPTZ | |
| events_count | INT NOT NULL DEFAULT 0 | |
| storage_key | TEXT | `tenants/{tid}/sessions/{sid}/replay.json` (set after merge) |
| rrweb_version | TEXT NOT NULL | Pinned capture library version |
| merged_at | TIMESTAMPTZ | When batch merge job finished |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_sessions_tenant_id`, `idx_sessions_project_id`, `idx_sessions_feedback_id`,
`idx_sessions_session_id`

### `issues`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| project_id | UUID NOT NULL FK → projects(id) ON DELETE CASCADE | |
| public_id | TEXT NOT NULL | `{SLUG_UPPER}-{YEAR}-{SEQ:0000}`, e.g. `TAU-2026-0001` |
| title | TEXT | Auto-set from feedback text (first 80 chars) or `[No text]` |
| status | issue_status NOT NULL DEFAULT 'open' | |
| merged_into_id | UUID FK → issues(id) ON DELETE SET NULL | Soft-merge target |
| merged_at | TIMESTAMPTZ | |
| reporter_email | TEXT | For notifications (copied from feedback) |
| notify_reporter | BOOLEAN NOT NULL DEFAULT false | |
| resolved_at | TIMESTAMPTZ | |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_issues_tenant_id`, `idx_issues_project_id`, `idx_issues_public_id`,
`idx_issues_status`, `idx_issues_merged_into_id`

### `issue_updates`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| issue_id | UUID NOT NULL FK → issues(id) ON DELETE CASCADE | |
| member_id | UUID FK → tenant_members(id) ON DELETE SET NULL | Who made the change |
| old_status | issue_status | |
| new_status | issue_status NOT NULL | |
| comment | TEXT | Optional internal note |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_issue_updates_tenant_id`, `idx_issue_updates_issue_id`, `idx_issue_updates_created_at`

### `issue_counters`

| Column | Type | Notes |
|--------|------|-------|
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | PK part 1 |
| year | INT NOT NULL | PK part 2 |
| next_seq | INT NOT NULL DEFAULT 1 | Atomic increment via `ON CONFLICT DO UPDATE` |

**Primary key**: `(tenant_id, year)`. No additional indexes needed.

### `integrations`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| kind | integration_kind NOT NULL | `slack`, `teams`, `webhook` (no `email`) |
| name | TEXT NOT NULL | Display label |
| webhook_url | TEXT | Slack/Teams/webhook URL |
| channel | TEXT | Slack channel override |
| custom_headers_ciphertext | BYTEA | Webhook-only headers encrypted by the application key |
| webhook_secret_ciphertext | BYTEA | Recoverable application-encrypted HMAC secret |
| webhook_secret_fingerprint | TEXT | Non-secret fingerprint for support/display |
| events | event_type[] NOT NULL DEFAULT '{}' | Subscribed events |
| is_active | BOOLEAN NOT NULL DEFAULT true | |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

**Indexes**: `idx_integrations_tenant_id`, `idx_integrations_kind`, `idx_integrations_active`

### `delivery_logs`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| outbox_event_id | UUID NOT NULL FK → outbox_events(id) ON DELETE CASCADE | Stable retry identity |
| integration_id | UUID FK → integrations(id) ON DELETE SET NULL | Null for reporter emails |
| event | event_type NOT NULL | What triggered the delivery |
| resource_type | TEXT NOT NULL | `feedback` or `issue` |
| resource_id | UUID NOT NULL | |
| attempt_number | INT NOT NULL | 1, 2, 3... (from asynq retry count) |
| status | delivery_status NOT NULL DEFAULT 'pending' | `pending`, `sent`, `failed`, `bounced` |
| provider_message_id | TEXT | Resend/Slack message ID on success |
| error_message | TEXT | On failure |
| sent_at | TIMESTAMPTZ | When provider confirmed send |
| webhook_event_id | TEXT | Last verified provider webhook event id |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | When this attempt was created |

**Indexes**: `idx_delivery_logs_tenant_id`, `idx_delivery_logs_integration_id`,
`idx_delivery_logs_resource` (composite: `resource_type, resource_id`),
`idx_delivery_logs_status`, `idx_delivery_logs_created_at`

Note: one row per attempt. `bounced` is the only post-creation status change (async bounce webhook
updates a `sent` row to `bounced`).

### `outbox_events`

| Column | Type | Notes |
|--------|------|-------|
| id | UUID PK DEFAULT gen_random_uuid() | Stable event/task identity |
| tenant_id | UUID NOT NULL FK → tenants(id) ON DELETE CASCADE | |
| event | event_type NOT NULL | Domain event |
| resource_type | TEXT NOT NULL | `feedback` or `issue` |
| resource_id | UUID NOT NULL | |
| event_key | TEXT NOT NULL DEFAULT '' | Distinguishes repeated events for one resource |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | Written in the domain transaction |
| dispatched_at | TIMESTAMPTZ | Set only after all deterministic destination tasks are accepted |
| attempts | INT NOT NULL DEFAULT 0 | Dispatcher attempts |
| last_error | TEXT | Last dispatcher error |

**Constraints**: `UNIQUE(event, resource_type, resource_id, event_key)`.

The dispatcher re-fetches active destinations and uses deterministic task IDs derived from
`outbox_event_id + destination_kind + destination_id`. Duplicate dispatch is therefore harmless.

### `stripe_events`

| Column | Type | Notes |
|--------|------|-------|
| event_id | TEXT PK | `evt_...` from Stripe |
| processed_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Webhook idempotency — `INSERT ... ON CONFLICT DO NOTHING` returns 0 rows for duplicates.

### `tenant_deletions` (compliance audit, not tenant-scoped, no RLS)

| Column | Type | Notes |
|--------|------|-------|
| tenant_id | UUID PK | The deleted tenant's ID, retained |
| tenant_slug | TEXT NOT NULL | |
| tenant_name | TEXT NOT NULL | |
| requested_at | TIMESTAMPTZ NOT NULL | |
| executed_at | TIMESTAMPTZ NOT NULL | |
| requested_by_member_id | UUID | Null for platform-admin force-delete |
| forced_by_platform_admin_id | UUID | Null for self-service |
| feedback_count | INT NOT NULL | |
| attachment_count | INT NOT NULL | |

## Plans & feature flags

All plan definitions live in `backend/internal/config/plans.go` so limits and features can be
changed without touching business logic. `-1` means unlimited.

| Plan | Monthly | Yearly | Projects | Feedback/mo | Users | Storage days | AI | Replay | Slack | Video | Audio | DOM | Custom domain |
|------|---------|--------|----------|-------------|-------|--------------|----|--------|-------|-------|-------|-----|---------------|
| starter | €4.99 | 7% off | 1 | 500 | 1 | 30 | no | no | no | no | no | no | no |
| growth | €9.99 | 14% off | 4 | 2,500 | 3 | 90 | yes | limited | no | no | yes | yes | no |
| scale | €27.99 | 14% off | 10 | 10,000 | 10 | 365 | yes | yes | yes | yes | yes | yes | no |
| enterprise | custom | custom | -1 | -1 | -1 | -1 | yes | yes | yes | yes | yes | yes | yes (future) |

The `features` object returned by `GET /v1/widget/config` is derived from this table at request
time, not stored per rule. Plan overrides rule (a `starter` tenant with `replay_mode='sampling'`
gets `features.session_replay=false` and the widget ignores the setting).

## Permissions

32 tenant permissions (`resource:action`, no wildcards):

`project:create|read|update|delete`, `widget_rule:create|read|update|delete`,
`feedback:read|update|delete`, `issue:read|update|delete|merge`,
`session:read|delete`, `member:read|invite|update|remove`,
`api_key:create|read|delete`, `integration:create|read|update|delete`,
`billing:manage|read`, `tenant:update|read`.

3 UI presets (stored as explicit arrays, not roles): admin (all), editor (no deletes/member/api_key/billing), viewer (read-only).

9 platform permissions: `platform:tenant:create|read|update|suspend|delete`,
`platform:admin:create|read|update|delete`.

Enforced by `RequirePerm(perm)` middleware per route. JWT carries `permissions` snapshot
(10-min staleness accepted). API keys use the same permission model.

## Monorepo structure

```
app.fidibeki/
├── .github/workflows/      # backend-ci, widget-ci, web-ci, deploy
├── backend/                # Go module
│   ├── cmd/fidibeki-api/main.go
│   ├── internal/
│   │   ├── config/         # config.go, plans.go
│   │   ├── server/         # chi router wiring
│   │   ├── middleware/     # tenantmw, authmw, planmw, corsmw, ratelimit
│   │   ├── auth/           # magiclink, jwt, sessions, permissions.go
│   │   ├── tenant/         # TenantResolver, SubdomainResolver, CustomDomainResolver (stub)
│   │   ├── project/        # project + widget_rules CRUD
│   │   ├── feedback/       # feedback API, attachments, presign
│   │   ├── issue/          # issues, issue_updates, merge, public_id allocation
│   │   ├── ai/             # Gemini client, prompts, worker handlers
│   │   ├── email/          # Resend client, templates, worker handlers
│   │   ├── storage/        # S3/MinIO client, presign, keys.go
│   │   ├── queue/          # asynq client, server, job definitions, reconciliation
│   │   ├── stripe/         # checkout, webhooks, portal
│   │   ├── integration/    # Slack, Teams, webhook delivery
│   │   ├── platformadmin/  # platform admin auth, tenant CRUD, force-delete
│   │   ├── replay/         # session recording API, batch merge job
│   │   ├── db/             # pgxpool setup, Querier interface, migrations helper
│   │   ├── logging/        # slog setup, PII redaction, request_id
│   │   ├── api/            # errors.go (envelope + code catalog), response helpers
│   │   └── testutil/       # testcontainers, fixtures, RLS assertion helpers
│   └── Dockerfile          # multi-stage alpine
├── widget/                 # vanilla JS, esbuild, Vitest + happy-dom
│   ├── src/
│   │   ├── loader.ts       # ~5KB entry, config fetch, trigger logic
│   │   ├── widget.ts       # Shadow DOM UI
│   │   ├── triggers.ts     # delay, scroll, exit_intent
│   │   ├── config.ts       # config cache, SPA navigation detection
│   │   ├── recorder.ts     # rrweb integration, batch upload
│   │   ├── messages.ts     # error.code → user-facing strings
│   │   └── modules/        # lazy-loaded: rating, audio, video, screenshot, error-reporting
│   ├── tests/unit/         # Vitest
│   ├── tests/e2e/          # Playwright
│   └── esbuild.config.js
├── web/                    # Nuxt 3 backoffice
│   ├── pages/              # /login, /, /feedback/[id], /issues, /settings, /billing, /integrations
│   ├── components/
│   ├── composables/        # useAuth, can()
│   ├── plugins/            # apiFetch (silent refresh on 401)
│   ├── middleware/         # auth.ts (page), server middleware (SSR)
│   ├── tests/unit/         # Vitest
│   ├── tests/e2e/          # Playwright
│   ├── nuxt.config.ts
│   └── Dockerfile
├── infra/
│   ├── migrations/         # ~33 numbered SQL file pairs
│   ├── docker-compose.yml  # postgres, pgbouncer, redis, minio, caddy, api, web
│   ├── Caddyfile           # *.fidibeki.com → {api, web}; widget.fidibeki.com → static
│   ├── railway.json
│   └── stripe/fixtures/    # webhook event fixtures for tests
├── docs/
│   ├── env.md              # all env vars documented
│   ├── runbook.md          # 10 failure scenarios
│   ├── api.md              # generated API docs
│   └── architecture.md     # this plan's decisions
├── plans/fidibeki.plan.md  # this file
├── .env.example            # all env vars with defaults/placeholders
└── README.md
```

pnpm workspace for `widget/` + `web/` (shared dev deps: vitest, typescript, esbuild).

## Env var catalog

See `.env.example` for the full list (~35 vars). Key groups: App, Database (3 URLs), Redis,
S3/MinIO, Auth (JWT_SECRET + TTLs), Stripe (secret + webhook + 6 Price IDs), Resend (key + 3
from/reply addresses), Gemini (key + model + RPM), Sentry (DSN + release), Turnstile (secret +
site key), Queue (4 concurrency overrides), Widget (CDN base).

## Migration sequence by phase

**Phase 0** (auth + tenant + RLS core, 10 migrations):
`create_extensions` (placeholder), `create_roles` (with `ALTER DEFAULT PRIVILEGES`),
`create_tenants` (+RLS), `create_tenant_members` (+RLS), `create_api_keys` (+RLS),
`create_platform_admins`, `create_platform_admin_sessions`,
`create_platform_admin_magic_links`, `create_tenant_member_sessions`, `create_tenant_magic_links`.

**Phase 2** (1): `add_tenant_magic_link_invite_permissions` (persists the explicit permission
snapshot for invitations whose `member_id` remains null until the link is consumed).

**Phase 3** (3): `create_projects` (+RLS), `create_widget_enums`, `create_widget_rules` (+RLS).

**Phase 5** (10): `create_feedback_status_enum`, `create_processing_status_enum` (with `skipped`),
`create_issue_status_enum`, `create_event_type_enum`, `create_issue_counters`, `create_issues`
(+RLS), `create_feedbacks` (+RLS, includes issue_id, reporter_email, notify_reporter,
client_feedback_id, ai_job_enqueued_at), `create_attachments` (+RLS, includes client_attachment_id,
complete_token_hash, completed_at), `create_outbox_events` (+RLS), `create_delivery_status_enum`.

**Phase 6** (4): `add_feedbacks_text_ai_columns` (text_cleaned, text_sentiment_score,
text_moderation_flags, text_process_status, text_processing_generation, ai_processed_at),
`add_attachments_error_columns` (error_message, processing_attempts, terminal-state backfill),
`create_ai_daily_usage` (+RLS, atomic per-tenant/day budget), `create_ai_gemini_files`
(+RLS, durable Gemini file cleanup ledger).

**Phase 7** (2): `create_issue_updates` (+RLS), `create_delivery_logs` (+RLS, reporter email attempts).

**Phase 8** (2): `create_sessions` (+RLS, includes feedback_id, rrweb_version, merged_at),
`add_feedbacks_session_link`.

**Phase 9** (3): `create_integration_kind_enum`, `create_integrations` (+RLS, includes encrypted
webhook secret and custom headers), `add_delivery_logs_integration` (nullable integration_id FK).

**Phase 10** (3): `create_stripe_events`, `add_tenants_billing_columns` (pending_plan, email_bounced,
deletion_requested_at), `create_tenant_deletions` (compliance audit, no RLS).

Every migration is one concept, inline RLS, paired `.down.sql`, tested via up→down→up in CI.

---

## Phase 0: Foundations

**User stories covered**: As a developer, I can run the entire platform locally so that I can
develop and test end-to-end.

### What to build

Set up the monorepo, local infrastructure (Postgres + pgbouncer, MinIO, Redis, Caddy reverse
proxy), the Go module with chi, slog, Sentry init, two pgxpool instances (app + admin), the
first 10 migrations (auth + tenant + RLS), tenant subdomain middleware with transaction-local context, the
platform/tenant router split, `/healthz` + `/readyz`, and integration tests against real
testcontainers (Postgres, Redis, MinIO) with RLS positive/negative/admin tests.

### Acceptance criteria

- [x] `docker compose up` starts Postgres, pgbouncer, MinIO, Redis, Caddy, and the API.
- [x] `go test ./...` runs unit + integration tests including testcontainers Postgres.
- [x] `make test-short` runs unit tests only (no testcontainers) in <1s with a warm build cache.
- [x] `GET /healthz` returns `200 OK` with `{ "status": "ok" }`.
- [x] `GET /readyz` returns `200` if DB+Redis+S3 pass, `503` with per-check status otherwise.
- [x] Two `pgxpool` instances (app + admin) with the right roles; migrator uses direct URL.
- [x] Middleware resolves `acme.localhost` → tenant, starts `pgx.Tx`, and calls
  `SELECT set_config('app.tenant_id', $1, true)`; it rolls back on panic and flushes the buffered response only after
  commit succeeds. Unknown subdomain → `404 unknown_tenant`. Suspended → `403 tenant_suspended`.
- [x] RLS enabled on all tenant-scoped tables; integration tests verify positive (tenant reads own),
  negative (tenant can't read other tenant's), and admin bypass.
- [x] `X-Request-ID` header on every response; `slog` logs include `request_id`, `tenant_id`, `tenant_slug`.
- [x] CI runs `go vet`, `golangci-lint`, `go test` (with testcontainers), up→down→up migration test.
- [x] `.env.example` committed; `Config.Validate()` exits with clear error on missing required vars.

---

## Phase 1: Multi-tenancy & Plans

**User stories covered**: As the platform, I can identify and limit each company account so that
data stays isolated and plan features are enforced.

### What to build

Tenant management, plan configuration in `plans.go`, runtime feature/limit checks via
`CanUseFeature(tenant, feature)` and `RequirePlanLimit` middleware. Platform admin auth (magic-link,
shared library), the transactional-email asynq worker and Resend adapter required by authentication,
plus `POST /admin/tenants` CRUD. A local capture adapter makes the flow testable without Resend.
Slug validation (3-24 chars, lowercase, not reserved, unique). Cross-tenant access rejected with
`403` in integration tests. A seed command creates or promotes the first platform admin.

### Acceptance criteria

- [x] `POST /admin/tenants` creates a tenant (platform-admin auth, `platform:tenant:create` perm).
- [x] Middleware injects `(tenant, status, plan, trial_ends_at)` into request context.
- [x] Unknown subdomains return `404 unknown_tenant`; suspended tenants return `403 tenant_suspended`.
- [x] `CanUseFeature(tenant, feature)` returns false when a feature is not in the tenant's plan.
- [x] `RequirePlanLimit` middleware returns `402 plan_limit_exceeded` with `upgrade_url` on exceeded limits.
- [x] `RequirePlanLimit` returns `402 plan_feature_unavailable` when a feature isn't in the plan.
- [x] A tenant's plan limits (projects, users, feedback/month, storage days, features) returned by a config endpoint.
- [x] Cross-tenant resource access is rejected with `403` in integration tests (RLS + app-layer).
- [x] Platform admin magic-link auth flow: `POST /admin/auth/login`, `POST /admin/auth/verify`,
  `POST /admin/auth/refresh`, `POST /admin/auth/logout`.
- [x] `fidibeki-api admin bootstrap --email ...` idempotently creates the first platform admin.
- [x] Magic-link delivery works through the local capture adapter and the production Resend adapter.
- [x] `RequirePlatformAdmin(perm)` middleware on platform routes; `aud=platform` JWT enforced.
- [x] Slug validation at app + DB level (regex + reserved words check).

---

## Phase 2: Backoffice authentication

**User stories covered**: As a company user, I can log into the backoffice via magic link and
invite teammates so that I can manage feedback securely.

### What to build

Magic-link registration/login flow for tenant members (reuses the `internal/auth/magiclink`
library from Phase 1, adds `TenantStore`). JWT access + refresh tokens with `aud=tenant`.
Permission-based access control via `RequirePerm` middleware. Invitation flow for additional
users (magic link with `member_id=null` until consumed). Refresh token rotation + revocation.

### Acceptance criteria

- [x] `POST /auth/login` sends a magic link email (async via asynq `email-pool`).
- [x] `POST /auth/verify` validates the magic link token, issues access + refresh, sets refresh cookie.
- [x] `POST /auth/refresh` rotates the refresh token (revoke old, issue new); reuse-of-revoked revokes all sessions.
- [x] `POST /auth/logout` revokes the session, clears the refresh cookie.
- [x] Admins have full permissions; custom permission sets assignable to members.
- [x] Backoffice routes require `tenant` context + `RequireTenantMember` + `RequirePerm(perm)`.
- [x] `aud=tenant` JWTs rejected on platform routes; `aud=platform` JWTs rejected on tenant routes.
- [x] Refresh tokens stored hashed; revocable per-session and per-member.
- [x] Invitations: `POST /auth/invite` sends a magic link to a non-member email; on verify, creates the member.

---

## Phase 3: Projects & widget configuration

**User stories covered**: As a product team, I can create a project, configure the widget per
page and get an embed script so that I can collect feedback on my site.

### What to build

CRUD for projects + widget_rules. Projects hold a unique public key (`pk_live_<22 base62>`)
and allowed domains (exact or `*.domain` wildcards). Widget rules stored per the revised schema
(typed columns for matcher, trigger, form fields, replay, theme). Save-time validation including
plan-feature alignment (e.g. `can_record_audio=true` requires plan's `AudioRecording`).

### Acceptance criteria

- [x] Members with `project:create`/`project:update` can manage projects and widget rules.
- [x] Each project has a unique `pk_live_` public key.
- [x] Widget rules can vary by URL pattern (`all_pages`, `url_exact`, `url_prefix`, `url_regex`).
- [x] `allowed_domains` supports exact + `*.domain` wildcard entries.
- [x] Save-time validation: trigger/matcher consistency, regex compiles, accent_color format,
  plan-feature alignment (`422` with field-level errors if misaligned).
- [x] `RequirePerm` middleware enforces `widget_rule:create/update/delete` per route.

---

## Phase 4: Widget embed

**User stories covered**: As an end user, I am asked for feedback at the right moment on a
website so that I can report confusing or helpful experiences easily.

### What to build

Zero-dependency vanilla JS widget built with esbuild into a ~5KB `loader.js` + lazy ES modules.
Shadow DOM for isolation. One-line embed (`<script src=... data-fidibeki-key=... async>`).
Config fetch from `GET /v1/widget/config` (public key + Origin check). Triggers: delay, scroll,
exit_intent with `sessionStorage` suppression. SPA navigation detection via history patch +
custom event; config refreshed every 5 min and on `visibilitychange`. Attach-element: HTML +
computed styles. Screenshot capture is independently plan/rule gated. Honeypot field for bot protection.

### Acceptance criteria

- [x] Widget loads on allowed domains and is blocked on unknown domains (`403` from config endpoint).
- [x] Trigger respects per-page config (delay, scroll depth, exit intent).
- [x] End user can rate 1-5 stars and write a text review.
- [x] End user can click a page element to attach its HTML + computed styles.
- [x] Widget sends OS, browser, viewport size, URL, referrer metadata.
- [x] Lazy modules load only for features the plan allows (`features` object from config).
- [x] Honeypot field silently drops bot submissions (returns `200` but no feedback created).
- [x] Sentry SDK lazy-loads on first error; PII scrubbed in `beforeSend`.
- [x] Vitest unit tests for trigger logic, config matching, suppression state.
- [x] Playwright E2E: open sample page → fetch config → trigger widget → serialize a submission
  against a contract-compatible mock API. The real submission/DB E2E is owned by Phase 5.

---

## Phase 5: Feedback API

**User stories covered**: As the widget, I can submit feedback and media to the platform so
that product teams see it in the backoffice.

### What to build

Public `POST /v1/feedback` on the dedicated RLS-enforced widget router (widget auth:
`X-Fidibeki-Public-Key` + Origin + rate limit). JSON metadata first → presigned S3 URLs →
completion callback. Auto-creates an issue per
feedback (transactional). Returns `{ feedback_id, issue_public_id, notify_reporter }`.
No reporter token (email-only via Q19e unsubscribe JWT). Rate-limited (10/min per IP per
project + 100/hour per project ceiling). Plan limit checked (402 if exceeded).

### Acceptance criteria

- [x] Valid project key + allowed origin required (`403` otherwise).
- [x] Monthly feedback count checked against plan; exceeded returns `402 plan_limit_exceeded`.
- [x] Rate limit: 10/min per IP per project (`429` with `Retry-After`); 100/hour per-project ceiling.
- [x] `client_feedback_id` + `client_attachment_id` provide idempotency (duplicate → returns existing).
- [x] Presigned S3 URLs returned (10-min TTL); widget PUTs directly; completion callback verifies
  token + S3 HEAD (size + MIME).
- [x] Invalid payloads return `422 validation_error` with field-level `details[]`.
- [x] Issue auto-created in the same transaction (`public_id` allocated via `issue_counters`).
- [x] Feedback and its `feedback.created` outbox event are committed atomically; the dispatcher
  uses deterministic task IDs and retries undispatched rows.
- [x] Integration tests: happy path, invalid origin, plan limit, rate limit, malformed body, idempotent retry.

---

## Phase 6: AI processing (Gemini)

**User stories covered**: As a product team, I can see transcribed and moderated feedback so
that I can triage faster.

### What to build

Per-attachment + per-text asynq jobs (`ai.attachment_process`, `ai.text_process`).
Gemini `2.5-flash` for all tasks. Stream MinIO → Gemini File API → `generateContent` (structured
JSON output) → delete Gemini file. `redis_rate` limiter keyed `gemini` (`GEMINI_RPM` env).
Completion check sets `feedbacks.ai_processed_at` when all passes done. Plan-gated: non-AI plans
get `processing_status='skipped'`. Video uses Gemini native video (no FFmpeg). Manual retry per
attachment + reprocess-all per feedback from backoffice.

### Acceptance criteria

- [x] Audio feedback transcribed when plan allows AI; `processing_status='completed'` with `transcription`.
- [x] Text feedback: cleaned (filler removed), sentiment score stored, moderation flags stored.
- [x] Screenshot OCR extracts visible text when plan allows AI.
- [x] Video: Gemini native video transcription (no FFmpeg).
- [x] Offensive language flagged in `moderation_flags`; not auto-hidden (flag-only, tenant decides).
- [x] AI jobs queued via asynq `ai-pool` (concurrency 3) with `redis_rate` Gemini limiter.
- [x] Transient failures (429, 500, timeout): revert to `pending`, asynq retries with backoff.
- [x] Permanent failures (content policy, malformed media): `processing_status='failed'`, `asynq.SkipRetry`.
- [x] `feedbacks.ai_processed_at` set when all passes finish.
- [x] Backoffice shows AI progress badge (Phase 12 dashboard dependency).
- [x] `POST /v1/attachments/{id}/retry` (re-enqueues) + `POST /v1/feedbacks/{id}/reprocess` (all).
- [x] Failures never crash the feedback submission flow (async, isolated).

---

## Phase 7: Issues & email notifications (Resend)

**User stories covered**: As an end user, I can receive email updates about the issue I reported
so that I know my feedback was acted on.

### What to build

Issues typed rows with `{SLUG}-{YEAR}-{SEQ}` public id (allocated via `issue_counters`).
Auto-created per feedback (Q20d); tenant can merge (soft-merge with `merged_into_id`).
Status workflow `open|in_progress|resolved|closed` with `issue_updates` history.
Reporter email notifications on issue status changes, sent via Resend `email-pool` async.
`delivery_logs` one row per attempt. Unsubscribe via JWT (`aud=reporter-unsubscribe`, 90d).
Bounce/complaint webhook auto-unsubscribes.

### Acceptance criteria

- [x] Each issue gets a public id like `TAU-2026-0001` (uppercase slug, 4-digit seq, yearly reset).
- [x] Auto-created per feedback (title = first 80 chars of text or `[No text]`).
- [x] Tenant can merge issues (soft-merge, `merged_into_id` preserved, un-merge possible).
- [x] Reporter email optional; if provided + opted-in, status updates trigger an email.
- [x] Emails sent from `updates@fidibeki.com` with `{Tenant name} via Fidibeki` from-name.
- [x] Unsubscribe link (JWT, 90d) in every reporter email; clicking sets `notify_reporter=false`.
- [x] `delivery_logs` records each attempt with `attempt_number`, `status`, `provider_message_id`.
- [x] Resend bounce/complaint webhook on `api.fidibeki.com`: updates `delivery_logs.status='bounced'`,
  auto-unsubscribes reporter on complaint.
- [x] `issue_updates` records every status change with `member_id`, `old_status`, `new_status`, `comment`.

---

## Phase 8: Session replay

**User stories covered**: As a product team, I can replay a user's session so that I can
understand exactly what confused them.

### What to build

rrweb (pinned) with PII masking. Session-level sampling + caps (30 min, 50k events).
Batch upload to S3 presigned PUT (~200KB per batch); post-session `replay.merge` job concatenates
into `replay.json` and deletes batches. `rrweb-player` in a Vue component; replay streamed
through Go API (auth + RLS, not direct S3). `sessions.rrweb_version` + `merged_at` columns.
Retention = plan's `storage_days` (lifecycle + daily DB cleanup cron).

### Acceptance criteria

- [x] Replay recording respects tenant sampling config (0%, 1%, 100%, etc.).
- [x] PII masking: `maskAllInputs=true`, `maskTextSelector` for extra fields.
- [x] Recording continuous across SPA navigations; ends on `pagehide` or 5-min idle.
- [x] Batch upload (one S3 object per ~50 events); `replay.merge` job produces final `replay.json`.
- [x] Backoffice shows "processing" until merge, then "ready"; player loads concatenated events.
- [x] Replay linked to feedback (both `sessions.feedback_id` and `feedbacks.session_id`).
- [x] Storage respects plan retention days (lifecycle policy + daily cleanup cron).
- [x] Feature blocked for plans without `SessionReplay=true` (`402 plan_feature_unavailable`).

---

## Phase 9: Slack integration

**User stories covered**: As a product team, I can receive feedback notifications in Slack so
that my team reacts immediately.

### What to build

Integrations in the generic `integrations` table (Slack/Teams/webhook, no email kind).
Per-integration HMAC webhook secret. Event dispatch: on `feedback.created` etc., enqueue
one `integration.deliver` job per active integration subscribed to the event. Slack Block Kit
messages. Webhooks signed with `X-Fidibeki-Signature: HMAC-SHA256`. Teams MessageCard for MVP.
`delivery_logs` per attempt. Transactional outbox dispatch + deterministic task identities.

### Acceptance criteria

- [x] Admins can save/remove integrations and select event types (`integration:create/delete` perm).
- [x] Webhook secret generated on creation, shown once, stored application-encrypted with a
  fingerprint; `X-Fidibeki-Signature` header on every webhook.
- [x] New feedback sends a Slack message (Block Kit) when configured for `feedback.created`.
- [x] Slack/Teams/webhook failures retried per asynq policy; logged in `delivery_logs`; don't block feedback flow.
- [x] One asynq job per integration (failure isolation); reporter email is a separate job.
- [x] Transactional outbox rows fan out to deterministic per-integration tasks; partial enqueue
  retries do not duplicate provider deliveries.
- [x] Feature only available for plans with `SlackNotifications=true` (`402` otherwise).

---

## Phase 10: Stripe billing

**User stories covered**: As a company, I can start a free trial, choose a plan and pay so
that the platform bills me automatically. I can also close my account.

### What to build

Self-service signup (`POST api.fidibeki.com/v1/signup`, Turnstile-protected). Stripe Checkout
(hosted) + Customer + Subscription + one Price per plan. Hybrid trial (Stripe + Fidibeki
`trial_ends_at` + daily cron + Resend dunning). 6 webhook events + `stripe_events` idempotency.
Downgrades: restrict-new-creation + `pending_plan` applied at next period. Customer Portal.
Account deletion: self-service with 7-day grace + platform admin force-delete +
`tenant_deletions` audit table.

### Acceptance criteria

- [x] `POST /v1/signup` creates a `trial` tenant + first admin member; sends magic link; Turnstile verified.
- [x] New tenant starts a 14-day trial with the chosen plan's limits.
- [x] Lazy Stripe customer creation on first billing interaction.
- [x] Stripe checkout creates a subscription and links it to the tenant.
- [x] Webhooks update plan, status, trial end date (`stripe_events` idempotency; load-from-Stripe).
- [x] Dunning emails at 3d/1d/0d before trial end (Resend async).
- [x] Unpaid trial converts to suspended; feedback API blocks suspended tenants.
- [x] Downgrades allowed; new resource creation blocked over limit (`402`); existing resources fully usable.
- [x] `pending_plan` applied at next billing period via `invoice.paid` webhook.
- [x] Customer portal link available for admins (`POST /billing/portal`).
- [x] `POST /v1/billing/delete-account` (typed confirmation, 7-day grace, email to members).
- [x] `POST /v1/billing/cancel-deletion` (during grace).
- [x] `DELETE /admin/tenants/{id}` (platform admin force-delete, immediate, audited).
- [x] Daily `tenant_deletion_executor` cron: cancel Stripe, revoke sessions, delete S3, `DELETE FROM tenants CASCADE`, audit log.
- [x] Delete unverified tenants (no magic-link consumption) after 24h.

---

## Phase 11: Backoffice Nuxt UI

**User stories covered**: As a company user, I can manage projects, view feedback, triage
issues and configure the widget in a branded backoffice.

### What to build

Nuxt 3 on `*.fidibeki.com`, hybrid SSR (`/login` + `/` SSR, rest SPA). `apiFetch` plugin with
silent-refresh-on-401. `useState` for auth. `can(perm)` for authz (plan limits surface as 402).
Pages: dashboard (metrics), project list, widget config editor, feedback list (cursor-paginated
+ 9 filters), feedback detail (metadata, screenshot, transcript, sentiment, replay player),
issue detail (status updates, merge), settings (members, api_keys, integrations), billing
(plan, portal, delete-account). No per-tenant branding for MVP (tenant name in header).

### Acceptance criteria

- [x] Login page per subdomain sends magic link; `GET /auth/verify` (SSR) → `POST /auth/verify` (Go).
- [x] Dashboard shows feedback count, issue count, satisfaction trend.
- [x] Members with correct permissions can manage projects and widget configuration.
- [x] Feedback list: cursor pagination + filters (project, status, rating, attachment, moderation, issue, date, sort).
- [x] Feedback detail: metadata, screenshot, transcript, sentiment, replay player (`rrweb-player`).
- [x] Issues can be created/updated/resolved/merged and trigger emails.
- [x] "Retry AI" per attachment + "Reprocess all" per feedback.
- [x] Settings: members (invite, permissions editor with 3 presets), API keys, integrations.
- [x] Billing: plan, customer portal link, delete-account with typed confirmation + grace banner.
- [x] Trial banner ("14-day trial") shown during trial.
- [x] Playwright E2E: login, project creation, feedback triage, issue merge, billing delete-request.

---

## Phase 12: Deploy & observability

**User stories covered**: As the platform, I run reliably in production with logs, CI/CD and
staging parity.

### What to build

Multi-stage alpine Dockerfiles (Go + Nuxt separate). Widget served as static files by Caddy/CDN.
Railway `railway.json` + pgbouncer config. GitHub Actions: PR pipeline (<8 min, fast + integration),
main pipeline (auto-deploy staging + post-deploy E2E + manual promote to prod). Migrations run
in deploy (`migrate up` via `DATABASE_URL_DIRECT`). Source maps uploaded to Sentry in CI.
`docs/env.md` + `docs/runbook.md` (10 scenarios). Data export tool (deferred — manual via runbook for now).

### Acceptance criteria

- [x] `docker build` produces runnable API and web images (alpine, multi-stage).
- [x] Widget built to `widget/dist/` (esbuild, content-hashed filenames, long Cache-Control).
- [x] Railway deploys `main` to staging automatically; prod is manual promote.
- [x] Migrations run before app start (`migrate up` via direct URL); failure = old instance stays live.
- [x] All env vars documented in `docs/env.md` + `.env.example`.
- [x] Healthcheck and DB connectivity verified on deploy (`/readyz`).
- [x] Railway staging bucket CORS supports browser presigned `PUT`; a real widget screenshot and
  replay batch uploaded, Gemini OCR completed, replay merge completed, and masked playback rendered.
- [x] Source-map upload is integrated into CI (Go release + widget + Nuxt); credentials gate execution.
- [x] `docs/runbook.md` covers: DB migration failure, Stripe webhook replay, Redis queue backup,
  AI processing failures, email delivery failures, tenant suspended incorrectly, widget not loading,
  S3 storage full, deploy rollback, secrets rotation.
- [x] Read-only Asynq queue operations UI mounted on `api.fidibeki.com` behind platform-admin auth.

Local packaging and deployment automation are complete. Railway staging is configured and verified;
the first production promotion remains intentionally manual.

### Production launch gates

- [x] Store an environment-scoped Railway production token in GitHub Actions.
- [ ] Provision isolated production Postgres, Redis, object storage, API, worker, web, and widget services;
  configure the production bucket's browser-upload CORS policy.
- [ ] Configure production-only runtime credentials for Resend, Stripe, Turnstile, Gemini, and Sentry.
- [ ] Configure and validate `api.fidibeki.com`, `*.fidibeki.com`, and `widget.fidibeki.com` DNS and TLS.
- [ ] Enable data backups and deployment alerts; set the intended production replica counts.
- [ ] Run the manually confirmed production promotion and complete production smoke tests.
