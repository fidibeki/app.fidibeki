# Plan: Fidibeki

> Source PRD: Conversation with founder — instant feedback capture SaaS for web apps.

## Architectural decisions

- **Runtime & backend**: Go 1.23, `chi` router, `slog` for structured logging.
- **Database**: PostgreSQL 16, row-level tenant isolation via `tenant_id` columns. Single shared database for the MVP.
- **Migrations**: `golang-migrate` with SQL files stored in `infra/migrations`.
- **Multi-tenancy routing**: Subdomains of `fidibeki.app` (e.g. `tauview.fidibeki.app`) resolve to a tenant via middleware. API routes also accept a `Host` header for tests.
- **Authentication backoffice**: Magic-link login with short-lived JWT access tokens + refresh tokens. No passwords stored.
- **Widget authentication**: Project public key + allowed-origin CORS check. No user-facing auth required.
- **File storage**: MinIO/S3-compatible object storage for screenshots, audio, video and replay sessions. Local filesystem fallback in dev via env var.
- **AI processing**: Google Gemini for audio transcription, text cleanup, sentiment analysis and moderation.
- **Email delivery**: Resend via async queue.
- **Queue**: Redis-backed Go workers for AI processing, email sending and Slack webhooks.
- **Payments**: Stripe Checkout + webhooks; 14-day free trial; plan limits enforced at runtime.
- **Backoffice UI**: Nuxt 3 (Vue) served per-tenant subdomain.
- **Deploy target**: Railway (Docker + Postgres + Redis + optional S3/MinIO).
- **Testing discipline**: TDD with ≥80% coverage. Go tests use `testcontainers-go` for Postgres integration tests. Widget uses Playwright E2E tests.

## Data model principles

All tenant data is stored in a single PostgreSQL database and isolated by `tenant_id`. For the MVP we avoid JSONB entirely: every attribute that matters is a typed column. Arrays and enums are allowed where the set of values is bounded and queryable. New features are added by extending tables with typed columns, not by stuffing blobs into JSONB.

## Key tables

- `tenants` — root company accounts with typed plan, status and billing columns.
- `tenant_members` — any actor that belongs to a tenant. Access is controlled by an explicit `permissions` text array, not coarse roles.
- `projects` — a site or app monitored by a tenant. Embed key and allowed domains are typed columns.
- `widget_rules` — per-page widget configuration. Trigger, form fields, replay sampling and theme are typed columns.
- `feedbacks` — a captured user response with typed rating, text, metadata, DOM snapshot and status.
- `attachments` — media linked to a feedback (screenshot, audio, video, replay) with typed transcription, sentiment and moderation flags.
- `sessions` — session replay bundles with typed start/end times and event counts.
- `issues` — grouping of feedbacks with typed public id and status workflow.
- `issue_updates` — status change history for an issue.
- `integrations` — generic external service connections (Slack, Teams, webhook). `kind`, `events[]` and config URL/channel are typed columns.
- `delivery_logs` — generic audit trail of every outgoing notification (email, Slack, webhook) with typed status and event.

## Plans & feature flags

All plan definitions live in `backend/internal/config/plans.go` so limits and features can be changed without touching business logic.

| Plan | Monthly | Yearly discount | Projects | Feedback/month | Users | Storage days | Notes |
|------|---------|-----------------|----------|----------------|-------|--------------|-------|
| starter | €4.99 | 7% | 1 | 500 | 1 | 30 | rating + text + screenshot |
| growth | €9.99 | 14% | 4 | 2,500 | 3 | 90 | + audio + DOM snapshot + limited replay + AI |
| scale | €27.99 | 14% | 10 | 10,000 | 10 | 365 | + video + unlimited replay + Slack + AI |
| enterprise | custom | custom | custom | custom | custom | custom | SLA + custom domain (future issue) |

---

## Phase 0: Foundations

**User stories covered**: As a developer, I can run the entire platform locally so that I can develop and test end-to-end.

### What to build

Set up the monorepo, local infrastructure (Postgres, MinIO, Redis), the Go module with chi, logging and environment config, the first database migrations, tenant subdomain middleware and a healthcheck endpoint with integration tests against a real Postgres container.

### Acceptance criteria

- [ ] `docker compose up` starts Postgres, MinIO and Redis.
- [ ] `go test ./...` runs unit and integration tests, including tests that spin up a Postgres container.
- [ ] `GET /healthz` returns `200 OK` with JSON `{ "status": "ok" }`.
- [ ] Middleware extracts a tenant from `tauview.fidibeki.app` style subdomains and rejects unknown or suspended tenants with `404` / `403`.
- [ ] CI runs tests, `go vet`, and builds the backend binary on every push.
- [ ] All code is committed with a clear history following TDD checkpoint commits.

---

## Phase 1: Multi-tenancy & Plans

