# Replay Identity Threat Model

**Date:** 2026-07-23
**Scope:** GitHub issue #53, amended by the rule-safe context contract in #54 and consent gate in #57
**Environment:** Product and staging only; no production or deployment work

## Problem

Session replay is intended to be anonymous: a person viewing a replay must not be able to
determine which authenticated user generated it, unless that person separately and explicitly
supplied reporter contact information through the feedback/contact workflow. Before this change,
replay used a random session identifier and masked input values by default, but multiple other
surfaces could still carry identity. This document enumerates those surfaces, states the
mitigation for each, and records what remains an accepted residual risk.

## Threat model by surface

| Surface | How identity could leak | Mitigation |
|---|---|---|
| Visible text nodes | rrweb serializes DOM text content verbatim; names, emails, account/organization names rendered anywhere on the page are captured | `widget/src/modules/replay.ts` forces `maskTextSelector: '*'` (every element, not an optional customer selector) with a custom `maskTextFn` (`widget/src/modules/privacy.ts`) that replaces all text by default. Text is only left unmasked inside an explicit `data-fidibeki-safe-text` subtree. |
| HTML comments and CDATA | rrweb's text-masking pipeline (`maskTextFn`) only applies to genuine Text nodes, so comments or CDATA could otherwise pass through unmasked | `sanitizeSnapshotNode` blanks Comment and CDATA `textContent` unconditionally because neither carries required visual replay value. |
| Form values | Typed input/textarea/select content can contain emails, names, or other PII | `maskAllInputs: true` is forced unconditionally in the widget and the backend `widget_rules.replay_mask_all_inputs` column now has a `CHECK` constraint requiring `true` (migration `000074`); it is no longer a customer-configurable toggle. |
| DOM `id`/`class`/`data-*` attributes | Application code commonly encodes identity into element metadata (`id="user-42"`, `class="account-jane"`, `data-email="..."`) | rrweb 2.1.1 has no per-attribute masking hook, so `widget/src/modules/privacy.ts` removes every serialized attribute except the canonical empty `data-fidibeki-safe-text` marker. This intentionally sacrifices attribute-based visual fidelity rather than trying to classify which arbitrary values identify a person. |
| ARIA/`title`/`alt`/`placeholder` | Accessible names and hints frequently restate visible identity text or add new identity text not present elsewhere | Covered by the same attribute allowlist; `aria-label`, `aria-labelledby`, `aria-describedby`, `title`, `alt`, and `placeholder` are all dropped by default. |
| Links (`href`) and other URL attributes | `mailto:` links, profile links, or internal deep links can encode identity | Dropped by the same attribute allowlist (not on it). Loses some visual/functional fidelity (link targets are not visible in replay) in exchange for the guarantee. |
| Avatar/media URLs (`src`, `srcset`, `poster`, etc.) | Image URLs frequently embed an email hash (e.g. Gravatar) or a user ID in the path | Dropped by the same attribute allowlist; the widget already sets `inlineImages: false`, so this closes the remaining leak from serialized `src`/`srcset` values. Avatars will not render in replay; that is an intentional fail-closed tradeoff. |
| Initial page URL | `window.location.href` can carry identity in credentials, query/hash values, or a dynamic pathname suffix | The widget applies the shared rule-safe projection: configured exact target, longest matching prefix base, or origin-only for regex/all-pages/unproven paths. The backend independently recomputes that projection from the selected rule, verifies it matches the authorized request origin, and persists only the result. |
| Navigation custom events | The widget observes SPA navigation, whose new URL may contain an identifying path | Navigation events are pinned to the already validated safe session location. The current browser URL is never serialized into the replay event. |
| rrweb's internal `Meta` event | rrweb itself emits an event of `type: 4` with `data.href = window.location.href` on every full snapshot, a path the widget does not construct directly | `sanitizeReplayEvent` replaces every Meta `href` with the safe session location. Backend batch promotion and final merge reject any Meta/navigation URL that differs from the session projection. |
| Canvas pixels | Canvas content can render identity as pixels (signatures, "Welcome, Jane" banners, cropped avatars); there is no reliable automatic pixel redaction | Canvas recording is always suppressed. `recordCanvas` is fixed to `false`; API normalization and a database constraint force `replay_record_canvas=false`; batch verification rejects rrweb canvas source `9`. Canvas elements can remain as empty structure, but no 2D or WebGL pixels are uploaded or playable. |
| Stylesheets, fonts, logs, and unknown/custom incremental sources | CSS rules, font URLs, console logs, or future rrweb sources can carry arbitrary strings that bypass text masking | The widget accepts only the explicitly enumerated safe incremental sources and drops stylesheet, style declaration, canvas, font, log, adopted stylesheet, custom-element, and unknown sources. The backend independently applies the same allowlist and rejects an entire batch containing another source. |
| Persistent analytics visitor ID | `widget/src/analytics.ts` maintains a long-lived random UUID in `localStorage` used for funnel analytics and abuse-protection rate limiting | The same UUID is sent as `visitor_id` on `POST /v1/replay/sessions`, but the backend only uses it in-memory for the abuse-policy rate-limit key (`backend/internal/replay/http.go#enforceAbusePolicy`); it is never written to the `sessions` table (confirmed by `TestReplayHTTPFlow`, which asserts the `sessions` table has no `visitor_id` column and that neither the session-creation response nor the backoffice metadata response ever contains it) and is not used for fingerprinting. |
| Replay metadata / backoffice projection | The tenant API (`GET /v1/sessions/{id}`) and stream endpoint project session fields to the backoffice player | The response only includes `id`, `session_id` (the random replay-scoped identifier), the rule-safe `page_url`, timestamps, counts, and status, never an authenticated account ID, reporter email, raw path, or visitor ID. |
| Reporter contact | A reporter can optionally supply an email through the feedback/contact workflow (`reporter_email` on the feedback submission) | Reporter contact is submitted on the feedback record, not the replay session; the two share only an opaque foreign key (`sessions.feedback_id`) used for lookup, never reporter content. `sanitizeReplayEvent` and the attribute/text masking above apply uniformly regardless of whether a reporter later supplies contact information, so contact details typed into the widget's own contact field never reach replay capture (that field is not part of the recorded DOM subtree rrweb observes for capture purposes it shares with the page). |
| Existing/legacy objects and deletion paths | Sessions created before this change may have raw metadata and already-uploaded objects captured under a weaker policy | Migration `000075` invalidated pre-anonymous objects as version zero. Migration `000077` then invalidates v1 objects because their Meta/navigation events may contain a real pathname, advances new sessions to privacy version two, and selects older merged and batch objects for object-first retention cleanup. |
| Upload, merge, and stream trust boundary | A modified or old widget can bypass client-side sanitization and upload opaque JSON directly to the presigned object URL | Completed batches remain quarantined until the backend parses every event and validates exact event kinds, equality with the safe session URL, mask-only text, marker-only attributes, safe structural mutations, and the incremental-source allowlist. Unsafe objects are rejected and removed. Merge revalidates promoted batches, and tenant streaming only serves ready current-version sessions. |
| Capture before permission | Loading the widget, matching a rule, or rendering a consent prompt could import rrweb, create a replay session, or buffer a snapshot before the user grants permission | Existing rules default to consent mode `none`. The loader resolves a current managed or customer assertion before dynamically importing the replay module. Session creation and every batch lifecycle transition also require current server-side evidence scoped to the project, rule, mode, and notice version. |
| Stale grant after configuration change | A browser could retain a grant after the operator changes consent mode, notice version, or disables replay | Browser evidence is keyed by project/rule/notice, expires locally, and is rejected when versions differ. The backend joins the current rule configuration during session and batch authorization, so old clients fail closed. Configuration changes are recorded as tenant audit events. |
| Revocation/upload race | Buffered events or an in-flight old client could continue uploading after withdrawal | The runtime stops the recorder and discards unsent events before sending the withdrawal request. The backend marks the evidence withdrawn idempotently and re-authorizes session/batch declare, complete, and finalize operations against current evidence. The merge worker refuses sessions without accepted linked evidence. |
| Consent evidence becomes an identity graph | CMP references, browser identifiers, page URLs, or request metadata could be joined with replay sessions to identify a visitor | The evidence ledger stores only tenant/project/rule, purpose, mode/source, optional customer reference, notice version, locale, state, and server timestamps. It excludes IP address, user-agent, authenticated ID, reporter ID/email, analytics visitor ID, fingerprint, and URL. Evidence is not exposed in the replay player or copied to event payloads. |

