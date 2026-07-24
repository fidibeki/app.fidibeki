# Settings Full-Width Panels Design

## Status And Scope

Approved through the founder's standing approval for recommended plans. This local product-quality
slice resolves GitHub issue #70 in the existing Dispatch project editor. It changes no product
behavior, data contract, infrastructure, deployment, or production environment.

## Audit

The replay privacy notices and replay/analytics consent status panels already carry the shared
`wide` class inside the two-column `.project-form-grid`. The grid span selector is incorrectly
limited to `label.wide`, so non-label blocks remain in the first implicit column and leave the
second column empty.

The consent status itself is a flex row. Its text group has `min-width: 0`, but no positive flex
growth, while the adjacent action is fixed-size. On narrow screens the current media rule assigns
grid properties without changing the status row from flex, so the intended stacking rules are
ineffective.

## Decision

Keep the existing markup and Dispatch styling. Define one direct-child grid contract:

- every `.project-form-grid > .wide` spans the complete explicit grid and fills its available width;
- wide children and their text groups use `min-width: 0`;
- the consent status text group grows and may shrink before the action;
- inline copy may wrap safely without creating horizontal overflow;
- at 600 pixels and below, the consent status becomes a one-column grid and its action fills the
  row beneath the icon/copy group.

This direct-child selector is preferred over a global `.wide` utility because `wide` already has
different local meanings elsewhere in the editor. Duplicating selectors for each notice is rejected
because it would preserve the underlying class-contract error.

## Verification

Extend the existing Playwright project-editor coverage with geometry assertions in English and
Portuguese:

- replay privacy notes and replay/analytics consent panels span the grid's useful width;
- desktop status copy retains useful width beside the action;
- mobile copy/action stack without overflow or overlap;
- long Portuguese labels and descriptions remain contained.

Run web typecheck, unit tests, build, focused desktop/mobile browser coverage, and inspect the
resulting screenshots. No staging or production deployment is part of this slice.
