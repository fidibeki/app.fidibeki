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

**Status:** Complete. Migration `107` added; `TestMigrationsEnforceTenantIsolation` extended to
insert a new row after the migration (asserts `edge_tab` default), roll back to before the
migration and reapply it against a pre-existing row (asserts backfill to `floating_button`), and
reject an invalid/`NULL` `launcher_variant` write. The existing final `migrator.Down()`/`Up()`
cycle in the same test covers down/up compatibility. Verified locally against a real PostgreSQL
16 testcontainer.

## Task 2: Carry The Typed Contract End To End

Add the two-value launcher variant type to project domain/input/value models. Normalize omitted
input to `edge_tab`, reject unknown values at `launcher_variant`, and include the field in every
widget-rule create/update/select/scan path and public widget configuration. Extend backend unit and
HTTP/PostgreSQL tests for defaulting, validation, create/update/list/get, and public serialization.

**Acceptance:** A public configuration created without the field contains `edge_tab`; an existing
backfilled rule remains `floating_button`; invalid input returns the exact field error.

**Status:** Complete. `WidgetLauncherVariant` added to `WidgetRule`, `WidgetRuleInput`, and
`WidgetRuleValues`; normalization defaults an omitted/empty value to `edge_tab`; `validateDefinition`
rejects unknown values at `launcher_variant`. Insert/update SQL, column lists, and scan order carry
the field; `PublicWidgetRule`/`publicWidgetRule()` project it into the public contract. Unit tests
cover defaulting and both accepted values; the HTTP integration test asserts the field on
create/update/get and in `LoadPublicConfiguration`; the widgetapi test asserts the public payload.
`go test ./...`, `go vet ./...`, and `go build ./...` pass.

## Task 3: Render Both Variants Through One Launcher Path

Extend widget types and markup with a launcher-variant class/data attribute. Keep one semantic
button, activation handler, panel, funnel path, and lifecycle implementation. Add desktop edge-tab,
mobile compact edge treatment, left/right geometry, focus, long-label containment, and the
missing-field `edge_tab` runtime fallback while retaining the current floating-button CSS.

**Acceptance:** Widget tests activate both variants and observe one interaction plus the same panel
and submit/dismiss/recurrence behavior, with no document overflow at desktop or mobile widths.

**Status:** Complete. `WidgetLauncherVariant` type added; `mountWidget` resolves
`options.rule.launcher_variant ?? 'edge_tab'` and threads it into `markup()` (variant class +
`data-launcher-variant`) unchanged elsewhere. Desktop CSS flushes `edge_tab` to the configured
side, vertically centers it with a bounded `max-height` and `writing-mode: vertical-rl` label with
asymmetric rounded corners; a `max-width:480px` override switches it to a compact horizontal
edge-attached control with wrapping text. `floating_button` CSS is unchanged. New unit tests
(`launcher.test.ts`) cover the missing-field default, class/attribute selection, label, accent, and
one deduplicated interaction; a new Playwright file (`launcher-variants.spec.ts`, 13 cases) covers
both variants, left/right placement, keyboard activation, submit/dismiss parity, long Portuguese
wrapping, and no document overflow at desktop/mobile widths. Widget unit/coverage, typecheck, build,
and the full 67-test Playwright suite (chromium + firefox) pass locally.

## Task 4: Add The Editor Selector And Faithful Preview

Extend web API types, rule defaults, clone/load/save payloads, localized English/Portuguese copy,
field-error mapping, and the launcher-only appearance editor. Use an accessible two-option visual
selector with Edge tab marked Recommended. Make the closed preview reflect selected variant,
effective side, accent, foreground, locale, and responsive geometry; keep the open preview on the
existing shared panel.

**Acceptance:** Backoffice unit/E2E tests switch both variants, persist exact payloads, and verify
closed/open preview geometry on desktop and mobile with a long Portuguese label.

**Status:** Complete. `WidgetLauncherVariant`/`launcher_variant` added to `web/types/api.ts`;
`defaultWidgetRule()` defaults to `edge_tab`; `selectRule()` loads `rule.launcher_variant ||
'edge_tab'`; `normalizedRule()` includes it in every save payload via the existing `...ruleForm`
spread. The editor adds a two-option accessible fieldset (native radios, `PanelRightOpen`/
`MessageCircle` icons, Edge tab marked Recommended) shown only when `presentation === 'launcher'`.
The closed preview now carries `data-launcher-variant`/`data-side` and dedicated CSS so it flushes
to the real configured side and shows vertical `edge_tab` geometry; the open preview is unchanged.
EN/PT copy added for the selector label, Recommended badge, and both variant names. New unit
coverage (`widget-setup.test.ts`) and two new Playwright cases cover the selector and the faithful
preview/exact-payload behavior. Web unit, typecheck, Nuxt build, and the full 219-test responsive
Playwright suite (desktop + mobile) pass locally.

## Task 5: Reconcile Records And Run Complete Gates

Record #71 in `plans/fidibeki.plan.md` without claiming staging validation. Run formatting and
diff checks; backend focused/full tests, vet, build, migration up/down/up; widget unit coverage,
typecheck, build, and focused/full Playwright; web unit, typecheck, build, and focused/full
Playwright. Inspect desktop/mobile screenshots and assert document-width containment.

**Acceptance:** Every local gate passes, the worktree is clean, and the delivery contains a commit
plus a concise handoff listing behavior, migrations, tests, residual staging validation, and exact
commit hash. Do not push, open a PR, merge, or deploy.

**Status:** Complete. `plans/fidibeki.plan.md` records `#71` under the post-roadmap issue sweep
without claiming staging validation. Backend `go test ./...`/`go vet`/`go build` and the PostgreSQL
migration integration test (backfill, default, invalid-value rejection, down/up) pass; widget
unit/coverage/typecheck/build and the full 67-test Playwright suite pass; web unit/typecheck/build
and the full 219-test responsive Playwright suite pass. Desktop/mobile screenshots for both
launcher variants and the editor preview were generated locally and visually inspected. No
deployment or staging validation was performed.
