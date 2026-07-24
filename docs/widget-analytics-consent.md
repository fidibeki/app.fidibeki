# Widget Analytics Consent

**Status:** Engineering contract for staging review
**Last technical review:** 2026-07-24

This document describes the implemented product and processor behavior for the `widget_analytics`
purpose. It is not legal advice, a published notice, or approval to enable analytics collection
broadly. Privacy/legal review is required before broad staging enablement. Production remains out
of scope until explicitly authorized.

This purpose is deliberately independent from session replay (`docs/session-replay-consent.md`) and
from reporter-contact consent (`purpose feedback_updates`, see `docs/privacy-data-map.md`). A grant,
denial, or withdrawal for one purpose never implies, and is never inferred from, a grant/denial/
withdrawal for another. The architecture mirrors session replay's consent orchestration (same
grant/deny/revoke lifecycle, same notice/locale/version contract, same evidence-minimization rules)
but uses a fully separate evidence ledger, storage keys, HTTP endpoints, and widget controller.

## Roles And Boundaries

- The customer determines the purpose and configuration of widget analytics and remains the factual
  controller for the website visitor data.
- Fidibeki processes funnel analytics and minimum consent provenance on the customer's instructions.
- A Fidibeki-managed prompt is presented on the customer's behalf. It does not make Fidibeki the
  controller and does not certify that the customer's wording or legal basis is sufficient.
- In customer-managed mode, the customer's CMP remains the source of truth for the complete consent
  proof. Fidibeki stores only the structured assertion needed to enforce the collection gate.
- Consent never relaxes the pseudonymous-treatment contract: the visitor identifier is stable across
  sessions and is not claimed to be anonymous absent a documented anonymization assessment.

## What Is Gated

Per widget rule, `widget_analytics` consent gates:

- Creating or reading the persistent visitor identifier (`fidibeki:visitor:v1` in the visitor's
  browser).
- Creating or reading the durable funnel event outbox (`fidibeki:analytics-outbox:v2:*`).
- Observing and transmitting `matched`, `impression`, `interaction`, `submitted`, and `dismissed`
  funnel events for that rule, whether via the batched `/v1/widget/events` endpoint or the optional
  `submitted` event embedded directly in a feedback submission.
- Calling the analytics ingestion endpoint at all for that rule.

It does **not** gate: strictly necessary feedback delivery, recurrence/suppression state
(`fidibeki:state:v2:*`, `fidibeki:session:v1`), or widget targeting/objective-policy signals
(`fidibeki:policy-visit:v1:*`, and in-memory-only engagement observation) — those are distinct
technical purposes classified separately in `docs/privacy-data-map.md`. Rejecting or withdrawing
analytics consent leaves feedback submission and configured recurrence behavior fully usable.

## Activation

Analytics collection is off by default. A rule's `analytics_consent.mode` defaults to `none`
(dormant) and existing rules migrate to `none` — this is an opt-in feature, not an opt-out one.

Enabling analytics collection in the rule editor requires one explicit mode:

1. `fidibeki_managed`: configure controller name, purpose, privacy-policy HTTPS URL, notice version,
   supported locales, localized notice copy, retention text, and withdrawal behavior.
2. `customer_cmp`: configure the expected purpose, notice version, locales, expiry policy, and the
   CMP source used in structured runtime assertions.

Changing the mode or notice version invalidates incompatible grants. Disabling analytics (returning
to `none`) blocks future collection for that rule without deleting previously collected funnel data
or unrelated feedback/replay data.

## Customer-Managed Runtime

The host page grants only after its CMP has resolved an affirmative choice:

```js
window.Fidibeki.analytics.grantConsent({
  consentId: 'cmp-reference',
  purpose: 'widget_analytics',
  noticeVersion: '2026-07',
  locale: 'pt-PT',
  source: 'customer_cmp',
})
```

Withdrawal uses:

```js
window.Fidibeki.analytics.revokeConsent()
```

Purpose, notice version, locale, source, and consent reference are validated against the active
rule. Repeating the same valid grant is idempotent. Query parameters and URL hashes are never part
of the persisted decision scope, the visitor identifier, or any analytics dimension.

