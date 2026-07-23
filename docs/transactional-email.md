# Transactional email

Fidibeki composes every transactional message through `backend/internal/email.Render`. The renderer
produces escaped HTML and equivalent plain text from structured content. Links are absolute and use
the active environment and workspace; templates do not accept arbitrary HTML or add tracking.

## Sender identities

| Purpose | From | Reply-To |
| --- | --- | --- |
| Authentication, billing, account, and reporter messages | `RESEND_AUTH_FROM` | Provider default |
| One-time founder welcome | `RESEND_WELCOME_FROM` | `RESEND_WELCOME_REPLY_TO` |

The founder address must be verified with the configured provider before enabling Resend delivery.
The default identity is `Acidiney from Fidibeki <acidiney.dias@fidibeki.com>` and replies go to the
same monitored address.

## Inventory and delivery policy

| Message | Trigger | Recipients | Policy |
| --- | --- | --- | --- |
| Trial signup and sign-in links | Requested authentication | Requesting address | Security, requested |
| Member invitation | Authorized invitation | Invited address | Access, requested |
| Member-domain verification | Authorized verification request | Verification address | Security, requested |
| Founder welcome | First owner activation for a new workspace | Activating owner | Account, one time |
| Trial ending | Trial reaches reminder window | Workspace members | Account, mandatory |
| Feedback receipt | Feedback accepted with explicit reporter consent | Reporter | Consent-controlled |
| Reporter progress and resolution | Material Issue lifecycle update | Consented reporter | Consent/unsubscribe-controlled |
| Payment failed or recovered | Persisted billing access transition | Billing managers and billing contact | Billing, mandatory |
| Subscription activation, plan change, or cancellation | Material persisted subscription change | Billing managers and billing contact | Billing, mandatory |
| Account deletion scheduled or cancelled | Authorized account lifecycle action | Workspace administrators | Account, mandatory |

Bounced or complained addresses are excluded where recipient state is available. Billing contacts
are deduplicated against authorized administrators. Later assignment, mention, security-change, and
digest emails remain deferred until member notification preferences exist.

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
