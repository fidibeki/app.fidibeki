# Founder decision package — issues #58, #62, #63 (prepared 2026-07-24)

Every remaining blocker on #58, #62, and #63 — and the final acceptance of #64 — is a fact or
approval that engineering cannot invent: legal classifications, retention periods, provider
contracts, lawful bases, and DPIA outcomes. The code-owned registries
(`backend/internal/privacy/retention_registry.go`, `governance_registry.go`, `ropa_registry.go`)
deliberately reject placeholders, so nothing ships until the entries below are answered.

This package lists each decision with an ID, the verified current facts, a proposed default, and
what the answer unblocks. **Proposals are engineering suggestions for review, not legal advice or
recorded conclusions.** Reply per decision ID with "approve" or an edited value; each answer is
then recorded in the corresponding registry field and the gated slices finish.

## Part A — #58: widget targeting / functional-storage classification

**D1. Classify widget browser storage for targeting and funnel analytics.**
**DECIDED 2026-07-24 (founder): option (a) — keep everything consent-required, current shipped
behavior. Recorded in `docs/widget-analytics-consent.md`; #58 closed on this decision.
Reclassification would require a new documented legal review.**

- Facts: the technical core is complete and consent-gated end to end (dormant default, no
  persistent identity/outbox/network before grant, withdrawal cleanup, fail-closed backend,
  documented in `docs/widget-analytics-consent.md`). The retention registry keeps
  `widget_targeting` (visit state) and `widget_analytics_browser` as `decision_required`.
- Decision: are targeting visit state and funnel-analytics browser storage (a) non-essential,
  consent-required storage (current behavior), or (b) partly "strictly necessary/functional"
  (would allow default-on for the functional subset)?
- Proposed default: **(a) keep everything consent-required.** It matches the shipped behavior,
  ePrivacy's narrow strictly-necessary reading, and requires no new engineering.
- Unblocks: documenting the decision closes the last #58 checkbox; broad enablement follows the
  documented classification.

## Part B — #62: retention periods (13 categories currently `decision_required`)

Facts: cleaners already enforce `auth_magic_links` (hourly, audited), attachments/replay/exports
(plan/config driven), and widget analytics. Sentry event scrubbing is enforced by allowlist
reconstruction. Everything below is inventoried but **not swept** until a period is approved.
"Start" = retention trigger. All periods become code-owned registry values and bounded, retryable,
hold-aware cleaners; tenant-configurable shorter retention (D15) applies where marked (T).

| ID | Category (registry key) | Data | Proposed period and action |
| --- | --- | --- | --- |
| D2 | `workspace_configuration` | tenants, members, projects, rules, API keys | Retain for workspace life; hard-delete via the existing tenant-deletion flow 30 days after deletion request completes |
| D3 | `workflow_metadata` (T) | feedback, issues, comments, activities, clarifications, member notifications | Default 24 months after last activity, then delete or anonymize free text and reporter identifiers; notifications 90 days |
| D4 | `authentication_security` | tenant/platform sessions (IP + user-agent), security events, reporter access tokens | Sessions deleted 30 days after expiry/revocation; IP and raw user-agent minimized (hash/truncate) 30 days after issuance; `platform_security_events` 12 months |
| D5 | `identity_assets` | logos, avatars | Until replaced or member/workspace removal, plus 30 days for backup convergence |
| D6 | `widget_analytics_browser` (T) | consent-gated browser keys | Follow D1; browser state max 12 months since grant, earlier on withdrawal (already enforced) |
| D7 | `widget_targeting` (T) | visit state for rule targeting | Follow D1; visit state max 6 months rolling |
| D8 | `consent_evidence` | purpose-specific consent/withdrawal evidence | Keep 3 years after withdrawal or expiry as accountability evidence, without content beyond the evidence record itself |
| D9 | `ai_processing` | AI ledgers and derived evidence | 12 months after creation; provider-side deletion evidence retained with the ledger |
| D10 | `integration_delivery` (T) | integration config, delivery logs/attempts, disclosure ledgers | Config until integration deletion; delivery attempt logs 90 days; disclosure ledgers 24 months (rights-support evidence) |
| D11 | `privacy_rights` | privacy cases, communications, exports evidence | 3 years after case closure; export objects already deleted on their own short clock |
| D12 | `access_security_audit` | sensitive-access audit events | 12 months, then delete (or aggregate counts only) |
| D13 | `billing_records` | billing/subscription evidence | Statutory accounting period of the applicable jurisdiction (confirm: which entity/jurisdiction applies — e.g. PT/EU 10 years); no sweep before confirmation |
| D14 | `breach_register` | breach incidents, assessments, notifications | 5 years after incident closure, under legal hold when open; also satisfies the #64 "retain under approved policy" criterion |

