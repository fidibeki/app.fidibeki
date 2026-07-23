# Fidibeki Technical Privacy Data Map

**Last technical review:** 2026-07-23

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
| Reporter contact and consent | Normalized email, notification preference, consent granted/withdrawn timestamps | Verification and opted-in status communication | PostgreSQL | Until supported anonymization, workspace deletion, or request expiry where applicable |
| Attachments and derived analysis | Screenshot/audio/video objects, transcript, sentiment, moderation flags, declared metadata | Give operational context to feedback | Private S3-compatible object storage and PostgreSQL manifest | Objects and derived transcript: plan storage limit (30/90/365 days); manifest retained with workflow |
| Session replay | rrweb batches/merged object; session-scoped random identifier; canonicalized page URL (origin + normalized pathname only, credentials/query/fragment stripped); timing and event counts; canvas pixels only when a rule explicitly enables bounded canvas capture | Reproduce the reported page experience without exposing the authenticated user's identity | Private S3-compatible object storage and PostgreSQL manifest | Anonymous by default: all visible text and form input values are masked unconditionally (not only via optional customer selectors), and identity-bearing DOM attributes (`id`, `data-*`, ARIA, `title`, `alt`, links, avatar/media URLs) are stripped by an attribute allowlist. `data-fidibeki-block` excludes a subtree entirely, including canvases; `data-fidibeki-safe-text` explicitly opts known-safe static text back in. See `docs/superpowers/specs/2026-07-23-replay-identity-threat-model.md`. Objects: plan storage limit (30/90/365 days); manifest retained with workflow |
| Anonymous widget analytics | Visitor/page-view/event IDs, page path, funnel event and daily counts | Measure matched, impression, interaction, submit, and dismiss funnels | PostgreSQL | Raw events 90 days; daily aggregates 730 days |
| Integration delivery | Slack/Teams/webhook configuration, encrypted credentials, attempts and errors | Send operational notifications and retry delivery | PostgreSQL, Asynq/Redis | Workspace/integration lifecycle; credentials are never exported |
| Email delivery | Recipient, message metadata, delivery/bounce/complaint events | Send authentication, lifecycle, and reporter messages | PostgreSQL, Asynq/Redis, configured email provider | Operational and audit lifecycle |
| AI processing | Submitted text/media required for configured analysis, job state, external file cleanup ledger | Suggest triage and analyze supported attachments | PostgreSQL, private storage, configured AI provider | Source follows workflow/media retention; provider file deletion uses the cleanup ledger |
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
- Anonymous widget identifiers are not intended for personal fingerprinting.
- The persistent anonymous analytics visitor identifier (`widget/src/analytics.ts`, stored in the
  visitor's browser) is used only for widget funnel analytics and replay abuse-protection rate
  limiting. It is never written to the replay session record, never exposed in the backoffice, and
  is isolated from any authenticated account identity.
- Reporter anonymization deletes/nulls session replay storage objects and manifests reachable
  through the reporter's feedback, in addition to attachments.

## Customer Responsibilities

Customers decide whether to enable and where to target contact collection, DOM capture, attachments,
and session replay. They are responsible for lawful configuration, appropriate notices/consent,
avoiding capture of unnecessary sensitive fields, access governance, and responding to manual-review
items that automated anonymization cannot safely rewrite. Free text, structured answers, comments,
page URLs, and clarification responses can contain identifiers and may require human review.

## Review Gaps

- Legal must define published retention and deletion commitments from these technical defaults.
- Provider contracts, current subprocessors, residency, cross-border transfers, and breach terms
  require separate review.
- Accounting/legal retention for billing and security audit records requires an explicit policy.
- A manual-review procedure is required for personal data embedded in free-form operational history.
