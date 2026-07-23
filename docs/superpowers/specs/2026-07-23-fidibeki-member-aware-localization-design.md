# Fidibeki Member-Aware Localization

**Date:** 2026-07-23
**Status:** Approved
**Issue:** #32
**Deployment:** Staging only

## Goal

Make the Fidibeki backoffice usable in English and Portuguese while resolving each member's
language and timezone consistently across SSR, client navigation, formatting, validation, and
accessible copy.

The precedence is:

1. authenticated member preference;
2. organization default;
3. browser locale/timezone;
4. English and UTC.

Customer-authored content, feedback, comments, names, tags, and other domain data are never
translated.

## Existing System Audit

- Nuxt has no i18n module and approximately one thousand user-facing template literals across the
  production backoffice surfaces.
- `web/utils/format.ts`, widget analytics, and several components instantiate formatters with
  `en-US` and `UTC`.
- Organization defaults now exist as `tenants.default_locale` and `tenants.default_timezone` and
  are returned in the authenticated session.
- Member records have a self-service profile, but no locale or timezone preference.
- The global auth middleware refreshes the session during SSR before the protected page renders.
- The widget has independent project-defined content localization and locale fallback. Its content
  catalogs remain independent from the backoffice.
- API errors use a compatible envelope with stable top-level codes, but many validation details are
  still English prose.

## Considered Approaches

### 1. Nuxt I18n with a small Fidibeki regional context

Use `@nuxtjs/i18n` 10.x / Vue I18n 11.x for catalogs, plural rules, fallback, SSR integration,
generated types, and reactive switching. Keep URLs unchanged with `no_prefix`. Add a small
Fidibeki composable for precedence, timezone, `Intl` formatting, and API error mapping.

This is the selected approach. It uses a maintained engine for the hard language mechanics while
keeping product-specific precedence explicit and testable.

### 2. Vue I18n through a custom Nuxt plugin

Install Vue I18n directly and own SSR initialization, locale detection, head attributes, lazy
loading, and Nuxt type integration.

This provides more control but duplicates integration already maintained by Nuxt I18n and creates
more hydration and typing risk.

### 3. A dependency-free typed dictionary

Build flat TypeScript catalogs and custom interpolation, plural, date, number, and fallback
functions.

This has a small dependency footprint, but reimplements mature locale behavior and would make the
large copy migration harder to validate. It is rejected.

## Locale Model

### Supported locales

- `en`: English messages and default fallback.
- `pt`: Portuguese messages.
- `pt-AO`: Portuguese messages with Angolan regional formatting.
- `pt-PT`: Portuguese messages with Portuguese regional formatting.
- `en-XA`: pseudo-locale used by tests and staging diagnostics.

Regional Portuguese locales fall back to the `pt` message catalog. Locale normalization is
case-insensitive and converts underscores to hyphens. Unsupported values fall back through the
precedence chain instead of being persisted.

### Member preferences

Add nullable `locale` and `timezone` columns to `tenant_members`.

- `NULL` means inherit.
- Locale values are normalized to the supported set.
- Timezones must be IANA identifiers accepted by Go's timezone database.
- `PATCH /v1/profile` updates display name, locale, and timezone in one transaction and one audit
  event.
- Session verification and refresh return the current preferences.
- Existing members remain unchanged and inherit organization/browser defaults.

The member profile exposes explicit `Inherit workspace/browser` choices. Switching locale updates
the current session and interface immediately without logout or hard refresh.

## Runtime Resolution And SSR

Nuxt I18n uses `strategy: "no_prefix"`; routes and links do not change.

For public pages, browser locale resolution uses `Accept-Language` during SSR and
`navigator.languages` on the client. For protected pages, the global auth middleware refreshes the
session first and then applies the effective member/organization locale before rendering the page.
The same locale is serialized in the Nuxt payload, preventing hydration mismatch.

Browser timezone has no reliable request header. The client stores the detected IANA timezone in a
first-party preference cookie after hydration. SSR uses that cookie on later requests and UTC on
the first request when no member, organization, or cookie value exists. The first client update is
deferred until hydration completes.

`<html lang>` is reactive and `<html dir>` is derived from locale metadata. All initial locales are
LTR, but the contract is ready for a future RTL catalog.

