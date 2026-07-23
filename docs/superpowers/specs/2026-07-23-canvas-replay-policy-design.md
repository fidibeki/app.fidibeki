# Canvas Replay Policy Design

**Date:** 2026-07-23  
**Scope:** GitHub issue #41 and Task 39  
**Environment:** Product and staging only; no production or deployment work

## Outcome

Session replay can capture supported Canvas 2D and WebGL output when an operator explicitly enables
it for a widget rule. Existing rules remain unchanged and keep canvas recording disabled.
Sensitive canvases remain excludable with the existing `[data-fidibeki-block]` contract.

## Existing System Audit

The replay path is already complete from the widget to object storage and the backoffice player:

- each rule controls replay sampling, duration, event count, input masking, and text masking;
- the public widget definition projects those settings into the runtime;
- the widget lazy-loads `rrweb` only for an eligible replay rule;
- replay batches are limited to 50 events and 2 MiB each, with 1,000 batches and 128 MiB per
  session as server-side hard limits;
- `[data-fidibeki-block]` already prevents rrweb from serializing a node and its descendants;
- capture errors are reported without blocking the feedback submission;
- `rrweb-player` already supports replaying Canvas mutation events.

The missing behavior is deliberate: `widget/src/modules/replay.ts` hardcodes
`recordCanvas: false`, and there is no persisted rule policy that can override it.

## Considered Approaches

### 1. Record every canvas mutation after one boolean opt-in

This is the most faithful mode, but `sampling.canvas: "all"` can generate an event for every draw
operation. It makes continuously animated 2D and WebGL canvases too expensive and gives operators
no useful resource control.

### 2. Opt in with a bounded capture frame rate

Persist a boolean and a frame-rate setting per rule. Pass `recordCanvas: true` and a numeric
`sampling.canvas` value to rrweb. This captures periodic canvas frames rather than every drawing
call. It is less exact between frames, but is predictable, works for supported 2D/WebGL canvases,
and fits the existing replay limits.

This is the selected approach.

### 3. Require an allowlist attribute on every recorded canvas

This has the strictest privacy default, but requires customers to modify every intended canvas and
would make the rule-level opt-in misleading. Fidibeki already has an explicit subtree exclusion
contract, so an additional inverse selector would add configuration friction without replacing the
need for an operator warning.

## Persisted Contract

Migration `000073` adds two columns to `widget_rules`:

- `replay_record_canvas boolean NOT NULL DEFAULT false`
- `replay_canvas_fps integer NOT NULL DEFAULT 2 CHECK (replay_canvas_fps BETWEEN 1 AND 5)`

The fields are additive across the domain model, create/update input, store, public widget
definition, web API types, and widget runtime types. Existing rows receive `false` and therefore
retain current behavior. The FPS value remains valid while capture is disabled so enabling the
option later does not require another choice.

The rule validator accepts only `1–5 FPS`. Canvas recording is effective only when replay itself is
enabled and available on the tenant plan; a stored canvas preference does not start a separate
recorder.

## Backoffice Experience

The existing Session replay section gains:

- a checkbox labelled `Capture canvas content`, disabled while replay mode is `none`;
- a compact FPS selector shown only when canvas capture is enabled;
- persistent privacy guidance explaining that canvas pixels may contain sensitive information and
  that adding `data-fidibeki-block` to the canvas or an ancestor excludes it.

The controls use the existing field-error and localization contracts. No separate settings page or
new replay mode is introduced.

## Runtime Behavior

The loader includes the two canvas fields in its replay fingerprint so changing the active
definition restarts capture with the new policy.

The recorder maps the policy as follows:

```ts
recordCanvas: rule.replay_record_canvas
sampling: rule.replay_record_canvas
  ? { canvas: rule.replay_canvas_fps }
  : undefined
```

The existing `blockSelector: "[data-fidibeki-block]"` remains the single exclusion rule for DOM and
canvas content. Existing duration, event count, batch count, batch byte, session byte, idle timeout,
and upload safety limits remain in force. A canvas serialization or upload failure follows the
existing non-blocking replay error path; feedback submission remains available and the replay can
surface as unavailable instead of losing the report.

## Verification

Backend coverage proves migration up/down/up compatibility, old-row defaults, create/update
round-tripping, validation, and public runtime projection.

Widget unit coverage proves disabled and enabled rrweb options, FPS bounds from the validated public
contract, fingerprint changes, exclusions, and non-blocking errors. Browser coverage draws and
mutates both an included canvas and a canvas under `[data-fidibeki-block]`, then verifies that
uploaded rrweb events contain canvas data only for the included surface.

A continuously updating canvas runs under the browser test for a bounded interval. The test records
animation-frame progress, emitted canvas-event count, and serialized byte volume, asserting that the
page keeps rendering and that event growth stays within the configured FPS plus scheduling
tolerance.

Backoffice browser coverage feeds a valid Canvas snapshot/mutation stream into `ReplayPlayer` and
asserts the rendered canvas pixels inside the replay iframe. Unit, typecheck, build, and focused
browser suites must pass before issue #41 is closed.

## Non-Goals

- No production deployment or infrastructure changes.
- No personal fingerprinting, new analytics events, or performance telemetry collection.
- No custom Canvas recorder or rrweb fork.
- No promise to reproduce browser-unsupported, tainted, or DRM-protected surfaces.
