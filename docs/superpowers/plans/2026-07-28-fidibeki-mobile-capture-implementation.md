# Fidibeki Mobile Capture Implementation Plan

> **Scope mode:** LOCKED
>
> **Design:** `docs/superpowers/specs/2026-07-28-fidibeki-mobile-capture-design.md`
>
> **Issue:** #136
>
> **Boundary:** local and staging-only product work. No production, DNS, provider, or
> infrastructure changes.

## Task 1: Prove The Persistence And Permission Boundary

Start with failing migration and authorization tests. Add a constrained Feedback source/context
projection, hashed short-lived mobile capture sessions with tenant-scoped RLS and cleanup indexes,
and `mobile_capture:create`. Backfill every existing Feedback to `web`. Prove up/down/up
compatibility, invalid-value rejection, API-key permission subset behavior, cross-tenant
isolation, and that no raw capture token is stored.

**Acceptance:** PostgreSQL integration tests prove the constraints, backfill, RLS, token lifecycle,
owner/admin permission backfill plus authorization-version invalidation, and permission model before
an HTTP route can use them. Existing non-admin members do not gain the permission.

**Status:** Completed locally on 2026-07-28. Migration 130, permission backfill,
hash-only storage, composite tenant/resource FKs, forced RLS, and bounded cleanup are covered by
PostgreSQL migration tests.

## Task 2: Mint One Bounded Capture Session

Add an API-key-only `POST /v1/mobile/capture-sessions` route. Validate project, active definition,
platform, application ID, normalized screen, optional allowlisted HTTPS URL, and requested display
locale. Return one opaque token and a public-safe resolved definition/contact notice. Bind the
issuer API key, exact resolved display locale, and definition/controller versions; enforce tenant
lifecycle, writable state, `mobile_capture:create`, existing API-key rate limits, and neutral
tenant/resource boundaries.

**Acceptance:** Unit and PostgreSQL HTTP tests cover valid iOS/Android sessions, strict input,
inactive/deleting/expired lifecycle states, inactive or non-launcher definitions, allowlisted
context URLs, tenant isolation, token entropy and hash-only persistence, and exact public-safe
configuration projection. Human bearer auth is rejected; deleting or expiring the issuer API key
immediately revokes the mobile bearer. Definition pause/edit and controller notice changes produce
the typed configuration conflict. Consent must use the session's display locale; a separate device
locale cannot become notice provenance, and app-language changes require a new reviewed grant.

**Status:** Completed locally on 2026-07-28. The API-key-only mint route resolves one public-safe
localized launcher configuration and binds the issuer, context, definition, and controller
versions into a short-lived session.

## Task 3: Submit Feedback Through The Existing Domain Pipeline

Extract the authorization-independent core of the current public feedback handler rather than
copying store, plan-limit, idempotency, outbox, attachment, or AI behavior. Add
`POST /v1/mobile/feedback` and the matching attachment completion route around that core. Bind
source/context to the locked capture session, permit one client feedback ID with exact retries, and
retain existing reporter-consent and screenshot verification behavior with server-assigned
`mobile_submission` consent provenance.

**Acceptance:** Focused and concurrency tests prove one Feedback/no Issue, exact idempotency,
different-ID rejection, expiry/revocation, plan and abuse limits, reporter consent, presigned
screenshot completion, partial attachment failure semantics, and inactive-tenant races.
The tests distinguish the first-submit deadline from bound retry and per-attachment completion
deadlines, exercise the minimal privileged hash resolver followed by FORCE-RLS tenant revalidation,
and reject unknown or duplicate JSON keys recursively. First submission and binding are atomic with
a canonical request digest; collisions with another mobile session or web submission return `409`
without exposing or rotating the existing attachment grants, under one documented lock order.
Definition/controller mutation invalidates only an unbound first submission; pausing or editing a
definition after acceptance cannot strand the session's already-bound exact retry or attachment
completion through its independent deadline.

**Status:** Completed locally on 2026-07-28. Mobile submission and screenshot completion reuse the
shared Feedback pipeline, with atomic binding, exact retries, lifecycle/configuration revocation,
abuse/quota enforcement, strict JSON, and mobile/web collision tests.

## Task 4: Make Mobile Feedback Operational In The Inbox

Carry source and mobile context through tenant list/detail APIs, source filtering, activity and
integration delivery. Update the Dispatch inbox and detail view with a compact source badge, screen
label, and safe conditional context link. Add English and Portuguese copy without changing web
feedback behavior.

**Acceptance:** Backend and responsive web tests show web/iOS/Android records, exact filtering,
screen context, safe URL omission, and additive Slack/Teams/webhook payload fields with no fake
page link.

**Status:** Completed locally on 2026-07-28. Feedback list/detail APIs expose the constrained
source and mobile context projection, exact source filters are supported, and the Dispatch inbox
renders compact web/iOS/Android context. Slack, Teams, and generic webhook deliveries include
additive source/context fields only when the linked Feedback and disclosure policy permit them;
unsafe URLs and fabricated page actions are omitted.

## Task 5: Ship The React Native SDK Slice

