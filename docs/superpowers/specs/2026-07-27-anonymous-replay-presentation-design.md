# Anonymous Replay Presentation

**Date:** 2026-07-27
**Scope:** GitHub issue #108
**Environment:** Implementation and staging validation only; no production deployment
**Decision:** Approved recommended approach under the user's standing approval for recommended plans

## Problem

Session replay reaches the backoffice player and the player reaches `Ready`, but the reconstructed
page is effectively raw HTML: masked text is shown in document flow with none of the original
layout, spacing, typography, or colors.

This is a capture-contract regression, not a storage or player failure. The anonymous replay work
for issue #53 correctly masks text and form values, removes identity-bearing URLs and metadata, and
rejects unsafe events at the backend. It also removes every serialized DOM attribute except
`data-fidibeki-safe-text` and drops stylesheets. Since normal pages obtain nearly all presentation
from classes and stylesheets, rrweb has no presentation information left to reconstruct.

The original requirement for anonymous replay was to preserve useful behavioral and visual context
while failing closed on identity. The current implementation satisfies the anonymity half but not
the useful-context half.

## Goals

- Preserve the page's visual hierarchy, layout, spacing, colors, and basic typography in new
  anonymous replay recordings.
- Keep visible text and input values masked by default.
- Never serialize application class names, IDs, data attributes, ARIA text, URLs, stylesheet
  content, CSS custom properties, generated content, images, fonts, or canvas pixels.
- Validate the presentation contract independently in the widget and backend.
- Keep old privacy-version-two recordings playable; do not delete or invalidate them.
- Prove the result by rendering a captured browser fixture through rrweb, not only by inspecting
  serialized JSON.

## Non-goals

- Recovering presentation information from existing recordings. That information was discarded
  before upload and cannot be reconstructed.
- Pixel-perfect capture of images, SVG artwork, canvas, videos, web fonts, pseudo-elements, CSS
  animations, or hover-only styles.
- Restoring raw class names, inline styles, external stylesheets, or asset URLs.
- Introducing a PII classifier or weakening the anonymous-by-default contract.
- Deploying to production.

## Considered approaches

### 1. Safe computed presentation snapshot

Resolve each serialized rrweb node back to its live DOM element through `record.mirror`, read a
small allowlist of computed presentation properties, convert those values to a canonical inline
`style` string, and discard every original attribute as before.

This preserves useful presentation without retaining the selectors, URLs, arbitrary strings, or
application metadata that produced it. It is the selected approach.

### 2. Restore raw classes, styles, and stylesheets

This offers the best fidelity and the smallest capture-time CPU cost, but arbitrary class names,
inline styles, selectors, CSS content, and URLs can contain identity. It would reverse the guarantee
delivered by issue #53 and is rejected.

### 3. Keep the strict capture and explain it in the player

This remains anonymous but leaves session replay operationally useless. It does not satisfy issue
#108 and is rejected.

## Capture architecture

`sanitizeReplayEvent` gains an optional node resolver:

```ts
type ReplayNodeResolver = (id: number) => Node | null
```

The replay recorder supplies a resolver backed by `record.mirror.getNode(id)`. Unit callers may
omit it, in which case the sanitizer retains today's privacy behavior and emits no presentation
style.

For a full snapshot and every newly added element, the sanitizer:

1. reads the original attributes only to detect the canonical safe-text marker;
2. resolves the serialized numeric node ID to the live element;
3. builds a safe computed presentation string;
4. replaces the complete attribute map with at most:
   - `data-fidibeki-safe-text`, with its existing canonical empty value; and
   - `style`, containing only generated presentation declarations.

For rrweb attribute mutations, the sanitizer resolves the mutation's node ID and replaces the
attribute delta with the element's complete current safe presentation style. This lets class and
inline-style changes affect the replay without serializing their source values. Added subtrees use
the same recursive snapshot path.

Text masking, input masking, URL pinning, blocked subtrees, canvas suppression, safe incremental
sources, consent, batching, and retention remain unchanged.

## Presentation contract

The style builder reads longhand computed values in a fixed order. The initial allowlist covers:

- box generation and positioning: `display`, `position`, `box-sizing`, offsets, `z-index`,
  `visibility`, and `opacity`;
- dimensions and spacing: width/min/max height and width, margins, padding, row/column gap,
  overflow, and aspect ratio;
