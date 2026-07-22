# Fidibeki Three-Pillar Product Roadmap Design

**Date:** 2026-07-22
**Status:** Approved
**Target:** Staging product development

## Product Goal

Fidibeki completes one operating loop:

`capture feedback -> analyze and triage -> decide action -> track resolution -> communicate outcome`

The roadmap has three complementary pillars, delivered sequentially as testable vertical slices:

1. Contextual Feedback Core;
2. Operations Completion;
3. Widget Platform.

The pillars are not competing product directions. The first establishes a usable end-to-end path,
the second completes the operational workflow, and the third deepens Fidibeki's contextual-capture
differentiation.

Staging is sufficient. Production, deployment, and infrastructure work remain out of scope unless
they directly block staging use. Production resumes only with explicit founder authorization.

## Existing Baseline

The implementation plan must audit and extend the current product rather than recreate existing
capabilities.

Already implemented:

- Feedback is independent from Issue and new submissions do not automatically create Issues.
- Operators can create an Issue from one or several Feedback records.
- Feedback can be linked to and unlinked from Issues individually or in bulk.
- Feedback and Issues own editable title, description, priority, assignee, and tags.
- The inbox supports atomic bulk triage.
- Issues have status transitions, merge support, status history, and transition comments.
- Reporter update emails and basic Slack, Teams, and generic webhook delivery exist.
- Widget targeting, durable recurrence state, normalized page identity, funnel analytics, and the
  analytics workspace already exist.

Capabilities that remain partial:

- Issue comments exist only as optional context on a status transition.
- Activity history does not cover every operational mutation.
- Reporter communication lacks receipt confirmation, a `needs_info` response flow, and a secure
  progress view.
- Slack, Teams, and generic webhook messages do not yet carry the complete operational context.

## Phase 1: Contextual Feedback Core

### Outcome

Provide one usable capture-to-inbox slice through a common runtime while preserving current embeds,
targeting, state, analytics, and submission contracts.

### Widget Definition

One `WidgetDefinition` supports four presentations:

- `launcher`: a discreet floating entry point that may be nudged but never automatically opened;
- `inline`: a layout-stable contextual element whose message and emphasis can adapt;
- `reaction`: a compact response surface shown after content or task engagement;
- `prompt`: a single-question interruption shown only with high confidence and strict fatigue
  controls.

Each definition owns:

- lifecycle status;
- presentation and placement;
- objective;
- response blocks;
- targeting reference;
- localized content;
- controlled theme tokens;
- cooldown, recurrence, and interruption guardrails.

The supported response blocks are free text, rating, reaction, and single choice. A widget may add
an optional comment and optional reporter contact with explicit consent. Multi-page forms,
conditional survey logic, and a general-purpose form builder are not part of this roadmap.

### Placement

Developers can place widgets declaratively or imperatively:

```html
<fidibeki-widget rule="checkout-feedback"></fidibeki-widget>
```

```js
Fidibeki.mount(element, { rule: 'checkout-feedback' })
```

The existing automatic embed remains compatible. Explicit placement provides context without
requiring a semantic event integration. `Fidibeki.track()` is deferred and tracked separately in
GitHub issue #7.

### Page Identity And State

Page identity is `origin + normalized pathname`:

- query parameters and hashes are ignored;
- trailing slashes are normalized, so `/chart` and `/chart/` are equivalent;
- state remains scoped by project, rule, and normalized page;
- submit and dismiss are meaningful transitions; opening alone never suppresses the widget;
- a new submission always receives a new idempotency identifier;
- cooldown and recurrence eventually allow a visitor to submit again.

If durable browser storage is unavailable, the runtime uses an in-memory fallback for the current
page lifetime and does not claim durable suppression.

### Localization

The runtime detects the browser locale and resolves the most specific supported variant before
falling back to the project's default language. For example, `pt-AO` may fall back to `pt` and then
to the project default.

