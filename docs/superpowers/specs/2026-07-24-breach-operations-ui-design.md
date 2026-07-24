# Breach Operations UI Design

## Status And Scope

Approved on 2026-07-24 under the founder's standing approval for recommended product decisions.
This is the next bounded local vertical for issue #64.

The slice makes the existing restricted breach register inspectable through the authenticated
platform-operations browser surface. It does not add production infrastructure, deploy, send a
notification, infer a legal decision, expose incident data to a tenant member, or add browser-based
mutations.

## Audit Result

The backend already provides:

- platform-admin-only incident list/detail APIs backed by the `fidibeki_admin` database role;
- immutable incident facts, chronology, assessments, containment and corrective-action revisions;
- staged controller notifications and delivery evidence;
- governed provider-incident intake snapshots;
- deterministic JSON and Markdown notification/report exports;
- a short-lived platform-admin browser cookie used by a server-rendered queue monitor.

There is no platform-admin SPA or incident page. The tenant Nuxt application is deliberately
scoped to tenant-member sessions and must not become a second platform-authentication boundary.
The queue monitor is the only existing browser operations surface, and its cookie is intentionally
limited to `/admin/queues`.

## Considered Approaches

1. Add breach pages to the tenant Nuxt application. Rejected because it would mix tenant and
   platform audiences, require a second session model in that application, and increase the chance
   of exposing a global register through tenant routing.
2. Build a complete mutable platform-admin SPA. Deferred because this first consumer does not yet
   have a browser CSRF contract and the existing API already supports controlled command-line or
   API operations.
3. Extend the server-rendered operations surface with a read-only breach workspace. Selected
   because it reuses the proven platform-admin browser boundary, adds no client-side dependency,
   and makes the full evidence record operationally reviewable without creating a new mutation
   path.

## Browser Authentication Boundary

Successful platform-admin verification additionally sets a short-lived, HTTP-only,
`SameSite=Strict` operations access cookie scoped only to `/admin/operations`. The existing queue
cookie and its path remain unchanged for compatibility.

Every incident page and export alias validates:

- the platform JWT audience;
- `platform:breach:read`;
- the operations cookie path;
- the existing database grant boundary.

Missing, tenant-audience, expired, or insufficient tokens fail before the handler reads the
register. Pages and downloads use `Cache-Control: private, no-store`, `Referrer-Policy:
no-referrer`, `X-Content-Type-Options: nosniff`, and a restrictive CSP.

## Information Architecture

`GET /admin/operations/privacy-incidents` renders a scan-oriented incident table with:

- public incident ID and link to detail;
- awareness time and fixed 72-hour deadline;
- controller/processor role;
- confidentiality, integrity, and availability impact flags;
- affected tenant count and affected environment categories.

`GET /admin/operations/privacy-incidents/{incidentID}` renders:

- immutable incident facts and affected categories;
- the authority clock without asserting notification necessity;
- append-only chronology and assessment history;
- current containment/corrective action state and complete revision history;
- staged controller notices and delivery attempts;
- governed provider-intake snapshots;
- links to the existing deterministic post-incident and notification exports.

Empty collections have explicit operational empty states. Customer/operator text is escaped by
`html/template`; IDs and evidence references remain visible as opaque references, never resolved
to raw evidence.

The queue monitor gains a compact navigation link to Privacy incidents. The incident workspace
links back to Queue operations.

## Export Reuse

Cookie-authenticated GET/HEAD aliases live below `/admin/operations/privacy-incidents`. They call
the exact existing export handlers and renderers, so JSON/Markdown shape, safe headers, filenames,
and Markdown injection protections cannot drift from the bearer-token API.

No export is persisted. A download remains a read of already-recorded facts and therefore uses
`platform:breach:read`.

## Failure And Safety Behavior

- Invalid or unknown incident and notification IDs return a neutral not-found page or response.
- Store failures render a bounded unavailable state without database/provider detail.
- HTML never includes idempotency keys, request fingerprints, tokens, raw provider payloads,
  reporter content, signed storage URLs, or evidence bodies.
- No form or JavaScript command is present. Create, append, queue, and retry operations remain on
  the bearer-token API until a later slice defines CSRF protection, confirmation, and command audit
  behavior.
- The UI never labels a provider as a confirmed subprocessor and never derives a legal outcome.

## Verification

Tests cover:

- cookie audience and permission denial before store access;
- list/detail rendering, empty states, deadline presentation, and all evidence sections;
- HTML escaping of adversarial operator-controlled values;
- GET/HEAD export parity and safe headers through the operations aliases;
- platform verification setting and clearing the correctly scoped operations cookie;
- queue-to-incident and incident-to-queue navigation;
- package, full backend, vet, build, governance, and diff checks.

A local browser screenshot at desktop and narrow mobile width must confirm readable tables,
responsive stacking, no overlap, and no horizontal page overflow. No staging or production
deployment is part of this slice.

## Follow-Up Boundary

A later #64 slice may add create/append/assessment/action/notification forms only after defining a
CSRF token lifecycle, command confirmations, idempotency behavior, validation/error recovery, and
browser E2E coverage. Staging capture and tabletop exercises remain separate and blocked on an
explicit deployment/exercise window.
