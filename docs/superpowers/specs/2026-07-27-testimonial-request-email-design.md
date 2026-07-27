# Testimonial Request Email

## Context

The testimonial request endpoints create a 30-day collection token and return it
once. The backoffice turns that token into a link, but the stored recipient
address is not used and `sent_at` is populated even though no email is sent.

Issue #129 requires both request paths to send the collection email and keep the
link visible as confirmation and a manual fallback.

## Decisions

- Both `POST /v1/testimonial-candidates/{feedbackID}/request` and
  `POST /v1/testimonial-requests` enqueue an email before returning `201`.
- The request insert and queue acceptance form the application-level success
  boundary. A queue failure returns a service-unavailable error; the surrounding
  tenant transaction rolls the request back so the caller can retry cleanly.
- The email task uses `testimonial-request:{requestID}` as its idempotency key.
  The existing email worker owns provider delivery and retries transient Resend
  failures.
- `sent_at` means accepted by the durable email queue. The timestamp is committed
  only when enqueue succeeds; a queue error makes the surrounding tenant
  transaction roll back.
- The plaintext token remains return-once and is never stored. The queued email
  necessarily contains the collection URL, while PostgreSQL retains only the
  token hash.
- The browser-facing collection URL is built with `tenant.PublicURL` from
  `WEB_PUBLIC_BASE_URL`, `BASE_DOMAIN`, and the resolved tenant slug. No
  environment hostname is hardcoded.
- The configured verified authentication sender is reused. Workspace identity
  appears in the subject and body, not in an unverified `From` address.
- Email copy supports Portuguese when the normalized locale begins with `pt`;
  English is the fallback for absent or unsupported locales.
- The optional personal prompt is included as customer-provided plain text in
  the existing escaped transactional email template.

## Backend Design

`testimonial.HTTPOptions` receives an email enqueuer, the browser and API public
base URLs, base domain, and verified sender. A focused message composer builds
the tenant collection URL, API-hosted brand image, and localized
`email.Message`.

Each request handler:

1. validates the recipient and request data;
2. generates the token and inserts the pending request with the attempted queue
   timestamp;
3. composes and enqueues the email with a deterministic idempotency key;
4. returns the request and one-time token response, allowing the transaction and
   `sent_at` timestamp to commit only after enqueue succeeds.

Configuration or composition errors fail the request without enqueueing. Queue
errors are reported as service unavailable and leave no committed request.

## Backoffice Design

The dialog keeps the existing one-time link block. Its copy changes from
"create/copy this link" to:

- the primary action sends the request;
- success states that the email was queued for the displayed recipient;
- the visible link remains available as a manual fallback;
- the request description explains that Fidibeki sends the email.

The API response remains backward compatible.

## Test Contract

- Both request endpoints enqueue a message addressed to the intended recipient.
- The message contains the tenant-aware collection URL, workspace identity,
  expiry context, optional prompt, and the expected locale.
- Enqueue failure returns a non-success response and does not commit a request.
- `sent_at` is present in the successful response and no request is committed
  when queue acceptance fails.
- Existing token collection behavior remains valid.
- Web typechecking, locale coverage, and unit tests remain green.

## Non-Goals

- Delivery/open tracking, reminders, and provider webhook status are separate
  lifecycle work.
- No production deployment or infrastructure change is part of this issue.
- No plaintext token column or new encryption key is introduced.
