# Fidibeki Widget Funnel Analytics Design

**Date:** 2026-07-22  
**Status:** Approved product scope; architecture selected for continuous execution  
**Scope:** Product Slice 4, after widget state, recurrence, and targeting  
**Deployment:** Staging only; production remains untouched

## Problem

Fidibeki currently records `widget/connected` when the loader selects a rule. That signal is useful
for installation diagnostics, but it happens before the widget is visibly rendered and cannot
measure whether a visitor saw, used, or completed the form. The existing Overview dashboard derives
feedback and issue metrics directly from domain tables and has no widget funnel, anonymous visit
identity, page breakdown, or daily analytics aggregate.

The product needs a trustworthy path from a rule matching through feedback acceptance. Metrics must
remain anonymous, survive retries without double counting, use the canonical page identity already
shared by targeting and recurrence, and result in an operator-facing dashboard rather than a write-
only event stream.

## Goals

- Record `matched`, `impression`, `interaction`, `submitted`, and `dismissed` at their real lifecycle
  boundaries.
- Keep `widget/connected` as an activation diagnostic and never count it as an impression.
- Identify visitors, page views, and events with random anonymous UUIDs without fingerprinting.
- Deduplicate client retries and repeated loader evaluation at both browser and database boundaries.
- Batch non-critical events and use `sendBeacon` when a page is being hidden or unloaded.
- Record accepted submissions transactionally with the feedback record.
- Retain immutable raw events and maintain repairable daily aggregates.
- Provide correct interaction, completion, and submit rates with project, rule, page, and period
  breakdowns.
- Add a focused `Analytics` workspace page while keeping the current Overview operational.

## Non-Goals

- Replacing external product analytics, session replay, or `widget/connected` diagnostics.
- Recording IP addresses, reporter identity, cookies, advertising identifiers, query parameters,
  fragments, DOM content, or a device fingerprint in funnel events.
- Real-time streaming infrastructure, a general-purpose analytics query language, cohort analysis,
  experiments, or revenue attribution.
- Production deployment, production configuration, or production data migration.

## Event Semantics

The funnel is scoped to the winning enabled rule after include/exclude evaluation and priority
ordering. Lower-priority rules that also match do not emit an event because they did not own the
experience.

### `matched`

Emitted once when a canonical page selects a winning rule, before recurrence suppression is checked.
This distinguishes targeting reach from actual visibility. A page can therefore be matched without
an impression when the trigger never fires or the rule is suppressed by a previous submission or
dismissal.

### `impression`

Emitted once only after the widget UI is mounted and visibly available. Arming a delay, scroll, or
exit-intent trigger is not an impression. The current full form emits after its host and open shadow
root are attached; a future compact launcher will emit after the launcher becomes visible.

### `interaction`

Emitted once for the first meaningful action on that impression. Meaningful actions are opening a
compact launcher, selecting a rating, entering feedback text, starting an attachment action, or
attempting submission. Passive pointer movement, focus caused by page load, and closing without
another action do not count as interaction.

### `submitted`

Recorded only by the backend when it accepts a new feedback submission. The widget supplies the
anonymous analytics context inside the feedback request; the backend inserts the event in the same
database transaction as the feedback. An idempotent retry of the same `client_feedback_id` returns
the original response and never adds another submitted event.

Legacy clients without analytics context continue creating feedback successfully. The backend never
infers historical impressions or interactions from a feedback row.

### `dismissed`

Emitted once when the visitor explicitly closes or dismisses the visible widget before submission.
Dismissal may occur without an interaction. A submitted form that later unmounts never emits
dismissed.

## Anonymous Identity And Page Views

The browser generates cryptographically random UUIDs:

- `visitor_id` is stored in `localStorage` and remains stable for that browser origin.
- `page_view_id` is generated on initial document load and whenever SPA navigation changes the
  canonical pathname.
- `event_id` is generated independently for every lifecycle event.

Changing only query parameters, the hash, or trailing slashes retains the current page view because
those URLs share a canonical page. Navigating to another canonical pathname creates a new page view,
including when browser history later returns to a previous pathname.

If persistent storage is blocked, the widget uses an ephemeral visitor UUID for the current document
and continues. No identifier is derived from IP address, user agent, screen size, fonts, or other
browser characteristics. Funnel payloads contain the canonical origin and pathname only.

## Browser Event Pipeline

The loader owns analytics identity, lifecycle deduplication, and delivery. The UI remains responsible
only for signalling visible, interacted, submitted, and dismissed transitions through callbacks.

