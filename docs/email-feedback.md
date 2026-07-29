# Email feedback campaigns

Email feedback campaigns let a customer backend request one post-transaction feedback invitation.
They are not audience uploads or marketing campaigns. Fidibeki sends only after a workspace owner
activates a campaign and the customer calls the invitation API.

## Trust boundary

- Keep the Fidibeki API key in the customer backend. Never put it in a browser, mobile application,
  analytics event, URL, or log.
- Create a key containing only `email_feedback:send`, with the shortest practical expiry and a rate
  limit appropriate for the integration.
- Use a unique, stable `external_event_id` for the customer event. Retrying the exact payload returns
  the existing invitation; reusing the ID with different input returns a conflict.
- The customer is controller for the solicitation. Campaign activation requires its controller and
  privacy-contact details, but Fidibeki does not infer or claim its lawful basis.
- Opt-out, complaint, bounce, and verified processing restrictions suppress future invitations for
  the normalized address without exposing whether the address was already suppressed.

## Configure a campaign

In the project configuration, open **Email feedback** and create a campaign. Select an active Widget
Definition and a published locale, then complete the controller profile and activate the campaign.
No email is sent by creating or activating it.

In **Settings > API keys**, create a separate integration key with only
`email_feedback:send`. The token is shown once.

## Request an invitation

Call the tenant host for the selected environment:

```http
POST /v1/email-feedback/invitations
Authorization: Bearer fk_live_...
Content-Type: application/json

{
  "campaign_id": "00000000-0000-4000-8000-000000000001",
  "external_event_id": "order_123_feedback_v1",
  "recipient_email": "customer@example.com",
  "locale": "pt",
  "context_url": "https://shop.example.com/orders/123"
}
```

`locale` and `context_url` are optional. The context must be HTTPS without credentials, query
parameters, or a fragment. A new request returns `201`; an exact retry returns `200` with the same
invitation ID. The response never contains the recipient address, public collection token, or
stored hashes.

The email contains rating links, a link to the full form, the customer's privacy policy, and an
opt-out link. Opening any link with `GET`, including a rating query string, does not create
Feedback. Only the explicit form `POST` creates one independent Feedback with source `email`.
It does not create an Issue or Testimonial automatically.

## Staging validation

Run this controlled check only in staging, using disposable external event IDs and inboxes that the
tester is authorized to use.

1. Confirm the staging API, worker, tenant web host, email provider, and Resend webhook are healthy.
2. Create and activate a temporary campaign through the project configuration.
3. Create a short-lived API key containing only `email_feedback:send`. Keep the token out of shell
   history and captured test output.
4. Call `POST /v1/email-feedback/invitations` twice with the exact same payload. Confirm `201`, then
   `200`, with one stable invitation ID and one delivered email.
5. Open a rating link and refresh it. Before submitting, confirm that no new `email` Feedback
   appeared in Dispatch.
6. Submit the form once. Confirm exactly one Feedback appears in Dispatch with source `Email`, the
   expected response and context, no linked Issue, and no Testimonial.
7. Send an invitation to a second disposable inbox. Open its opt-out link, confirm the action, then
   request another invitation for the same normalized address with a new `external_event_id`.
   Confirm the API returns a neutral `suppressed` invitation and no second email is delivered.
8. Delete the temporary API key and archive the temporary campaign. Archive the QA Feedback if it
   should not remain in the active inbox.

Do not use a real customer address for the opt-out check: the one-way suppression digest is
intentionally retained until workspace deletion or an approved policy change. Record only
invitation IDs, public Feedback IDs, statuses, timestamps, and the staging commit. Do not record
API keys, collection links, recipient addresses, or provider webhook payloads.

The automated counterpart is
`TestEmailFeedbackCampaignHTTPFlow`. It creates the campaign and least-privilege key, proves exact
retry and scanner-safe GET behavior, submits one Feedback with no Issue or Testimonial, and opts out
a second recipient before proving that a later event is suppressed without an outbox send.
