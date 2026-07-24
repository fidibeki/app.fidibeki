# Contextual Feedback Controller Notice Design

**Issue:** #63
**Status:** Approved under the user's standing authorization for recommended decisions
**Environment boundary:** Local and staging only; no production or deployment work

## Problem

Fidibeki already has several privacy building blocks:

- the organization profile stores the workspace name, support email, and privacy-policy URL;
- widget rules have localized feedback/contact copy and an incrementing definition version;
- replay and analytics have separate purpose-specific notices and consent evidence;
- AI has an explicit, rule-scoped disclosure;
- reporter contact opt-in stores purpose, notice version, locale, and source.

The reporter contact notice is still code-owned generic text linking to
`https://fidibeki.com/privacy`. That substitutes Fidibeki's policy for the customer's controller
notice. The current evidence also cannot prove which customer controller details and localized
purpose/retention wording the reporter saw.

This slice fixes the base feedback and reporter-contact layer. It does not replace the independent
replay, analytics, or AI purpose gates.

## Options Considered

1. **Reuse organization identity plus rule localizations (recommended).**
   Store controller identity once on the tenant, store purpose/lawful-basis/retention wording in
   each rule's existing localizations, and snapshot the resolved notice on accepted contact opt-in.
2. **Add a standalone notice CMS and version tables.**
   This can support complex approval workflows later, but duplicates existing rule localization
   and definition-version machinery before the product needs that complexity.
3. **Add controller fields independently to every purpose configuration.**
   Replay and analytics currently carry such fields, but extending that duplication to base
   feedback/contact would make controller identity drift between surfaces.

Option 1 is selected. Later work may normalize replay and analytics onto the shared profile without
changing their independent consent ledgers.

## Controller Privacy Profile

The organization profile gains:

- `privacy_contact_email`: the controller's general privacy contact;
- `privacy_rights_email`: the controller's rights-request contact;
- `privacy_profile_version`: server-managed integer, initially `0`.

The existing organization `name` is the controller name and the existing
`privacy_policy_url` is the customer policy. Product support email remains separate.

The server increments `privacy_profile_version` atomically whenever controller name,
privacy-policy URL, privacy contact, or rights contact changes. A complete public profile requires:

- non-empty controller name;
- absolute HTTPS policy URL with no credentials, query, or fragment;
- valid privacy and rights email addresses;
- profile version greater than zero.

Incomplete profiles are never exposed as a complete controller notice. Existing tenants are not
backfilled with invented contacts; version `0` is explicitly unconfigured.

## Rule-Localized Notice

`WidgetLocalization` gains three bounded fields:

- `privacy_purpose_summary`;
- `privacy_lawful_basis_summary`;
- `privacy_retention_summary`.

They describe the customer's configured base feedback/contact processing in the same locale as the
prompt. They are required when `collect_contact` is enabled. For older rules without the fields,
feedback remains usable but contact collection fails closed and is omitted from public config.

When a rule has a complete controller profile and complete localized notice, the widget shows a
compact customer-controller privacy layer for every feedback form. It identifies the controller,
purpose, lawful-basis wording, retention summary, privacy contact, rights contact, and customer
policy link. The reporter-contact checkbox follows this notice, so opt-in is never requested before
the notice is available.

The widget must not link the base/contact notice to Fidibeki's policy. Fidibeki's own policy may be
added as a separate second-layer service-provider link in a later published-policy slice, but it
cannot replace the customer policy.

## Public Configuration And Fail-Closed Behavior

Widget configuration schema version `3` adds a root `controller_profile` object:

- `version`;
- `name`;
- `privacy_contact_email`;
- `privacy_rights_email`;
- `privacy_policy_url`.

The object is present only when the profile is complete. A public rule sets `collect_contact=true`
only when:

- the stored rule enables it;
- the controller profile is complete;
- every public rule locale contains the three notice summaries.

