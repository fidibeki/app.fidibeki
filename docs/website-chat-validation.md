# Tenant website chat validation

Scope: tenant dashboard → visitors on that tenant's own website, using the existing embed. Operator-first is the default; a project flag now permits visitors to start conversations. Platform backoffice support remains separate. The current release is covered by the [v0.4.0 launch audit](evidence/website-chat-launch-audit.md). The sections below preserve the earlier release history.

The isolated real-stack fixture uses PostgreSQL 16, Redis 7, the real Go API, Nuxt tenant dashboard, built widget assets and a separate storefront origin. It creates unique fixture accounts and cleans up by generated IDs. It never uses a production database.

The journey covers enabling chat, anonymous presence, first operator message, incoming preview, visitor reply, persisted reload, multi-tab presence, offline recovery, transcript download, existing feedback submission, desktop/mobile layout, browser reset and tenant deletion. Evidence:

- [Tenant dashboard, desktop](evidence/website-chat-dashboard-desktop.png)
- [Tenant dashboard, mobile](evidence/website-chat-dashboard-mobile.png)
- [Website visitor, desktop](evidence/website-chat-visitor-desktop.png)
- [Website visitor, mobile](evidence/website-chat-visitor-mobile.png)

Backend integration tests exercise authorization and forced RLS across tenants/projects/visitors, origin/key/credential denial, member permission changes, two-factor gates, API-key denial, expiry/revocation, reply-only behavior, idempotency conflicts, history pagination, unread markers, SSE updates, presence projection, rate limits, deletion and retention/legal holds. Additional regressions cover permission backfill, pre-transaction body buffering and continued cleanup of an exhausted backlog.

Browser regressions cover pre-mount logout reset, reset with configuration failure and navigation, feedback restoration after chat is disabled, preserving loaded inbox pages through refresh, and changing conversations while an older-history request remains pending.