## Fidibeki-Managed Runtime

The prompt:

- is localized for the rule's supported locales;
- gives accept and reject equivalent prominence with no preselected choice;
- identifies the controller, purpose, retention information, and privacy-policy link;
- supports keyboard use and moves initial focus to rejection;
- records rejection without starting collection;
- exposes withdrawal through the same compact widget surface after acceptance.

Rejecting or dismissing analytics consent does not suppress the normal feedback workflow.

## Enforcement And Evidence

Before a current grant for a rule, the widget does not create/read the persistent visitor
identifier, does not create/read the outbox, does not observe or transmit any funnel event for that
rule, and does not call the analytics endpoint. The backend independently authorizes every ingested
event: `/v1/widget/events` rejects a batch outright if any event's rule has no configured analytics
consent mode, or if the event's consent token does not resolve to a currently active, non-expired
`widget_analytics` grant scoped to that exact rule/notice-version/source. A `submitted` event
optionally embedded in a feedback submission uses the same widget-side gate as the batch endpoint —
a compliant client simply omits it when ungranted, which never blocks the feedback submission
itself.

The consent ledger (`widget_analytics_consents`, structurally identical to and fully separate from
`replay_consents`) stores:

- tenant, project, and rule IDs;
- purpose `widget_analytics`;
- mode/source and optional customer CMP reference;
- notice version and locale;
- accepted, withdrawn, and expiry timestamps;
- active, withdrawn, or expired state.

It does not store IP address, raw user-agent, authenticated account ID, email, reporter identity,
replay session/consent identity, fingerprint, or page URL. Evidence is not copied into funnel event
rows, replay events, integrations, application logs, or backoffice diagnostics beyond aggregate
state.

## Expiry, Change, And Withdrawal

- A grant expires according to the configured policy and requires a fresh choice.
- A changed notice version or consent mode requires a fresh compatible grant.
- Withdrawal aborts any in-flight delivery request, discards the queued outbox events (both the
  in-memory queue and every persisted `fidibeki:analytics-outbox:*` entry for that browser), and
  removes the persistent visitor identifier — all before the network revocation call completes, so a
  `pagehide`/`visibilitychange` race can never trigger a `sendBeacon` retry of the just-discarded
  queue.
- The backend makes withdrawal idempotent and fail-closes: once withdrawn, a token no longer
  authorizes ingestion, even for an old or modified client that keeps resending it.
- Previously collected funnel events follow the approved analytics retention policy (raw events 90
  days, daily aggregates 730 days); withdrawal does not retroactively delete already-ingested
  aggregate counts and does not silently delete unrelated feedback or replay data.

## Consent Copy Governance

Treat notice copy and translations as controlled product content, on the same terms as session
replay (`docs/session-replay-consent.md`):

1. Assign a new immutable notice version for a substantive purpose, controller, retention, sharing,
   or rights change.
2. Obtain privacy/legal review for the source copy and every supported locale.
3. Record the approved controller, purpose, privacy-policy URL, retention text, and locales in the
   rule configuration.
4. Test rejection, acceptance, expiry, notice-version change, and withdrawal in staging.
5. Confirm the rule diagnostic remains blocked before a grant and becomes active only after one.

Do not reuse an old notice version for changed meaning. Copy-only corrections that legal considers
non-substantive still need an auditable configuration revision.

## Operational Review Checklist

- Confirm the customer selected the intended managed or CMP mode.
- Confirm all notice fields and translations match the approved material.
- Confirm the privacy-policy URL is HTTPS and controlled by the customer.
- Verify no analytics storage key or request occurs before acceptance.
- Verify withdrawal immediately clears queued events, the visitor identifier, and blocks retry.
- Verify consent diagnostics expose state and timestamps without identifiers or raw CMP references.
- Verify evidence and analytics retention against the approved DPA/privacy terms.
- Verify that granting or withdrawing `session_replay` or reporter-contact consent has no effect on
  `widget_analytics` state, and vice versa.

Passing these technical checks is necessary but does not replace privacy/legal approval.
