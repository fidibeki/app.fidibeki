---
skill: task-breakdown
version: 1
date: 2026-07-23
status: active
scope: locked
source: docs/superpowers/specs/2026-07-23-public-submission-abuse-protection-design.md
---

# Public Submission Abuse Protection Tasks

## Shared Context

- Extend existing public-key/origin authorization, Redis GCRA, tenant transactions, Asynq queues,
  storage, moderation, and Settings patterns.
- Keep the current API error envelope compatible; new reason and retry fields are additive.
- Use HMAC-SHA-256 for short-lived limiter identities. Persist no raw network, origin, visitor, page,
  or digest data in diagnostics.
- Keep feedback text when optional upload, replay, diagnostics, or AI work fails.
- Keep AI `processing_status` separate from attachment/replay `safety_status`.
- Staging only. No production deployment or infrastructure changes.

## Task 1: Stable Public Decisions

**Depends on:** None
**Status:** Complete

**Outcome:** The API can serialize a stable abuse reason and retry delay without changing existing
error consumers.

**Why:** Every protected public endpoint and the widget need one compatible failure contract.

**Acceptance:** `go test ./internal/api/...` proves old envelopes remain unchanged when optional
fields are absent and new fields serialize when present.

**Autonomy:** AFK

## Task 2: Privacy-Preserving Policy Evaluator

**Depends on:** Task 1
**Status:** Complete

**Outcome:** One abuse service derives rotating keyed buckets and evaluates named multi-scope
policies using the existing limiter.

**Why:** Feedback, media, replay, and AI must not invent divergent identities or decisions.

**Acceptance:** `go test ./internal/abuse/...` proves deterministic/current-previous key handling,
scope composition, denial precedence, retry calculation, and limiter failure classification.

**Autonomy:** AFK

## Task 3: Aggregate Diagnostic Ledger

**Depends on:** Task 2
**Status:** Complete

**Outcome:** Tenant/project hourly abuse categories are stored and retained without visitor
identifiers.

**Why:** Operators need pressure and failure visibility without product analytics fingerprinting.

**Acceptance:** The database migration and `internal/abuse` integration suite prove RLS isolation,
atomic concurrent aggregation, bounded queries, and 90-day cleanup.

**Autonomy:** AFK

## Task 4: Feedback Bounds And Coordinated Limits

**Depends on:** Tasks 2 and 3
**Status:** Complete

**Outcome:** Feedback submission enforces project/origin/network/visitor policies and bounded
attachment declarations while preserving honeypot, idempotency, and text intake.

**Why:** The highest-volume public path must reject abuse before database, storage, or AI cost.

**Acceptance:** Feedback HTTP integration tests prove authorization order, four-file/125-MiB bounds,
duplicate rejection, stable retry responses, distributed decisions, and text acceptance during
limiter failure.

**Autonomy:** AFK

## Task 5: Tenant Diagnostics API

**Depends on:** Task 3
**Status:** Complete

**Outcome:** Authorized members can read bounded submission-health totals and hourly series.

**Why:** The aggregate ledger is useful only through a tenant-isolated operational contract.

**Acceptance:** Abuse HTTP integration tests prove `tenant:read`, date/project bounds, zero-state,
aggregation, and cross-tenant denial.

**Autonomy:** AFK

## Task 6: Attachment Safety Schema

**Depends on:** Task 4
**Status:** Complete

**Outcome:** Attachments have an explicit declaration expiry and independent safety lifecycle, with
completed legacy objects preserved as safe.

**Why:** Upload state must be distinct from optional AI processing before quarantine is introduced.

**Acceptance:** Migration `up/down/up` tests prove enum/check constraints, legacy backfill, RLS, and
lossless rollback.

**Autonomy:** AFK

## Task 7: Quarantined Attachment Verifier

**Depends on:** Task 6
**Status:** Complete

**Outcome:** Completion consumes an unexpired declaration into quarantine and a deterministic worker
marks only signature-valid bounded objects safe.

**Why:** Storage metadata alone cannot establish media type or safety.

