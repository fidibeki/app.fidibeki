# Fidibeki Technical Privacy Data Map

**Last technical review:** 2026-10-01

This document is an engineering inventory for founder and legal review. It is not a published
privacy policy, data processing agreement, residency commitment, legal opinion, or guarantee.
Actual provider configuration and contractual terms must be reviewed separately before making
customer-facing claims.

## Executable Retention Registry

`GET /v1/privacy/retention` is backed by registry version 1. Its 36 lifecycle categories cover
every one of the 103 PostgreSQL tables created by current migrations exactly once, plus the known
object-prefix, queue, browser-key, and provider boundaries. Automated validation rejects duplicate
resources and rejects `enforced` or `configurable` claims without a bounded lifecycle and a real
implementation reference.

The registry uses four explicit states:

- `enforced`: a bounded cleanup or expiry implementation exists.
- `configurable`: a bounded plan or rule setting is applied by an existing implementation.
- `decision_required`: a maximum period or hold behavior still requires founder/legal approval.
- `external_control`: the effective period depends on verified provider configuration or contract.

The registry is descriptive and read-only. It does not add cleaners, change existing expiry clocks,
or authorize deletion. Resource coverage is tested against migration DDL; the non-database
inventory remains an engineering review boundary rather than a claim about provider enablement.

The endpoint also returns a separate operational `health` object for every enforced cleaner.
Migration 105 keeps one current aggregate row per code-owned operation in
`retention_maintenance_status`, and migration 109 extends the same contract from the two
magic-link cleaners to the attachment, export-archive, reporter-request, replay-session,
widget-funnel (raw and daily), abuse-diagnostic, AI-budget, identity-asset, SAML-auth-artifact,
and mobile-capture-session cleaners; the ledger stores schedule metadata, last attempt/success, bounded counters,
budget exhaustion, and only the generic failure code `cleanup_failed`. It stores no tenant, person,
credential, token, IP/user-agent, or raw error data and no append-only run history. Health is derived
as `healthy`, `attention`, `failed`, `overdue`, or `not_observed` from each operation's schedule and
15-minute grace window. The bilingual Data & privacy surface shows the sixteen tracked operations,
their last success, next expected run, counts, and alert reason. Every enforced cleaner now reports
into this ledger; categories without a cleaner still have no health row and are not represented
as healthy.

## Records Of Processing Activities (ROPA) And DPIA Screening

`backend/internal/privacy/ropa_registry.go` groups the retention registry's 36 categories into 21
code-owned processing activities — GDPR Article 30's actual unit of record — each covering one or
more retention categories exactly once. Every activity carries its Fidibeki role (derived from, and
validated against, the retention registry rather than hand-duplicated), data-subject (including a
closed `third_parties` class for content that can name or show someone other than the interacting
subject) and data-category classifications, security-control references, a lawful-basis status with
a named, non-placeholder reviewer, and a DPIA screening snapshot (technically-evidenced risk
indicators drawn from the EDPB WP248 nine criteria, an outcome, a reason, and owner/review/approval
evidence). Recipient/provider references are never hand-authored; they are derived from the
`RetentionProvider` resources the retention registry already attaches to each activity's member
categories.

Every activity in this registry starts, and today remains, `lawful_basis: pending` and
`dpia.outcome: pending`. `PublicationReady` is computed from completeness rules rather than a literal
field — a source literal that sets it directly fails validation — so it cannot be made `true` by
editing the registry, only by recording real founder/legal lawful-basis and DPIA evidence in the
fields the validator already checks. A `dpia.outcome: required` screening can never make an activity
`PublicationReady`, however complete its evidence, because this registry has no model for evidencing
that the required DPIA was actually completed; only a fully evidenced `not_required` screening can
currently satisfy that gate. This is an engineering foundation for the ROPA and DPIA screening #63
requires, not a published ROPA, an executed DPIA, or a legal conclusion. Processor-role activities'
lawful-basis reasoning is explicit that Fidibeki processes only on the customer controller's
documented instructions under the applicable Article 28 contract — Article 28 is a
processing-instruction and safeguards framework, not itself an Article 6 lawful basis.
`make governance-check` validates this registry alongside the provider governance registry.

## Data Categories And Purpose

