# Fidibeki Technical Privacy Data Map

**Last technical review:** 2026-07-24

This document is an engineering inventory for founder and legal review. It is not a published
privacy policy, data processing agreement, residency commitment, legal opinion, or guarantee.
Actual provider configuration and contractual terms must be reviewed separately before making
customer-facing claims.

## Data Categories And Purpose

| Category | Examples | Technical purpose | Primary store | Effective technical retention |
|---|---|---|---|---|
| Workspace identity | Workspace name, slug, plan, status | Tenant routing, authorization, billing state | PostgreSQL | Workspace lifetime and deletion workflow |
| Members and authentication | Email, permissions, session/token hashes, login and invitation records | Authenticate people and enforce RBAC | PostgreSQL, Redis for rate limits/queues | Operational lifecycle; secrets and hashes are never exported |
| Projects and widget configuration | Domains, rules, targeting, appearance, form blocks | Decide where and how feedback is collected | PostgreSQL | Workspace lifetime |
| Feedback and issue workflow | Original description, title, rating, answers, comments, page URL, status, tags, assignments | Triage, decision, resolution, and reporter communication | PostgreSQL | Workspace lifetime until explicit workspace deletion or supported anonymization |
| Reporter contact and consent | Normalized email; notification preference; structured consent evidence (purpose, notice version, locale, source); server-authoritative grant/withdrawn timestamps | Verification and opted-in status communication | PostgreSQL | Email and preference until anonymization, workspace deletion, or unsubscribe; non-identifying evidence metadata (purpose/notice version/locale/source/timestamps) is retained after withdrawal or anonymization as the minimum audit record proving consent was collected and honored |
| Attachments and derived analysis | Screenshot/audio/video objects, transcript, sentiment, moderation flags, declared metadata | Give operational context to feedback | Private S3-compatible object storage and PostgreSQL manifest | Objects and derived transcript: plan storage limit (30/90/365 days); manifest retained with workflow |
| Session replay | rrweb batches/merged object; session-scoped random identifier; rule-safe page context (configured exact target, longest matching prefix base, or origin only; credentials/query/fragment and unproven dynamic suffixes removed); timing and event counts; no canvas pixels | Reproduce the reported page experience without exposing the authenticated user's identity | Private S3-compatible object storage and PostgreSQL manifest | Anonymous by enforced contract: visible text and form values are masked; serialized attributes are empty except the explicit `data-fidibeki-safe-text` marker; Meta/navigation events are pinned to the server-validated session projection; canvas, stylesheets, fonts, logs, and unknown custom sources are dropped. The backend structurally validates every event before promotion and again before merge. Pre-contract objects are not playable and are selected for object-first retention cleanup. `data-fidibeki-block` excludes a subtree; `data-fidibeki-safe-text` explicitly opts known-safe static text back in. Objects otherwise follow the plan limit (30/90/365 days); manifests remain with workflow. See `docs/superpowers/specs/2026-07-23-replay-identity-threat-model.md`. |
| Session replay consent evidence | Tenant/project/rule scope; purpose `session_replay`; consent mode/source; customer consent reference where supplied; notice version; locale; server-authoritative accepted, withdrawn, and expiry timestamps; active/withdrawn/expired state | Enforce the replay capture gate and retain the minimum provenance needed for audit | PostgreSQL | Evidence follows the approved consent-evidence retention policy. It is separate from replay events and reporter-contact consent. The ledger does not contain IP address, user-agent, authenticated account ID, email, reporter ID, visitor ID, fingerprint, or page URL. Legal review must approve the external retention statement before broad staging enablement. |
| Pseudonymous widget analytics | Visitor/page-view/event IDs, page path, funnel event and daily counts | Measure matched, impression, interaction, submit, and dismiss funnels once a rule holds a current `widget_analytics` grant | PostgreSQL | Raw events 90 days; daily aggregates 730 days. Collection is consent-gated: see `docs/widget-analytics-consent.md`. The visitor identifier is stable across sessions and is therefore treated as pseudonymous, not anonymous, absent a documented anonymization assessment. |
| Widget analytics consent evidence | Tenant/project/rule scope; purpose `widget_analytics`; consent mode/source; customer consent reference where supplied; notice version; locale; server-authoritative accepted, withdrawn, and expiry timestamps; active/withdrawn/expired state | Enforce the analytics collection gate and retain the minimum provenance needed for audit | PostgreSQL | Evidence follows the approved consent-evidence retention policy. It is a fully separate ledger/table from session replay and reporter-contact consent evidence — grants are never inferred from, or joined across, purposes. The ledger does not contain IP address, user-agent, authenticated account ID, email, reporter ID, visitor ID, fingerprint, or page URL. Legal review must approve the external retention statement before broad staging enablement. |
| Widget targeting and objective-policy signals | Per-page visit counts keyed by session; observed active time, scroll depth, and interaction counts (in-memory only, never persisted); explicit `data-fidibeki-*` engagement/content/friction flags | Decide whether and when a configured presentation (launcher, prompt, reaction, inline) becomes eligible to display, per the customer's targeting/objective configuration | Browser `localStorage` only (`fidibeki:policy-visit:v1:*`); never transmitted to the backend | Same-origin browser storage for the life of the browser profile. This is a distinct technical purpose from widget_analytics funnel measurement: it decides *whether the widget shows*, not *what gets measured about a visitor*, and observed engagement signals are never persisted or transmitted, only visit counts. Whether this purpose additionally requires consent under a strict ePrivacy Article 5(3) reading is an open legal question — see Review Gaps. |
| Widget recurrence and suppression state | Per-page widget state (new/submitted/minimized/dismissed/snoozed) and a random session identifier | Avoid re-showing a widget the visitor already dismissed, submitted, or snoozed, per the customer's configured recurrence policy | Browser `localStorage`/`sessionStorage` only (`fidibeki:state:v2:*`, `fidibeki:session:v1`); never transmitted to the backend | Same-origin browser storage; cooldown/session/never per rule configuration. Functional/strictly-necessary in nature (it exists to make the feedback UI usable rather than repetitive) and is technically and evidentially isolated from analytics and replay purposes. |
| Integration delivery | Slack/Teams/webhook configuration, encrypted credentials, attempts and errors | Send operational notifications and retry delivery | PostgreSQL, Asynq/Redis | Workspace/integration lifecycle; credentials are never exported |
| Email delivery | Recipient, message metadata, delivery/bounce/complaint events | Send authentication, lifecycle, and reporter messages | PostgreSQL, Asynq/Redis, configured email provider | Operational and audit lifecycle |
| AI processing | Rule-owned authorization; minimized submitted text or explicitly selected media; redacted advisory results; provider/model/purpose/category/status evidence; category-only redaction manifest; external file deletion evidence | Suggest triage and analyze only the input categories explicitly enabled for the matching rule | PostgreSQL, private storage, Google Gemini Developer API when explicitly enabled | Existing and new rules default off. Source follows workflow/media retention; reporter anonymization removes derived values, manifests, and runs. Provider files are deleted immediately after processing with a retry ledger; Google documents a 48-hour automatic expiry as a fallback, not Fidibeki's deletion target. |
| Billing | Plan/subscription state and external customer/subscription references | Entitlement, invoicing, dunning, and deletion scheduling | PostgreSQL and configured billing provider | Contractual/accounting review required; billing identifiers are never exported |
| Audit and observability | Actor/resource IDs, change metadata, request/error telemetry | Security investigation, accountability, and service operation | PostgreSQL and configured telemetry provider | Technical operational lifecycle; avoid direct reporter email in privacy audit payloads |
| Privacy jobs and requests | Hashed verification token, one-way email digest, job state, counts, private archive key | Verified access/anonymization, export, retry, and audit | PostgreSQL, Asynq/Redis, private object storage | Archives 7 days; unverified requests 48 hours; verified access 30 days |