**Acceptance:** Verifier integration tests prove supported signatures, forged/truncated/oversized
rejection, object cleanup, idempotent reconciliation, and no silent safe state on scanner failure.

**Autonomy:** AFK

## Task 8: Safe-Only Attachment Consumers

**Depends on:** Task 7
**Status:** Complete

**Outcome:** Streaming, previews, exports, and attachment AI can consume only safe objects.

**Why:** Quarantine is ineffective if any downstream path bypasses it.

**Acceptance:** Cross-domain integration tests prove quarantined/rejected/expired objects cannot be
downloaded, exported as available, or claimed by AI while safe objects retain current behavior.

**Autonomy:** AFK

## Task 9: Replay Protection And Verification

**Depends on:** Tasks 2, 3, and 7
**Status:** Complete

**Outcome:** Replay grants are rate/byte bounded, expire, quarantine uploaded batches, and accept only
bounded structurally valid rrweb arrays.

**Why:** Replay is a separate high-volume storage path with the same abuse boundary.

**Acceptance:** Replay integration tests prove coordinated limits, aggregate caps, expiry, token
consumption, malformed/trailing/event-count rejection, cleanup, and safe-only finalization.

**Autonomy:** AFK

## Task 10: Idempotent AI Cost Budget

**Depends on:** Tasks 3 and 8
**Status:** Complete

**Outcome:** Text and safe-attachment analysis reserve tenant/project daily units once per work
generation and defer without losing feedback when unavailable.

**Why:** The provider RPM limit controls bursts but not tenant-attributed cost.

**Acceptance:** AI integration and concurrency tests prove one reservation across retries, budget
isolation, no provider call after denial/failure, visible defer state, and explicit retry recovery.

**Autonomy:** AFK

## Task 11: Widget Retry Experience

**Depends on:** Tasks 1 and 4
**Status:** Complete

**Outcome:** The widget maps stable reasons to inline states, honors retry delay, and retains all
entered content and local attachments.

**Why:** Abuse protection must not turn recoverable failures into lost feedback.

**Acceptance:** Widget unit/browser tests prove reason-specific text, countdown/retry, preserved
form/client ID/media, and accepted-feedback attachment-failure behavior.

**Autonomy:** AFK

## Task 12: Submission Health UI

**Depends on:** Task 5
**Status:** Complete

**Outcome:** Settings displays a compact responsive breakdown of rate, spam, quota, upload, AI, and
provider pressure.

**Why:** Administrators need to distinguish operational causes without inspecting logs.

**Acceptance:** Playwright desktop/mobile tests prove period/project filtering, category states,
empty/loading/error states, permission behavior, and no horizontal overflow.

**Autonomy:** AFK

## Task 13: End-To-End Quality Gate

**Depends on:** Tasks 9, 10, 11, and 12
**Status:** Complete

**Outcome:** The complete public protection flow is documented, tested, and ready in staging.

**Why:** Issue #26 closes only when every layer works together under real deployment behavior.

**Acceptance:** Migration replay, full backend/widget/web suites, static checks, responsive
screenshots, CI, and one staging journey all pass for the same commit.

**Completion:** Commit `4fd55a5` passed backend coverage, formatting, vet, golangci-lint, Compose,
container, security, and release CI before Railway promoted API and worker in staging. The real
Tauview widget accepted an independent four-star feedback, persisted one normalized submitted state
across query/hash and trailing-slash variants, suppressed repeat display, surfaced the unlinked
feedback in the inbox, completed AI analysis, and allowed the QA record to be archived.

**Autonomy:** AFK

# Organization Identity And Profile

**Source:** `docs/superpowers/specs/2026-07-23-fidibeki-organization-identity-design.md`

## Task 14: Forward-Compatible Profile Schema

**Depends on:** Task 13
**Status:** Complete

**Outcome:** Tenant and member records hold organization profile and versioned identity metadata.

**Acceptance:** Migration up/down/up tests prove constraints, RLS, grants, preservation, and
rollback.

**Autonomy:** AFK

