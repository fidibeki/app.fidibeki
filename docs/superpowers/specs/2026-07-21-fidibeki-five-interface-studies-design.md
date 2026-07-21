# Fidibeki Five Interface Studies

**Date:** 2026-07-21
**Status:** Approved for visual prototyping

## Purpose

Create five comparable interface studies for the Fidibeki backoffice before committing Phase 11 to one visual and interaction model. The studies must feel like polished SaaS operations software in the direction of Netlify's current backoffice: compact scope controls, quiet navigation, border-led hierarchy, dense filters, and full-width data surfaces.

The studies must not use editorial composition, oversized marketing typography, terminal styling, decorative dashboard filler, nested cards, or theme-only variations. Each option must change the information architecture and primary workflow while presenting the same product data.

## Prototype Boundary

The work is a temporary design lab inside the future Nuxt application, not five production backoffices.

- Create a Nuxt 3 application under `web/`.
- Add a design-lab index at `/designs`.
- Use `/designs/<concept>/<view>` for every study. Concept slugs are `clearspace`, `dispatch`, `signal-observatory`, `project-deck`, and `flowboard`; view slugs are `overview`, `feedback`, and `feedback-detail`.
- Each concept exposes the same three representative views: `Overview`, `Feedback`, and `Feedback detail`. Visiting `/designs/<concept>` redirects to that concept's `overview` view.
- A persistent design-lab switcher allows direct movement between concepts and views.
- All content comes from shared local fixtures. No production API, authentication, persistence, billing mutation, or email behavior is part of this prototype.
- The selected concept will later become the Phase 11 production foundation. Non-selected studies remain reference-only and are not maintained as alternate themes.

## Common Product Scenario

Every concept uses the same tenant, projects, metrics, and feedback records so the comparison tests structure rather than content.

- Tenant: `Acme Studio`, on a Pro trial with nine days remaining.
- Projects: Storefront, Account Portal, and Docs.
- Overview metrics: feedback received, open issues, and satisfaction trend.
- Feedback records cover positive and negative ratings, AI processing states, moderation flags, screenshots, audio/video, session replay, linked and unlinked issues, and multiple projects and dates.
- The selected feedback record includes cleaned text, reporter and browser metadata, screenshot, transcript, sentiment, moderation state, linked issue, and replay readiness.
- Filters represent the Phase 11 surface: project, status, rating, attachment, moderation, issue, date, sentiment, and sort.
- Screenshot and replay previews use the same concrete Storefront checkout scene across all studies. The asset must show inspectable product UI rather than an abstract placeholder.

All concepts share the same language, icon set, semantic status colors, and fixture values. They may vary typography, shell, density, and spatial composition.

## Study 1: Clearspace

Clearspace is the closest study to the Netlify reference and the lowest-learning-curve option.

- A 232px light sidebar holds tenant context, primary product navigation, administration, trial state, and account controls.
- A compact contextual header provides breadcrumbs, project scope, and page actions.
- Overview uses three restrained metric panels, a satisfaction trend, a triage summary, recent feedback, and project health.
- Feedback uses a routed, cursor-paginated table. Filters open in an anchored popover and active filters appear as removable chips.
- Feedback detail is a dedicated two-column page: evidence and replay on the left, metadata and triage context on the right.
- Project configuration uses local tabs and a form beside a stable live widget preview.
- Visual character: off-white canvas, white surfaces, graphite text, teal action color, blue links, 6px radii, borders instead of shadows.

This study favors familiarity and breadth over maximum triage speed.

## Study 2: Dispatch

Dispatch treats Fidibeki as a high-throughput feedback workstation.

- A 56px global rail establishes tenant and product scope.
- A 216px queue rail contains saved views such as Open, Moderation, AI failed, Has replay, Unlinked, Spam, and Archived.
- The main area is a dense feedback stream with a persistent detail inspector.
- Selection updates the inspector without leaving the queue; arrow navigation and previous/next controls preserve position.
- Multi-select reveals a fixed bulk-action bar for archive, spam, issue linking, and AI reprocessing.
- The inspector separates Feedback, Evidence, and Replay while keeping triage actions pinned.
- Overview is an attention queue led by work requiring action, with metrics presented as a compact ledger rather than a card grid.
- Visual character: compact rows, pale neutral surfaces, strong selection state, teal focus color, restrained semantic colors, overlay shadows only.

This study favors triage throughput and context retention over spaciousness.

## Study 3: Signal Observatory

Signal Observatory treats feedback as a time-based product signal while keeping evidence one action away.

- A restrained top bar and horizontal product rail replace the permanent sidebar.
- Overview uses a continuous full-width signal canvas rather than a grid of cards.
- Feedback volume, satisfaction, sentiment, and topic movement share an aligned time axis.
- A plain-language movement register explains material changes and links directly to filtered evidence.
- Feedback uses a chronological signal table; selecting a row opens a 42% focus pane over the current context.
- Topic labels and chart regions become filters rather than decorative analytics.
- Comparison choices are intentionally limited to a previous period or another project across fixed time ranges.
- Visual character: cool neutral canvas, crisp grid lines, teal primary signal, blue/green/amber/red semantics, sparse indigo AI annotations, tabular numerals.

This study favors monitoring and pattern recognition over the simplicity of a conventional dashboard.

## Study 4: Project Deck

Project Deck organizes Fidibeki around project workspaces, mirroring the way platform consoles establish project context.