| Category | Examples | Technical purpose | Primary store | Effective technical retention |
|---|---|---|---|---|
| Workspace identity | Workspace name, slug, plan, status | Tenant routing, authorization, billing state | PostgreSQL | `decision_required`: no approved maximum period or general cleanup is implemented |
| Members and authentication | Email, permissions, session/token hashes, login and invitation records | Authenticate people and enforce RBAC | PostgreSQL, Redis for rate limits/queues | Login and invitation magic links (`tenant_magic_links`, `platform_admin_magic_links`) are hard-deleted, consumed or not, within 1 hour of their configured expiry (default 10 minutes) by bounded hourly maintainers; each deletion records a non-content audit event (`auth.magic_link_expired` in `tenant_access_events`, or `auth.platform_magic_link_expired` in the global `platform_security_events` ledger for platform-admin links). `retention_maintenance_status` retains only the current non-personal aggregate health of the enforced cleaners (these two included) and drives overdue/failure/budget alerts in Data & privacy. Those minimized security events themselves, and other authentication/security records (sessions, platform admins, reporter access tokens), remain `decision_required`; no maximum period is claimed |
| Projects and widget configuration | Domains, rules, targeting, appearance, form blocks | Decide where and how feedback is collected | PostgreSQL | `decision_required`: no approved maximum period or general cleanup is implemented |
| Private platform support | Member/staff messages, sequence and read positions; ephemeral active/idle section | Staff-initiated support inside the app | PostgreSQL messages; Redis presence | Conversations deleted 180 days after their last message, subject to member privacy holds; member/workspace deletion cascades. Presence expires after 60 seconds and index after 120 seconds without heartbeats. Participant-only transcript download; workspace/operator member exports omit private chats. See [live support](live-support.md) |
| Feedback and issue workflow | Original description, title, rating, answers, comments, page URL, status, tags, assignments | Triage, decision, resolution, and reporter communication | PostgreSQL | `decision_required`: supported subject anonymization is narrower than a general workflow-retention policy |
| Email feedback invitations | Customer event ID, normalized recipient while delivery requires it, one-way recipient/request/token digests, locale, privacy-safe context URL, captured configuration, delivery state, and optional Feedback link | Send a customer-directed post-transaction solicitation, enforce idempotency, and collect Feedback only after an explicit form submission | PostgreSQL, Asynq/Redis, configured email provider | Plaintext recipient data is removed after successful delivery or terminal failure. Reporter access/erasure/restriction matches the normalized recipient digest. The remaining invitation and associated outbox event are hard-deleted by a bounded daily maintainer 90 days after invitation expiry |
| Email feedback suppressions | One-way recipient digest, opt-out/bounce/complaint reason, and lifecycle timestamps | Prevent unwanted solicitation from resuming | PostgreSQL | Retained until the customer removes the workspace or an approved policy changes; deleting it earlier would resume unwanted email. Workspace deletion cascades to the ledger |
| Reporter contact and consent | Normalized email; notification preference; structured consent evidence (purpose, notice version, locale, source); server-authoritative grant/withdrawn timestamps | Verification and opted-in status communication | PostgreSQL | Contact follows supported anonymization/unsubscribe behavior. The maximum period for minimized consent evidence after withdrawal is `decision_required` |
| Attachments and derived analysis | Screenshot/audio/video objects, transcript, sentiment, moderation flags, declared metadata | Give operational context to feedback | Private S3-compatible object storage and PostgreSQL manifest | Attachment objects use the configurable plan storage limit (30/90/365 days). Workflow manifests and derived-data maximum periods remain `decision_required` |
| Session replay | rrweb batches/merged object; session-scoped random identifier; rule-safe page context; timing and event counts; native page styling and ordinary UI text; no canvas pixels | Reproduce the page experience while masking private content | Private S3-compatible object storage and PostgreSQL manifest | Form values and marked private regions are masked before upload. Recognizable contact and payment numbers are redacted from text. Native CSS, layout identifiers, SVG geometry, and static assets remain available. Arbitrary data attributes, signed/private media URLs, generated CSS content, comments, canvas pixels, and unknown custom sources are removed. The backend validates content and CSS before promotion and merge. Names, addresses, and other application-specific personal content require private markers or the configured masking selector; automatic anonymization of unmarked content is not guaranteed. Consent, access controls, and plan retention remain unchanged. Existing recordings cannot regain discarded content. See [replay capture](session-replay-capture.md). |
| Session replay consent evidence | Tenant/project/rule scope; purpose `session_replay`; consent mode/source; customer consent reference where supplied; notice version; locale; server-authoritative accepted, withdrawn, and expiry timestamps; active/withdrawn/expired state | Enforce the replay capture gate and retain the minimum provenance needed for audit | PostgreSQL | `decision_required`: the ledger is isolated and minimized, but its post-withdrawal maximum period has not been approved |
| Pseudonymous widget analytics | Visitor/page-view/event IDs, page path, funnel event and daily counts | Measure matched, impression, interaction, submit, and dismiss funnels once a rule holds a current `widget_analytics` grant | PostgreSQL | Raw events 90 days; daily aggregates 730 days. Collection is consent-gated: see `docs/widget-analytics-consent.md`. The visitor identifier is stable across sessions and is therefore treated as pseudonymous, not anonymous, absent a documented anonymization assessment. |
| Widget analytics browser state | Stable visitor ID and retry outbox for consented funnel events | Maintain analytics identity and retry accepted events | Browser `localStorage` only; never transmitted except in consented analytics requests | `decision_required`: retryable events are bounded in the outbox, but the stable visitor identifier has no approved maximum period |
| Widget analytics consent evidence | Tenant/project/rule scope; purpose `widget_analytics`; consent mode/source; customer consent reference where supplied; notice version; locale; server-authoritative accepted, withdrawn, and expiry timestamps; active/withdrawn/expired state | Enforce the analytics collection gate and retain the minimum provenance needed for audit | PostgreSQL | `decision_required`: the ledger is separate and minimized, but its post-withdrawal maximum period has not been approved |
| Widget targeting and objective-policy signals | Per-page visit counts keyed by session; observed active time, scroll depth, and interaction counts (in-memory only, never persisted); explicit `data-fidibeki-*` engagement/content/friction flags | Decide whether and when a configured presentation (launcher, prompt, reaction, inline) becomes eligible to display, per the customer's targeting/objective configuration | Browser `localStorage` only (`fidibeki:policy-visit:v1:*`); never transmitted to the backend | `decision_required`: same-origin visit state currently follows the browser-profile lifetime and has no bounded expiry. Whether this purpose additionally requires consent under a strict ePrivacy Article 5(3) reading is also open |
| Widget recurrence and suppression state | Per-page widget state (new/submitted/minimized/dismissed/snoozed) and a random session identifier | Avoid re-showing a widget the visitor already dismissed, submitted, or snoozed, per the customer's configured recurrence policy | Browser `localStorage`/`sessionStorage` only (`fidibeki:state:v2:*`, `fidibeki:session:v1`); never transmitted to the backend | Same-origin browser storage; cooldown/session/never per rule configuration. Functional/strictly-necessary in nature (it exists to make the feedback UI usable rather than repetitive) and is technically and evidentially isolated from analytics and replay purposes. |
| Integration delivery | Slack/Teams/webhook configuration, encrypted credentials, versioned per-destination field allowlist, delivery result, disclosed data-category names, and a truthful capability snapshot (never the delivered payload) | Send minimized operational notifications, explain disclosure, and retry delivery | PostgreSQL, Asynq/Redis | Data minimization is enforced, but configuration/outbox/attempt/disclosure-ledger maximum periods are `decision_required`; provider-retained copies are `external_control` |
| Integration downstream actions | Privacy-case ID, stable destination key and name/kind snapshot, action kind, disclosed category names, successful-disclosure count/window, truthful destination capabilities, operator completion evidence, and optional recipient-notice link | Track manual rectification/erasure follow-up for copies already disclosed to external destinations | PostgreSQL | `decision_required`: follows the privacy-case evidence boundary, whose maximum period is not yet approved. Current incoming webhooks cannot update, delete, or recall provider-retained messages |
| Email delivery | Recipient, message metadata, delivery/bounce/complaint events, and customer-directed post-transaction feedback invitation content | Send authentication, lifecycle, reporter, and email-feedback campaign messages | PostgreSQL, Asynq/Redis, configured email provider | Invitation application data follows the bounded policy above; other application evidence remains `decision_required`; configured provider retention is `external_control` |
| AI processing | Rule-owned authorization; minimized submitted text or explicitly selected media; redacted advisory results; provider/model/purpose/category/status evidence; category-only redaction manifest; external file deletion evidence | Suggest triage and analyze only the input categories explicitly enabled for the matching rule | PostgreSQL, private storage, Google Gemini Developer API when explicitly enabled | Provider files target immediate deletion with a retry ledger and documented 48-hour provider fallback. Source and derived application-data maximum periods are `decision_required` |
| Billing | Plan/subscription state and external customer/subscription references | Entitlement, invoicing, dunning, and deletion scheduling | PostgreSQL and configured billing provider | `decision_required` for application evidence; provider retention is `external_control` pending contractual/accounting review |
| Personal-data breach register | Incident facts and affected tenant references; authority/high-risk/communication assessments; chronology; containment controls; post-incident actions, owners, deadlines, outcomes, corrective references, opaque evidence references; frozen processor-to-controller notification/contact snapshots; delivery attempts and restricted outbox evidence; external-provider incident notices with frozen code-owned governance facts, affected data-flow categories, and incident-scoped tenant mappings | Preserve restricted decision and response evidence, deliver explicitly queued, minimized controller notifications without exposing the workflow to the tenant runtime role, record bounded external-provider notices without asserting a legal subprocessor classification, and let a platform admin download a frozen notification snapshot or a deterministic point-in-time post-incident report (JSON or Markdown) rendered read-only from these same facts | PostgreSQL through the platform-admin-only connection boundary; configured transactional email provider after an explicit platform-admin queue command | `decision_required`: incident/action/content/provider snapshots and delivery attempts are append-only, notification delivery state is narrowly mutable, and every table is excluded from `fidibeki_app`; the register's maximum period and legal-hold behavior require founder/legal approval. Exports add no storage and no new lifecycle |
| Audit and observability | Actor/resource IDs, change metadata, request/error telemetry | Security investigation, accountability, and service operation | PostgreSQL and configured telemetry provider | `decision_required` for database security evidence. Telemetry-provider *retention* remains `external_control`, pending legal and provider evidence. Outgoing Sentry error events are technically scrubbed on both the backend and widget by a deterministic `BeforeSend`/`beforeSend` allowlist (`backend/internal/observability/scrub.go`, `widget/src/modules/error-reporting.ts`): only exception type/structural stack frames/timestamp/release/environment (plus a known widget phase and bounded `project_id`/`widget_rule_id`) leave the application; request URL/headers/cookies/body, user/IP/user-agent, frame absolute paths/source context/locals, breadcrumbs, arbitrary contexts/tags, and the free-text exception/message value itself are always removed or replaced with a fixed placeholder. |
| Privacy cases, jobs, and requests | Case subject/right/evidence, hashed reporter verification token, one-way email digest, job state, counts, private archive key | Verified reporter/member access, assessed portability, anonymization execution, retry, and audit | PostgreSQL, Asynq/Redis, private object storage | Archives are enforced at 7 days; unverified reporter requests at 48 hours; verified reporter access at 30 days. The privacy-case evidence maximum period is `decision_required` |