**D15. Tenant-configurable shorter retention.** Approve the mechanism: tenants may shorten (T)
categories below the defaults, never below plan-independent privacy minimums (proposed floor:
30 days), never overriding legal holds. Unblocks the `configurable` registry states and UI.

**D16. Hold and alert scope.** Approve that (a) legal/customer holds block deletion only for the
documented scope/period (already case-scoped for rights data), and (b) overdue-cleanup alerts are
metric-only with no personal data in telemetry (the health ledger already stores only counters).

Unblocks: each approved row turns into a cleaner + registry update + staging gate evidence; after
all rows are approved and enforced, #62 closes.

## Part C — #63: provider facts, contracts, and transfers (12 boundaries)

Facts verified from staging configuration today: `EMAIL_DRIVER=resend` (Resend is live in
staging); object storage runs in Railway bucket region **ams (EU-Netherlands)**; Railway services
(API/web/worker), Postgres, and Redis have **no explicit region override** — the actual regions
must be read from the Railway dashboard before recording them; Sentry DSN is configured on API and
worker; Turnstile site key on web; Gemini and Stripe keys on API/worker.

For each provider the registry requires: legal entity, processing locations, transfer mechanism,
contract status, owner, review date, approval. Engineering pre-filled likely values to confirm —
**none are recorded yet**:

