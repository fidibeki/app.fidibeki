# Fidibeki Member-Aware Localization Implementation

**Date:** 2026-07-23
**Status:** Active
**Scope:** Locked
**Source:** `docs/superpowers/specs/2026-07-23-fidibeki-member-aware-localization-design.md`
**Issue:** #32
**Deployment:** Staging only

## Shared Context

- Extend the existing organization profile, self-service member profile, auth refresh middleware,
  API error envelope, and widget locale normalization.
- Use `@nuxtjs/i18n` with `no_prefix`; do not add localized routes.
- Member preference overrides organization, then browser, then `en`/`UTC`.
- Keep domain/customer content untouched and keep widget content localization independent.
- Existing clients, sessions, UTC API timestamps, and English behavior remain compatible.

## Task 1: Add Member Regional Preference Schema

**Depends on:** None

**Outcome:** Active member records can persist nullable normalized locale and IANA timezone
preferences without changing existing members.

**Acceptance:** Migration `up/down/up` proves preservation, constraints, RLS, grants, and clean
rollback for member locale/timezone columns.

**Autonomy:** AFK

## Task 2: Extend Self-Service Profile And Session Contracts

**Depends on:** Task 1

**Outcome:** Members can atomically set or clear display name, locale, and timezone, and verification,
refresh, listing, and audit responses expose the current values.

**Acceptance:** Tenant-member integration tests prove partial updates, clear-to-inherit,
normalization, validation, tenant isolation, audit, and refreshed-session propagation.

**Autonomy:** AFK

## Task 3: Establish SSR-Safe Localization Runtime

**Depends on:** Task 2

**Outcome:** Nuxt resolves supported public and authenticated locales with unchanged routes and
matching SSR/client state.

**Acceptance:** Web unit/browser tests prove member -> organization -> browser -> English
precedence, Portuguese regional fallback, reactive `lang`/`dir`, and hydration without warnings.

**Autonomy:** AFK

## Task 4: Centralize Regional Formatting And Error Copy

**Depends on:** Task 3

**Outcome:** Shared composables format dates, numbers, currency, percent, relative time, lists, and
known API errors from the effective locale/timezone.

**Acceptance:** Unit tests prove UTC/midnight/DST cases, regional numeric output, plural behavior,
known error mapping, invalid-value handling, and safe unknown fallback.

**Autonomy:** AFK

## Task 5: Deliver Localized Profile And Shared Navigation

**Depends on:** Tasks 2, 3, and 4

**Outcome:** Members switch language/timezone from profile and immediately see localized shared
navigation, account, dialogs, states, and titles.

**Acceptance:** Desktop/mobile browser tests prove immediate English/Portuguese switching, persisted
inheritance, localized accessible copy, and unchanged account/identity workflows.

**Autonomy:** AFK

## Task 6: Localize Authentication And Onboarding

**Depends on:** Tasks 3 and 4

**Outcome:** Login, signup, verification, domain verification, getting started, and access-denied
surfaces use catalogs and regional formatting.

**Acceptance:** Browser tests render the complete public/auth onboarding journey in English and
Portuguese with localized errors, validation, titles, and accessible names.

**Autonomy:** AFK

## Task 7: Localize Dashboard And Analytics

**Depends on:** Tasks 3 and 4

**Outcome:** Dashboard, widget funnel analytics, filters, metrics, empty states, and charts use
catalogs and effective regional formatters.

**Acceptance:** Unit/browser tests prove English/Portuguese metric copy, percent/number/date
formatting, loading/empty/error states, and unchanged funnel calculations.

**Autonomy:** AFK

## Task 8: Localize Feedback Operations

**Depends on:** Tasks 3 and 4

**Outcome:** Feedback queues, filters, inspector, detail, attachments, replay, moderation, bulk
triage, and issue-linking workflows use catalogs without translating feedback data.

**Acceptance:** Browser tests complete inspect, filter, bulk triage, archive, replay, and issue-link
workflows in Portuguese while preserving customer text verbatim.

**Autonomy:** AFK

## Task 9: Localize Issue Collaboration

**Depends on:** Tasks 3 and 4

**Outcome:** Issue list/detail, status, triage, comments, mentions, activity, reporter access, merge,
and notifications use catalogs without translating domain data.

**Acceptance:** Browser tests complete issue collaboration and notification workflows in Portuguese
with correct plurals, dates, statuses, accessible names, and unchanged mutations.

**Autonomy:** AFK

## Task 10: Localize Projects And Widget Configuration UI

**Depends on:** Tasks 3 and 4

**Outcome:** Project list/detail, rules, targeting, recurrence, appearance, diagnostics, preview, and
settings labels use catalogs while customer-authored widget localizations stay unchanged.

**Acceptance:** Browser tests edit a project and widget definition in Portuguese while preserving
configured customer content and exact API payloads.

**Autonomy:** AFK

## Task 11: Localize Settings, Billing, And Remaining Shared Components

**Depends on:** Tasks 3 and 4

**Outcome:** Workspace/member/API/integration/privacy/submission-health settings, billing, and
remaining production shared components use catalogs and regional formatters.

**Acceptance:** Browser tests cover permissions, organization settings, diagnostics, privacy, and
billing in Portuguese with localized states and unchanged authorization behavior.

**Autonomy:** AFK

## Task 12: Enforce Catalog Completeness And Pseudo-Locale Resilience

**Depends on:** Tasks 5 through 11

**Outcome:** Catalog parity, covered-template extraction, and expanded pseudo-localized layouts are
enforced automatically.

**Acceptance:** Static/unit and desktop/mobile pseudo-locale tests fail on missing keys, unjustified
user-facing literals, lost interpolation tokens, clipping, overlap, or horizontal overflow.

**Autonomy:** AFK

## Task 13: Localization Quality Gate And Staging

**Depends on:** Task 12

**Outcome:** #32 is regression-tested and usable in staging without production work.

**Acceptance:** Migration, backend/web/widget suites, type/static/build/browser checks, CI, and one
real staging English/Portuguese/profile/timezone journey pass for the same commit.

**Autonomy:** AFK