## Task 15: Organization Profile Contract And Inheritance

**Depends on:** Task 14
**Status:** Complete

**Outcome:** Authorized administrators update organization defaults with audit, and existing project
and widget overrides remain effective.

**Acceptance:** Backend integration tests prove validation, permissions, audit, clear-to-fallback,
and organization -> project -> widget inheritance.

**Autonomy:** AFK

## Task 16: Bounded Identity Asset Lifecycle

**Depends on:** Task 14
**Status:** Complete

**Outcome:** Workspace logos and self-managed avatars are securely normalized, versioned, served,
replaced, and removed.

**Acceptance:** Image and HTTP integration tests prove supported/invalid inputs, rate limits, RBAC,
tenant isolation, cache busting, cleanup, and storage failure behavior.

**Autonomy:** AFK

## Task 17: Shared Identity UI

**Depends on:** Tasks 15 and 16
**Status:** Complete

**Outcome:** Settings and operational surfaces render organization/member identity with immediate
updates and resilient initials fallbacks.

**Acceptance:** Web unit and browser tests prove edit/upload/remove, field errors, read-only states,
broken-image fallback, and responsive layouts.

**Autonomy:** AFK

## Task 18: Organization Identity Quality Gate

**Depends on:** Task 17
**Status:** Complete

**Outcome:** Issues #30 and #31 are fully usable in staging.

**Acceptance:** Migration, backend, web, widget, static, browser, CI, and real staging checks pass
for the same commit without production work.

**Completion:** Commit `92496cd` passed backend, web, security, and release CI. Railway staging
applied migration 71 and serves the corresponding API/worker/web revisions. Authenticated Tauview
journeys proved avatar and workspace-logo upload, immutable reads, immediate propagation, removal,
fallback restoration, and no cleanup lock; all temporary identity data was removed.

**Autonomy:** AFK

# Member-Aware Localization

**Source:** `docs/superpowers/specs/2026-07-23-fidibeki-member-aware-localization-design.md`

## Task 19: Member Regional Preference Schema

**Depends on:** Task 18
**Status:** Complete

**Outcome:** Member records hold nullable locale and timezone preferences without changing existing
behavior.

**Acceptance:** Migration `up/down/up` proves preservation, constraints, RLS, grants, and rollback.

**Completion:** Migration 72 adds nullable normalized `locale` and syntactically safe IANA
`timezone` preferences to existing member rows. The migration integration cycle proves defaults,
supported/invalid values, app grants, tenant-scoped updates, cross-tenant denial, preservation,
rollback, and clean reapplication.

**Autonomy:** AFK

## Task 20: Profile And Session Regional Contract

**Depends on:** Task 19
**Status:** Complete

**Outcome:** Self-service profile updates and every member/session read expose validated regional
preferences.

**Acceptance:** Tenant-member integration tests prove partial update, inherit, validation, audit,
isolation, and refresh propagation.

**Completion:** `PATCH /v1/profile` now distinguishes omitted fields from explicit clears, validates
and normalizes the supported locale set and IANA timezones, and updates name, locale, and timezone
atomically with a single audit event only when values change. Authentication refresh and member
listing return the nullable preferences without changing JWT claims. Focused normalization and
tenant-member integration tests prove update, inherit, invalid input, no-op, audit, cross-tenant
isolation, and refresh propagation; the full package test and `go vet` pass.

**Autonomy:** AFK

## Task 21: SSR-Safe Localization Runtime

**Depends on:** Task 20
**Status:** Complete

**Outcome:** Nuxt resolves reactive no-prefix locales through member, organization, browser, and
English precedence.

**Acceptance:** Unit/browser tests prove Portuguese fallback, matching SSR hydration, and reactive
`lang`/`dir`.