## Tenant website conversations

`website_chat_visitors` and `website_chat_messages` hold anonymous, tenant/project-bound conversations started by a tenant operator. A visitor cannot start a thread or access another visitor's messages with the public project key. PostgreSQL stores a SHA-256 credential hash, origin, sequence/read markers, operator identity and message content; the browser's `fidibeki:chat:<project>` entry stores only the 24-hour opaque credential. The host logout reset clears that entry and attempts server revocation. Transcript download is a direct, bounded browser download and creates no export archive.

The `website_chat` retention category deletes conversations 180 days after their last message and unused visitor records 24 hours after credential expiry, with tenant-wide legal holds respected. The `website_chat_presence` category covers per-tab Redis presence (60-second TTL), its index (120-second TTL), and the expiring browser credential. Presence contains only anonymous IDs, active/idle state and an existing-rule-projected page location without query strings/fragments. Unmatched pages expose only the origin. No message content is sent in Pub/Sub invalidations or access-audit metadata. Both categories belong to the processor-role feedback workflow processing activity. The dedicated worker operation reports to retention health and resumes exhausted batches promptly.

See [Website conversations](website-chat.md) for permissions, host logout integration and the independent platform-support boundary.

## Technical Data Flows

1. The widget submits feedback, optional contact data, attachments, and replay batches to the
   tenant/project-scoped API.