- flex and grid layout: direction, wrapping, growth/shrink/basis, order, alignment, auto-flow,
  and resolved row/column tracks;
- basic decoration: background color, foreground color, border widths/styles/colors/radii, and
  outline width/style/color;
- basic typography: font size, weight, style, line height, letter spacing, text alignment,
  white-space, text transform, plus a generated generic `serif`, `sans-serif`, or `monospace`
  family classification;
- replaced-element and table layout: object fit/position, border collapse, and table layout;
- numeric transforms: computed matrix and transform origin.

The contract deliberately excludes:

- `background-image`, `list-style-image`, `content`, `cursor`, filters, masks, clip paths, and every
  other URL- or string-bearing property;
- custom properties;
- animation and transition names;
- raw font-family names;
- grid area and line names;
- browser-prefixed or unknown properties.

Values are accepted by property-specific validators, not by a general "safe string" regex:

- keyword properties use closed enumerations;
- lengths and track lists accept only finite normalized numbers with `px`, `%`, or the property's
  explicitly allowed keywords;
- colors accept only canonical `rgb(...)`, `rgba(...)`, or `transparent`;
- transforms accept only `none`, `matrix(...)`, or `matrix3d(...)` with finite numbers;
- the generated font family accepts only the three generic families.

Declarations use lowercase property names, no whitespace around `:`, a single `;` delimiter, fixed
property order, no duplicates, and no trailing arbitrary content. Each value is capped at 256
bytes and each complete style at 4 KiB. Properties that cannot be normalized safely are omitted.
The full event remains subject to the existing 2 MiB batch ceiling.

The widget constructs this canonical form. The backend independently parses the declaration list,
checks property order, applies the same per-property grammar and size limits, and rejects the whole
batch on any violation. Raw `style` attributes from the customer page are never forwarded.

## Failure behavior

- If a node cannot be resolved, its generated style is omitted while all original attributes are
  still removed.
- If reading computed style throws, that element is serialized with the existing marker-only
  contract.
- If one computed property is unsupported or unsafe, only that property is omitted.
- If the backend sees an unknown property, invalid value, duplicate, non-canonical order, or size
  violation, it rejects and deletes the quarantined batch as it does for every other unsafe replay
  event.
- A presentation failure never falls back to raw attributes, styles, CSS, URLs, or text.

## Compatibility and versioning

This is an additive extension of privacy version two: both marker-only attributes and
marker-plus-generated-style attributes satisfy the same anonymity guarantee. The privacy version
does not change, so existing recordings remain playable and eligible under the current retention
rules.

Existing recordings remain visually degraded because their presentation data no longer exists.
Only sessions captured by the updated widget gain the new presentation snapshot.

rrweb remains pinned to `2.1.1`; the generated `style` attribute is already part of rrweb's normal
snapshot format, so the player and merged-object schema need no change.

## Verification

### Widget unit tests

- A styled live element becomes a canonical, presentation-only `style` attribute.
- Raw classes, inline style text, URL values, custom properties, attributes, and sentinel identity
  never survive.
- Unknown or unsafe computed values are omitted.
- Full snapshots, added nodes, and attribute mutations all use the same contract.
- A missing/throwing resolver fails closed to marker-only attributes.
- Style size and ordering limits are deterministic.

### Backend tests

- Accept marker-only and canonical generated presentation.
- Reject raw styles, unknown/duplicate/out-of-order properties, unsafe keywords, URLs, strings,
  non-finite numbers, oversized values, and oversized styles.
- Apply the same validation to full snapshots, added nodes, and attribute mutations.

### Browser proof

Extend the existing sentinel-identity Playwright fixture with class-driven grid/flex layout,
spacing, borders, background colors, and typography. Capture the fixture through the real widget,
then render the uploaded events using rrweb `Replayer`.

Assert:

- no sentinel identity occurs in session requests or uploaded events;
- no original class, ID, URL, raw inline-style string, or stylesheet content occurs;
- replayed elements have the expected layout relationship, dimensions, spacing, and computed
  colors at desktop and mobile fixture widths;
- screenshots contain non-background pixels in the expected content regions;
- batches remain below the existing 2 MiB limit.

### Regression suites

Run widget unit, typecheck, build, and Chromium E2E suites; backend replay tests, full backend tests,
formatting, and vet; then the repository CI. Deploy only the widget, API, and worker components
required by the changed capture/verification path to staging and validate a newly recorded replay
there before closing issue #108.

