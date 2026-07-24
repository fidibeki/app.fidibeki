# Edge-Tab Launcher Implementation Plan

> **Scope mode:** LOCKED
>
> **Design:** `docs/superpowers/specs/2026-07-24-edge-tab-launcher-design.md`
>
> **Issue:** #71
>
> **Boundary:** local and staging-only product work. No deployment, production, provider, DNS, or
> infrastructure changes.

## Task 1: Prove Migration Compatibility

Add migration `107` with a constrained, non-null `launcher_variant`. Backfill rows that existed
before the migration to `floating_button`, then make `edge_tab` the database default for future
rows. Extend migration integration coverage to prove old-row preservation, new-row defaulting,
invalid-value rejection, and down/up compatibility.

**Acceptance:** The focused migration test demonstrates all four behaviors against PostgreSQL.

## Task 2: Carry The Typed Contract End To End

Add the two-value launcher variant type to project domain/input/value models. Normalize omitted
input to `edge_tab`, reject unknown values at `launcher_variant`, and include the field in every
widget-rule create/update/select/scan path and public widget configuration. Extend backend unit and
HTTP/PostgreSQL tests for defaulting, validation, create/update/list/get, and public serialization.

**Acceptance:** A public configuration created without the field contains `edge_tab`; an existing
backfilled rule remains `floating_button`; invalid input returns the exact field error.

## Task 3: Render Both Variants Through One Launcher Path

Extend widget types and markup with a launcher-variant class/data attribute. Keep one semantic
button, activation handler, panel, funnel path, and lifecycle implementation. Add desktop edge-tab,
mobile compact edge treatment, left/right geometry, focus, long-label containment, and the
missing-field `edge_tab` runtime fallback while retaining the current floating-button CSS.

**Acceptance:** Widget tests activate both variants and observe one interaction plus the same panel
and submit/dismiss/recurrence behavior, with no document overflow at desktop or mobile widths.

## Task 4: Add The Editor Selector And Faithful Preview

Extend web API types, rule defaults, clone/load/save payloads, localized English/Portuguese copy,
field-error mapping, and the launcher-only appearance editor. Use an accessible two-option visual
selector with Edge tab marked Recommended. Make the closed preview reflect selected variant,
effective side, accent, foreground, locale, and responsive geometry; keep the open preview on the
existing shared panel.

**Acceptance:** Backoffice unit/E2E tests switch both variants, persist exact payloads, and verify
closed/open preview geometry on desktop and mobile with a long Portuguese label.

## Task 5: Reconcile Records And Run Complete Gates

Record #71 in `plans/fidibeki.plan.md` without claiming staging validation. Run formatting and
diff checks; backend focused/full tests, vet, build, migration up/down/up; widget unit coverage,
typecheck, build, and focused/full Playwright; web unit, typecheck, build, and focused/full
Playwright. Inspect desktop/mobile screenshots and assert document-width containment.

**Acceptance:** Every local gate passes, the worktree is clean, and the delivery contains a commit
plus a concise handoff listing behavior, migrations, tests, residual staging validation, and exact
commit hash. Do not push, open a PR, merge, or deploy.