## What is explicitly not attempted

- **No content-based PII classifier.** The mitigation for text and attributes is masking/allowlisting by default, not a heuristic identity detector (an email-regex replacement was explicitly called out in the issue as insufficient, and it is not used here).
- **No pixel-level canvas redaction.** Pixel analysis is not attempted; canvas recording is suppressed at every enforced boundary.
- **No personal fingerprinting.** The persistent visitor ID is not correlated with replay content, not exposed in the backoffice, and not used to infer identity.
- **No URL-content classifier.** Page minimisation is derived from the configured targeting rule, not from heuristics that try to recognize names, account IDs, or tokens.
- **No consent inferred from configuration.** Enabling replay, matching a rule, or a historical
  replay setting is never treated as an end-user grant.
- **No alternative legal-basis switch.** The product exposes managed and customer-managed explicit
  consent only. Any future non-consent mode requires separate product and legal approval.

## Accepted residual risks

- The explicit `data-fidibeki-safe-text` marker is a developer assertion that its subtree contains
  no identifying content. Incorrect use can opt unsafe text back in; the default remains masked and
  the backend only accepts the canonical empty marker.
- Removing all ordinary serialized attributes and string-bearing incremental sources reduces replay
  visual fidelity. This is an intentional fail-closed tradeoff for the anonymous replay mode.