This preserves anonymous feedback on legacy/incomplete configuration while preventing new contact
data from being collected under the wrong policy. The backoffice shows the missing profile/locale
requirements and links the operator to organization settings.

## Notice Evidence

The new code-owned reporter notice version is `2026-07-controller-v1`. The widget submits:

- purpose `feedback_updates`;
- notice version;
- resolved locale;
- controller profile version;
- widget definition version;
- granted `true`.

The backend compares both versions with the authoritative current configuration. It rejects stale,
future, missing, or contradictory values. It then builds the evidence snapshot itself; the client
cannot submit arbitrary notice text.

The snapshot is stored as bounded JSONB on both feedback and issue consent state:

```json
{
  "schema_version": 1,
  "notice_version": "2026-07-controller-v1",
  "controller_profile_version": 1,
  "widget_definition_version": 2,
  "locale": "pt-PT",
  "controller_name": "Example",
  "privacy_contact_email": "privacy@example.com",
  "privacy_rights_email": "rights@example.com",
  "privacy_policy_url": "https://example.com/privacy",
  "purpose_summary": "...",
  "lawful_basis_summary": "...",
  "retention_summary": "..."
}
```

All strings have source-level and database-level bounds. The snapshot contains controller business
contact data only; it never contains the reporter email, feedback, attachment data, IP, user agent,
or analytics identifier.

The existing `2026-07` evidence remains valid for already-consented reporters. It is not accepted
for new submissions after this slice: cached old widget assets fail closed and must refresh their
configuration/assets before collecting contact. New widget assets always submit the v1 controller
notice. Notification and merge logic recognizes both stored versions, requires the authoritative
snapshot for the new version, and never rewrites legacy evidence.

## Database Migration

Migration `103` is reserved for this slice because migration `102` is assigned to the parallel
platform-admin magic-link retention slice.

Migration `103`:

- adds the two tenant contact fields and version counter;
- adds `reporter_consent_notice_snapshot` JSONB to `feedbacks` and `issues`;
- replaces consent consistency constraints with additive legacy/new-version branches;
- rebuilds active-consent indexes to recognize both supported versions;
- leaves all legacy evidence and timestamps unchanged.

No new PostgreSQL table is added, so the executable retention registry table count is unchanged by
this slice.

## Backoffice

Organization settings add dedicated privacy and rights email inputs beside the existing customer
policy field. The rule content editor adds the three localized notice summaries and renders them
only when relevant. The widget preview includes the same compact disclosure and customer policy
link used at runtime.

No card nesting or marketing layout is introduced. The controls stay in the existing dense
organization and rule editing surfaces.

## Testing

Backend coverage:

- migration up/down/up and legacy evidence preservation;
- privacy profile normalization, HTTPS safety, version increments, no-op updates, and audit data;
- public config omission when any controller/locale field is missing;
- contact consent acceptance only for matching controller/rule versions and locale;
- server-built snapshot contents and bounds;
- legacy evidence compatibility;
- issue create/link/merge consistency and notification eligibility;
- tenant isolation.

Widget coverage:

- no Fidibeki policy URL in base/contact markup;
- controller policy and contacts rendered from configuration;
- disclosure appears before contact opt-in;
- contact omitted when controller notice is incomplete;
- locale switching updates all summaries;
- submitted evidence carries matching versions, not notice text.

Backoffice coverage:

- organization privacy fields;
- localized summary editing and validation;
- runtime-equivalent preview;
- desktop and mobile layout without overflow.

Run focused tests, full backend/widget/web tests, typecheck, lint, and Playwright screenshots. Any
authenticated staging E2E remains an explicit staging gate; this slice does not deploy itself.

## Non-Goals

- Publishing Fidibeki's own privacy notice or a DPA.
- Approving lawful bases, retention periods, provider contracts, or transfer mechanisms.
- Replacing replay/analytics consent ledgers or AI authorization.
- Attachment-specific second-layer expansion beyond the configured base summary.
- A general notice CMS, approval workflow, or historical template browser.
- Production work.
