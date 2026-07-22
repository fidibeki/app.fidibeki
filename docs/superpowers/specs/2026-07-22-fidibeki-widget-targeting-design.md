# Fidibeki Widget Targeting Design

**Status:** Approved
**Scope:** Product Slice 3B, following the completed widget state and recurrence slice
**Deployment:** Staging only; production remains untouched

## Problem

Fidibeki already supports one `all_pages`, exact URL, URL prefix, or URL regular-expression matcher
per widget rule. The matching logic works, and the backend rejects syntactically invalid regular
expressions, but the current model cannot express exclusions or combine several relevant pages in a
single rule. The project editor also defaults new rules to all pages without an explicit targeting
decision and offers no way to test an unsaved rule against a representative URL.

The current widget applies matching to the complete browser URL. Query parameters and fragments can
therefore make two visits to the same page behave differently. Product state and targeting should
instead share one page concept: origin and normalized pathname, with query parameters and fragments
ignored.

## Goals

- Let one widget rule contain multiple include and exclude conditions.
- Show a rule when at least one include matches and no exclude matches.
- Preserve all existing rules by backfilling their current matcher as one include condition.
- Make all-pages targeting an explicit choice in project setup and first-project onboarding.
- Canonicalize pages consistently across targeting, the URL tester, and recurrence state.
- Reject malformed or unsafe regular expressions before they reach the public widget.
- Let an operator test an unsaved rule against an absolute HTTP(S) URL before saving.
- Keep tenant and public API changes additive during the compatibility window.

## Non-Goals

- Query-parameter or fragment targeting.
- Conditional targeting based on identity, cookies, DOM content, device, or personal fingerprinting.
- Changes to trigger timing, recurrence policies, widget funnel analytics, or notification delivery.
- Production deployment or production configuration.

## Canonical Page Identity

Canonicalization parses an absolute HTTP(S) URL and produces:

```text
origin + normalized pathname
```

The algorithm:

1. Require the `http:` or `https:` scheme.
2. Use the URL parser's normalized origin.
3. Discard the complete query string and fragment.
4. Remove all trailing slashes from the pathname, except that the root remains `/`.
5. Preserve pathname case, percent encoding, and internal repeated slashes.

Examples:

| Input | Canonical target |
| --- | --- |
| `https://site.test/chart?asset=BAI#price` | `https://site.test/chart` |
| `https://site.test/chart/?asset=BIC` | `https://site.test/chart` |
| `https://site.test/` | `https://site.test/` |
| `https://site.test/Chart` | `https://site.test/Chart` |

The widget's local state stores only the normalized pathname because browser storage is already
scoped to the origin. Targeting and the URL tester compare the full canonical target.

Exact and prefix condition values are canonicalized when saved. Prefix retains the current literal
`startsWith` semantics. A regex pattern is stored unchanged but always runs against the canonical
target, so it can never observe a query parameter or fragment.

## Matching Semantics

Each enabled widget rule has:

- one to twenty include conditions;
- zero to twenty exclude conditions.

A condition uses the existing matcher types: `all_pages`, `url_exact`, `url_prefix`, or
`url_regex`. `all_pages` is valid only as the sole include condition. It may be combined with
excludes, enabling configurations such as all pages except account administration. It is never
valid as an exclude.

For one rule:

```text
eligible = any(include matches) && !any(exclude matches)
```

An exclude always wins within its rule. After eligibility is calculated, the existing rule priority
and stable ordering choose the first eligible rule. Conditions do not have independent priority.

If a rule contains a regex condition and its safe regex module cannot be loaded or compiled, the
entire rule is ineligible. This prevents an unavailable regex exclude from accidentally exposing the
widget on a page that should have been excluded. The widget reports the technical failure when error
reporting is configured and never disrupts the host page.

## Safe Regular Expressions

The backend continues to compile patterns with Go `regexp`, which follows RE2 syntax and linear-time
matching principles. Browser evaluation uses the pure JavaScript `re2js` engine without optional
lookbehind extensions, keeping the supported syntax aligned with Go and avoiding native JavaScript
backtracking and ReDoS. RE2 is designed to match untrusted expressions in time linear to input
length, and RE2JS implements the same DFA/NFA architecture for browsers:

- <https://github.com/google/re2#readme>
- <https://github.com/le0pard/re2js#readme>

The widget build exposes regex evaluation as a separate lazy-loaded module. Rules containing only
all-pages, exact, or prefix conditions do not download the regex engine. A regex is limited to 2048
characters, matching the current contract. Invalid or unsupported syntax is rejected by tenant CRUD
and displayed inline in Dispatch.

A shared, data-only contract fixture covers canonicalization and matching cases. Go backend tests,
widget tests, and web tester tests all consume those cases so language-specific implementations
cannot silently diverge.

## Data Model

Migration 48 introduces `widget_rule_target_conditions`:

| Column | Purpose |
| --- | --- |
| `id` | UUID primary key |
| `widget_rule_id` | Cascading foreign key to `widget_rules` |
| `kind` | `include` or `exclude` |
| `matcher_type` | Existing matcher enum |
| `matcher_value` | Nullable matcher value |
| `position` | Stable display/evaluation order |
| `created_at`, `updated_at` | Audit timestamps |

Database checks enforce null values for `all_pages`, non-empty values for other types, non-negative
positions, and uniqueness of a rule/kind/position tuple. Tenant isolation and runtime grants follow
the existing widget-rule ownership path.

The migration creates one include condition from every existing rule's `matcher_type` and
`matcher_value`. Exact and prefix values are canonicalized when they are valid absolute HTTP(S)
URLs. Regex patterns are copied unchanged. Existing relationships and rule IDs are preserved.

The legacy `widget_rules.matcher_type` and `matcher_value` columns remain during the compatibility
window. New writes project the first include condition back into these fields. They are no longer
authoritative when condition arrays are present and are not removed in this slice.

## API Contract

Tenant widget-rule create, update, get, and list responses add:

```json
{
  "include_conditions": [
    { "id": "...", "matcher_type": "url_prefix", "matcher_value": "https://site.test/docs", "position": 0 }
  ],
  "exclude_conditions": [
    { "id": "...", "matcher_type": "url_exact", "matcher_value": "https://site.test/docs/private", "position": 0 }
  ]
}
```

Create and update requests may provide condition arrays without IDs. When arrays are present they
are authoritative and replace the complete condition set in the same transaction as the rule
update. Indexed validation errors identify the failing array item and field. At least one valid
include is required.

Legacy requests containing only `matcher_type` and `matcher_value` remain accepted and are converted
to one include. Responses continue returning the legacy fields as an additive compatibility
projection.

The public widget configuration adds ordered `include_conditions` and `exclude_conditions` without
removing its current matcher fields. During rollout the widget first gains support for optional
condition arrays and falls back to the legacy matcher when they are absent. Only after that widget is
healthy may the API and Dispatch expose multi-condition editing.

## Dispatch Experience

The existing project rule editor keeps its dense, unframed Dispatch layout. Page matching becomes:

- an explicit `All pages` / `Specific pages` segmented choice;
- an include list with matcher type, value, and icon-only remove control;
- an exclude list with the same compact row pattern;
- icon-plus-text commands for adding include or exclude conditions;
- a URL tester below the lists.

New rules have no confirmed targeting choice. Selecting all pages creates the sole all-pages include;
selecting specific pages creates the first empty include row. Save remains unavailable until the
choice and every visible condition are valid. Existing rules open with their migrated selection.

The first-project onboarding asks the same explicit targeting question before it creates the initial
rule. It must not silently create an all-pages rule. This does not rework the rest of onboarding.

The tester accepts an absolute HTTP(S) URL and evaluates the current unsaved form. Its compact result
shows one of:

- included and eligible;
- included but excluded, identifying the matching exclude;
- no include matched;
- invalid URL or invalid condition.

Testing is optional and never performs an API request. Saving is blocked by invalid form state, not
by the absence of a test run.

## Error Handling

- Tenant CRUD returns `400 validation_failed` with indexed condition fields for invalid counts,
  combinations, URLs, or regex syntax.
- Exact and prefix values must be absolute HTTP(S) URLs and are stored canonically.
- Public configuration never emits disabled rules or orphaned conditions.
- The widget treats malformed public conditions as a configuration error and skips that rule.
- Regex module load/compile failure skips the complete affected rule, reports asynchronously, and
  leaves the host application untouched.
- Storage, recurrence, feedback submission, and attachment behavior remain unchanged.

## Testing

Backend coverage includes:

- migration up/down/up and backfill preservation;
- condition constraints, limits, ordering, replacement, and tenant isolation;
- legacy request compatibility and additive responses;
- exact/prefix canonicalization and Go regex validation;
- public configuration ordering and disabled-rule filtering;
- all shared contract fixtures.

Web coverage includes:

- form conversion between all-pages and specific modes;
- include/exclude add, remove, validation, and request normalization;
- URL tester decisions using unsaved values;
- explicit first-rule targeting in onboarding;
- desktop and mobile Playwright journeys and visual checks.

Widget coverage includes:

- canonical URL identity, including query/hash/trailing-slash equivalence;
- include-any/exclude-any behavior and priority across rules;
- lazy regex loading, safe syntax parity, and fail-closed errors;
- fallback to legacy public configuration;
- navigation across equivalent and distinct canonical pages.

After all local suites pass, staging receives the compatible widget first, then API/migration, then
web. A controlled staging smoke creates or updates a temporary rule, tests representative URLs,
loads the real public widget, and restores or removes temporary data. Production is not deployed or
modified.

## Follow-On Work

Slice 4 may use the same canonical page and rule-condition decision to attribute `matched` funnel
events. Later onboarding work can improve examples and empty states without changing this targeting
contract.