## Catalog Architecture

Use lazy TypeScript locale files under `web/i18n/locales/`, organized by stable product domains:

- `common`, `navigation`, and `errors`;
- `auth` and `onboarding`;
- `dashboard` and `analytics`;
- `feedback` and `issues`;
- `projects` and widget configuration labels;
- `settings`, `profile`, and `billing`;
- shared operational components.

English is the source schema. Portuguese catalogs must satisfy the same resource shape. No catalog
message contains HTML; rich composition uses `<i18n-t>` or separate translated fragments.

Keys describe meaning, not the current English sentence. Dynamic domain content is passed as
interpolation data and is escaped by Vue.

The design-study routes, customer-authored widget content, reporter data, and email templates are
not part of this backoffice catalog migration.

## Formatting

Replace fixed global formatters with `useRegionalFormat()`, which reads the effective locale and
timezone and exposes:

- date and date-time;
- integer and decimal number;
- currency with an explicit currency code;
- percent;
- relative time;
- list formatting;
- plural selection through Vue I18n.

Formatter instances are cached by locale, timezone, and options. Invalid API dates return a neutral
placeholder rather than throwing during render. API and database timestamps remain UTC/ISO 8601.

Organization profile preview formatting uses the selected draft locale/timezone locally so the
preview remains useful before saving.

## Error Localization

The client maps known API `error.code` values and stable public `reason` values to catalog keys.
Field errors use field names plus stable reason codes where the backend exposes them. Additive
machine-readable reason fields may be added to the existing envelope; existing prose remains for
compatibility.

Unknown errors never expose arbitrary backend or provider text as product copy. They use a generic
localized fallback, while structured details remain available to observability.

## Copy Migration

Migrate production surfaces in vertical slices:

1. runtime, session preferences, shared formatters, navigation, and profile;
2. authentication and onboarding;
3. feedback and issues;
4. analytics and dashboard;
5. projects and widget configuration UI;
6. settings, privacy administration, billing, and remaining shared components.

Each slice removes direct user-facing copy from its covered templates, including page titles,
labels, placeholders, empty/loading/error states, toasts, dialog copy, tooltips, and accessible
names. Technical identifiers, domain values, and customer-authored content stay literal.

An extraction audit scans the covered production templates. Any intentional literal must be
documented in a narrow allowlist.

## Pseudo-Locale

`en-XA` expands and accents English messages while preserving interpolation tokens. It is available
to tests and staging diagnostics, not presented as a normal production language choice.

Responsive browser tests run core navigation, feedback triage, issue collaboration, project
configuration, settings, and profile surfaces in the pseudo-locale. They assert no horizontal
overflow, clipped controls, or overlapping text.

## Compatibility And Security

- Migration 71 is additive; its down migration removes only member preference columns.
- Existing sessions remain valid because preferences are response metadata, not authorization
  claims.
- Existing profile clients that send only `name` retain their behavior.
- Locale changes do not alter routes, tenant resolution, API contracts, or widget content.
- Catalog interpolation is text-only and strict message compilation rejects embedded HTML.
- Preference cookies are first-party, `SameSite=Lax`, and contain only normalized locale/timezone
  values.

## Testing

### Backend

- migration `up/down/up`, constraints, preservation, grants, and RLS;
- profile normalization, clear-to-inherit, partial updates, audit, active-member enforcement, and
  tenant isolation;
- verification/refresh/list responses include preferences without changing token claims.

### Web unit and integration

- locale normalization and full precedence;
- `pt-AO -> pt` and `pt-PT -> pt` message fallback;
- UTC, midnight, DST, number, currency, percent, relative time, and plural cases;
- stable API error mapping and unknown fallback;
- catalog parity, missing-key detection, and pseudo-localization token preservation;
- copy extraction audit for every covered surface.

### Browser

- public browser-locale SSR and matching hydration;
- organization inheritance and member override;
- immediate English/Portuguese switching from profile;
- page titles, `lang`, accessible copy, and format changes;
- pseudo-locale desktop/mobile overflow checks;
- one real staging journey with production untouched.

## Completion

#32 is complete when all covered backoffice surfaces use catalogs, member precedence works through
fresh and refreshed sessions, regional formatting is centralized, local and CI gates pass, and the
same commit is validated in staging.
