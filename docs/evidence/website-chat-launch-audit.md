# Website chat launch audit

Verified on 1 October 2026 against release `v0.4.0`, commit `a8aadfd861aedfcef310f7e5d1dfb02c02d90e96`. [PR #182](https://github.com/fidibeki/app.fidibeki/pull/182) is merged. Its tested merge tree and the released tree are identical: `f57db7d4c35844c5951d7a06caa53b35c1f878b3`.

The final scope is tenant-to-own-website conversations. First-party platform support remains a separate feature and passes its regression journey. The later user request supersedes the original reply-only requirement: each project now has a default-off switch that lets visitors send the first message.

## Requirement evidence

| Requirement | Current evidence |
|---|---|
| Tenant sees visitors to its own website in real time | Real-stack browser test enables chat, loads a separate storefront origin, observes one visitor and opens that visitor's conversation. Backend tests cover active/idle presence, privacy-filtered page locations, expiry and tenant/project isolation. |
| Tenant sends a custom message and visitor replies immediately | Real-stack test sends an operator message, opens the incoming preview, replies from the storefront, and observes the reply in the dashboard without reload. |
| Optional visitor initiation controlled in tenant Conversations | Real-stack test switches the project flag on, observes the composer appear in the already-open widget, sends the first visitor message and observes it in Inbox. Backend test asserts the new unread conversation and idempotent first-message retry. |
| Default off, per-project policy, independent from chat enabled | Migration 140 defines `NOT NULL DEFAULT false`. Backend tests cover partial updates and retained preference when chat is disabled. Desktop/mobile dashboard tests cover saved state across reload and independent project switching. |
| Switching off blocks new starts but preserves existing replies | Real-stack and backend tests cover continued replies in an existing conversation, a fresh browser session with no composer and direct API rejection of a new first message. |
| Visitors can leave a message while the team is away | The first-message authorization depends on the project flag and visitor credential, with no operator-presence requirement. Backend integration accepts the first message without registering operator presence. The widget explains that replies may come later. |
| Permissions and failure handling | Settings require `project:update`; read-only saves and cross-project access are denied. Dashboard failed-save test preserves the prior switch value. Widget test retains a draft when flag-off races a first send and restores it when the flag is enabled again. |
| Durable history, reconnect, multi-tab presence, unread/read, paging and download | Real-stack journey covers reload, offline recovery, one visitor across two tabs and a complete three-message transcript. Backend and shared-client tests cover read markers, pagination, deduplication and bounded export. |
| Feedback stays integrated in the chat panel | Real-stack journey submits feedback and confirms exactly one database record. Widget browser tests cover shared branding, one close control, light/dark themes, confirmation and restored standalone feedback after chat is disabled. |
| Mobile layout and content-sized feedback | Browser regressions cover a compact 52px launcher, bottom navigation clearance, viewport changes, whole-form sizing and growth, submission access at 320×420 and confirmation shrinkage. Desktop/mobile screenshots were inspected. Forms fitting the viewport have no internal scroll; oversized forms retain whole-panel scrolling for access. |
| Centered Conversations empty state | Released Conversations CSS centers the icon with `margin: 0 auto 16px`; responsive dashboard evidence records the current layout. |
| Widget loads on the actual mobile host | Tauview [PR #381](https://github.com/acidiney/tauview.app/pull/381) is merged as `e9f0b9c50f3b38ff2b49653e41b83126337d6af3`. Railway serves that exact commit successfully. The final live browser check below uses the real public site and visitor API. |
| Isolation, privacy and lifecycle | Backend integration covers forced RLS, separate tenants/projects/visitors, origin/key/credential binding, revoked permissions, API-key denial, two-factor gates, expiry, limits, retention and legal holds. Browser regressions cover logout/reset before mounting, failed configuration and navigation. Cleanup drains bounded batches. |
| Schema, review and release procedure | Full migration replay and up/down/up pass. Independent security/performance/correctness review is recorded below. The runbook specifies API-before-widget-before-web deployment and application rollback without destructive down migrations. |

## Checks on the released code

- [Backend CI](https://github.com/fidibeki/app.fidibeki/actions/runs/36930015783): all packages, 80.1% internal statement coverage, 85.1% website-chat coverage, unchanged coverage floors, formatting/lint, Compose policy and API image passed. Local website-chat and database integration tests also passed with the race detector.
- [Widget CI](https://github.com/fidibeki/app.fidibeki/actions/runs/36930015785): 374 unit tests and 82 Chromium/Firefox browser tests passed, together with type checks, bundle, image and edge-routing checks.
- [Tenant web CI](https://github.com/fidibeki/app.fidibeki/actions/runs/36931411733) on the merged commit: 197 unit tests, 269 browser tests and 61 existing conditional skips; type checks and image passed. The preceding PR run had one unrelated test pass on retry; the merged run had no retries reported.
- [Real-stack CI](https://github.com/fidibeki/app.fidibeki/actions/runs/36931411777) on the merged commit: both tenant website chat and platform support journeys passed against real PostgreSQL, Redis, Go, Nuxt and built widget assets.
- [Backoffice CI](https://github.com/fidibeki/app.fidibeki/actions/runs/36931411975), [security CI](https://github.com/fidibeki/app.fidibeki/actions/runs/36931411747) and [release artifacts](https://github.com/fidibeki/app.fidibeki/actions/runs/36931411811) passed on the merged commit.

Independent review covered security, correctness and performance. The visitor-initiation review found one stale migration-version assertion, fixed from 139 to 140 and verified by a separate reviewer. No findings remain unresolved. The preceding full website-chat review resolved eight findings in two rounds.

- [Visitor-initiation review](https://github.com/fidibeki/app.fidibeki/blob/a8aadfd861aedfcef310f7e5d1dfb02c02d90e96/.agents/meta/review-chain-report.md)
- [Full website-chat review](https://github.com/fidibeki/app.fidibeki/blob/4a697c01bd61e51a1eac014f3ccf8c845fb226e5/.agents/meta/review-chain-report.md)
- [Original platform-support launch and production smoke](live-support-production.md)

## Production verification

The API, widget and tenant dashboard were promoted sequentially from the tested commit. Each workflow and Railway deployment succeeded:

| Service | Workflow | Railway deployment |
|---|---|---|
| API and migration 140 | [36931429114](https://github.com/fidibeki/app.fidibeki/actions/runs/36931429114) | `97476c66-5a81-4eb6-87a7-d37e605359cd` |
| Widget | [36931754203](https://github.com/fidibeki/app.fidibeki/actions/runs/36931754203) | `ee1bc5ab-0971-4d46-9a87-878274736ded` |
| Tenant web and v0.4.0 tag | [36931919886](https://github.com/fidibeki/app.fidibeki/actions/runs/36931919886) | `8603ccad-0e5d-4469-acb4-be08cf659293` |

API readiness reports database, Redis and storage healthy; backoffice health also passes. The public widget serves `chat-GLXPMAYY.js` with visitor initiation, and the tenant dashboard serves the flag control and its CSS. A fresh public Tauview visitor request returned the boolean flag and an empty conversation. The flag was false immediately after release and true at the final audit, reflecting the tenant's current setting; the audit did not change it.

The final production browser check used a fresh anonymous 390×844 mobile context on `https://tauview.app/en`, real published SDK assets and the real visitor API, with no mocked chat requests. It verified:

- No widget loader before a consent decision.
- A visible 52×52 launcher at x326/y716 after rejecting optional tracking.
- The empty **Send us a message** view and visible **Your message** composer.
- A disabled send button for an empty draft.
- One loader and one visitor session after desktop/mobile resizing.

No message was sent. The temporary visitor credential was revoked with HTTP 204 after the browser closed. The screenshot was inspected directly. Tauview's current frontend deployment is `419d56af-f12f-420a-a0c5-377a21c560dc`, `SUCCESS`, on merged mobile-fix commit `e9f0b9c50f3b38ff2b49653e41b83126337d6af3`.

- [Live Tauview mobile composer](website-chat-production-mobile.png)
- [Tenant flag on mobile](website-chat-incoming-setting-mobile.png)
- [Visitor-first flow on mobile](website-chat-incoming-visitor-mobile.png)

## Reproduction and limits

Run `pnpm test:live-support` for both isolated real-stack journeys. Backend tests live in `backend/internal/websitechat`; dashboard regressions in `web/tests/e2e/website-chat.spec.ts`; widget regressions in `widget/tests/e2e/widget.spec.ts`. The current behavior, retention, integration and rollback are documented in [Website conversations](../website-chat.md).

No capacity benchmark or physical iOS keyboard test was performed. Automated viewport and keyboard-inset behavior, Linux Chromium/Firefox, and the live mobile-width browser check cover the implemented launch scope. Staging remains dormant; launch verification uses the isolated full stack and production checks described above.
