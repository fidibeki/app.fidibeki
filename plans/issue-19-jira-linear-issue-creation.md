# Plan: Create Jira and Linear issues when enabled (issue #19)

> Source: https://github.com/acidiney/app.fidibeki/issues/19
> Scope: one-way creation of an external Jira/Linear issue from a Fidibeki Issue, opt-in per
> workspace. No bidirectional sync, no comment/attachment/status mirroring.

## Shape

Reuse the established patterns instead of inventing new ones:

- Credentials are encrypted at rest with the existing `integration.Cipher`
  (AES-256-GCM under `INTEGRATION_ENCRYPTION_KEY`), never returned to the browser.
- Tenant isolation via RLS policies identical to `integrations`.
- Config changes land in the `tenant_access_events` ledger (Settings → Audit tab).
- Creation attempts land in `issue_activities` (the issue timeline).
- Plan gate reuses `config.FeatureSlack` (the existing "integrations" entitlement).
- RBAC reuses `integration:*` for configuration and `issue:update` for creation.

## Data model (migration `000111_add_issue_tracker_integrations`)

`issue_tracker_connections` — one row per (tenant, provider):

- `provider` enum `jira | linear`, `site_url` (Jira cloud site), `account_label`
- `credential_ciphertext bytea`, `credential_fingerprint text` (never exposed)
- destination: `destination_id/key/name` (Jira project / Linear team),
  `issue_type_id/name` (Jira), `sub_destination_id/name` (optional Linear project)
- `is_enabled boolean NOT NULL DEFAULT false` — opt-in, off by default
- `validated_at`, `last_error`
- `UNIQUE (tenant_id, provider)`, enable requires a validated destination

`issue_external_links` — one row per creation attempt:

- `issue_id`, `provider`, `status` enum `pending | created | failed`
- `idempotency_key` with `UNIQUE (tenant_id, idempotency_key)`
- request snapshot (title/description/priority/destination) reviewed by the user
- result: `external_id`, `external_key`, `external_url`, `last_error`, `attempt_count`
- partial `UNIQUE (tenant_id, issue_id, provider) WHERE status <> 'failed'` so a
  retry or double-submit can never produce a second external issue

## Backend (`backend/internal/issuetracker`)

- `models.go` — providers, connection, link, inputs, sentinel errors
- `provider.go` — `Client` interface (`Validate`, `Destinations`, `CreateIssue`),
  `ProviderError` carrying an actionable, non-secret message + retryable flag
- `jira.go` — Jira Cloud REST v3 (`/myself`, `/project/search`,
  `/issue/createmeta/{key}/issuetypes`, `POST /issue` with ADF description), Basic auth
- `linear.go` — Linear GraphQL (`viewer`, `teams`, `issueCreate`), API-key auth
- `store.go` — connection upsert/configure/enable/delete, link insert/complete/fail
- `service.go` — creation orchestration: persist `pending` row, call the provider
  **outside** the transaction, then mark `created`/`failed`. A provider failure never
  touches the Fidibeki issue.
- `http.go` — routes:
  - `GET    /v1/issue-trackers` (integration:read)
  - `PUT    /v1/issue-trackers/{provider}` connect + live validation (integration:create)
  - `GET    /v1/issue-trackers/{provider}/destinations` (integration:update)
  - `PATCH  /v1/issue-trackers/{provider}` destination + enable/disable (integration:update)
  - `DELETE /v1/issue-trackers/{provider}` (integration:delete)
  - `GET    /v1/issues/{issueID}/external` (issue:read)
  - `POST   /v1/issues/{issueID}/external` (issue:update, writable)
  - `POST   /v1/issues/{issueID}/external/{linkID}/retry` (issue:update, writable)

## Frontend

- `web/components/settings/IssueTrackers.vue` in the Settings → Integrations tab:
  connect, validate, pick destination, enable/disable, disconnect; secrets are
  write-only and shown as a fingerprint afterwards.
- Issue detail (`web/pages/issues/[id].vue`): an "External tracker" panel that only
  renders for enabled providers, opens a review dialog (title, description, priority,
  destination) before confirming, then shows the external key as a direct link, or the
  error plus a retry button.
- Types in `web/types/api.ts`; copy in `admin.en/pt` + `en/pt` catalogs; new activity
  kinds in `web/utils/issue-activity.ts`.

## Tests

- Go unit: provider contracts against `httptest` fakes (Jira + Linear success, auth
  failure, destination missing, provider 5xx), priority mapping, validation.
- Go integration (testcontainers): connect → configure → enable → create → duplicate
  request returns the same link → forced failure keeps the issue and allows retry;
  RBAC and plan gating; disabled provider is invisible.
- Web unit: external-tracker view model (enabled providers, status copy).
- Docs: README section + `docs/env.md` if new env is needed (none expected).

## Follow-up: automatic creation (requested during review)

`issue_tracker_connections.auto_create` (migration 000112) turns the manual, confirm-first flow
into an automatic one for workspaces that want every Fidibeki issue mirrored. It reuses the
existing outbox rather than adding a new trigger:

- `issuetracker.OutboxResolver` answers `issue.created` events with one destination per opted-in
  provider, so the deterministic outbox task ID gives redelivery protection for free.
- `issuetracker.AutoCreateHandler` runs on the domain queue, applies the same privacy restriction
  check as integration delivery, and reuses the single live-attempt slot per issue and provider.
- The flag can only be armed on an enabled provider, and disabling a provider clears it.