**User stories covered**: As the platform, I can identify and limit each company account so that data stays isolated and plan features are enforced.

### What to build

Implement tenant management, plan configuration and runtime feature/limit checks. Build admin-level endpoints to create tenants and add seed data. Enforce that one tenant cannot access another tenant's resources.

### Acceptance criteria

- [ ] `POST /admin/tenants` creates a tenant with a unique subdomain slug and selected plan.
- [ ] Middleware injects the active tenant into request context for all routes under subdomains.
- [ ] Unknown subdomains return `404`; suspended tenants return `403`.
- [ ] `CanUseFeature(tenant, feature)` helper returns false when a feature is not in the tenant's plan.
- [ ] A tenant's plan limits (projects, users, feedback/month, storage days) are returned by a public config endpoint.
- [ ] Cross-tenant resource access is rejected with `403` in integration tests.

---

## Phase 2: Backoffice authentication

**User stories covered**: As a company user, I can log into the backoffice via magic link and invite teammates so that I can manage feedback securely.

### What to build

Magic-link registration/login flow for the first user of a tenant plus invitation flow for additional users. JWT access + refresh tokens. Permission-based access control (e.g. `project:create`, `project:delete`, `billing:manage`, `member:invite`). Each tenant member holds an explicit `permissions` array.

### Acceptance criteria

- [ ] `POST /auth/login` sends a magic link email to a backoffice user.
- [ ] `POST /auth/verify` validates the magic link token and issues access + refresh JWTs.
- [ ] `POST /auth/refresh` rotates the access token using a valid refresh token.
- [ ] Admins have full permissions; custom permission sets can be assigned to members.
- [ ] Backoffice routes require `tenant` context and relevant permissions.
- [ ] Refresh tokens are stored hashed and can be revoked.

---

## Phase 3: Projects & widget configuration

**User stories covered**: As a product team, I can create a project, configure the widget per page and get an embed script so that I can collect feedback on my site.

### What to build

CRUD for projects and their widget rules. Projects hold a public embed key and allowed domains. Widget rules are stored in a dedicated `widget_rules` table with typed columns for matcher, trigger, form fields, replay sampling and theme.

### Acceptance criteria

- [ ] Members with `project:create`/`project:update` can manage projects and widget rules.
- [ ] Each project has a unique public key used by the embed script.
- [ ] Widget rules can vary by URL pattern (e.g. `/assets/*` vs `/dashboard`).
- [ ] The embed script endpoint returns the correct JS URL for a given project key.
- [ ] Only requests from allowed domains can fetch widget config (CORS + origin validation).

---

## Phase 4: Widget embed

**User stories covered**: As an end user, I am asked for feedback at the right moment on a website so that I can report confusing or helpful experiences easily.

### What to build

Zero-dependency vanilla JS widget loaded via `<script>`. It detects the current page, fetches its config and triggers after configured delay/scroll/exit intent. UI appears bottom-left with greeting, star rating, text input and DOM element attachment. Captures metadata and DOM snapshot and sends feedback to the public API.

### Acceptance criteria

- [ ] Widget loads on allowed domains and is blocked on unknown domains.
- [ ] Trigger respects per-page config (delay, scroll depth, exit intent).
- [ ] End user can rate 1-5 stars and write a text review.
- [ ] End user can click a page element to attach its HTML + computed styles + screenshot.
- [ ] Widget sends OS, browser, viewport size, URL and referrer metadata.
- [ ] Playwright E2E test opens a sample page, triggers the widget and submits feedback.

---

## Phase 5: Feedback API

**User stories covered**: As the widget, I can submit feedback and media to the platform so that product teams see it in the backoffice.

### What to build

Public `POST /v1/feedback` endpoint authenticated by project public key. Stores a `feedbacks` row with rating, text, metadata and DOM snapshot. Screenshot/audio uploads create `attachments` rows linked to the feedback. Returns an optional issue public id and reporter token if the user opts into email updates.

### Acceptance criteria

- [ ] Valid project key and allowed origin are required.
- [ ] Monthly feedback count is checked against the tenant plan; exceeded limits return `429` with a clear error.
- [ ] Screenshot and audio files create `attachments` rows and are uploaded to object storage.
- [ ] Invalid payloads return `400` with structured validation errors.
- [ ] End user can optionally provide an email to receive issue updates.
- [ ] Integration tests cover happy path, invalid origin, plan limit and malformed body.

---

## Phase 6: AI processing (Gemini)

**User stories covered**: As a product team, I can see transcribed and moderated feedback so that I can triage faster.

### What to build

Async worker that sends audio/video to Gemini for transcription, cleans the text, analyses sentiment and flags offensive content. Results are stored in typed columns on the `attachments` row. AI features only enabled for plans with `AIProcessing=true`.

### Acceptance criteria