## Technical Data Flows

1. The widget submits feedback, optional contact data, attachments, and replay batches to the
   tenant/project-scoped API.
2. PostgreSQL stores workflow records under forced tenant RLS. Binary media and replay objects use
   tenant-prefixed keys in private S3-compatible storage.
3. Background work uses Redis/Asynq for email, AI, replay merge, integrations, and privacy exports.
   Queue payloads contain internal identifiers needed to load tenant-scoped records; queue payloads
   are not included in exports.
4. Configured email, AI, billing, telemetry, and integration providers receive only the data needed
   for their enabled operation.
5. Workspace and verified reporter exports are explicit allowlists. They omit credentials, token
   hashes, API keys, sessions, magic links, billing provider identifiers, internal queue payloads,
   and attachment/replay binaries.

## Configurable Providers

The application has technical adapters or configuration for:

- PostgreSQL for durable application data.
- Redis and Asynq for rate limiting and background queues.
- S3-compatible private object storage for attachments, replay objects, and temporary exports.
- Resend for configured transactional email delivery.
- Google Gemini for configured AI analysis.
- Stripe for configured billing and subscription processing.
- Sentry for configured error telemetry.
- Slack, Microsoft Teams, and generic customer webhooks for enabled operational notifications.

This list describes code paths, not the providers enabled in any particular environment. Deployment
configuration, provider accounts, locations, subprocessors, contracts, and transfer mechanisms must
be verified independently.

