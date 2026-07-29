# Email Feedback Campaigns

**Date:** 2026-07-29
**Issue:** #113
**Status:** Approved through the standing approval for recommended product decisions

## Goal

Add an opt-in post-transaction email channel that turns a customer-owned event into
one scanner-safe feedback invitation and, only after an explicit form submission,
one independent Fidibeki Feedback.

The first vertical slice is:

`customer event -> invitation -> email -> public form -> Feedback`

It does not collect direct email replies, create Issues or Testimonials, or infer
consent from the fact that an address received an invitation.

## Existing Capabilities To Reuse

- Tenant API keys already support least-privilege permissions and service-to-service
  authentication.
- Widget Definitions already own response blocks, localizations, contact consent,
  and AI-processing configuration.
- `feedback.SubmissionCore` already enforces plan limits, idempotent feedback
  creation, the transactional `feedback.created` outbox event, and the existing AI
  pipeline.
- The outbox dispatcher and email worker already provide deterministic retries.
- The shared email composer already provides branded HTML and text output.
- Public testimonial collection demonstrates opaque-token resolution, tenant RLS
  entry, neutral not-found responses, and tenant-aware public URLs.
- Reporter privacy requests, the retention registry, ROPA registry, and Resend
  webhook already provide the governance surfaces this channel must extend.

## Considered Approaches

### 1. A campaign owns another form builder

This keeps email copy and questions independent from widgets, but duplicates response
validation, localization, contact notices, AI consent, and backoffice configuration.
It also creates two competing definitions for the same Feedback domain object.

### 2. A campaign points to an existing Widget Definition (selected)

The campaign selects a project and Widget Definition and owns only solicitation
settings. Invitation creation captures the exact definition and controller-profile
versions. This reuses the established form contract without applying browser
targeting, recurrence, launcher, replay, or attachment behavior to email.

### 3. Send a generic rating-only email

This is smaller, but loses the contextual questions customers already configured and
cannot complete the product's feedback model. It would become a throwaway endpoint.

## Domain Model

### Campaign

An `email_feedback_campaign` belongs to one tenant and project and references one
Widget Definition. It has:

- stable UUID and customer-facing key;
- editable name;
- `draft`, `active`, `paused`, or `archived` status;
- default locale;
- invitation subject and introductory copy;
- invitation lifetime in days;
- created/updated timestamps.

Activation is an explicit opt-in. It requires an active Widget Definition, a
published localization for the campaign locale, and a complete customer controller
profile. Pausing stops new invitations but does not invalidate already-sent links.
Archiving stops new invitations and preserves historical records.

The campaign does not inherit widget targeting, presentation, recurrence, replay, or
attachment controls. It reuses only the definition's response blocks, localization,
contact option, and AI-processing policy.

### Invitation

An `email_feedback_invitation` is the durable processing ledger for one customer
event. It records:

- tenant, project, campaign, Widget Definition, and issuing API key;
- customer-provided `external_event_id`;
- a digest of the normalized request for idempotency conflict detection;
- normalized recipient email while delivery needs it, plus a stable email digest;
- locale and optional privacy-safe context URL;
- a hash of the opaque public token, never the raw token;
- captured definition and controller-profile versions;
- `queued`, `sent`, `submitted`, `suppressed`, `bounced`, `complained`, `failed`, or
  `expired` status and relevant timestamps;
- provider message ID when delivery succeeds;
- the created Feedback ID after submission;
- expiry and retention-cleanup timestamps.

`(campaign_id, external_event_id)` is unique. An exact retry returns the existing
invitation; a retry with a different payload returns a conflict. Logs and API
responses never include the recipient or raw token.

The email address is not copied into Feedback. If the public form offers follow-up
contact, the reporter must explicitly opt in and submit an email under the existing
purpose-specific consent contract.

### Suppression

An `email_feedback_suppression` is tenant-wide and keyed by the normalized email
digest. It records opt-out, bounce, or complaint without retaining a new plaintext
copy. New invitation requests for a suppressed recipient return an accepted,
non-delivery result so customer systems cannot use the endpoint to probe addresses.

## API Contracts

### Backoffice campaign API

Authenticated members use:

- `GET /v1/email-feedback-campaigns`
- `POST /v1/email-feedback-campaigns`
- `PATCH /v1/email-feedback-campaigns/{campaignID}`

Campaign reads require `project:read`; mutation requires `project:update`. The first
UI lives in the selected project's configuration rather than adding another global
navigation destination.

### Server-to-server invitation API

`POST /v1/email-feedback/invitations` requires a tenant API key with the new
`email_feedback:send` permission. It accepts:

- `campaign_id`;
- `external_event_id`;
- `recipient_email`;
- optional `locale`;
- optional HTTPS `context_url` without query or fragment.

The request returns `201` for a new invitation and `200` for an exact retry.
Campaign inactivity, unsupported locale, malformed context, or a changed retry
payload are explicit validation/conflict responses. A suppression returns an
accepted response with `status: suppressed` and sends no email.

Creating an invitation and its `email_feedback.invitation_created` outbox event is
one database transaction. The outbox fanout enqueues a deterministic task on the
email queue.

### Public collection API

