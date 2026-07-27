# Anonymous Replay Presentation Implementation

> Source design:
> `docs/superpowers/specs/2026-07-27-anonymous-replay-presentation-design.md`

## Durable Decisions

- Preserve anonymity by replacing raw presentation sources with generated computed presentation;
  never restore original classes, stylesheets, URLs, attributes, canvas pixels, or unmasked text.
- Use `record.mirror` as the only bridge from serialized rrweb node IDs to live elements.
- Emit a canonical inline `style` containing only property-specific allowlisted values.
- Validate the same contract independently in TypeScript and Go, with shared valid/invalid fixtures.
- Keep replay privacy version two; existing recordings remain playable but cannot regain discarded
  presentation.
- Validate and deploy only to staging. Production is out of scope.

## Tracer Bullets

1. Add `contracts/replay-presentation.json` with representative valid canonical styles and invalid
   identity/URL/string/order/size cases. Make both widget and backend tests consume it.
2. RED: add widget unit coverage for converting a class-driven live element to presentation-only
   attributes. GREEN: add a focused presentation builder with fixed property order, per-property
   normalization, generic font-family classification, and size limits.
3. RED/GREEN: extend full snapshots, added nodes, and attribute mutations to accept a node resolver
   and apply the same generated presentation contract. Cover missing nodes and thrown style reads
   as marker-only fail-closed behavior.
4. RED/GREEN: pass `record.mirror.getNode` from the recorder and verify the real capture path emits
   presentation without original identity-bearing attributes.
5. RED: extend the existing Playwright sentinel fixture with class-driven layout and decoration,
   replay the captured events through rrweb `Replayer`, and assert layout, dimensions, colors,
   nonblank pixels, payload bounds, and absence of every sentinel. GREEN: correct capture behavior
   until the real rendered proof passes.
6. RED/GREEN: extend `validReplayAttributes` in the backend with a strict canonical style parser.
   Apply it unchanged to full snapshots, added nodes, and mutation attributes; cover the shared
   fixture plus direct adversarial cases.
7. Update the replay threat model and privacy data map to describe generated computed presentation,
   its exclusions, legacy-session limitation, and backend verification boundary.
8. Run focused tests after every tracer bullet, then full widget unit/typecheck/build/Chromium E2E,
   full backend tests/vet/format checks, and repository CI.
9. Review the complete diff independently, integrate to `main`, deploy the widget/API/worker path
   only to staging, record and inspect a new staging replay, then close issue #108 with evidence.

## Acceptance

- [ ] A newly captured replay reconstructs useful layout, spacing, colors, borders, and basic
      typography instead of raw document flow.
- [ ] Text and inputs remain masked; raw class/style/CSS/URL/attribute/canvas/font/media identity
      does not occur in stored events.
- [ ] Widget and backend agree on the canonical presentation contract through shared fixtures.
- [ ] Invalid presentation causes backend batch rejection and never falls back to raw data.
- [ ] Full snapshots, added nodes, and class/inline-style attribute mutations preserve current safe
      presentation.
- [ ] Browser capture-to-replay proof passes at desktop and mobile fixture sizes and includes a
      nonblank pixel assertion.
- [ ] Existing privacy-version-two sessions remain playable; old degraded sessions are explicitly
      documented as unrecoverable.
- [ ] Focused and full widget/backend suites, CI, and staging validation pass.
- [ ] No production deployment or infrastructure change is performed.