Non-submission events enter a bounded browser outbox:

- at most 100 events are retained;
- a batch contains at most 20 events;
- the loader flushes after five seconds, at ten queued events, and when the document becomes hidden;
- ordinary flushes use CORS `fetch` with `keepalive`;
- hidden/page-unload flushes use `navigator.sendBeacon` when available and fall back to `fetch`;
- events older than 24 hours are discarded locally;
- transient failures remain retryable and database idempotency makes repeated delivery harmless.

Analytics delivery is always best effort. It must never prevent configuration loading, targeting,
widget rendering, feedback input, submission, or attachment upload. Technical failures use the
existing scrubbed widget error reporting path when configured.

The submitted event does not use this outbox. Its IDs and timestamp travel inside the feedback
request so the backend acceptance boundary is authoritative.

## Public API Contract

The widget API adds:

```http
POST /v1/widget/events?key=pk_live_...
```

```json
{
  "events": [
    {
      "event_id": "uuid",
      "event_type": "impression",
      "visitor_id": "uuid",
      "page_view_id": "uuid",
      "rule_id": "uuid",
      "page_url": "https://site.test/chart",
      "occurred_at": "2026-07-22T12:00:00Z"
    }
  ]
}
```

The endpoint accepts 1 through 20 events and a body of at most 64 KiB. It supports the existing
public-key header for `fetch` and the query key for `sendBeacon`. Authorization reuses the project
public key, active-tenant check, and exact allowed-origin policy. The server verifies every rule
belongs to the project and every canonical page has the request origin. Client timestamps may be at
most 24 hours old or five minutes in the future.

The endpoint accepts `matched`, `impression`, `interaction`, and `dismissed`; it rejects client-
reported `submitted`. A successful batch returns the accepted and duplicate event IDs. Invalid
events produce indexed validation errors and do not partially persist the batch.

The feedback submission request adds an optional `analytics` object with the same anonymous IDs,
rule, canonical page, and occurred timestamp. The analytics rule must equal `widget_rule_id`, and the
page must canonicalize to the submitted metadata URL. This request change is additive.

Both public paths use a Redis limiter keyed by project and source address. The event endpoint permits
300 events per minute and returns the existing rate-limit response shape when exceeded.

## Data Model

Migration 49 introduces `widget_funnel_events` for immutable raw events:

| Column | Purpose |
| --- | --- |
| `event_id` | Client UUID and primary idempotency key |
| `tenant_id`, `project_id` | Tenant ownership and project dimension |
| `rule_id`, `rule_name` | Validated rule identity plus historical name snapshot |
| `event_type` | Funnel event enum |
| `visitor_id`, `page_view_id` | Anonymous random identities |
| `page_url`, `page_key` | Canonical page and SHA-256 dimension key |
| `feedback_id` | Nullable accepted feedback link for submitted events |
| `occurred_at`, `received_at` | Client lifecycle time and trusted server receipt time |

The project relation cascades on project deletion. `rule_id` is intentionally a validated snapshot
rather than a deleting foreign key, so removing a widget rule does not erase its historical funnel.
`rule_name` makes that history readable. A feedback relation uses `ON DELETE SET NULL`, preserving
anonymous aggregate history after domain-record deletion.

A unique constraint on project, rule, page view, and event type prevents repeated evaluation from
counting the same stage twice. The global event ID provides retry idempotency. Checks enforce that
only submitted events may contain `feedback_id`.

`widget_funnel_daily` stores UTC daily counts keyed by tenant, project, rule snapshot, canonical page
key, and date. It contains matched, impression, interaction, submitted, and dismissed counters plus
the readable page URL. Counters are non-negative 64-bit values. RLS follows project tenant ownership
for both tables.

Only newly inserted raw events increment the daily row, in the same transaction. A worker
reconciles the latest three UTC days from raw events each day, making aggregates repairable after an
interrupted deployment or manual recovery.

Raw events are retained for 90 days. Daily aggregates are retained for 730 days. The worker runs
retention only after reconciliation and deletes in bounded batches. Project or tenant deletion still
cascades immediately.

## Analytics API

Authenticated workspace members with `project:read` receive:

```http
GET /v1/analytics/widget?range=30d&project_id=&rule_id=&page=
```

Allowed ranges are 7, 30, and 90 UTC calendar days, including the current partial day. Filters are
optional and tenant-scoped. The response contains:

