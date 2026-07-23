# Automatic Feedback Context Privacy Design

**Date:** 2026-07-23
**Status:** Approved for implementation
**Scope:** GitHub issue #54
**Related:** GitHub issues #53 and #55

## Goal

Automatic feedback context must remain useful for triage without transmitting or
persisting personal data, secrets, or user-authored content. Reporter text and an
explicitly consented contact address are separate inputs and must never be copied
into automatic metadata, analytics, replay, or element context.

The browser may inspect the current page transiently to select a widget rule. Raw
locations and raw DOM must not cross the network boundary.

## Current Data Flow

The existing implementation sends `window.location.href`, `document.referrer`,
and a clone of the selected DOM subtree. Those values are persisted on feedback
and funnel records and are subsequently exposed to:

- member-facing feedback APIs and the backoffice;
- AI processing;
- Slack, Teams, and generic webhook deliveries;
- workspace and reporter privacy exports;
- analytics aggregates;
- logs when structured values are not classified as sensitive.

Screenshot, audio, and video attachment objects use opaque generated storage keys.
Their file contents follow the existing quarantine, cleaning, verification, and
retention lifecycle. This change covers their surrounding automatic metadata, not
media-content redaction.

## Canonical Location Contract

One location policy must be implemented in equivalent, contract-tested widget and
backend helpers and reused by feedback, funnel analytics, and replay.

1. Parse only absolute HTTP or HTTPS URLs.
2. Reject credentials and discard query strings and fragments.
3. Normalize the hostname, default port, duplicate path separators, and trailing
   slash. `/` remains `/`.
4. Use the raw page only ephemerally to evaluate targeting.
5. For a matched `url_exact` include, persist its configured canonical target.
6. For a matched `url_prefix` include, persist only its configured canonical base.
   Any path suffix is untrusted and is discarded.
7. When several static includes match, prefer an exact match, then the longest
   prefix. This makes the projection deterministic and maximally specific without
   retaining a visitor-controlled suffix.
8. For `url_regex`, `all_pages`, an invalid candidate, or a match that cannot be
   proven from a static include, persist only the request origin. The rule ID
   remains the route dimension.
9. Exclude conditions affect eligibility but are never a source of a persisted
   path.

The widget sends the safe projection. The backend independently repeats the
projection using the selected enabled rule and request origin. This both supports
older widgets that still send a full URL and prevents a modified client from
persisting a more specific location. Feedback metadata and its submitted funnel
event must resolve to the same safe projection.

`document.referrer` is no longer collected. The backend discards the field even
when an older client sends it. No replacement referrer dimension is introduced.

The raw browser user-agent is also unnecessary once coarse browser, operating
system, and device values have been derived in memory. The widget does not send
it, the backend discards it from older payloads, and remediation nulls existing
values. Language and viewport remain bounded operational context; they are not
combined into a fingerprint or used as an identity.

## Element Context Contract

Element attachment remains opt-in per rule and is available only when the enforced
sanitizer is present. The browser constructs a new structural representation
instead of redacting and serializing a clone.

Allowed HTML:

- a small allowlist of inert structural tags;
- parent-child structure and bounded depth/node count;
- no text or comment nodes;
- no attributes;
- no executable, embedded, form, media, metadata, or navigation elements.

The selector contains only lower-case tag names and bounded `:nth-of-type()`
segments. IDs, classes, names, attributes, and their values are prohibited.

Allowed styles are limited to layout and non-content presentation properties that
remain useful in the backoffice preview. Values are accepted only when they:

- are bounded printable scalar values;
- contain no `url()`, generated `content`, CSS variables, escapes, controls, or
  external resource references;
- contain no identifier-bearing custom property or property outside the allowlist.

The backend parses the submitted fragment with a real HTML parser and applies the
same allowlist. It never persists the client payload directly. A canonical
round-trip mismatch, malformed input, an over-limit structure, or any unsafe style
causes the element context to be omitted while the feedback itself is still
accepted. This is the compatibility behavior for old or modified widgets.

The API records a bounded, non-sensitive omission warning for product diagnostics;
it never echoes rejected content. If the server sanitizer is unavailable, element
capture fails closed.

## Defense In Depth

Only normalized values are passed into storage. Consequently AI, integrations,
exports, backoffice APIs, and rendering consume the same safe database projection
rather than maintaining independent redaction code.

Additional controls:

- classify `page_url`, `referrer`, selectors, DOM HTML, and DOM styles as sensitive
  log fields;
- keep the DOM preview sandboxed even though its input is sanitized;
- omit the "View page" integration action when only an origin is available;
- do not put automatic context in attachment object keys;
- keep screenshots/media behind the existing safety and retention lifecycle.

## Existing Data Remediation

The migration is intentionally privacy-destructive and its down migration does
not recreate removed data.

- replace existing feedback, replay, funnel-event, and daily-aggregate page URLs
  with origin-only projections unless a later worker can prove a configured
  static projection transactionally;
- set all stored feedback referrers to null;
- set all stored raw user-agent values to null while retaining the coarse
  browser, operating system, device, language, and viewport fields;
- remove all existing DOM snapshot HTML and styles because their safety cannot be
  proven;
- recompute page keys and merge daily aggregate collisions without losing counts;
- invalidate completed workspace/reporter export archives that may contain the
  old values, remove their objects through a dedicated cleanup lifecycle, and
  require a fresh export;
- retain ordinary uploaded media under its existing policy;
- ensure reporter anonymization and tenant deletion continue clearing every new
  or retained field.

The remediation must be idempotent and covered by migration up/down/up tests.
Any object deletion uses the durable storage cleanup worker; a database migration
must not pretend that nulling an object key deleted the object.

## User Experience

The rule editor explains that automatic page context is reduced to an approved
rule route or origin, referrers are not collected, and element context contains
only sanitized structure. Element capture remains disabled by default and cannot
be enabled when the sanitizer capability is absent.

The feedback detail view labels this value as "Page context" and does not imply
that an origin-only value opens the exact reporter page.

Enabling element context broadly still requires a recorded human privacy/legal
review. Engineering does not claim that this implementation is legal advice.

## Verification

Tests use unique sentinels for names, emails, account IDs, bearer/reset tokens,
query/hash values, dynamic path segments, form values, ARIA/title/alt text,
IDs/classes/data attributes, image/avatar URLs, event handlers, comments, scripts,
and CSS URLs.

The suite must prove that none of those sentinels appears in:

- widget feedback, analytics, or replay requests;
- feedback, replay, funnel-event, or aggregate database rows;
- logs captured by tests;
- member APIs or rendered backoffice HTML;
- AI inputs;
- integration payloads;
- privacy exports or object-storage archives.

Compatibility coverage includes older payloads, malformed URLs and fragments,
cross-origin referrers, duplicate/trailing slashes, exact/prefix/regex/all-pages
rules, server-side tampering attempts, sanitizer failure, and remediation.

The staging gate exercises one exact rule, one prefix rule with a personal-looking
suffix, and one regex/all-pages fallback, then confirms the database and all
downstream projections contain only the approved values.
