# Edge-Tab Launcher Design

**Issue:** #71
**Status:** Approved under the founder's standing authorization for recommended decisions
**Environment boundary:** Local implementation and staging validation only; no deployment,
production, or infrastructure work

## Problem

The `launcher` presentation currently has one closed state: a horizontal floating button near a
bottom corner. Fidibeki needs a quieter default that remains discoverable without occupying as much
of the host page. The new edge tab is an appearance variant of `launcher`, not a presentation or
trigger. Delay, scroll, exit-intent, targeting, policy, analytics, suppression, recurrence,
submission, consent, and the opened feedback panel must remain shared.

## Approaches

### 1. Typed launcher variant on `WidgetDefinition` (selected)

Add `launcher_variant: edge_tab | floating_button` through the domain model, database, tenant API,
public widget configuration, TypeScript contracts, editor, preview, and runtime. The field is
validated with the other definition fields and can evolve without overloading presentation or
appearance JSON.

This is the clearest contract and the only option that makes invalid values fail at save time while
keeping the existing `presentation: launcher` behavior intact.

### 2. Store the variant in appearance overrides

This avoids a dedicated column, but appearance overrides currently model inherited visual tokens.
A behavioral mode hidden in that JSON would weaken typing, validation, backfill semantics, and
public-contract clarity.

### 3. Add a fifth presentation

This would be easy to branch on in the runtime, but it would duplicate launcher behavior and
contradict the approved product model. An edge tab opens the same launcher panel; it is not a new
feedback presentation.

## Domain And Compatibility

Introduce:

```text
LauncherVariantEdgeTab        = "edge_tab"
LauncherVariantFloatingButton = "floating_button"
```

`launcher_variant` is present on `WidgetRule`, `WidgetRuleInput`, normalized values, the tenant API,
and the public widget configuration. An omitted or empty input normalizes to `edge_tab`; unknown
values produce a field-level `launcher_variant` validation error.

Migration `107` adds a constrained, non-null text column:

1. existing rows are backfilled to `floating_button`, preserving every currently installed
   widget's closed state;
2. the database default becomes `edge_tab` for future rows;
3. a check constraint permits only the two documented values.

The API normalization remains authoritative. A new or replacement payload that omits the field
resolves to `edge_tab`, as required by the public contract. Existing persisted rows remain explicit
`floating_button` until a customer changes or replaces their configuration.

The definition version continues to increment through the existing update path. No new endpoint,
provider call, feature flag, trigger type, or analytics dimension is introduced.

## Runtime

The widget renders the same semantic `<button type="button">` with the localized `launcher_label`,
accessible foreground, accent, theme, side, focus behavior, and event handler for both variants.
Only a variant class/data attribute and closed-state geometry differ.

### Desktop

- `edge_tab` is flush with the configured left or right viewport edge;
- it is centered vertically within the usable viewport, with enough top/bottom clearance to avoid
  browser controls and common fixed navigation;
- the label uses vertical writing and a bounded height;
- the exposed corners are rounded while the edge-attached corners remain flush;
- its interactive box is at least 44 by 44 CSS pixels.

### Mobile

At `480px` and below, `edge_tab` becomes a compact horizontal edge-attached control near the lower
portion of the configured side. This avoids a tall vertical obstruction, preserves a 44px target,
and allows long localized labels to wrap without clipping. It remains visually distinct from the
inset floating button.

`floating_button` keeps the current desktop and mobile geometry.

Activating either variant calls the existing launcher-open path. The closed button is hidden and
the same panel becomes visible. Closing, submitting, reopening after policy permits, localization
changes, consent, attachment retries, and cleanup use existing code.

## Analytics And State

No event schema changes:

- `impression` occurs only when the selected launcher is actually visible;
- the first activation emits the existing deduplicated `interaction`;
- opening does not suppress the widget;
- `submitted`, `dismissed`, cooldown, never-show, and session recurrence semantics are unchanged;
- switching variants does not create a second event path or idempotency identifier.

Tests must compare both variants through the same activation and lifecycle callbacks, not duplicate
the analytics implementation.

## Backoffice

The rule editor shows the choice only when `presentation` is `launcher`. It uses a two-option visual
mode selector:

- **Edge tab** first, marked Recommended;
- **Floating button** second, described as the existing inset option.

Both options use familiar icons from the existing Lucide dependency, native radio semantics, visible
focus, and localized English/Portuguese labels. The selected value is always included in create and
update payloads.

The preview's closed state renders the real side, accent, foreground, label, and responsive
geometry for the selected variant. Its Open mode continues to show the unchanged panel. Changing
presentation resets only preview open/closed state; changing launcher variant does not discard
other form values.

## Validation And Failure Behavior

- Invalid variants fail with `422 validation_error` at `launcher_variant`.
- The database constraint prevents invalid bypass writes.
- A runtime configuration compiled by an older backend or manually missing the field safely
  defaults to `edge_tab`.
- The variant has no effect for `inline`, `reaction`, or `prompt`; retaining it in their stored
  definition is harmless and makes switching back to launcher deterministic.
- Invalid or missing localization continues through the existing localization fallback.

## Verification

### Backend

- normalization defaults omitted input to `edge_tab`;
- both values validate and serialize;
- unknown values fail at the exact field;
- create/update/list/get/public configuration preserve the field;
- migration up proves existing rows become `floating_button`, new rows default to `edge_tab`, and
  invalid SQL writes fail;
- migration down/up remains compatible.

### Widget

- unit tests cover missing-field default, class/attribute selection, side, accent, label, focus, and
  one interaction event;
- Playwright covers both variants, left/right placement, keyboard activation, panel parity,
  submit/dismiss/recurrence parity, long Portuguese labels, no page-level overflow, and desktop plus
  mobile geometry;
- the Shadow DOM and host page remain layout-isolated.

### Backoffice

- unit/type coverage includes default form values and exact API payloads;
- responsive Playwright covers the visual selector and closed/open preview for both variants;
- preview and shipped widget geometry are compared through stable attributes and bounded layout
  assertions rather than screenshot-only evidence.

All backend, widget, and web gates run locally. Staging validation can follow when the current
no-deployment constraint is lifted or the exact commit already exists there.

## Non-Goals

- A new presentation, trigger, targeting mode, analytics event, or recurrence rule.
- Arbitrary CSS or customer-supplied launcher markup.
- Drag positioning, free vertical-offset controls, launcher icons, or animation customization.
- Production, deployment, DNS, provider, or infrastructure changes.
