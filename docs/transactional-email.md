# Transactional email

Fidibeki composes every transactional message through `backend/internal/email.Render`. The renderer
produces escaped HTML and equivalent plain text from structured content. Links are absolute and use
the active environment and workspace; templates do not accept arbitrary HTML or add tracking.

## Sender identities

| Purpose | From | Reply-To |
| --- | --- | --- |
| Authentication, billing, account, and reporter messages | `RESEND_AUTH_FROM` | Provider default |
| One-time founder welcome | `RESEND_WELCOME_FROM` | `RESEND_WELCOME_REPLY_TO` |

The configured welcome sender must be verified with the provider before enabling Resend delivery.
Set `RESEND_WELCOME_FROM` and `RESEND_WELCOME_REPLY_TO` to monitored addresses for the deployment.

## Inventory and delivery policy

| Message | Trigger | Recipients | Policy |
| --- | --- | --- | --- |
| Trial signup and sign-in links | Requested authentication | Requesting address | Security, requested |
| Member invitation | Authorized invitation | Invited address | Access, requested |
| Member-domain verification | Authorized verification request | Verification address | Security, requested |
| Founder welcome | First owner activation for a new workspace | Activating owner | Account, one time |
| Email feedback invitation | Customer calls an active campaign with a unique external event ID | Customer-selected recipient | Customer-directed post-transaction solicitation; opt-out/bounce/complaint/restriction-controlled |
| Trial ending | Trial reaches reminder window | Workspace members | Account, mandatory |
| Feedback receipt | Feedback accepted with explicit, evidenced reporter consent | Reporter | Consent-controlled |
| Reporter progress and resolution | Material Issue lifecycle update | Consented reporter | Consent/unsubscribe-controlled |
| Payment failed or recovered | Persisted billing access transition | Billing managers and billing contact | Billing, mandatory |
| Subscription activation, plan change, or cancellation | Material persisted subscription change | Billing managers and billing contact | Billing, mandatory |
| Account deletion scheduled or cancelled | Authorized account lifecycle action | Workspace administrators | Account, mandatory |
| Member activity and digests | Optional workspace events inside a delivered window | Members who opted in | Optional, preference-controlled |

Bounced or complained addresses are excluded where recipient state is available. Billing contacts
are deduplicated against authorized administrators.

Email feedback campaigns are opt-in customer configuration. Fidibeki acts on the customer's
documented server-to-server instruction and does not infer or claim the customer's lawful basis.
An active verified restriction, opt-out, bounce, or complaint produces a neutral suppressed
invitation with no delivery outbox. Delivery removes the plaintext recipient; the minimized
invitation ledger expires 90 days after the public invitation expires, while the one-way
suppression digest remains until workspace deletion or an approved policy change.

## Member notification preferences

Optional product notifications are the only class a member can route. Each member chooses, per
workspace, whether assignments, mentions, comments, important Issue changes, and new-feedback
summaries arrive immediately, in a daily digest, in a weekly digest, or not at all. Defaults are
low-noise: directly addressed work (assignments, mentions) is immediate, comments are off, Issue
changes are daily, and new feedback is a weekly summary. The new-feedback category has no immediate
mode at all — in the API contract and in the database CHECK constraint — so one email per feedback
submission is unreachable by configuration rather than merely disabled by default.

Mandatory authentication, security, account-deletion, and critical billing messages are composed by
their own paths and are never checked against a preference row. Reporter consent and the reporter
unsubscribe link stay separate: they live on the feedback and Issue rows and are not affected by any
member preference.

All three cadences run through one windowed pipeline. Immediate delivery is a short window rather
than a second notification path, so the transactional write path for comments, mentions, and
assignments is untouched. A window is half-open, `[start, end)`, and is claimed in
`member_digest_deliveries` under a unique `(tenant, member, cadence, window_start)` key before
anything is sent; the claim is the deduplication point. A window with no events is never claimed, so
empty digests leave no row and send no mail. Only delivered or suppressed windows advance the
cursor, an unfinished window is resumed at its own start, and each hand-off carries a delivery-scoped
idempotency key, so a retry after a crash or provider failure cannot duplicate or omit events.

Digests are prepared for the member's own timezone (falling back to the workspace default, then UTC)
at their chosen hour and weekday. Every digest carries a workspace settings link and a signed
one-click opt-out at `/v1/member-notifications/unsubscribe`. That endpoint renders a confirmation
form on `GET` so a mail scanner cannot silence anyone, applies the change on `POST`, and returns a
byte-identical response for valid, forged, expired, and unknown tokens, so it cannot be used to
enumerate members or workspaces. Bounced and complained addresses are suppressed for this optional
class and the withheld window stays visible in the member's delivery diagnostics. Preference changes
and emailed opt-outs are recorded in the append-only `tenant_access_events` ledger.

Reporter emails require explicit, structured consent evidence (purpose, notice version, locale,
affirmative state), not just a boolean preference. Consent is checked twice: once when the
notification is enqueued from the outbox event, and again immediately before the Resend call, so a
withdrawal that happens while the message is being composed still prevents delivery. Every reporter
email carries a feedback- or issue-scoped unsubscribe link and a link to the published privacy
policy in its footer; withdrawal is idempotent, takes effect immediately, and never deletes the
underlying feedback. Rows consented before consent evidence existed (pre-migration `000076`) are
treated as not actively consented until the reporter opts in again through a new submission.

## Reliability

Authentication and existing account messages use the email queue directly. Founder and billing
lifecycle messages originate as transactional outbox events and run on the same retryable email
queue. Stripe event IDs and outbox resource keys suppress repeated provider webhooks; each provider
request also carries a stable idempotency key. A delivery failure cannot roll back authentication,
account activation, or an already persisted billing transition.

Existing workspaces are marked as having completed founder welcome during migration `000059`, so
only newly activated workspaces receive it. No authentication token is included in the welcome.

## Preview

Set `EMAIL_PREVIEW_DIR` and run the email package tests to write representative welcome and billing
HTML documents for browser inspection:

```sh
cd backend
EMAIL_PREVIEW_DIR=/tmp/fidibeki-email-previews go test ./internal/email -run TestWriteEmailPreviews
```