- Exact targets and prefix bases are administrator-configured assertions that the route is safe to retain. Regex, all-pages, unmatched, and otherwise unproven dynamic paths fall back to origin-only.
- A customer CMP remains the source of truth for the complete proof behind a customer-managed
  assertion. Fidibeki retains only enough provenance to enforce its gate and support audit.
- The managed prompt is technical infrastructure presented on the customer's behalf. Its presence
  is not a legal determination that the configured notice or consent is sufficient.

## Verification

- Widget unit tests assert forced masking, rule-safe URL projection, and that a browser fixture containing sentinel identity values never appears in a session request or captured/uploaded batch.
- A Playwright browser test (`widget/tests/e2e/widget.spec.ts`) plants sentinel identity values in text, inputs, ARIA/title/alt attributes, DOM/data attributes, avatar URLs, query/hash values, and custom navigation, then asserts none of the sentinels occur in the uploaded batches.
- Backend tests (`backend/internal/replay/verification_test.go`, `replay_test.go`,
  `http_integration_test.go`, `backend/internal/project/validation_test.go`, and
  `backend/internal/db/migrations_test.go`) cover structural batch rejection, merge revalidation,
  rule-safe URL enforcement, forced input masking/canvas suppression, legacy stream refusal and object
  cleanup, and isolation of `visitor_id` from storage and API responses.
- Consent tests cover absent/malformed grants, managed and customer modes, idempotency, withdrawal,
  expiry, notice/configuration changes, direct API bypass attempts, and tenant-scoped diagnostics.
  Browser tests assert that no replay network request or rrweb capture happens before a grant and
  that revocation stops further capture without weakening the sentinel privacy fixtures.