- A 56px global top bar contains tenant, project switcher, global search, trial state, and account actions.
- A project-scoped horizontal rail contains Overview, Feedback, Issues, Widget, Activity, and Access.
- There is no permanent sidebar; the active project occupies the full canvas.
- Overview starts with a compact project status band, followed by feedback health, recent activity, issue movement, and widget collection state in stacked full-width sections.
- Cross-project information exists only in the project switcher and a fleet-style Projects page.
- Feedback uses a full-width table with a contextual right drawer for rapid inspection; a deliberate `Open full detail` action enters the dedicated evidence and replay view.
- Widget configuration is a focused workspace with rule navigation, configuration controls, and a live preview.
- Visual character: bright white canvas, graphite navigation, teal and cobalt actions, thin separators, slightly larger spacing than Dispatch, no decorative containers.

This study favors a clear project mental model and configuration workflows over cross-project triage.

## Study 5: Flowboard

Flowboard makes the conversion from raw feedback to resolved product work visible and collaborative.

- A slim icon-and-label rail holds global navigation; project scope and search remain in the top toolbar.
- Overview is a lifecycle board with New, Reviewed, Linked, and Resolved lanes, preceded by a compact metric and satisfaction strip.
- Feedback items are stable repeated work units showing rating, excerpt, project, evidence types, issue relationship, and age.
- Cards open a right-side evidence sheet; moving an item changes its triage status but never invents an issue automatically.
- Bulk selection and filters can narrow or move compatible items. Issue creation and linking remain explicit actions.
- Feedback also offers a table toggle for scanning and accessibility; the board is a workflow view, not the only representation.
- Feedback detail dedicates the main canvas to evidence and replay, with the lifecycle and issue actions in a fixed side column.
- Visual character: warm white canvas balanced by graphite structure, teal selection, blue linked-work states, amber attention, red moderation, and restrained lane backgrounds.

This study favors workflow visibility and collaboration over information density at very high volume.

## Shared Components And Boundaries

The design lab uses shared data and utilities but keeps each concept's layout independent.

- `fixtures`: immutable tenant, project, metric, feedback, issue, and replay-preview data.
- `DesignSwitcher`: changes concept and representative view without being part of any production shell.
- `StatusIcon`, `Rating`, `AttachmentKinds`, and `RelativeTime`: shared semantic primitives.
- `OverviewStudy`, `FeedbackStudy`, and `FeedbackDetailStudy`: concept-owned compositions. A single configurable mega-component is explicitly out of scope.
- `PrototypeNotice`: identifies fixture-only actions and prevents a prototype interaction from implying persistence.

Shared primitives may define accessibility and semantics. Navigation shells, layouts, tables, inspectors, boards, and charts remain concept-specific so the studies do not collapse into one themed design.

## Interaction And State

- The URL identifies concept and representative view so every study can be linked and refreshed directly.
- Filter controls update local prototype state and visibly affect the fixture list.
- Row, chart, queue, or board selection updates the appropriate detail surface for that concept.
- Replay opens a stable, non-playing preview surface; actual rrweb integration belongs to Phase 11.
- Prototype mutations such as status changes or bulk actions update local state only and display a clear temporary confirmation.
- Invalid concept or view routes redirect to `/designs?invalid=1`, where a concise not-found notice is shown.
- Empty, loading, and failed-processing examples are represented in fixtures. No simulated network latency or random failures are added.

## Responsive Behavior

Each study must be useful at 1440px desktop and 390px mobile widths.

- Desktop layouts may use rails, inspectors, drawers, or aligned visualizations where specified.
- Mobile replaces side rails with drawers or selectors and turns inspectors into full-screen detail routes.
- Tables become information-preserving list rows, not horizontally clipped desktop tables.
- Signal Observatory reduces to one selected track plus a movement summary.
- Flowboard becomes grouped vertical lists with a view selector.
- Feedback triage actions remain reachable in a stable bottom action area.
- Text, icons, controls, charts, and replay previews must not overlap or cause layout shifts.

## Accessibility

- All controls are keyboard reachable with visible focus treatment.
- Familiar Lucide icons accompany or replace commands where appropriate and unfamiliar icons have tooltips.
- Status meaning never depends on color alone.
- Controls meet a 40px minimum target on touch layouts.
- Charts expose a textual movement summary and the underlying filtered feedback list.
- Reduced-motion preferences disable transitions that are not essential to comprehension.
- Contrast targets WCAG AA for text and interactive states.

## Verification

- Run Nuxt type checking and the repository's formatter/linter for the new app.
- Add focused tests for fixture filtering, concept routing, and local selection behavior.
- Use Playwright to exercise concept switching, view switching, filtering, row selection, drawer/inspector behavior, and mobile navigation.
- Capture and inspect all five concepts at `1440x1000` and `390x844`.
- Check for nonblank rendering, overflow, clipped labels, text collisions, unstable row geometry, and inaccessible focus states.
- Confirm all five studies show identical fixture values and required Phase 11 information.

## Selection Criteria

The final review evaluates each study on first-use clarity, triage speed, signal comprehension, project configuration ergonomics, responsive behavior, accessibility, and the ability to scale across all Phase 11 pages.

The review records one selected foundation. It may also name one specific interaction to borrow from another study; broader mixing requires a revised design. The implementation plan for the production backoffice begins only after that selection.

## Out Of Scope

- Production authentication, API integration, permissions, silent refresh, or tenant host routing.
- Real issue, billing, member, integration, email, or account-deletion mutations.
- Actual rrweb playback, upload handling, or AI processing.
- Five maintained themes or a runtime appearance selector.
- Final brand identity, marketing pages, dark mode, or tenant branding.