**Completion:** Nuxt now uses `@nuxtjs/i18n` with unchanged `no_prefix` routes and lazy English,
Portuguese, regional Portuguese, and pseudo-locale catalogs. A shared regional context resolves
locale and timezone independently through member, organization, browser, and safe defaults; SSR
uses `Accept-Language` plus first-party browser cookies and defers first-client detection until
mount. The auth middleware applies the resolved locale before render, catalog switching is atomic,
and root `lang`/`dir` attributes react to preference changes. Unit normalization/precedence tests,
typecheck, production build, 139 full browser tests with CI-equivalent concurrency, and four
production-build SSR/hydration tests pass across desktop and mobile.

**Autonomy:** AFK

## Task 22: Regional Format And Error Helpers

**Depends on:** Task 21
**Status:** Complete

**Outcome:** Shared locale/timezone-aware formatters and safe localized API errors replace fixed
English/UTC helpers.

**Acceptance:** Unit tests prove DST/midnight, numeric formats, plurals, known errors, and unknown
fallback.

**Completion:** A shared cached `Intl` formatter factory and reactive composable now format dates,
times, numbers, currencies, percentages, relative time, lists, and plural categories with the
resolved member context. A centralized localized API-error layer maps only allowlisted stable
codes, reasons, and structured field reasons, preventing arbitrary backend or provider text from
reaching users while preserving a generic fallback. English and Portuguese catalogs cover the
stable contract, including plural messages. Focused DST/midnight, Angolan numeric, plural,
known/unknown error, and legacy compatibility tests pass; the full 93-test web unit suite,
typecheck, production build, and four responsive regional-runtime browser tests also pass.

**Autonomy:** AFK

## Task 23: Profile And Shared Navigation Localization

**Depends on:** Tasks 20, 21, and 22
**Status:** Complete

**Outcome:** Members switch preferences without logout and shared shell/account surfaces update
immediately.

**Acceptance:** Responsive browser tests prove switching, inheritance, accessible copy, and
identity regression safety.

**Completion:** English and Portuguese `common`, `navigation`, and `profile` catalogs now drive
the shared shell, queue rail, account menu, trial/deletion states, profile title, labels, controls,
notices, tooltips, and accessible names. Members can save a personal language and IANA timezone or
explicitly inherit workspace/browser values; the returned session member is applied immediately
without logout, and profile errors use the safe localized mapper. Desktop/mobile browser coverage
proves immediate `pt-AO`/Luanda switching, localized navigation and title, clear-to-inherit behavior
through a fresh session, and unchanged display-name/avatar workflows. Visual checks show no
overflow or overlap. The 93-test unit suite, typecheck, production build, and full 141-pass browser
suite succeed.

**Autonomy:** AFK

## Task 24: Authentication And Onboarding Localization

**Depends on:** Tasks 21 and 22
**Status:** Complete

**Outcome:** Public authentication and onboarding surfaces use English/Portuguese catalogs.

**Acceptance:** Browser tests cover the complete public/auth onboarding journey in both languages.

**Completion:** English and Portuguese catalogs now drive login, signup, magic-link verification,
member-domain verification, access-denied, Turnstile, first-project activation, and the shared
targeting editor. Public failures use safe localized fallbacks while structured field errors remain
actionable; plan limits, dates, numbers, validation, page titles, tooltips, and accessible names use
the effective regional context. Targeting diagnostics retain precise safe-regex and matched-exclude
details in both languages without changing canonical URL or request payload behavior. Dedicated
desktop/mobile browser journeys prove Portuguese signup, verification, onboarding, targeting, and
access guidance, while the complete 93-test unit suite, typecheck, production build, and 147-pass
browser suite remain green.

**Autonomy:** AFK

## Task 25: Dashboard And Analytics Localization

**Depends on:** Tasks 21 and 22
**Status:** Complete

**Outcome:** Dashboard and widget analytics copy and metrics use effective regional settings.

**Acceptance:** Unit/browser tests prove localized metrics/states without changing calculations.

**Completion evidence:** English and Portuguese catalogs cover the dashboard and widget analytics;
regional numbers, percentages, dates, relative times, and plurals retain the existing calculations;
provider errors are replaced with safe localized messages while customer-authored feedback, rule
names, and URLs remain unchanged. Unit (93), typecheck, build, focused English/Portuguese browser
flows, responsive screenshots, and the full browser suite (149 passed, 47 skipped) pass. The full
gate also exposed and now covers a magic-link hash-clearing race in both viewports.