- `GET /v1/public/email-feedback/{token}` returns the captured form contract.
- `POST /v1/public/email-feedback/{token}` explicitly creates Feedback.
- `GET /v1/public/email-feedback/{token}/opt-out` returns confirmation data.
- `POST /v1/public/email-feedback/{token}/opt-out` records suppression.

GET requests never mutate invitation state and never create Feedback. Rating links
append `?rating=1` through `?rating=5`; the browser uses the query only to preselect a
field. The server ignores rating on GET.

POST validates the response against the captured definition, locks the invitation,
and calls `feedback.SubmissionCore` with `source=email`. The invitation ID provides a
stable client feedback ID, so concurrent or repeated POST requests cannot create
duplicates. The same transaction binds the resulting Feedback and marks the
invitation submitted.

Unknown, expired, suppressed, already-submitted, or invalid tokens use neutral
responses that do not reveal tenant or recipient state.

## Email

The invitation uses the shared branded composer and the campaign locale. It contains:

- customer controller name;
- campaign introduction;
- five rating links;
- a primary link to open the form without a preselection;
- the controller privacy-policy link;
- a Fidibeki-hosted opt-out link.

The public URL is built from the configured base domain and tenant slug, never from a
hardcoded environment. The delivery task stores the provider message ID before
removing the plaintext recipient from the invitation. Resend bounce and complaint
events update the invitation and create or strengthen the tenant-wide suppression.

## Feedback Compatibility

The `feedback_source` enum gains `email`. Email Feedback:

- has no mobile context fields;
- uses the optional privacy-safe `context_url` as `page_url`, otherwise an empty
  page URL;
- appears in inbox source filters and badges;
- does not render mobile-only detail fields;
- never creates an Issue or Testimonial automatically;
- enters the existing AI pipeline through the existing `feedback.created` event, so
  no additional paid AI call is introduced.

## Privacy And Retention

This channel is processor functionality performed only after a customer activates a
campaign and sends documented server-to-server instructions. The customer remains
controller for the solicitation.

Activation is blocked until the controller name, privacy contact, privacy-rights
contact, and privacy-policy URL are complete. The form and email identify that
controller. The campaign UI states that the customer must have a lawful basis for
contacting recipients.

Reporter export, erasure, and restriction scopes include invitation rows matched by
normalized email digest. Erasure removes plaintext delivery data and disconnects any
optional reporter identity while preserving a minimized event/idempotency ledger.
Workspace export includes campaigns, minimized invitations, and suppressions.

The retention registry gains:

- invitation delivery data: plaintext recipient removed after successful delivery
  or terminal failure; remaining delivery ledger removed on a bounded schedule after
  expiry;
- suppression digests: retained until the customer removes the workspace or an
  approved policy changes, because deleting them prematurely would resume unwanted
  email.

ROPA and provider-governance records include post-transaction solicitation under the
existing feedback workflow and Resend delivery boundary. No legal basis is invented
by code.

## UI

Project configuration gets an `Email feedback` section with:

- campaign list and status;
- create/edit form selecting a Widget Definition and locale;
- activation readiness diagnostics;
- generated API example using campaign ID and `external_event_id`;
- explicit statement that no email is sent until the customer calls the API.

The public collection page is mobile-first, uses the existing localized definition
labels, shows the controller identity and privacy link, and has clear expired,
submitted, and opted-out states. It does not load the web widget.

## Failure And Concurrency Rules

- An outbox event is committed with every new invitation.
- Deterministic task IDs make dispatch retries safe.
- The delivery worker locks/claims the invitation before send and treats an already
  sent or terminal row as complete.
- Provider failure retries remain bounded; terminal failure clears plaintext
  delivery data.
- Submission locks the invitation before the shared Feedback idempotency lock.
- Opt-out locks the invitation and suppression key; it wins over delivery that has
  not reached the provider.
- Feedback submission and invitation binding commit together.

## Test Strategy

Backend unit and integration tests cover:

- permission validation and API-key-only enforcement;
- campaign validation and activation readiness;
- exact idempotent retries and changed-payload conflicts;
- suppression without address enumeration;
- GET scanner safety, including rating query strings;
- concurrent POST creating one Feedback;
- source `email`, no automatic Issue/Testimonial, and existing AI outbox fanout;
- no recipient copy without explicit contact consent;
- bounce/complaint suppression;
- RLS and cross-tenant token isolation;
- retention, export, erasure, and restriction behavior;
- tenant-aware URLs and email text/HTML.

Frontend tests cover campaign configuration, activation diagnostics, rating
preselection, localized form rendering, explicit submit, and terminal states.

The staging E2E proves:

1. create an active campaign;
2. create a least-privilege API key;
3. post the same external event twice and observe one invitation;
4. open a rating link without creating Feedback;
5. submit the form and observe one inbox Feedback with source `email`;
6. verify no Issue or Testimonial exists;
7. opt out another recipient and verify later events do not send.

## Out Of Scope

- Direct reply and MIME ingestion.
- Marketing/bulk audience uploads.
- Automated reminders.
- Campaign analytics beyond delivery state and resulting Feedback.
- Per-recipient personalization beyond locale and context URL.
- Attachments on the email collection form.
- Automatic Issue or Testimonial creation.
