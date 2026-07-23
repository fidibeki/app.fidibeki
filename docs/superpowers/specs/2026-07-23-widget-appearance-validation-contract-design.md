# Widget Appearance And Validation Contract

**Date:** 2026-07-23
**Status:** Approved
**Issues:** #36, #37, #39
**Environment:** Staging-only; no deployment or infrastructure work

## Goal

Make widget appearance colors accessible without rejecting valid brand colors, and make every
actionable appearance or widget-rule validation error visible in the form that owns it.

## Existing Contracts

- `widgetconfig.AppearanceOverrides` is the persisted tenant/project/rule input.
- `widgetconfig.Appearance` is the resolved public output after applying those layers.
- Appearance validation already returns a field map directly in the tenant endpoint.
- Widget-rule validation returns the same field map nested under `details.fields`.
- The web client already has partial field-error parsing, but does not unwrap `details.fields`.
- `WidgetAppearanceEditor` owns the appearance controls but accepts no error contract.
- The widget and backoffice preview hardcode white text on solid accent surfaces.

These contracts are extended in place. No second appearance model, validation envelope, or error
transport is introduced.

## Accent Foreground

`accent_color` remains the only persisted customer input. Any normalized opaque six-digit hex color
is valid. Contrast is enforced on the derived foreground rather than by rejecting the background.

The resolved `widgetconfig.Appearance` adds the additive field:

```json
{
  "accent_color": "#46c754",
  "accent_foreground": "#171a19"
}
```

Resolution considers foreground candidates in deterministic design-system order:

1. Fidibeki ink `#171a19`;
2. white `#ffffff`;
3. black `#000000` only when neither design token reaches WCAG AA.

The first candidate with at least `4.5:1` contrast is selected. White or black guarantees a valid
result for every valid opaque background. The backend is authoritative for saved configuration.
Web and widget clients use the returned value and implement the same pure fallback for unsaved
previews and compatibility with older API responses.

`accent_foreground` is resolved output, not an override. It is not stored and requires no migration.
Every solid accent surface uses it, including widget launchers, primary actions, and backoffice
previews. The appearance editor shows the resulting text treatment and contrast ratio.

## Structured Field Errors

One pure parser accepts the existing API variants:

- direct maps: `details: { "accent_color": "..." }`;
- nested maps: `details: { "fields": { "localizations.pt...": "..." } }`;
- array entries with `field` plus a message or stable reason, for compatibility.

Only structured validation details are rendered as field copy. Unknown and non-validation errors
continue through the localized safe fallback. Multiple fields are preserved.

### Settings ownership

Settings uses separate state for:

- Reporter Access errors;
- Widget Defaults field errors and form fallback;
- existing member, key, and integration modal errors.

`WidgetAppearanceEditor` receives a plain field-error map. It marks the matching control with
`aria-invalid`, connects an adjacent error with `aria-describedby`, and emits the edited field so
the owner clears only that error. Unknown errors stay inside Widget Defaults.

### Widget-rule ownership

The rule form owns a field-error map and a form fallback. Known controls declare their backend path.
Nested response-block and localization controls declare dynamic paths based on index, locale, block
key, and option.

After a failed save:

1. preserve every returned field in an accessible summary;
2. switch to the first affected localization when its path identifies a configured locale;
3. focus and scroll the first mapped invalid control after rendering;
4. retain unmapped fields in the summary rather than dropping them.

Delegated input/change handling clears only the edited path. Appearance and targeting child
components emit their edited backend path. A successful save or a newly selected rule clears the
entire validation state.

## Accessibility And Error Behavior

- Field errors have stable IDs and are referenced by their control.
- The summary is an alert and links to mapped invalid controls.
- Native constraints remain useful, but backend validation stays authoritative.
- Focus changes only after a failed submit, never while the user is typing.
- Appearance success continues to use the shared toast from #34.
- Server and transport failures render as localized form-level errors in the owning form.

## Compatibility

- Existing saved colors resolve without data changes.
- Older API responses without `accent_foreground` remain usable through client fallback.
- Existing legacy rule fields and appearance overrides remain accepted.
- The API change is additive.

## Verification

- Backend unit coverage: normalization, invalid hex, light/dark colors, token-boundary colors,
  `#46c754`, and fallback candidate behavior.
- Backend HTTP/project coverage: additive resolved foreground in tenant/project/widget responses.
- Web unit coverage: field-error envelope parsing, foreground derivation, layered appearance
  resolution, and unknown-error fallback.
- Component-level browser coverage: appearance field messages and accessible associations.
- Widget unit/E2E: light and dark foreground selection and CSS variable use.
- Backoffice E2E: appearance error ownership, nested
  `localizations.pt.block_labels.feedback`, focus/clear/retry, and `#46c754` preview rendering.
- Full relevant backend, web, and widget type/unit/build gates before closure.

## Non-Goals

- No customer-selectable foreground override.
- No appearance migration.
- No redesign of all forms or all legacy field-error consumers.
- No staging deployment unless separately required to unblock staging use.
