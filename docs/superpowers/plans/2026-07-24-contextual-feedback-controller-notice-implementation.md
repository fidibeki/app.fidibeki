# Contextual Feedback Controller Notice Implementation Plan

**Issue:** #63
**Design:** `docs/superpowers/specs/2026-07-24-contextual-feedback-controller-notice-design.md`
**Boundary:** Local and staging only; migration `103`; no deployment or production work

## Task 1: Add compatible notice evidence storage

Files:

- `infra/migrations/000103_add_contextual_feedback_controller_notice.up.sql`
- `infra/migrations/000103_add_contextual_feedback_controller_notice.down.sql`
- `backend/internal/db/migrations_test.go`

Add tenant privacy contacts/version and feedback/issue notice snapshots. Preserve the legacy
`2026-07` branch, add the `2026-07-controller-v1` branch, retain timestamps, and cover up/down/up.

## Task 2: Version the organization privacy profile

Files:

- `backend/internal/tenant/tenant.go`
- `backend/internal/tenant/resolver.go`
- `backend/internal/tenantmember/profile.go`
- relevant tenant/profile HTTP and tests
- `web/types/api.ts`
- `web/components/settings/OrganizationProfile.vue`
- locale message files

Normalize the two new emails, tighten customer policy validation for the public notice, increment
the version only on controller-field changes, and include before/after/version in existing audit.
Add the existing-form UI and focused backend/frontend tests.

## Task 3: Extend and validate localized rule notices

Files:

- `backend/internal/project/models.go`
- `backend/internal/project/validation.go`
- project store/HTTP tests
- `web/pages/projects/[id].vue`
- `web/types/api.ts`
- locale message files

Add the three localized summaries, require them for contact collection, preserve legacy anonymous
rules, and expose precise field errors. Extend the rule editor and preview using existing
localization tabs.

## Task 4: Publish only complete controller configuration

Files:

- `backend/internal/project/models.go`
- `backend/internal/project/store.go`
- `backend/internal/widgetapi/http.go`
- widget API tests
- `widget/src/types.ts`
- `widget/src/config.ts`

Load the tenant controller profile with public configuration, emit schema version `3`, and suppress
public contact collection whenever the profile or any locale is incomplete.

## Task 5: Render the layered customer notice

Files:

- `widget/src/widget.ts`
- `widget/src/localization.ts`
- widget unit and E2E tests

Remove the hardcoded Fidibeki policy constant from base/contact markup. Render the resolved customer
controller layer before the opt-in, update it on locale changes, hide contact fail-closed, and send
only the two version assertions plus existing consent fields.

## Task 6: Validate and preserve consent evidence

Files:

- `backend/internal/feedback/models.go`
- `backend/internal/feedback/validation.go`
- `backend/internal/feedback/store.go`
- `backend/internal/feedback/tenant_store.go`
- `backend/internal/issue/store.go`
- `backend/internal/issue/notification.go`
- `backend/internal/issue/reporter_progress.go`
- related integration/unit tests

Validate v1 assertions against current controller/rule versions and locale, build the bounded
snapshot server-side, persist/copy/merge it, reject the legacy version on new submissions, and
retain legacy reporter notification eligibility without rewriting old evidence.

## Task 7: Reconcile product truth and verify

Files:

- `docs/privacy-data-map.md`
- `plans/fidibeki.plan.md`
- relevant web/widget Playwright specs

Record the completed tracer without closing #63. Run migration, backend, widget, and web suites;
format, lint, typecheck, and build; inspect desktop/mobile screenshots; then run an independent
fresh-eyes review before the implementation commit.
