# Replay Identity Threat Model

**Date:** 2026-07-23
**Scope:** GitHub issue #53
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
| Initial page URL | `window.location.href` includes query parameters and a fragment that can carry emails, tokens, or user IDs | The widget canonicalizes to origin + normalized pathname (`widget/src/targeting.ts#canonicalPage`, reused via `widget/src/modules/privacy.ts#canonicalizeReplayURL`) before sending `page_url` to `POST /v1/replay/sessions`. The backend independently re-derives the same canonical form (`backend/internal/replay/http.go#canonicalReplayURL`) so persistence is enforced even if a legacy or compromised client sends a raw URL. |
| Navigation custom events | The widget emits a `navigation` custom rrweb event carrying `window.location.href` on every SPA route change | Canonicalized the same way as the initial URL before `record.addCustomEvent` is called; the event is dropped entirely (fails closed) if canonicalization fails. |
| rrweb's internal `Meta` event | rrweb itself emits an event of `type: 4` with `data.href = window.location.href` on every full snapshot — a path the widget does not construct directly | `sanitizeReplayEvent` in `widget/src/modules/privacy.ts` intercepts every serialized event before upload and rewrites `Meta` event `href` through the same canonicalizer, or drops the event if it cannot be canonicalized. |
| Canvas pixels | Canvas content can render identity as pixels (signatures, "Welcome, Jane" banners, cropped avatars); there is no reliable automatic pixel redaction | Canvas recording is always suppressed. `recordCanvas` is fixed to `false`; API normalization and a database constraint force `replay_record_canvas=false`; batch verification rejects rrweb canvas source `9`. Canvas elements can remain as empty structure, but no 2D or WebGL pixels are uploaded or playable. |
| Stylesheets, fonts, logs, and unknown/custom incremental sources | CSS rules, font URLs, console logs, or future rrweb sources can carry arbitrary strings that bypass text masking | The widget accepts only the explicitly enumerated safe incremental sources and drops stylesheet, style declaration, canvas, font, log, adopted stylesheet, custom-element, and unknown sources. The backend independently applies the same allowlist and rejects an entire batch containing another source. |
| Persistent analytics visitor ID | `widget/src/analytics.ts` maintains a long-lived random UUID in `localStorage` used for funnel analytics and abuse-protection rate limiting | The same UUID is sent as `visitor_id` on `POST /v1/replay/sessions`, but the backend only uses it in-memory for the abuse-policy rate-limit key (`backend/internal/replay/http.go#enforceAbusePolicy`); it is never written to the `sessions` table (confirmed by `TestReplayHTTPFlow`, which asserts the `sessions` table has no `visitor_id` column and that neither the session-creation response nor the backoffice metadata response ever contains it) and is not used for fingerprinting. |
| Replay metadata / backoffice projection | The tenant API (`GET /v1/sessions/{id}`) and stream endpoint project session fields to the backoffice player | The response only ever included `id`, `session_id` (the random replay-scoped identifier), `page_url` (now canonical), timestamps, counts, and status — never an authenticated account ID, reporter email, or the visitor ID. `TestReplayHTTPFlow` and `TestReplayHTTPFlowRetentionAndAggregateLimits`-style coverage assert this. |
| Reporter contact | A reporter can optionally supply an email through the feedback/contact workflow (`reporter_email` on the feedback submission) | Reporter contact is submitted on the feedback record, not the replay session; the two share only an opaque foreign key (`sessions.feedback_id`) used for lookup, never reporter content. `sanitizeReplayEvent` and the attribute/text masking above apply uniformly regardless of whether a reporter later supplies contact information, so contact details typed into the widget's own contact field never reach replay capture (that field is not part of the recorded DOM subtree rrweb observes for capture purposes it shares with the page). |
| Existing/legacy objects and deletion paths | Sessions created before this change may have raw metadata and already-uploaded objects captured under the weaker policy | Migration `000075` canonicalizes stored page URLs and marks all existing sessions with replay privacy version zero. Version-zero sessions report `expired`, have no replay URL, cannot be streamed or merged, and are selected immediately by the retention cleaner. The cleaner deletes merged and batch objects before nulling storage keys. New sessions default to version one. Reporter anonymization continues to remove objects linked through reporter feedback. |
| Upload, merge, and stream trust boundary | A modified or old widget can bypass client-side sanitization and upload opaque JSON directly to the presigned object URL | Completed batches remain quarantined until the backend parses every event and validates exact event kinds, URL canonicality, mask-only text, marker-only attributes, safe structural mutations, and the incremental-source allowlist. Unsafe objects are rejected and removed. Merge revalidates promoted batches, and tenant streaming only serves ready version-one sessions. |

## What is explicitly not attempted

- **No content-based PII classifier.** The mitigation for text and attributes is masking/allowlisting by default, not a heuristic identity detector (an email-regex replacement was explicitly called out in the issue as insufficient, and it is not used here).
- **No pixel-level canvas redaction.** Pixel analysis is not attempted; canvas recording is suppressed at every enforced boundary.
- **No personal fingerprinting.** The persistent visitor ID is not correlated with replay content, not exposed in the backoffice, and not used to infer identity.
- **No #54 (automatic feedback/element-context capture) scope.** The primitives in `widget/src/modules/privacy.ts` (`canonicalizeReplayURL`, `sanitizeReplayEvent`, `SAFE_TEXT_ATTRIBUTE`, attribute allowlist) are written generically so a future automatic feedback/element-context capture path can reuse them, but no such capture path is implemented here.

## Accepted residual risks

- The explicit `data-fidibeki-safe-text` marker is a developer assertion that its subtree contains
  no identifying content. Incorrect use can opt unsafe text back in; the default remains masked and
  the backend only accepts the canonical empty marker.
- Removing all ordinary serialized attributes and string-bearing incremental sources reduces replay
  visual fidelity. This is an intentional fail-closed tradeoff for the anonymous replay mode.
- `sessions.page_url` (canonicalized pathname only) is not nulled during reporter anonymization, consistent with how `feedbacks.page_url` is already treated elsewhere in the schema; a path could theoretically embed identity (e.g. `/users/jane.doe@example.com`). This mirrors existing product behavior for feedback and was not in scope to change here.

## Verification

- Widget unit tests (`widget/tests/unit/replay.test.ts`, new privacy-focused cases) assert the forced masking options, URL canonicalization, and that a browser fixture containing sentinel identity values never appears in a captured/uploaded batch.
- A Playwright browser test (`widget/tests/e2e/widget.spec.ts`) plants sentinel identity values in text, inputs, ARIA/title/alt attributes, DOM/data attributes, avatar URLs, query/hash values, and custom navigation, then asserts none of the sentinels occur in the uploaded batches.
- Backend tests (`backend/internal/replay/verification_test.go`, `replay_test.go`,
  `http_integration_test.go`, `backend/internal/project/validation_test.go`, and
  `backend/internal/db/migrations_test.go`) cover structural batch rejection, merge revalidation,
  URL canonicalization, forced input masking/canvas suppression, legacy stream refusal and object
  cleanup, and isolation of `visitor_id` from storage and API responses.