| ID | Provider (registry `ProviderID`) | To confirm and record |
| --- | --- | --- |
| D17 | `postgresql_runtime`, `redis_asynq` (Railway) | Railway Corp. legal entity; actual staging deploy/DB regions from dashboard; Railway DPA/SCCs; contract status |
| D18 | `s3_compatible_object_storage` | Railway bucket, region ams (verified); same Railway DPA; confirm backups/replication location |
| D19 | `resend_transactional_email` | Resend Inc. (US); sending region and data location; Resend DPA + SCC/DPF status |
| D20 | `email_capture_filesystem` | Dev-only boundary, not enabled in staging (`EMAIL_DRIVER=resend`); approve classifying as internal, no contract |
| D21 | `google_gemini_api` | Contracting Google entity; paid-tier terms (no training on submitted data) vs free tier; data location; Gemini API terms/DPA |
| D22 | `stripe_billing_api` | Stripe entity (Stripe Payments Europe vs Stripe Inc.); DPF/SCCs; DPA status |
| D23 | `sentry_error_telemetry` | Sentry (Functional Software Inc.); choose US vs EU data residency for the project; provider-side retention setting (default 90 days) and deletion configuration; DPA |
| D24 | `slack_incoming_webhook`, `microsoft_teams_incoming_webhook`, `customer_generic_webhook` | Approve classification: customer-configured destinations selected by the customer (customer instruction boundary), no Fidibeki subprocessor contract; disclosure minimization is already enforced (#66) |
| D25 | `cloudflare_turnstile` | Cloudflare Inc.; global edge processing; DPF/SCCs; confirm what Turnstile receives at signup |

**D26. Article 28 DPA.** Approve and publish/execute the Fidibeki↔customer DPA covering subject
matter, duration, nature/purpose, categories, instructions, confidentiality, security, assistance,
deletion/return, audits, and breach notification (the #64 notification workflow implements the
breach-notification term). Engineering supplies the technical annexes from the registries.

**D27. Subprocessor change process.** Approve the authorization/notification/objection mechanism.
Proposal: published subprocessor register + 30-day advance email notice to workspace owners for
new/replaced subprocessors + documented objection path (terminate with export assistance).

**D28. Residency claims.** Until D17–D25 are recorded, approve that UI/marketing/contracts make
**no EU-residency claim** (the governance gate already blocks unverified claims).

Unblocks: recording D17–D28 satisfies the registry completeness gate; then engineering finishes
staging reconciliation, link tests, and localized notice E2E, and #63 closes.

## Part D — #63: lawful bases and DPIA screening (19 ROPA activities)

Each activity in `ropa_registry.go` is `lawful_basis: pending` and `dpia.outcome: pending`. The
founder/legal reviewer records the basis (with reviewer + timestamp) and a DPIA screening outcome
(`not_required` needs evidenced reasons; `required` demands an actual DPIA before publication).
Proposed starting points for review — Fidibeki-controller activities cite Art. 6 GDPR; processor
activities record the instruction framework instead (Art. 28; the customer holds the Art. 6 basis):

| ID | Activity | Proposed basis / DPIA screening |
| --- | --- | --- |
| D29 | workspace_identity_and_access | Contract 6(1)(b) / not required |
| D30 | authentication_and_account_security | Legitimate interests (security) 6(1)(f) / not required |
| D31 | billing_and_subscription_management | Contract + legal obligation 6(1)(c) / not required |
| D32 | feedback_workflow_management; attachment_evidence_handling; identity_media_assets | Processor on customer instructions / not required |
| D33 | session_replay_reproduction | Processor on customer instructions / **screening likely `required`** (systematic monitoring indicators) — decide with mitigations already shipped (consent gate, masking, retention) |
| D34 | widget_funnel_analytics; widget_presentation_behavior | Consent (per D1) / not required with reasons (pseudonymous, consent-gated) — confirm |
| D35 | consent_evidence_management | Legal obligation/accountability 6(1)(c)+(f) / not required |
| D36 | ai_assisted_triage | Processor on customer instructions / screening decision needed (automated analysis indicator) |
| D37 | integration_notification_delivery | Processor on customer instructions / not required (minimized payloads, #66) |
| D38 | privacy_rights_operations | Legal obligation 6(1)(c) / not required |
| D39 | security_and_access_audit | Legitimate interests 6(1)(f) / not required |
| D40 | breach_response_recordkeeping | Legal obligation 6(1)(c) / not required |
| D41 | abuse_and_bot_diagnostics; signup_abuse_verification | Legitimate interests 6(1)(f) / not required |
| D42 | external_communications_delivery; infrastructure_and_platform_operations | Follows the provider decisions in Part C / not required |
| D43 | identity confirmation | Name the accountable reviewer (person) whose identity+timestamp the registry records for every basis/DPIA entry |

**D44. DPO / supervisory consultation.** Record the documented position on when DPO/legal or
supervisory-authority consultation must be considered (registry requires an explicit statement,
not an assumption of always/never).

## Part E — what each answer unblocks

- **D1** → closes #58.
- **D2–D16** → #62 cleaners are built and swept per approved periods, staging gate runs, #62
  closes. D14 also satisfies #64's register-retention criterion.
- **D17–D28** → #63 registry completeness gate passes; DPA published; reconciliation/link/E2E
  slices run; #63 closes. D26 also provides the DPA notification terms #64's workflow references.
- **D29–D44** → ROPA/DPIA publication-readiness passes; part of #63 closure.
- #64's remaining engineering evidence (staging tabletop exercises) is being produced
  independently and does not wait on this package; only its final acceptance references D14/D26.

Nothing in this package changes production; all validation stays in staging per the standing
constraint.