- total matched, impressions, interactions, submissions, and dismissals;
- interaction rate = interactions / impressions;
- completion rate = submissions / interactions;
- submit rate = submissions / impressions;
- impression rate = impressions / matched as a targeting/visibility diagnostic;
- a daily impressions/interactions/submissions trend;
- project, rule, and canonical-page breakdowns ordered by impressions, then submissions;
- available project and rule filter options.

Rates are `null` when their denominator is zero, never fabricated as zero or infinity. Counts and
rates are calculated from the same aggregate snapshot. A rule deleted after collection remains in
breakdowns with its captured name and a deleted marker.

## Dispatch Experience

The primary navigation adds `Analytics` with a chart icon. The current Overview remains focused on
feedback volume, work requiring attention, issues, and project status.

`/analytics` uses the existing quiet Dispatch/Netlify backoffice direction:

- one compact toolbar for range, project, rule, and canonical-page filtering;
- a stable metric ledger for impressions, interaction rate, completion rate, and submit rate;
- matched and dismissed counts as supporting diagnostic values;
- an unframed daily trend below the ledger;
- one dense breakdown table with `Projects`, `Rules`, and `Pages` tabs;
- rows showing counts and the three required rates from their correct denominators.

The page does not introduce nested cards, marketing copy, terminal styling, or an editorial hero.
Desktop and mobile keep fixed metric dimensions, wrap filters without overlap, and preserve a
scannable table. Empty periods show a concise zero-data state without manufacturing rates.

## Error Handling And Privacy

- Invalid public keys, origins, rules, canonical pages, IDs, timestamps, or event types are rejected
  before a database transaction.
- Duplicate event IDs and lifecycle keys are successful no-ops and never increment aggregates.
- A failed analytics batch remains non-blocking in the widget and is retried within the outbox cap.
- Malformed browser storage fails open with a fresh anonymous identity.
- Funnel events contain no feedback text, rating, reporter email, attachments, referrer, query,
  fragment, raw user agent, IP address, or replay data.
- Request IP may be used transiently for Redis rate limiting but is not persisted in analytics.
- Tenant API filters are always applied inside the tenant transaction; cross-tenant rule and project
  IDs resolve as not found.
- `widget/connected` retains its current behavior and remains absent from every funnel query.

## Compatibility And Rollout

The event endpoint and feedback analytics object are additive. The old widget continues working
after migration/API deployment, and the new widget treats an unavailable analytics endpoint as a
non-blocking telemetry failure. The existing dashboard response is unchanged.

Staging rollout order is migration/API and worker first, then widget, then web. Production is not
deployed or configured. The staging smoke uses a reversible QA rule, a local allowed origin, known
anonymous IDs, and representative eligible, suppressed, dismissed, interacted, and submitted paths.
It verifies the analytics API and dashboard, then restores the rule/domains and removes smoke events
and aggregates.

## Testing

Database and backend coverage includes:

- migration up/down/up, constraints, indexes, grants, RLS, cascades, and retention queries;
- batch authorization, CORS, size/count limits, rate limiting, timestamp bounds, and indexed errors;
- event-ID and lifecycle-key idempotency under repeated and concurrent requests;
- canonical page/origin validation and historical rule snapshots;
- atomic submitted-event creation with feedback idempotent retries;
- aggregate increments, reconciliation, retention, filters, rate denominators, and deleted rules;
- proof that `widget/connected` does not change funnel tables.

Widget coverage includes:

- anonymous identity persistence and storage failure fallback;
- page-view rotation only on canonical pathname navigation;
- one event per lifecycle stage despite configuration refresh and repeated navigation events;
- matched before suppression, no impression for suppressed or untriggered rules, and impression only
  after mount;
- first meaningful interaction, dismissal without interaction, and submitted analytics context;
- batch caps, timed flush, `sendBeacon`, transient retry, expiry, and non-blocking failure;
- query/hash/trailing-slash equivalence and distinct-path isolation.

Web coverage includes:

- response typing, null-rate formatting, filters, tabs, sorting, and empty/error states;
- desktop and mobile browser journeys with representative funnel fixtures;
- visual inspection for overflow, overlap, stable metrics, and Dispatch consistency.

Staging validation covers the complete real widget-to-event-to-aggregate-to-dashboard path and
directly confirms that no production service or configuration changed.

## Follow-On Work

Rich notifications in Slice 5 may link to Analytics using project and canonical-page filters, but do
not consume raw anonymous IDs. Interactive integrations, collaboration, and reporter communication
remain separate slices and do not expand the funnel payload.
