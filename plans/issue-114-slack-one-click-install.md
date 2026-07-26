# Plan: One-click Slack install, guided Teams setup (issue #114)

> Source: https://github.com/fidibeki/app.fidibeki/issues/114
> Scope: OAuth install for Slack, guided Workflows setup for Teams, webhook kind untouched.
> Non-goal: a Teams bot app, Slack Marketplace submission, any inbound Slack traffic.

## Shape

The whole point is that this is an *acquisition* change, not a delivery change. Slack's
`incoming-webhook` scope hands back a channel-scoped webhook URL, which is precisely what
`integrations.webhook_url` already holds. Everything past the install is untouched:

- `slackMessage()` (Block Kit), `notificationBody()`, the outbox, the Asynq delivery worker,
  retries, `delivery_logs`, and the privacy profile all stay as they are.
- Credentials encrypted with the existing `integration.Cipher` (AES-256-GCM under
  `INTEGRATION_ENCRYPTION_KEY`) — the pattern `issuetracker` established.
- Tenant isolation via the existing RLS policies on `integrations`.
- Plan gate reuses `config.FeatureSlack`; RBAC reuses `integration:*`.
- Install/uninstall land in `tenant_access_events` (Settings → Audit).

If the diff touches `delivery.go` beyond the Teams URL validation, something has gone wrong.

## The redirect-URI problem

Slack matches the redirect URL's host and port exactly against a URL registered on the app, over
HTTPS. Tenants are resolved from the subdomain (`tenant.SlugFromHost`,
`backend/internal/middleware/tenant.go:45`), so no tenant host can be registered.

One fixed callback on the apex API host; the tenant rides in a signed state:

```
POST /v1/integrations/slack/install     tenant-scoped, integration:create
  → mint signed state, return slack.com/oauth/v2/authorize?client_id=…&scope=incoming-webhook&state=…

GET  /v1/integrations/slack/callback    apex router, no tenant middleware
  → verify state → oauth.v2.access → write integration under the state's tenant
  → 302 https://<slug>.<BASE_DOMAIN>/settings?tab=integrations
```

The callback is the security-critical surface: it runs *outside* `TenantTransaction`, so it
resolves and scopes the tenant itself. State requirements, all testable:

- HMAC-signed, bound to tenant ID **and** the acting member ID
- Single-use, held in Redis, TTL of minutes not hours
- Forged / replayed / expired / cross-tenant → rejected and audited, never a partial write

Uninstall calls `auth.revoke` before deleting the row, so revoking in Fidibeki actually revokes in
Slack rather than orphaning a live webhook.

## Data model

No new table. `integrations` gains OAuth provenance, nullable so every existing row stays valid:

- `slack_team_id`, `slack_team_name`, `slack_channel_id` — what Slack returned, for display
- `slack_access_token_ciphertext` — the bot token, needed only for `auth.revoke`
- `install_source` enum `manual | oauth` (default `manual`) so the UI can tell a one-click install
  from a pasted URL, and so uninstall knows whether a revoke is possible

`channel` already exists and is Slack-only; the OAuth flow fills it from
`incoming_webhook.channel` instead of asking the user.

## Backend (`backend/internal/integration`)

- `slack_oauth.go` — state minting/verification, `oauth.v2.access` and `auth.revoke` clients
  behind an `HTTPDoer` seam so tests drive them against `httptest` (mirrors
  `issuetracker/provider.go`)
- `http.go` — the two new routes; install stays on `tenantRoutes`, callback registers on the apex
  router in `main.go` next to the other non-tenant routes
- `validate.go` — Teams host validation: accept Workflows hosts, reject retired connector hosts
  with a distinct field error the UI can map to migration copy. **Confirm the accepted host list
  against Microsoft's current documentation during implementation** — do not hardcode it from
  memory or from this plan.
- `store.go` — persist OAuth provenance; unchanged read path

Config: `SLACK_CLIENT_ID`, `SLACK_CLIENT_SECRET`, `SLACK_REDIRECT_URL`. Absent → one-click path
inactive, manual path unaffected, same posture as `SENTRY_DSN`.

## Frontend

`web/pages/settings/index.vue`, integrations section — the current single "type" dropdown becomes
three explicit choices:

- **Slack** — an *Add to Slack* button carrying our mark; opens the authorize URL; on return, the
  new integration is already in the list with its channel. Events and privacy fields are chosen
  after the install, on the created row, not before.
- **Microsoft Teams** — guided panel with the Workflows steps and a validated URL field; a retired
  connector URL gets migration copy, not a generic validation error.
- **Webhook** — unchanged.

Copy in `admin.en` / `admin.pt`; types in `web/types/api.ts`.

## Branding — done

It is the Fidibeki app: a customer installs Fidibeki into their Slack. Fidibeki logo, Fidibeki
name, same mark as the rest of the product.

The icon is built and committed:

- `branding/slack-app-icon.svg` — the master. The mark's `transform` and path `d` are copied
  verbatim from `web/public/brand/fidibeki-symbol.svg`, so the icon cannot drift from the brand
  symbol.
- `branding/slack-app-icon.png` — 512×512, opaque RGB, rendered from that SVG. This is the file
  uploaded to the Slack app dashboard.

Composition: white mark on brand teal `#087f73` (`--teal-fill`), full-bleed square with no baked-in
rounded corners — Slack masks app icons itself, and pre-rounding them double-rounds. The mark is
centred and sized to 64% of the icon on its widest axis (`viewBox="-243.75 -243.75 1687.5 1687.5"`,
derived from the symbol's measured 1080 × 1020.66 bounds centred on 600, 600). Verified legible
down to 24px on both light and dark backgrounds.

Still to do when the app is registered: app name **Fidibeki**, the product description, a link to
the marketing site, and the same mark on the *Add to Slack* button in Settings.

Not the tenant's workspace logo — `identity.WorkspaceLogoURL` and `WorkspaceMark.vue` belong to
the widget and appear nowhere in this work.

## Governance

`backend/internal/privacy/governance_registry.go:265` describes `slack_incoming_webhook` as
"a tenant-created integrations row with kind=slack". That is still true, but the destination is
now obtained through an OAuth grant rather than pasted, which changes the boundary description —
update the entry. `docs/env.md` gains the three new variables.

## Tests

- Go unit: state sign/verify/expire/replay; `oauth.v2.access` success, Slack-level `ok:false`,
  transport failure; Teams host validation both directions
- Go integration (testcontainers): install → callback → integration exists under the right tenant
  with the right channel; cross-tenant state rejected with no write; plan gating; RBAC; uninstall
  revokes then deletes
- Web unit: the three-way setup view model
- Existing integration tests must pass untouched — that is the evidence delivery didn't move

## Sequencing

1. Slack app created, distribution enabled, icon uploaded (blocks nothing else, but has a lead time)
2. State + OAuth client + tests
3. Routes and wiring
4. Settings UI
5. Teams guided setup and host validation
6. Governance registry, `docs/env.md`, README

Steps 2–4 are the critical path; step 5 is independent and can land first if #115 makes it urgent.
