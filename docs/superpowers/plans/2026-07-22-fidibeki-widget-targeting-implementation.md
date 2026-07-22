# Fidibeki Widget Targeting Implementation Plan

**Design:** `docs/superpowers/specs/2026-07-22-fidibeki-widget-targeting-design.md`
**Scope:** Targeting conditions and URL tester within product Slice 3B
**Deployment:** Staging only; production remains untouched

## Architectural Decisions

- Page targeting compares an absolute HTTP(S) origin plus normalized pathname. Query parameters and
  fragments are discarded, and trailing slashes are removed except for the root pathname.
- One enabled rule is eligible only when any include condition matches and no exclude condition
  matches. Excludes always win and existing rule priority still chooses the first eligible rule.
- Rules contain one to twenty includes and zero to twenty excludes. `all_pages` is valid only as the
  sole include and can be combined with excludes; it is never a valid exclude.
- Migration 48 stores ordered conditions in a normalized child table and backfills one include for
  every existing rule. Legacy matcher columns stay in place as an additive compatibility projection.
- Tenant and public APIs add ordered condition arrays while accepting legacy matcher-only requests.
- Exact and prefix values are canonicalized on write. Go `regexp` validates regular expressions;
  the browser evaluates them with a lazy-loaded RE2-compatible module and fails the complete rule
  closed if that module cannot load or compile.
- Backend, widget, and web consume one data-only contract fixture so canonicalization and matching
  semantics remain aligned without sharing runtime code.
- Dispatch and first-project onboarding require an explicit All pages or Specific pages decision.
  The URL tester evaluates unsaved form state locally and never calls the API.
- Each task starts with a focused failing test. Production is never deployed or modified.

## Task 1: Establish The Cross-Runtime Targeting Contract

**Depends on:** None

**Outcome:** A shared fixture defines canonical URL identity, exact/prefix/regex behavior,
include-any/exclude-any decisions, invalid inputs, and trailing-slash equivalence for all runtimes.

**Acceptance:** Focused Go, widget, and web tests load the same fixture and initially expose any
semantic differences instead of maintaining three independent sets of examples.

## Task 2: Make The Widget Understand Additive Conditions

**Depends on:** Task 1

**Outcome:** The deployed widget can evaluate optional include/exclude arrays, canonicalizes the
current page, preserves legacy matcher fallback, and selects rules using existing priority order.

**Acceptance:** Widget unit and browser tests prove any-include/no-exclude semantics, all-pages with
exclusions, query/hash/trailing-slash equivalence, malformed-condition rejection, legacy fallback,
and unchanged behavior for rules without regular expressions.

## Task 3: Evaluate Regular Expressions Safely And Lazily

**Depends on:** Task 2

**Outcome:** Regex-bearing rules load a browser-safe RE2-compatible evaluator only when required;
load or compilation failures skip the complete rule and report the technical error without affecting
the host page.

**Acceptance:** Tests prove supported syntax parity with backend fixtures, no regex chunk request for
exact/prefix-only rules, one lazy load for regex rules, and fail-closed handling for both include and
exclude failures. Typecheck, build, bundle inspection, and widget Playwright pass.

## Task 4: Persist Ordered Target Conditions Without Data Loss

**Depends on:** Task 1

**Outcome:** Migration 48 adds the condition kind and normalized child table, constraints, grants,
tenant isolation, and one ordered include backfilled from every existing widget rule.

**Acceptance:** Migration tests prove constraints, ordering, cascading deletion, tenant isolation,
legacy-rule preservation, canonicalization of valid exact/prefix values, unchanged regex values, and
a clean up/down/up cycle ending at version 48.

## Task 5: Expose Conditions Through Compatible APIs

**Depends on:** Task 4

**Outcome:** Tenant create/update/list/get and public configuration expose ordered condition arrays.
Array writes replace conditions transactionally and project their first include into legacy fields;
legacy-only requests still create one include.

**Acceptance:** Focused Go validation, store, and HTTP integration tests cover indexed errors,
cardinality and all-pages rules, URL canonicalization, safe regex syntax, transactional replacement,
tenant isolation, legacy compatibility, public ordering, and disabled-rule filtering.

## Task 6: Configure And Test Targeting In Dispatch

**Depends on:** Task 5

**Outcome:** The project editor presents explicit All pages or Specific pages targeting, ordered
include/exclude rows, compact add/remove controls, inline validation, and a local tester for the
current unsaved configuration.

**Acceptance:** Web unit and component tests cover form/API conversion, limits, invalid combinations,
canonicalization, indexed errors, and all tester outcomes. Desktop and mobile Playwright prove that
an operator can create, edit, test, save, and reopen a multi-condition rule without layout overlap.

## Task 7: Require Targeting During First-Project Onboarding

**Depends on:** Task 6

**Outcome:** Onboarding pauses after project creation for an explicit All pages or Specific pages
choice, validates the selected conditions, then creates the first rule and continues installation.

**Acceptance:** Unit and activation-flow Playwright tests prove no rule is created before the choice,
both valid paths submit the new additive contract, invalid specific targeting blocks progress, and a
retry preserves the operator's selection.

## Task 8: Regress Navigation, State, And Staging Rollout

**Depends on:** Tasks 2-7

**Outcome:** Canonical targeting and recurrence state agree across equivalent URLs while distinct
paths remain independent, and the complete Slice 3B journey works on staging.

**Acceptance:** Backend migration/full tests, web unit/type/build/Playwright, widget
unit/coverage/type/build/Playwright, and desktop/mobile visual checks pass. Staging receives the
compatible widget first, then API/migration, then web; a controlled smoke test covers include/exclude
and URL equivalence, then restores temporary data. Production remains untouched.