**Autonomy:** AFK

## Task 26: Feedback Operations Localization

**Depends on:** Tasks 21 and 22
**Status:** Complete

**Outcome:** Feedback queue, detail, evidence, replay, moderation, triage, and linking use catalogs.

**Acceptance:** Portuguese browser workflows preserve customer-authored content and exact mutations.

**Evidence:** 93 unit tests, typecheck, Nuxt build, and the 40-test feedback/replay/upload
browser slice pass (29 executed, 11 intentional viewport skips). The Portuguese desktop
and mobile journey verifies exact bulk mutations, link/unlink, archive, evidence safety,
regional sentiment, replay controls, and preservation of customer-authored content.

**Autonomy:** AFK

## Task 27: Issue Collaboration Localization

**Depends on:** Tasks 21 and 22
**Status:** Complete

**Outcome:** Issue operations, comments, mentions, activity, reporter access, and notifications use
catalogs.

**Acceptance:** Portuguese collaboration workflows prove copy, plurals, dates, accessibility, and
mutation parity.

**Evidence:** Issue list/detail, standalone comments, mentions, notifications, activity, merge,
reporter access, and public reporter progress use EN/PT catalogs, regional formatting, and safe
errors. The focused Portuguese collaboration/reporter slice passes eight desktop/mobile journeys
with exact mutation payloads and customer content preserved; the integrated localization gate,
unit suite, typecheck, and build also pass.

**Autonomy:** AFK

## Task 28: Project And Widget Configuration Localization

**Depends on:** Tasks 21 and 22
**Status:** Complete

**Outcome:** Project and widget configuration UI uses catalogs while customer widget content stays
independent.

**Acceptance:** Portuguese browser tests preserve customer content and exact API payloads.

**Evidence:** Project list/detail, widget targeting, lifecycle, decision diagnostics, response
blocks, localized customer copy, preview, appearance, replay, and settings use catalogs and regional
formatting. The Portuguese desktop/mobile journey passes with exact create/update/delete payloads,
safe provider failures, and customer content preserved. Unit (103), typecheck, build, and the
integrated 18-journey browser slice pass.

**Autonomy:** AFK

## Task 29: Settings, Billing, And Shared Localization

**Depends on:** Tasks 21 and 22
**Status:** Complete

**Outcome:** Administration, diagnostics, privacy, billing, and remaining shared production
components use catalogs.

**Acceptance:** Portuguese browser tests preserve permissions and operational behavior.

**Evidence:** 93 unit tests, typecheck, and production build pass. The focused administration
suite passes 35 browser tests with 13 intentional skips, plus two Portuguese desktop/mobile
journeys covering exact permissions, privacy mutations, submission-health filters, billing,
member lifecycle, and the public privacy page.

**Autonomy:** AFK

## Task 30: Catalog And Pseudo-Locale Quality Gate

**Depends on:** Tasks 23 through 29
**Status:** Complete

**Outcome:** Catalog parity, extraction coverage, and expanded layout resilience are enforced.

**Acceptance:** Static/unit and responsive pseudo-locale tests detect missing keys, literals,
interpolation loss, clipping, overlap, and overflow.

**Evidence:** Complete EN/PT catalogs have key and interpolation parity except the intentional
English fallback probe. A source gate rejects uncatalogued visible production copy, and the
generated `en-XA` catalog expands every message while preserving plural/interpolation syntax.
Responsive browser coverage traverses authentication, dashboard, feedback, issue list/detail,
project list/editor, settings, and profile in desktop/mobile, preserves customer-authored content,
and detects viewport overflow, clipping, and overlap. The gate fixed compact mobile search and
wrapped expanded Settings tabs; 103 unit tests, typecheck, build, and 18 integrated E2E journeys
pass.

**Autonomy:** AFK

## Task 31: Localization Staging Gate

