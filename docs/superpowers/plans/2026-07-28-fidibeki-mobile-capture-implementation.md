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

**Status:** Pending.

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

**Status:** Pending.

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

**Status:** Pending.

## Task 4: Make Mobile Feedback Operational In The Inbox

Carry source and mobile context through tenant list/detail APIs, source filtering, activity and
integration delivery. Update the Dispatch inbox and detail view with a compact source badge, screen
label, and safe conditional context link. Add English and Portuguese copy without changing web
feedback behavior.

**Acceptance:** Backend and responsive web tests show web/iOS/Android records, exact filtering,
screen context, safe URL omission, and additive Slack/Teams/webhook payload fields with no fake
page link.

**Status:** Pending.

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

**Status:** Pending.

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

**Status:** Pending.