Fidibeki supplies translations for system text. Every customer-authored message can have locale
variants. A missing custom translation uses the project's default-language value. Customers are not
required to translate Fidibeki system copy.

### Submission Contract

A successful submission creates one independent Feedback record and never creates an Issue. The
original response remains the Feedback description; the editable title is a short summary or the
existing AI suggestion. Reporter identity is anonymous by default, with optional contact and
consent. Attachments continue through the existing idempotent upload flow.

Phase 1 is complete when all four presentations can collect Feedback, respect targeting and
recurrence, appear in the inbox, and preserve compatibility with existing installations.

## Phase 2: Operations Completion

### Preserved Operational Capabilities

This phase does not rebuild inbox filtering, bulk triage, priority, assignment, tags, Issue creation,
linking, unlinking, status history, merge, or existing notifications. It begins with an explicit
complete/partial/missing audit against the current code and staging behavior.

### Remaining Work

- Add standalone internal comments that do not require a status transition.
- Add member mentions with internal notifications and RBAC enforcement.
- Record a complete activity log for status, metadata, assignment, tags, links, comments, merges,
  and reporter communication.
- Send reporter receipt confirmation, important progress changes, `needs_info`, and final
  resolution communication.
- Provide rich Slack Block Kit, Teams, and generic webhook messages with actor, old/new values,
  comments, attachments, timestamps, environment-aware resource links, and additive versioned
  payloads.

Interactive Slack and Teams actions remain a later extension. Incoming webhooks must not simulate
commands that require signed callbacks, identity mapping, RBAC, audit, and idempotency.

### Reporter Progress Access

Reporter progress access is enabled by default and can be opted out:

- the tenant defines the default;
- each project may inherit or override the tenant setting;
- disabling access immediately denies existing links in that scope;
- disabling the portal does not automatically disable consented email notifications.

Access uses an opaque random token stored only as a hash. It is resource-scoped, revocable,
rate-limited, and protected from enumeration. The reporter sees only the public progress information
required to understand the outcome, never internal comments, members, or tenant-only metadata.

When an Issue enters `needs_info`, the reporter receives a secure link to one simple response form.
Submitting the form appends reporter-provided information to the linked work and records an activity
event. It is not a threaded conversation or a general support inbox.

Phase 2 is complete when a team can triage intake, decide and track action, collaborate internally,
request one clarification, and communicate the result without leaving the Fidibeki workflow.

## Phase 3: Widget Platform

### Outcome

Deepen the shared runtime after the four essential presentations work end to end. This phase adds
product breadth without rewriting targeting, recurrence, analytics, or submission infrastructure.

### Controlled Customization

Customers can configure:

- colors;
- logo;
- localized copy;
- language defaults;
- position;
- presentation format.

Arbitrary CSS, arbitrary DOM injection, and full survey-page composition are excluded. Controlled
tokens keep accessibility, responsiveness, isolation, and support costs predictable.

### Objective-Based Intelligence

The backoffice asks for a goal instead of exact timing mechanics. Initial objectives are:

- continuous feedback;
- after engagement;
- validate content;
- detect friction.

The runtime uses deterministic local scoring. Signals may include active time, scroll depth,
interactions, visit and widget history, `document.title`, canonical URL, public description/Open
Graph metadata, and explicit public data attributes. It must not read inputs, private page content,
network payloads, or construct a personal fingerprint.

The policy layer applies presentation-specific behavior:

- launcher remains available discreetly and may only receive a nudge;
- inline remains layout-stable and may adapt message, emphasis, or compactness;
- reaction waits for meaningful content or task engagement;
- prompt requires the highest confidence and the strictest cooldown.

The backoffice includes preview, URL testing, decision explanation, and installation diagnostics.
The explanation reports which non-personal signals and guardrails affected a decision.

### Lifecycle

Widgets are never deleted merely to stop delivery. Their lifecycle is:

- `draft`: saved but not published;
- `active`: eligible for matching and display;
- `paused`: temporarily disabled while preserving configuration and analytics;
- `archived`: hidden from primary lists but recoverable.

Paused and archived widgets do not render and do not record impressions. Explicit mounts resolve to
a safe disabled result, while automatic matching excludes them. Historical Feedback, analytics, and
configuration remain intact.

## Shared Architecture

Configuration follows this hierarchy:

`tenant defaults -> project overrides -> widget definition`

The runtime is split into bounded modules for mounting, presentation, targeting, policy decisions,
state, analytics, and submission. Presentations share the behavioral modules and implement only
their view and interruption policy.

The backend keeps these concepts separate:

- `WidgetDefinition`: versioned widget configuration;
- `Feedback`: the original independent intake record;
- `Issue`: actionable work aggregating zero or more Feedback records;
- `ActivityEvent`: an immutable operational history entry;
- `ReporterAccess`: secure, revocable access to public progress information.

Public widget configuration exposes only runtime-safe fields. All schema changes are additive and
preserve existing widgets, Feedback, Issues, relations, public IDs, state, and analytics.

## End-to-End Data Flow

1. A member configures a project and widget in the backoffice.
2. The backend validates lifecycle, targeting, languages, blocks, theme tokens, and guardrails.
3. The runtime obtains the safe public configuration and normalizes the current page identity.
4. Targeting, locale, prior state, signals, and the objective policy decide whether and when to show.
5. Analytics records `matched`, `impression`, `interaction`, `submitted`, and `dismissed` at the
   correct behavioral boundaries.
6. An idempotent submission creates Feedback; attachments use the existing storage flow.
7. Members triage Feedback and create or update Issues through the existing operational contracts.
8. Mutations create ActivityEvents and transactional outbox events.
9. Integrations and reporter notifications consume those events idempotently.
10. When enabled and consented, ReporterAccess exposes safe progress and the one-response
    `needs_info` flow.
11. Resolution communication closes the loop and remains visible in activity history.

## Errors, Privacy, And Security

- Invalid configuration is rejected at save time. An invalid runtime rule fails closed without
  rendering.
- Widget failures are isolated and cannot break the host application.
- Submission and analytics APIs retain their idempotency and deduplication guarantees.
- Attachment failure does not erase an already accepted Feedback record.
- Decision signals exclude private content, form values, network payloads, and fingerprinting.
- Reporter tokens are hashed, scoped, revocable, rate-limited, and non-enumerable.
- Opt-out is enforced at request time so stale links stop working immediately.
- Tenant, project, and RBAC constraints apply to every new configuration and operations contract.
- Interruptive presentations have stricter confidence, frequency, and cooldown requirements.

## Verification Strategy

Every phase ends with a testable staging vertical slice and includes:

- additive migration `up/down/up` verification and data-preservation checks;
- backend tests for configuration hierarchy, lifecycle, localization, RBAC, opt-out, activity, and
  reporter access;
- runtime unit tests for page normalization, targeting, scoring, cooldown, locale resolution, and
  all four presentation policies;
- compatibility tests for existing widget and API contracts;
- backoffice tests for configuration, preview, lifecycle, and diagnostics;
- end-to-end tests covering submission, triage, Issue work, reporter communication, and
  integrations;
- explicit checks that paused and archived widgets neither render nor record impressions;
- accessibility, responsive layout, Shadow DOM isolation, and performance-budget validation;
- one real staging journey per phase without production changes.

## Explicit Non-Goals

- Production rollout or infrastructure expansion.
- A general-purpose survey or form builder.
- Arbitrary widget CSS or DOM injection.
- Paid real-time AI decisions in the widget runtime.
- Automatic Issue creation based on rating, AI, or thresholds.
- Full reporter chat or customer-support ticketing.
- Interactive Slack or Teams commands in these three phases.
- `Fidibeki.track()` in v1.