**Depends on:** Task 30
**Status:** Complete

**Outcome:** #32 is fully usable in staging.

**Acceptance:** Full local, CI, and real staging locale/timezone journeys pass on the same commit.

**Evidence:** The complete local and CI-equivalent catalog, unit, typecheck, build, responsive
browser, SSR/hydration, and pseudo-locale gates pass. Staging web deployment
`c21ea9ce-a1b5-49dc-a779-7ca6213b4479` served the exact code. A real Tauview member session
inherited organization `pt`/`Europe/Lisbon`, accepted personal `en`/`UTC` and
`pt-PT`/`Europe/Lisbon` overrides, then returned to null/null inheritance. Unauthenticated SSR
resolved Portuguese through `Accept-Language`. The temporary magic link/session were revoked and
deleted, and the member preferences were verified restored. No push or production action was used.

**Autonomy:** AFK

# Open GitHub Issue Reconciliation

**Source:** Open non-`postpone` issues in `acidiney/app.fidibeki`, audited on 2026-07-23.

## Task 32: Validate Completed Configuration Refresh Fix

**Depends on:** Task 30
**Status:** Complete

**Outcome:** Confirm issue #44 is fully covered by the existing raw-clone hydration fix, including
saved rule values and hard refresh, then close it only if the focused regression passes.

**Acceptance:** Existing configuration hydrates every persisted field, a hard refresh does not
throw a clone error, focused unit/browser coverage passes, and the GitHub issue records the evidence.

**Evidence:** Rule hydration now routes reactive API data through `cloneReactive`, which unwraps
the Vue proxy before structured cloning. A focused unit regression clones the complete nested
localization shape from a real reactive proxy, while the localized desktop/mobile editor journey
verifies persisted identity, targeting, content, and capture values. Unit (104), typecheck, build,
and focused browser gates pass; real staging refresh remains part of the deferred staging gate.

**Autonomy:** AFK

## Task 33: Repair Privacy Request Journeys

**Depends on:** Task 30
**Status:** Complete

**Outcome:** Resolve workspace export generation, reporter anonymization creation, and reporter
verification link failures in issues #46, #49, and #50.

**Acceptance:** Backend integration coverage proves durable idempotent job creation and one-time,
tenant-scoped verification URL handling; browser coverage proves success and actionable retry
states. Staging validation is deferred unless the fix directly requires it.

**Evidence:** Issues #46 and #49 were caused by byte-encoded JSON audit payloads in runtime mode
and were already repaired by `4fd55a5`; historical Railway logs identified the exact PostgreSQL
`22P02` failures and later real requests proved both creation paths. Issue #50 was a separate host
routing defect: generated email links targeted the tenant host while the public handlers were
mounted only on the platform router. The shared routes now mount on both hosts, with integration
coverage for the generated email URL, re-verification, status, expiry, invalid tokens, export,
anonymization, idempotency, and tenant isolation. Staging API deployment
`06c035b6-9d10-47a5-8027-2420b648e843` applied migration 73 and passed first verification,
idempotent re-verification, status, public-page, and neutral-invalid-token checks on the Tauview
tenant host. The synthetic request, job, export object, and token were removed after validation.

**Autonomy:** AFK

## Task 34: Repair Replay Fullscreen

**Depends on:** Task 30
**Status:** Complete

**Outcome:** Complete issue #33 with responsive fullscreen sizing and browser/fullscreen state
synchronization.

**Acceptance:** Aspect ratio, controls, Escape/control exit, repeated cycles, listener cleanup, and
supported-browser E2E pass.

**Evidence:** ReplayPlayer now owns fullscreen through `document.fullscreenElement` and one
`fullscreenchange` listener, fits the recorded aspect ratio inside the available viewport, keeps
controls visible, restores inline geometry across repeated cycles, and exposes the toggle state to
assistive technology. Web unit (121), typecheck, production build, and all three focused desktop
replay browser journeys pass.

**Autonomy:** AFK

## Task 35: Complete Focused Visual Regressions