Add `@fidibeki/mobile` to the workspace. Implement the typed session client, form state,
idempotent submission/upload client, storage/recurrence engine, provider, launcher, modal sheet,
hook, AsyncStorage adapter, and explicit host-owned screenshot provider. Render all supported
response blocks and optional reporter consent through native React Native controls; do not use a
WebView, request camera/photo-library permission automatically, or collect analytics/device IDs.

**Acceptance:** Unit/component tests exercise iOS and Android, response blocks, localization,
validation, recurrence, exact retry, attachment outcomes, accessibility, safe areas, keyboard
layout, reduced motion, and forbidden network/data behavior. Typecheck and package builds pass.
Session recurrence tests cover provider remount, brief backgrounding, the 30-minute foreground
session reset, and cold restart. The example app also compiles, launches, and completes a runtime
smoke on an iOS simulator and an Android emulator or equivalent device CI; JS-only mocks are
insufficient. Screenshot tests cover host cancellation, permission/provider error, URI
non-disclosure, invalid MIME/size/read, direct upload, and deterministic cleanup.

**Status:** Completed on 2026-07-29. The package, example application, unit/component coverage,
typechecks, ESM/CJS/type builds, dry-run package, Expo native generation, Android debug/release
builds, and Pixel emulator staging submission/upload/cold-restart smoke are green. The Android
release manifest has no camera, photo-library, or storage permissions. Xcode 26.6 regenerated and
compiled the release example for an iPhone 17 Pro simulator running iOS 26.5. The native iOS local
smoke acquired a host-minted session, exercised the modal, keyboard and safe-area layout, submitted
the configured responses, uploaded and completed a screenshot, rendered the success state, and
retained `never` recurrence across a cold restart with an absent launcher. The final SDK hardening
also covers draft isolation and reconciliation, stale-session invalidation, host re-render
stability, cooldown wake-up, native safe-area placement, automatic appearance, URL validation,
PII-free bounded idempotency evidence, concurrent retry coalescing, malformed response recovery,
and corrupt recurrence storage.

## Task 6: Document, Audit, Validate, And Publish To Staging

Document the customer-backend token exchange and explicit private-key prohibition. Update the
privacy data map, retention registry, API docs, roadmap TODO, and example integration. Run
format/diff checks; migration replay; full backend coverage/race/vet/build/vulnerability gates;
mobile unit/type/build; web unit/type/build/full responsive Playwright; unchanged widget gates; and
an independent security/correctness review.

After a clean review, integrate to `main`, push, wait for CI, deploy only affected services to
staging, and execute disposable-key submissions from the iOS-simulator and Android example-app
runtimes with one completed screenshot. Confirm inbox visibility, source/filter/context, no Issue
creation, readiness, and clean logs. Record exact evidence on #136 and close it only when the full
first-slice contract is proven.

**Acceptance:** Local and CI gates are green, staging evidence covers both platforms, the issue
comment links commits/tests/deployments, and no production action occurred.

**Status:** Completed on 2026-07-29. Documentation, privacy/retention registration, backend
hardening, mobile source/context delivery, Android runtime evidence, and an independent backend
and SDK security/correctness re-review are complete. Local repository gates pass, including the
full backend coverage threshold, focused mobile-capture race test, 62 mobile tests with 84.77%
statement coverage, package/example builds, Android debug/release builds, and the release
permission audit. The implementation is pushed to `main` through `05646cf`, and Backend, Web,
Mobile, Security, Widget, Backoffice, Marketing, and Release Artifacts CI are green. Staging
deployments from that source are healthy: API `44b2627d-b8d7-4d0b-95b7-a136f5241c2a`, worker
`85c5b98d-007b-4516-b1bc-0546257afa02`, and web
`c05b138a-e23f-4ad4-b06b-52a5e9b20b88`.

The public staging contract accepted `fb_BUNRresU` with a completed safe screenshot and no Issue.
The release example running in a Pixel emulator then accepted `fb_zvVsKR3O` through the native UI
with Android source, rating 5, normalized `/checkout/review` screen, a 39,754-byte safe completed
screenshot, and no Issue. A cold restart retained the 30-day cooldown, reported `ineligible`, and
did not render the launcher. The disposable least-privilege API key and its capture sessions were
deleted after the smoke, and the existing staging rule's attachment setting was restored.

The iPhone 17 Pro release example then submitted `fb_5a9Q2hAH` through the native UI against
staging. The persisted result has iOS source, application `com.fidibeki.mobileexample`, normalized
`/checkout/review` screen, app version `0.1.0`, OS version `26.5`, rating 5, a 142,907-byte
completed and safe PNG screenshot, and no Issue. The tenant inbox API returned that exact item
under the combined `source=ios`, `issue=unlinked`, and `attachment=screenshot` filters. A cold
restart retained the 30-day cooldown, reported `ineligible`, hid the launcher, and prevented the
explicit host button from reopening the form. Staging worker deployment
`24f35f95-5b43-45fd-9f69-0e491b48ab7e` processed the queued attachment after the sleeping worker
was restarted. API and worker error logs were clean. The disposable capture and read-only inbox
keys and their capture sessions were deleted, the staging presentation was restored to `prompt`,
and no production action occurred.
