# Fidibeki Five Interface Studies Implementation Plan

**Design:** `docs/superpowers/specs/2026-07-21-fidibeki-five-interface-studies-design.md`

## Goal

Deliver a working Nuxt 3 comparison lab containing five structurally distinct Fidibeki backoffice studies, each with Overview, Feedback, and Feedback detail views at desktop and mobile sizes.

## Tasks

1. **Scaffold the Nuxt design lab**
   - Create the `web` workspace package, Nuxt configuration, app shell, and shared CSS foundation.
   - Add Nuxt 3.21, Vue, Lucide Vue, Vitest, and Playwright dependencies.
   - Add root scripts for web development, build, type checking, unit tests, and E2E tests.

2. **Create shared fixtures and semantics**
   - Define typed tenant, project, metric, feedback, issue, activity, and signal fixtures.
   - Implement deterministic filtering and selection helpers.
   - Build shared icons, rating, status, attachment, replay preview, and prototype notice primitives.

3. **Build the comparison shell**
   - Implement `/designs` as the concept index.
   - Implement `/designs/<concept>/<view>` routing and invalid-route handling.
   - Add a compact design switcher that preserves the representative view across concepts.

4. **Implement Clearspace and Dispatch**
   - Build all three Clearspace views with routed detail and conventional console navigation.
   - Build all three Dispatch views with queue rail, dense stream, persistent inspector, and local bulk state.

5. **Implement Signal Observatory**
   - Build aligned signal tracks, movement register, evidence-linked filtering, signal table, and focus pane.
   - Provide text equivalents for every plotted signal.

6. **Implement Project Deck and Flowboard**
   - Build project-scoped horizontal navigation, full-width bands, table, drawer, and detail for Project Deck.
   - Build lifecycle board, table toggle, evidence sheet, and detail workspace for Flowboard.

7. **Complete responsive and accessible behavior**
   - Adapt rails, tables, inspectors, signal tracks, and boards for 390px width.
   - Verify keyboard focus, labels, tooltips, semantic states, reduced motion, and stable dimensions.

8. **Verify behavior and rendering**
   - Add unit tests for filtering and route metadata.
   - Add Playwright coverage for concept/view switching, filters, selection, drawers, and mobile navigation.
   - Run type checking, unit tests, production build, and E2E tests.
   - Capture and inspect all five concepts at 1440x1000 and 390x844; correct overflow, clipping, overlap, blank content, and inconsistent fixture values.

9. **Prepare the design review**
   - Run the design lab on an unused local port.
   - Provide direct links to the comparison index and each concept.
   - Record material strengths and risks discovered during implementation without selecting a winner on the user's behalf.