**Depends on:** Task 30
**Status:** Complete

**Outcome:** Complete issues #38, #45, #47, and #48 without changing operational behavior.

**Acceptance:** Owner badge, notification empty state, unrated feedback metadata, and destructive
hover states remain aligned, legible, accessible, and stable across responsive viewports.

**Evidence:** Issues #38, #45, #47, and #48 now share explicit stable geometry, centered empty-state
layout, one localized non-interactive rating badge across dashboard/inbox/detail, and a destructive
hover color whose computed contrast remains at least 4.5:1. Geometry, contrast, overflow, localized
labels, and responsive screenshots pass on desktop and mobile.

**Autonomy:** AFK

## Task 36: Standardize Confirmations And Transient Notices

**Depends on:** Task 35
**Status:** Complete

**Outcome:** Complete issues #34 and #35 with shared accessible toast and confirmation primitives.

**Acceptance:** Saves do not shift forms or stack stale notices; no production route uses
`window.confirm`; focus, Escape, pending, backdrop, restore, desktop, and mobile behavior pass.

**Evidence:** Widget appearance saves now use one global live-region toast that replaces repeated
notices without changing form geometry. Project, rule, API-key, and integration deletion use one
localized native-dialog confirmation with resource consequences, initial Cancel focus, backdrop and
Escape dismissal, focus restoration, in-modal errors, and a guarded pending state. No production
`window.confirm` remains. Web unit (104), typecheck, production build, and focused browser coverage
pass, including the project flow on desktop and mobile.

**Autonomy:** AFK

## Task 37: Complete Appearance And Validation Contracts

**Depends on:** Task 36
**Status:** Complete

**Outcome:** Complete issues #36, #37, and #39 through one consistent structured field-error and
accent-foreground contract across API, backoffice preview, and widget.

**Acceptance:** Errors stay in their owning forms and fields, nested rule paths remain actionable,
accessible foreground derivation is deterministic, and backend/web/widget/browser tests pass.

**Design:** `docs/superpowers/specs/2026-07-23-widget-appearance-validation-contract-design.md`

**Evidence:** Appearance accepts every normalized six-digit accent, derives a deterministic
WCAG-AA foreground in backend/web/widget, and exposes the additive resolved value without a
migration. Settings keeps Reporter Access and Widget Defaults failures in their owning forms.
Widget-rule validation now unwraps `details.fields`, preserves multiple and unmapped errors, maps
backend array paths including response blocks and targeting conditions, switches locale, focuses
the first actionable field, and clears only edited paths. Backend full tests, web unit (113),
widget unit (129), both typechecks/builds, focused widget Chromium, and three focused backoffice
browser journeys pass.

**Autonomy:** AFK

## Task 38: Make Widget Page Language Reactive

**Depends on:** Task 30
**Status:** Complete

**Outcome:** Complete issue #40 using the host page language as the reactive widget locale source.

**Acceptance:** Exact/primary/default/browser precedence, SPA language changes, preserved draft and
presentation state, event deduplication, metadata, cleanup, and browser coverage pass.

**Evidence:** Issue #40 is complete locally. A shared per-document observer follows valid root
`lang` changes and is disconnected with the final subscriber. Resolution uses page exact/primary,
project default, first configured, then browser compatibility fallback only when page language is
absent or invalid. Mounted copy is patched in place, including validation/retry state and rating
labels, while selections, draft fields, open state, attachments, recurrence, and funnel identities
remain intact. Submission metadata stores the resolved locale. Widget unit (124), typecheck, build,
and all 38 browser journeys pass.

**Autonomy:** AFK

## Task 39: Add Explicit Canvas Replay Policy

**Depends on:** Task 34
**Status:** Complete

**Outcome:** Complete issue #41 with opt-in canvas recording, privacy exclusions, bounded resource
use, and faithful playback.

**Acceptance:** Configuration, API/runtime projection, rrweb events, playback, failure behavior,
privacy guidance, performance measurement, and automated coverage pass.

**Design:** `docs/superpowers/specs/2026-07-23-canvas-replay-policy-design.md`

