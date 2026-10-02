# Session Replay Consent

**Status:** Engineering contract for staging review
**Last technical review:** 2026-07-24

This document describes the implemented product and processor behavior. It is not legal advice, a
published notice, or approval to enable replay broadly. Privacy/legal review is required before
broad staging enablement. Production remains out of scope until explicitly authorized.

The `widget_analytics` purpose (`docs/widget-analytics-consent.md`) mirrors this same
grant/deny/revoke architecture but is a fully independent purpose, evidence ledger, and gate. A
session replay grant never authorizes analytics collection, and vice versa.

## Roles And Boundaries

- The customer determines the purpose and configuration of session replay and remains the factual
  controller for the website visitor data.
- Fidibeki processes replay and minimum consent provenance on the customer's instructions.
- A Fidibeki-managed prompt is presented on the customer's behalf. It does not make Fidibeki the
  controller and does not certify that the customer's wording or legal basis is sufficient.
- In customer-managed mode, the customer's CMP remains the source of truth for the complete consent
  proof. Fidibeki stores only the structured assertion needed to enforce capture.
- Consent never relaxes replay privacy controls. Private-region/input masking, attribute filtering,
  rule-safe URLs, canvas suppression, event validation, retention, and access controls still apply.

The DPA and public privacy materials should describe session replay, consent-evidence processing,
retention/deletion, access controls, and the currently configured subprocessors. The adapter
inventory is maintained in `docs/privacy-data-map.md`; provider contracts, regions, and transfer
mechanisms must be checked against the actual environment before publication.

## Activation

Replay is disabled by default. Existing rules migrate to consent mode `none` and cannot capture.

Enabling replay in the rule editor requires one explicit mode:

1. `fidibeki_managed`: configure controller name, purpose, privacy-policy HTTPS URL, notice version,
   supported locales, localized notice copy, retention text, and withdrawal behavior.
2. `customer_managed`: configure the expected purpose, notice version, locales, expiry policy, and
   the CMP source used in structured runtime assertions.

Changing the mode or notice version invalidates incompatible grants. Disabling replay blocks future
session and batch operations without deleting unrelated feedback or historical data.

## Customer-Managed Runtime

The host page grants only after its CMP has resolved an affirmative choice:

```js
window.Fidibeki.replay.grantConsent({
  consentId: 'cmp-reference',
  purpose: 'session_replay',
  noticeVersion: '2026-07',
  locale: 'pt-PT',
  source: 'customer_cmp',
})
```

Withdrawal uses:

```js
window.Fidibeki.replay.revokeConsent()
```

A boolean global is not a valid integration. Purpose, notice version, locale, source, and consent
reference are validated against the active rule. Repeating the same valid grant is idempotent.
Query parameters and URL hashes are never part of the persisted decision scope.

## Fidibeki-Managed Runtime

The prompt:

- is localized for the rule's supported locales;
- gives accept and reject equivalent prominence with no preselected choice;
- identifies the controller, purpose, retention information, and privacy-policy link;
- supports keyboard use and moves initial focus to rejection;
- records rejection without starting replay;
- exposes withdrawal through the same compact widget surface after acceptance.

Rejecting or dismissing replay consent does not suppress the normal feedback workflow.

## Enforcement And Evidence

Before a current grant, Fidibeki does not import/start rrweb, create a replay session, serialize a
snapshot, buffer events, or upload batches. The backend independently authorizes session creation
and every batch lifecycle transition. The merge worker refuses unconsented sessions.

The consent ledger stores:

- tenant, project, and rule IDs;
- purpose `session_replay`;
- mode/source and optional customer CMP reference;
- notice version and locale;
- accepted, withdrawn, and expiry timestamps;
- active, withdrawn, or expired state.

It does not store IP address, raw user-agent, authenticated account ID, email, reporter identity,
analytics visitor ID, fingerprint, or page URL. Evidence is not copied into replay events, widget
analytics, integrations, application logs, or player responses.

## Expiry, Change, And Withdrawal

- A grant expires according to the configured policy and requires a fresh choice.
- A changed notice version or consent mode requires a fresh compatible grant.
- Withdrawal stops capture and discards unsent events before the network request is attempted.
- The backend makes withdrawal idempotent and blocks subsequent sessions/uploads even for an old or
  modified client.
- Previously accepted objects follow the approved replay retention/deletion policy; withdrawal does
  not create indefinite retention and does not silently delete unrelated feedback.

## Consent Copy Governance

Treat notice copy and translations as controlled product content:

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
- Verify no replay request occurs before acceptance.
- Verify withdrawal immediately stops capture and new uploads.
- Verify marked private content, form values, recognizable contact details, and canvas pixels never reach uploaded or rendered replay. See [capture and private content](session-replay-capture.md).
- Verify consent diagnostics expose state and timestamps without identifiers or raw CMP references.
- Verify evidence and replay retention against the approved DPA/privacy terms.
- Verify the current subprocessor list and environment-specific provider configuration.

Passing these technical checks is necessary but does not replace privacy/legal approval.