## Controls And Boundaries

- Tenant data access uses forced PostgreSQL RLS and tenant-scoped foreign keys.
- Human privacy operations require an active member with `tenant:update`; effective retention is
  readable with `tenant:read`.
- Reporter request tokens are random, stored only as SHA-256 hashes, rate limited, and expire.
- Object deletion precedes manifest mutation. Partial storage failures leave the database candidate
  retryable.
- Export downloads are private, authorization is rechecked, and signed links last ten minutes.
- Privacy request and archive expiry is append-only audited without storing raw reporter email in
  audit data.
- Widget-issued identifiers are not intended for personal fingerprinting and are never derived from
  IP address, raw user-agent, or other browser characteristics.
- The persistent widget analytics visitor identifier (`widget/src/analytics.ts`, stored in the
  visitor's browser as `fidibeki:visitor:v1`) is stable across sessions and is therefore treated as
  pseudonymous, not anonymous. It is used only for widget funnel analytics under an active
  `widget_analytics` grant. It is never written to the replay session record, never exposed in the
  backoffice, is isolated from any authenticated account identity, and — as of issue #58 — is no
  longer passed into replay's abuse-protection request at all, closing the prior cross-purpose
  coupling; replay abuse protection now relies solely on origin/network-based rate limiting for
  visitors it has no other basis to identify.
- Widget analytics collection is off by default. Before an applicable `widget_analytics` grant for
  the current rule, the widget creates or reads no `fidibeki:visitor:*` or
  `fidibeki:analytics-outbox:*` storage key, observes no funnel-relevant signal, and transmits no
  matched/impression/interaction/submitted/dismissed event; a rule with consent mode `none` stays
  permanently dormant for analytics regardless of visits. Withdrawal aborts any in-flight delivery,
  deletes the queued outbox entries and the persistent visitor identifier from storage, and — because
  the outbox object itself is discarded on withdrawal — prevents any subsequent `sendBeacon` retry
  from reaching the backend. The backend independently fail-closes: `/v1/widget/events` rejects any
  event whose rule has no configured analytics consent mode or whose token does not resolve to a
  currently active, non-expired grant for that rule. See `docs/widget-analytics-consent.md`.
- `session_replay`, `widget_analytics`, and reporter-contact (`feedback_updates`) consent are
  independent purposes with independent evidence ledgers, storage keys, and runtime gates. A grant,
  denial, or withdrawal for one purpose has no effect on, and cannot be inferred from, the others;
  this is enforced structurally (separate tables, separate widget controllers, separate storage key
  prefixes) rather than by convention.
- Reporter anonymization deletes/nulls session replay storage objects and manifests reachable
  through the reporter's feedback, in addition to attachments.
- Plan entitlement never enables AI processing. AI is disabled by default per widget rule and can
  only be enabled after an authorized operator acknowledges the current provider/purpose policy,
  chooses allowed input categories, and supplies an HTTPS customer privacy notice. The current rule
  is rechecked before scheduling, claim, and persistence; disabling it discards queued and in-flight
  results.
- Written feedback is deterministically minimized before the Gemini request. Reporter email,
  contact consent, page/referrer/device metadata, replay, analytics IDs, and resource IDs are not
  included. Media uploads use a constant display name. Every returned text field is redacted again
  by the backend using the exact `[REDACTED]` marker before persistence; invalid output is
  quarantined. Redaction manifests contain categories/counts only.
- AI processing evidence never stores prompts, source content, raw provider output, or removed
  values. Workspace and verified reporter exports include safe run/deletion evidence. Reporter
  anonymization removes derived analysis, manifests, and the reporter's processing runs.
- The application uses the Gemini Developer API `generateContent` and Files API without grounding,
  explicit context caching, the Interactions API, tuning, or dataset contribution. Provider project
  billing, logging/data-sharing settings, executed DPA, ZDR status, and transfer terms remain
  environment controls that must be verified before broad enablement. See
  `docs/ai-provider-assessment.md`.
- Canvas recording is forced off in the widget, API normalization, and database constraint. A
  modified or old client cannot make canvas events playable because batch verification rejects the
  canvas source before storage promotion.
- Replay is off by default and existing rules migrate to consent mode `none`. Loading or matching a
  rule cannot import/start rrweb, create a session, buffer events, or upload batches until the
  backend accepts a current structured grant for that rule and notice version. Revocation first
  stops capture and discards the browser buffer, then records withdrawal; the backend independently
  refuses new session and batch operations after withdrawal, expiry, rule disablement, or a
  configuration revision.
- Replay consent evidence is purpose-limited and is not copied into rrweb events, analytics
  dimensions, integrations, logs, or player responses. Customer-managed consent references are
  provenance assertions, not cross-session identity keys. See `docs/session-replay-consent.md`.
- Replay objects captured before the current anonymous-event contract have privacy version zero.
  Tenant metadata reports them as expired, stream and merge paths refuse them, and the retention
  cleaner deletes their referenced objects before nulling the manifest keys.
- Reporter email is only collected and only enters the notification pipeline after an explicit,
  unchecked-by-default opt-in carrying structured consent evidence (purpose `feedback_updates`, a
  recognized notice version, locale, and affirmative state). Unknown or contradictory combinations
  (email without consent, consent without notify, an unsupported notice version) are rejected by
  the public API, not silently normalized.
- Every reporter notification/status/clarification/resolution email re-checks active consent both
  when the send is enqueued and again immediately before the provider call, closing the race where
  a reporter withdraws between those two points. Every such email carries a feedback-scoped
  unsubscribe link and a link to the published privacy policy.
- Rows consented before the structured-evidence migration (`000076`) have no purpose/notice
  version/locale/source recorded. They are not treated as actively consented for new deliveries;
  only feedback carrying full evidence is eligible for reporter email until the reporter re-opts in
  through a fresh submission.

## Customer Responsibilities

Customers decide whether to enable and where to target contact collection, DOM capture, attachments,
and session replay. They are responsible for lawful configuration, appropriate notices/consent,
avoiding capture of unnecessary sensitive fields, access governance, and responding to manual-review
items that automated anonymization cannot safely rewrite. Free text, structured answers, comments,
page URLs, and clarification responses can contain identifiers and may require human review.

For session replay, the customer remains the controller and chooses either a customer-managed CMP
integration or a Fidibeki-managed prompt presented on the customer's behalf. The customer owns the
accuracy of controller identity, purpose, notice copy/version, locales, privacy-policy URL, CMP
assertions, and use of the safe-text marker. Fidibeki's managed prompt does not change the factual
controller/processor roles.

For widget analytics, the customer remains the controller and chooses either a customer-managed CMP
integration or a Fidibeki-managed prompt, exactly as for session replay but as a fully independent
purpose/configuration. The customer owns the accuracy of controller identity, purpose, notice
copy/version, locales, and privacy-policy URL for the analytics notice specifically.

## Review Gaps

- Legal must define published retention and deletion commitments from these technical defaults.
- Provider contracts, current subprocessors, residency, cross-border transfers, and breach terms
  require separate review.
- Accounting/legal retention for billing and security audit records requires an explicit policy.
- A manual-review procedure is required for personal data embedded in free-form operational history.
- No self-service reconfirmation flow exists yet for reporters whose consent predates structured
  evidence; today they simply stop receiving reporter email until they submit new feedback with a
  fresh opt-in. A dedicated reconfirmation prompt is a candidate follow-up, not yet built.
- Privacy/legal review must approve the replay consent copy, withdrawal/expiry behavior, evidence
  retention, DPA wording, and current subprocessor inventory before replay is enabled broadly in
  staging. Production enablement and deployment remain explicitly out of scope.
- Privacy/legal and platform owners must close the blocking checks in
  `docs/ai-provider-assessment.md` before AI is broadly enabled, including proof that the Gemini API
  project is a Paid Service, request logging and voluntary data sharing are off, the applicable DPA
  and transfer mechanism are accepted, and the DPIA screening decision is recorded.
- Privacy/legal review must approve the widget_analytics consent copy, withdrawal/expiry behavior,
  and evidence retention before analytics collection is enabled broadly in staging, on the same
  terms as replay. Production enablement and deployment remain explicitly out of scope.
- Legal must confirm whether the persistent widget analytics visitor identifier can ever be treated
  as anonymous rather than pseudonymous under a documented anonymization assessment (EDPB
  anonymisation/pseudonymisation guidance); absent that assessment this document and the product
  surface treat it as pseudonymous and gate it behind consent.
- Legal must confirm whether widget targeting/objective-policy signals (visit counts used only to
  decide whether a widget becomes eligible to display, never transmitted) require their own
  ePrivacy Article 5(3) consent, or qualify as strictly necessary to deliver the feedback
  functionality the site operator configured. CNPD's guidance treats analytics cookies as requiring
  consent; this document does not assume targeting storage is exempt without that review, and no
  additional technical gate has been added pending the answer — the technical work in issue #58 is
  scoped to the `widget_analytics` measurement purpose specifically.
- Legal must confirm the Portuguese Law 41/2004 / CNPD position on the Fidibeki-managed analytics
  notice and on customer-managed CMP integrations before broad staging enablement.