**Evidence:** Migration 73 and the project/public widget contracts default canvas capture off and
bound explicit capture to 1-5 FPS. The widget projects that policy to rrweb, retains the
`data-fidibeki-block` exclusion, forwards capture errors, and keeps the existing duration, event,
batch, and storage limits. Real browser coverage records one continuously changing canvas while
excluding a second blocked canvas, measures host responsiveness and upload volume, and verifies the
captured pixels render in the backoffice player. The privacy map and localized rule editor explain
the risk and exclusion. Full backend tests and vet, web unit (121), typecheck/build, focused
desktop/mobile browser journeys, widget unit (130) with coverage, typecheck/build, and all 39 widget
browser journeys pass.

**Autonomy:** AFK

## Task 40: Complete Reporter Progress Experience

**Depends on:** Task 30
**Status:** Complete

**Outcome:** Complete issues #42 and #43 with a Dispatch-aligned public progress page and visible,
safe clarification responses in the issue timeline.

**Acceptance:** Public states are localized and private by construction; responses persist and
render with attribution/order/newlines; desktop/mobile browser journeys pass.

**Evidence:** The public reporter page now uses the approved Fidibeki wordmark and shared Dispatch
tokens with localized loading, invalid/expired, transient error/retry, empty, clarification,
answered, and timeline states. Reporter responses already persisted in `reporter.responded`
activity data; the issue timeline now renders the complete text with actorless attribution,
chronological order, preserved newlines, long-text containment, and escaped interpolation. Focused
backend coverage, web unit/i18n tests, typecheck, production build, and 14 related Playwright
journeys pass on desktop/mobile, including submit-to-backoffice visibility and executable HTML
escaping. Staging web deployment `c21ea9ce-a1b5-49dc-a779-7ca6213b4479` passed a real reporter API
and SSR page journey in English and Portuguese; its temporary access token was removed.

**Autonomy:** AFK

## Task 41: Reconcile Remaining Open Issues

**Depends on:** Tasks 31 through 44
**Status:** In Progress

**Outcome:** Close materially completed issues with test evidence and leave only explicitly
postponed or genuinely staging-gated work open.

**Acceptance:** GitHub state matches the code and test evidence; no production, deployment, or
infrastructure work is performed without renewed authorization.

**Autonomy:** AFK

## Task 42: Enforce Anonymous Session Replay

**Depends on:** Task 39
**Status:** In Progress

**Outcome:** Complete issue #53 so replay artifacts cannot expose an authenticated user through
identity-bearing text, attributes, URLs, canvas pixels, or stable cross-session identifiers.

**Acceptance:** Replay-scoped identity, canonical locations, fail-closed rrweb sanitization,
canvas anonymity, deletion coverage, privacy documentation, and sentinel backend/widget/browser
tests satisfy the issue contract without relying only on customer-authored selectors.

**Autonomy:** AFK

## Task 43: Require Auditable Reporter Consent

**Depends on:** Task 40
**Status:** In Progress

**Outcome:** Complete issue #55 with explicit, specific, revocable consent before any reporter
email address is transmitted, persisted, enqueued, or delivered.

**Acceptance:** Widget disclosure, structured consent evidence, safe legacy migration, contradictory
payload rejection, enqueue/send-time checks, withdrawal race protection, bilingual email flows,
retention, and end-to-end tests satisfy the issue contract.

**Autonomy:** AFK

## Task 44: Minimize Automatic Feedback Context

**Depends on:** Tasks 42 and 43
**Status:** Pending

**Outcome:** Complete issue #54 by applying shared canonical-location and sanitization primitives to
widget metadata, referrers, element attachments, backend defense in depth, downstream projections,
existing data remediation, and retention.

**Acceptance:** PII/secrets in URL credentials, query/hash/path context, referrers, DOM text and
attributes, CSS URLs, logs, AI, integrations, exports, storage, APIs, and backoffice rendering fail
closed under sentinel unit/integration/browser coverage.

**Autonomy:** AFK
