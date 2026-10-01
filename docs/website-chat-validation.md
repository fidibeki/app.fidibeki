# Tenant website chat validation

Scope: tenant dashboard → visitors on that tenant's own website, using the existing embed. Operator starts each conversation; visitors can only reply. Platform backoffice support remains separate.

The isolated real-stack fixture uses PostgreSQL 16, Redis 7, the real Go API, Nuxt tenant dashboard, built widget assets and a separate storefront origin. It creates unique fixture accounts and cleans up by generated IDs. It never uses a production database.

The journey covers enabling chat, anonymous presence, first operator message, incoming preview, visitor reply, persisted reload, multi-tab presence, offline recovery, transcript download, existing feedback submission, desktop/mobile layout, browser reset and tenant deletion. Evidence:

- [Tenant dashboard, desktop](evidence/website-chat-dashboard-desktop.png)
- [Tenant dashboard, mobile](evidence/website-chat-dashboard-mobile.png)
- [Website visitor, desktop](evidence/website-chat-visitor-desktop.png)
- [Website visitor, mobile](evidence/website-chat-visitor-mobile.png)

Backend integration tests exercise authorization and forced RLS across tenants/projects/visitors, origin/key/credential denial, member permission changes, two-factor gates, API-key denial, expiry/revocation, reply-only behavior, idempotency conflicts, history pagination, unread markers, SSE updates, presence projection, rate limits, deletion and retention/legal holds. Additional regressions cover permission backfill, pre-transaction body buffering and continued cleanup of an exhausted backlog.

Browser regressions cover pre-mount logout reset, reset with configuration failure and navigation, feedback restoration after chat is disabled, preserving loaded inbox pages through refresh, and changing conversations while an older-history request remains pending.

Independent review: [two-round review report](../.agents/meta/review-chain-report.md). All eight confirmed findings were resolved.

## Local results

- Real-stack journeys: 2 passed, including the existing platform support regression.
- Widget unit tests: 374 passed; existing coverage gates pass (86.52% statements).
- Widget Chromium browser tests: 75 passed. Three pre-existing Firefox attachment tests could not launch on this macOS host (`sandbox_extension_issue_file_to_process` / `RenderCompositorSWGL`); the complete Firefox suite remains enabled in Linux CI.
- Tenant web unit tests: 197 passed. New inbox/history regressions pass on desktop and mobile.
- Backoffice unit tests: 9 passed.
- Tenant web, widget and backoffice type checks pass; production builds for the changed web/widget surfaces pass.
- Go formatting, vet and privacy governance checks pass.

The pull request's required CI checks run the full backend coverage gates, both browser engines, tenant web/backoffice regressions and the isolated real-stack journeys against the committed artifact.