2. PostgreSQL stores workflow records under forced tenant RLS. Binary media and replay objects use
   tenant-prefixed keys in private S3-compatible storage.
3. Background work uses Redis/Asynq for email, AI, replay merge, integrations, and privacy exports.
   Queue payloads contain internal identifiers and, for some tasks, message or delivery content
   needed by the worker. Queue payloads are not included in exports.
4. Configured email, AI, billing, telemetry, and integration providers receive only the data needed
   for their enabled operation.
5. Workspace, verified reporter, and verified member-access exports are explicit allowlists.
   Workspace and reporter exports omit authentication sessions and magic links entirely. A
   member-access export includes only sanitized session/magic-link metadata and omits refresh/token
   hashes. All scopes omit credentials, API keys, billing provider identifiers, internal queue
   payloads, and attachment/replay/avatar binaries.

## Configurable Providers

The application has technical adapters or configuration for:

- PostgreSQL for durable application data.
- Redis and Asynq for rate limiting and background queues.
- S3-compatible private object storage for attachments, replay objects, identity media, and
  temporary exports.
- Resend for configured transactional email delivery, including customer-directed post-transaction
  feedback invitations.
- Google Gemini for configured AI analysis.
- Stripe for configured billing and subscription processing.
- Sentry for configured error telemetry. Outgoing events are technically scrubbed by a deterministic
  allowlist before send; the provider's retention period, region, legal entity, and contract status
  are not yet verified — see the Audit and observability row above.