Independent review: [two-round review report](https://github.com/fidibeki/app.fidibeki/blob/4a697c01bd61e51a1eac014f3ccf8c845fb226e5/.agents/meta/review-chain-report.md). All eight confirmed findings were resolved.

## Local results

- Real-stack journeys: 2 passed, including the existing platform support regression.
- Widget unit tests: 374 passed; existing coverage gates pass (86.52% statements).
- Widget Chromium browser tests: 75 passed. Three pre-existing Firefox attachment tests could not launch on this macOS host (`sandbox_extension_issue_file_to_process` / `RenderCompositorSWGL`); the complete Firefox suite remains enabled in Linux CI.
- Tenant web unit tests: 197 passed. New inbox/history regressions pass on desktop and mobile.
- Backoffice tests: 9 unit and 4 browser tests passed.
- Tenant web, widget and backoffice type checks pass; production builds for the changed web/widget surfaces pass.
- Go formatting, vet and privacy governance checks pass.

Backend package tests pass after updating the expected browser-key inventory for the new credential/reset keys. Combined coverage from the full run and corrected privacy-package rerun is 80.1% overall and 85.1% for website chat; the 80% overall and 60% package gates pass. The four web permission-editor assertions were updated for the added chat permission group and pass on desktop/mobile.

Linux CI passed all 78 widget browser tests, including Firefox, and both real-stack journeys. Its widget image build detected a missing `COPY shared shared`; the Dockerfile is corrected and the image now builds locally and serves `/v1/chat.js`. The edge CI gate explicitly checks that entry point.

## Final CI results

[PR #178](https://github.com/fidibeki/app.fidibeki/pull/178) merged as `4a697c01bd61e51a1eac014f3ccf8c845fb226e5`. Its tree is identical to the tested pull-request merge tree (`546e4b563dd17dc30fb774d41f66a5455f97eabe`). Every check passed:

- [Backend](https://github.com/fidibeki/app.fidibeki/actions/runs/36919465727): full integration suite, 80.1% overall coverage, 85.1% website-chat coverage, formatting/lint, Compose policy and API image.
- [Widget](https://github.com/fidibeki/app.fidibeki/actions/runs/36919465704): 374 unit tests, 78 browser tests across Chromium/Firefox, image build and edge routing including `/v1/chat.js`.
- [Tenant web](https://github.com/fidibeki/app.fidibeki/actions/runs/36919465780): 197 unit tests; browser suite reported 266 passed, 61 skipped and one existing login test that passed on its configured retry. Type checks and image build passed.
- [Backoffice](https://github.com/fidibeki/app.fidibeki/actions/runs/36919465881): regression checks passed.
- [Real-stack journeys](https://github.com/fidibeki/app.fidibeki/actions/runs/36919465694): tenant-to-website chat and platform support both passed.
- [Security](https://github.com/fidibeki/app.fidibeki/actions/runs/36919465931): configured vulnerability and secret checks passed.

## Production release

[Production promotion](https://github.com/fidibeki/app.fidibeki/actions/runs/36921072777) deployed the tested merged revision to API, worker, widget, backoffice and tenant web, then tagged `v0.3.0` on 1 October 2026. The backoffice had already auto-deployed the same revision, so Railway skipped its identical promotion build. API database/Redis/storage readiness and backoffice health passed, and the public widget served `/v1/chat.js` successfully.

## Integrated feedback and mobile refinement

The follow-up removes the nested feedback frame, duplicate branding/close button and nested scrolling. Chat adopts the configured feedback appearance and live theme changes. Feedback confirmation stays visible after submission; the chat reset action stays in Messages. The mobile launcher is a 52px button above a 64px navigation allowance plus the device safe area, with a documented offset override. The expanded panel follows the visible viewport and hides the launcher.

Validation: 374 widget unit tests, 77 Chromium browser tests, type checking and production build passed. Both isolated real-stack journeys passed again. Added browser coverage checks light/dark surfaces, a single close control and scroll area, theme changes, successful feedback submission, persistent confirmation, bottom-navigation clearance, custom placement offset, and a 320×420 viewport. Software-keyboard placement uses the Visual Viewport API; the reduced-height browser test does not emulate a physical iOS keyboard.

- [Integrated feedback, dark desktop](evidence/website-chat-integrated-dark-desktop.png)
- [Integrated feedback, dark mobile](evidence/website-chat-integrated-dark-mobile.png)
- [Compact launcher above mobile navigation](evidence/website-chat-launcher-dark-mobile.png)
- [Integrated feedback, light desktop](evidence/website-chat-integrated-light-desktop.png)
- [Integrated feedback, light mobile](evidence/website-chat-integrated-light-mobile.png)

The design detector reported the intentionally hidden, configuration-populated logo and the pre-existing standalone widget accent border. The embedded feedback presentation removes that border. Rendered light/dark and desktop/mobile evidence was inspected directly.

## Content-sized feedback follow-up

Feedback now uses its natural content height instead of the chat transcript's 620px height. It can grow to the available viewport, including the area previously reserved for the launcher, and contracts when submission replaces the form with its confirmation. The feedback form has no inner scrolling region. Viewports shorter than the complete form use scrolling on the whole panel as an accessibility fallback.

Browser regression coverage uses a form with rating, written feedback, page-element/audio/screenshot attachments and an AI-processing notice. It checks growth when the visitor resizes the textarea, no overflow when the form fits on desktop or mobile, access to submission at 320×420, contraction after submission and restoration of the message view's height.

- [Expanded form on desktop](evidence/website-chat-content-height-desktop.png)
- [Complete form on mobile](evidence/website-chat-content-height-mobile.png)

## Existing Linux test fixture correction

The full backend CI gate twice failed `TestFFmpegExtractPosterSkipsTheFirstSecond` with `ETXTBSY` while every website-chat test passed. A Linux/ARM64 container reproduced 22 failures across 200 repeated FFmpeg test groups. The five tests wrote executable shell scripts and invoked them in parallel, matching the [Go write/exec descriptor race](https://github.com/golang/go/issues/22315). Those five fixture consumers now run serially; all argument/error assertions remain intact, with no production-code retry or skip. The corrected group passed 1,000 Linux repetitions and the complete backend CI gate.