- [ ] Audio feedback is transcribed when plan allows AI.
- [ ] Text is cleaned (trim, filler removal) and sentiment score is stored.
- [ ] Offensive language is flagged and optionally hidden in the UI.
- [ ] AI jobs are queued via Redis and retried on transient failures.
- [ ] Failures are logged but never crash the feedback submission flow.

---

## Phase 7: Issues & email notifications (Resend)

**User stories covered**: As an end user, I can receive email updates about the issue I reported so that I know my feedback was acted on.

### What to build

Issues are typed rows in `issues`, grouped to feedbacks via `issue_id`. Public human-readable issue id, status workflow (`open`, `in_progress`, `resolved`). Email notifications to reporters when status changes are recorded in `delivery_logs` with actual delivery via Resend. Async queue for Resend.

### Acceptance criteria

- [ ] Each issue gets a public id like `TAU-2026-0001` based on tenant slug.
- [ ] Feedback can be linked to an existing issue or create a new one automatically.
- [ ] Reporter email is optional; if provided and opted-in, status updates trigger an email.
- [ ] Email sends and status updates are recorded in `delivery_logs`.
- [ ] Unsubscribe/update preferences can be managed via a signed URL.

---

## Phase 8: Session replay

**User stories covered**: As a product team, I can replay a user's session so that I can understand exactly what confused them.

### What to build

Lightweight client-side event recorder (DOM mutations, clicks, scrolls, inputs) configurable by sampling rate (0-100%). Storage of event stream + snapshots. Replay player in the backoffice.

### Acceptance criteria

- [ ] Replay recording respects tenant sampling config (0%, 1%, 100%, etc.).
- [ ] Recorded events reproduce the same visual sequence in the player.
- [ ] Replay is linked to a feedback item when both exist.
- [ ] Storage respects plan retention days.
- [ ] Feature is blocked for plans without `SessionReplay=true`.

---

## Phase 9: Slack integration

**User stories covered**: As a product team, I can receive feedback notifications in Slack so that my team reacts immediately.

### What to build

Integrations are stored in the generic `integrations` table with typed columns. Slack is one `kind`; other kinds (Teams, webhook) can be added by extending the `kind` enum. A worker reads events from the queue and dispatches to all active integrations subscribed to that event. Deliveries are audited in `delivery_logs`.

### Acceptance criteria

- [ ] Admins can save/remove a Slack integration and select event types.
- [ ] New feedback sends a Slack message when configured.
- [ ] Slack failures are retried and logged but do not block feedback flow.
- [ ] Feature only available for plans with `SlackNotifications=true`.

---

## Phase 10: Stripe billing

**User stories covered**: As a company, I can start a free trial, choose a plan and pay so that the platform bills me automatically.

### What to build

Stripe Checkout integration, 14-day trial, plan upgrades/downgrades, webhook handling for `checkout.session.completed`, `invoice.paid`, `customer.subscription.updated/deleted`. Automatic suspension when trial ends unpaid. Self-service customer portal link.

### Acceptance criteria
- [ ] New tenant starts a 14-day trial automatically.
- [ ] Stripe checkout creates a subscription and links it to the tenant.
- [ ] Webhooks update plan, status and trial end date.
- [ ] Unpaid trial converts to suspended; feedback API blocks suspended tenants.
- [ ] Downgrades are allowed but existing over-limit resources are read-only until within limit.
- [ ] Customer portal link is available for admins.

---

## Phase 11: Backoffice Nuxt UI

**User stories covered**: As a company user, I can manage projects, view feedback, triage issues and configure the widget in a branded backoffice.

### What to build

Nuxt 3 app served at tenant subdomains. Dashboard with metrics, project list, widget config editor, feedback list with filters, issue detail with status updates, replay player, integrations and Stripe settings.

### Acceptance criteria

- [ ] Login page per subdomain sends magic link.
- [ ] Dashboard shows feedback count, issue count and satisfaction trend.
- [ ] Members with correct permissions can manage projects and widget configuration.
- [ ] Feedback detail shows metadata, screenshot, transcript and sentiment.
- [ ] Issues can be created/updated/resolved and trigger emails.
- [ ] Playwright E2E tests cover login, project creation and feedback triage.

---

## Phase 12: Deploy & observability

**User stories covered**: As the platform, I run reliably in production with logs, CI/CD and staging parity.

### What to build

Multi-stage Dockerfiles for Go API and Nuxt app. Railway `railway.json` and environment templates. GitHub Actions deploy to staging on merge to `main`. Structured logs, basic error tracking and runbooks.

### Acceptance criteria

- [ ] `docker build` produces runnable API and web images.
- [ ] Railway deploys `main` to a staging environment.
- [ ] All env vars are documented in `docs/env.md`.
- [ ] Healthcheck and DB connectivity are verified on deploy.
- [ ] Runbook covers common failures: DB migrations, Stripe webhook, Redis queue.