- Slack, Microsoft Teams, and generic customer webhooks for enabled operational notifications.
- Cloudflare Turnstile for signup bot/abuse verification. The signup challenge token and the
  submitting request's IP address are sent to Cloudflare (`backend/internal/billing/turnstile.go`);
  required in staging and production.

This list describes code paths, not the providers enabled in any particular environment. Deployment
configuration, provider accounts, locations, subprocessors, contracts, and transfer mechanisms must
be verified independently.

## Controls And Boundaries

- Tenant data access uses forced PostgreSQL RLS and tenant-scoped foreign keys.
- Human privacy operations require an active member with `tenant:update`; effective retention is
  readable with `tenant:read`.
- Privacy-case integration checklists are derived only from provider-confirmed successful delivery
  rows that match the verified reporter/member scope. Failed or suppressed attempts are excluded.
  Manual completion is blocked until the case contains the corresponding correction or settled
  erasure evidence; recording a sent recipient notice does not prove that the destination completed
  the action.
- Reporter request tokens are random, stored only as SHA-256 hashes, rate limited, and expire.
- Object deletion precedes manifest mutation. Partial storage failures leave the database candidate
  retryable.
- Export downloads are private, authorization is rechecked, and signed links last ten minutes.
- Member-access exports are linked to the verified tenant-scoped case, run under a repeatable-read
  snapshot, and expose only an allowlist of the subject's profile/authentication metadata,
  subject-authored activity, subject-addressed notification metadata, sanitized access events, and
  case history. Other member content, raw audit payloads, refresh/token hashes, and avatar binary
  are not copied.
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
assertions, and marking application-specific private content. Fidibeki's managed prompt does not change the factual
controller/processor roles.

For widget analytics, the customer remains the controller and chooses either a customer-managed CMP
integration or a Fidibeki-managed prompt, exactly as for session replay but as a fully independent
purpose/configuration. The customer owns the accuracy of controller identity, purpose, notice
copy/version, locales, and privacy-policy URL for the analytics notice specifically.

## Review Gaps

- Founder/legal must record a lawful basis and a DPIA screening outcome (required or not required,
  with mitigations, owner, and approval evidence) for each of the 19 processing activities in
  `backend/internal/privacy/ropa_registry.go`; every activity is currently `pending` on both.
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
