# Fidibeki Mobile Capture

`@fidibeki/mobile` collects feedback through native React Native controls on iOS and Android.
It does not embed the web widget and it never receives a Fidibeki API key.

## Trust Boundary

The integration has two separate network boundaries:

1. A trusted customer backend authenticates the mobile app according to the customer's own
   application policy and mints a short-lived Fidibeki capture session.
2. The mobile app uses only the returned `fmc_` capture token to submit feedback and complete
   a granted screenshot upload.

Create a Fidibeki API key with only `mobile_capture:create`. Store that key in the customer
backend's secret manager. Never place it in JavaScript bundles, native resources, build-time
mobile environment variables, over-the-air updates, logs, analytics, or crash reports.

Deleting or expiring the issuing API key revokes every capture session minted by it. Capture
tokens are stored by Fidibeki only as SHA-256 hashes and expire independently for first
submission, exact retry, and attachment completion.

Expired capture rows become eligible for hard deletion after a 24-hour grace from the latest
applicable deadline. An hourly bounded sweep performs deletion and reports failures or backlog
through retention health; the grace is not represented as an absolute deletion-time guarantee
while a sweep is unhealthy or budget-exhausted.

## Mint A Capture Session

The customer backend calls the tenant-scoped API:

```http
POST /v1/mobile/capture-sessions
Authorization: Bearer fk_live_...
Content-Type: application/json

{
  "project_id": "00000000-0000-4000-8000-000000000001",
  "widget_definition_key": "mobile-feedback",
  "platform": "ios",
  "application_id": "com.example.product",
  "screen": "/chart",
  "context_url": "https://app.example.com/chart",
  "display_locale": "pt-PT"
}
```

Use the tenant host for the selected environment, for example
`https://{tenant}.staging.fidibeki.com` in staging. `context_url` is optional, must be HTTPS,
and must match an allowed project domain. Query parameters, fragments, repeated separators, and
trailing slashes are removed from page and screen context.

The response contains:

```json
{
  "token": "fmc_...",
  "submit_expires_at": "2026-07-28T22:00:00Z",
  "configuration": {
    "schema_version": 1,
    "project_id": "00000000-0000-4000-8000-000000000001",
    "display_locale": "pt",
    "definition": {}
  }
}
```

Forward this response unchanged from the customer backend to the SDK. Do not persist or log the
raw token in the customer backend. The resolved definition contains only public-safe presentation,
response-block, recurrence, appearance, contact-notice, and AI-disclosure configuration.

## Submit Feedback

The SDK submits directly to the widget API base for the selected environment:

```http
POST /v1/mobile/feedback
Authorization: Bearer fmc_...
Content-Type: application/json

{
  "client_feedback_id": "00000000-0000-4000-8000-000000000002",
  "responses": {
    "feedback": "The chart labels are difficult to read"
  },
  "comment": "This happens in landscape mode",
  "app_version": "2.4.0",
  "os_version": "18.5",
  "device_locale": "pt-PT"
}
```

Reporter email is optional. When enabled, the request must also include
`"notify_reporter": true` and the complete consent assertion rendered from the exact
`configuration.contact_notice` and `configuration.display_locale` returned for that session.
The device locale is diagnostic context and is never consent provenance.

The first accepted request creates one independent Feedback and no Issue. Retrying after an
ambiguous network result must reuse the exact same request body and `client_feedback_id`.
A changed payload, an ID already used by the web widget, or an ID bound to another mobile session
returns `409`.

## Optional Screenshot

A screenshot is requested only after an explicit user action through the host application's
`screenshotProvider`. The host owns any camera or photo-library UI and permissions. The SDK accepts
PNG, JPEG, or WebP up to 10 MiB, verifies the declared size and MIME type against the bytes it reads,
and never transmits the local URI.

The feedback request adds one declaration:

```json
{
  "screenshot": {
    "client_attachment_id": "00000000-0000-4000-8000-000000000003",
    "content_type": "image/png",
    "size_bytes": 2048
  }
}
```

After Feedback acceptance, the SDK uploads directly to the returned presigned URL, then calls:

```http
POST /v1/mobile/attachments/{attachment_id}/complete
Authorization: Bearer fmc_...
Content-Type: application/json

{
  "complete_token": "..."
}
```

Fidibeki verifies the object with storage HEAD, enforces the declared size and MIME type, and sends
the attachment through the existing quarantine and safety pipeline. A failed upload is reported as
an attachment outcome; it does not roll back the accepted Feedback or create a duplicate.

## Data Boundary

The first mobile slice records only:

- platform, application ID, normalized screen, app version, OS version, and optional device locale;
- the explicitly submitted answers, comment, and optional reporter contact consent;
- an optional user-selected screenshot;
- an optional allowlisted HTTPS context URL supplied by the customer backend.

It collects no advertising, vendor, installation, hardware, contact-list, log, analytics, replay,
or fingerprinting identifier. Mobile funnel analytics are not emitted by this slice.
