# Fidibeki Mobile Capture Design

**Issue:** [#136](https://github.com/fidibeki/app.fidibeki/issues/136)
**Status:** Approved under the founder's standing pre-approval for recommended product decisions
**Rollout:** Local and staging only; no production or infrastructure changes

## Outcome

Deliver the first usable mobile capture path:

`native launcher -> feedback sheet -> secure submission -> Fidibeki inbox`

The initial SDK targets React Native applications running on iOS and Android. It reuses the
existing Widget Definition, response blocks, localization, recurrence, attachment verification,
AI-processing, independent Feedback, and inbox contracts. It does not embed the browser widget in a
WebView and does not require a private Fidibeki API key inside the mobile binary.

Independent Swift and Kotlin SDKs may follow after the cross-platform product contract is proven.
They are not implemented by duplicating the first slice before its API and interaction model have
been validated.

## Existing Baseline

The browser widget already has a mature public capture path:

- project public key plus exact allowed-origin authorization;
- active-tenant and unexpired-trial checks;
- versioned active Widget Definitions and structured response blocks;
- idempotent feedback and attachment identifiers;
- presigned attachment upload followed by server-side size, MIME, and safety verification;
- explicit reporter-contact consent evidence;
- abuse limits, plan limits, AI processing, recurrence, and an independent Feedback inbox.

Native requests do not have a browser-enforced `Origin`. A project public key is also visible in a
compiled application and cannot be treated as a secret. Removing the origin check or trusting a
bundle/package identifier supplied by the app would create a new unauthenticated submission route.

## Considered Approaches

### Hosted WebView

A hosted form could reuse much of the browser runtime and work on both platforms quickly. It was
rejected as the primary path because it feels foreign inside a native application, complicates
keyboard/accessibility behavior and attachment handoff, and still needs a secure native
authorization boundary.

### Public Key Plus App Identifier

The native app could send the existing project public key and an iOS bundle or Android package
identifier. This has the lowest integration cost, but both values can be copied or spoofed. They
are useful labels, not authentication. Rate limits alone do not make that a satisfactory default.

### Brokered Capture Session

The customer's backend uses a least-privilege Fidibeki API key to mint a short-lived, one-feedback
capture session. The mobile SDK receives that opaque session from a customer-supplied callback and
submits directly to Fidibeki. This is the selected approach because it works on iOS and Android,
keeps private credentials server-side, binds the allowed project/definition/context before the
device can submit, and leaves room for Apple App Attest or Google Play Integrity as a later
backendless option.

## Customer Journey

1. A workspace owner creates an API key containing only `mobile_capture:create`.
2. The customer's backend exposes an authenticated app-specific endpoint that calls Fidibeki
   `POST /v1/mobile/capture-sessions`.
3. The mobile app configures `@fidibeki/mobile` with a `sessionProvider` callback. The callback calls
   the customer's backend; the private Fidibeki key never reaches the device.
4. Fidibeki returns an opaque session token plus the resolved, public-safe Widget Definition.
5. The SDK applies the definition's recurrence state for the current screen and renders a native
   launcher/sheet when eligible, or the host opens it imperatively.
6. A successful submit creates one independent Feedback record. Attachment uploads continue
   idempotently after the accepted-feedback boundary.
7. The Feedback appears in the existing inbox with its mobile source and screen context. It never
   creates an Issue automatically.

The integration documentation must show the trusted backend exchange and explicitly prohibit
embedding a tenant API key in JavaScript bundles, iOS applications, Android applications, or mobile
configuration files.

## Authorization And Session Lifecycle

Add the tenant permission `mobile_capture:create`. API keys can request it only when the creating
member already has it, using the existing permission-subset rule. Existing active owners and active
administrative members who already hold `api_key:create`, `project:read`, and `widget_rule:read`
receive it in the migration; their `authorization_version` is incremented so existing sessions
cannot continue with a stale permission set. Other existing members do not receive a new
submission-minting capability implicitly.

`POST /v1/mobile/capture-sessions` is an API-key-only, tenant-authenticated, writable route requiring
that permission. Human access tokens cannot mint a device bearer directly. It accepts:

- `project_id`;
- stable `widget_definition_key`;
- `platform`: `ios` or `android`;
- `application_id`: iOS bundle ID or Android application ID;
- `screen`: a normalized logical route such as `/checkout/review`;
- optional absolute HTTPS `context_url`;
- requested `display_locale`.

The application ID is a bounded reverse-DNS-style label, not an authenticator. The context URL, when
present, must use an exact/wildcard host already allowed by the project; query parameters and
fragments are removed through the existing canonical page contract.

The server resolves an active `launcher` definition in that project and rejects inactive tenants,
expired trials, pending deletion, unknown projects/rules, disabled definitions, and unsupported
presentations without widening tenant visibility. Mobile mounting is explicit: browser URL
targeting, scroll/delay triggers, and objective scoring are not reinterpreted against a native
screen. The customer backend chooses the active definition and binds the platform, application ID,
screen, context URL, project, definition, and server-resolved display locale into the session; the
device cannot replace them. Locale fallback uses the existing project/default localization
contract, and the response names the exact `display_locale` used for every returned prompt, label,
controller notice, and AI disclosure.

The session token:

- uses a distinct `fmc_` prefix and at least 256 bits of randomness;
- is returned once and stored only as a hash;
- has a ten-minute first-submission deadline;
- authorizes exactly one `client_feedback_id`;
- binds to the first created `feedback_id`;
- permits exact retries for that bound client feedback ID for ten minutes after acceptance;
- remains usable only for its bound attachment completions through each attachment's own upload
  expiry, even when the first-submission deadline has passed;
- rejects a different feedback ID, project, definition, or context;
- binds to the issuing API key and is denied immediately when that key is deleted or expired; and
- is revoked by tenant/project deletion or an inactive/expired tenant lifecycle state.

Concurrent first submissions lock the session row. At most one feedback can be bound to it, while
an exact retry returns the existing resource and any still-valid attachment grants.

The bearer therefore has separate deadlines:

- `submit_expires_at`: ten minutes from minting, for an unbound first submission;
- `retry_expires_at`: ten minutes from the accepted Feedback timestamp, for exact bound retries;
- per-attachment `upload_expires_at`: the authoritative deadline for completion of that bound
  attachment.

An expired submission deadline can never create new work. A completion request after that deadline
must identify an attachment belonging to the session's already-bound Feedback and must still pass
the attachment's independent completion token, object HEAD, size/MIME checks, and upload deadline.
The cleanup job removes the session only after every applicable retry/upload deadline has passed.

The existing API-key rate limit applies to session minting. Mobile submissions and attachment
grants also use the existing project/network abuse ceilings, keyed by the capture session rather
than a claimed device identifier. No persistent hardware identifier or fingerprint is introduced.

### Token Resolution And RLS

The bearer token does not expose a tenant ID. A minimal resolver on the existing privileged
database pool hashes the bearer and returns only `tenant_id` and `capture_session_id`; it never
returns project configuration, context, reporter data, Feedback content, or attachment metadata.
The handler then starts the standard app-role tenant transaction, sets `app.tenant_id`, locks the
session by tenant/session ID plus token hash, and revalidates:

- issuer API key existence and expiry;
- session and per-operation deadline;
- tenant/trial/deletion lifecycle;
- project lifecycle;
- bound client Feedback and attachment ownership.

An unbound first submission additionally requires the exact active definition and controller
versions minted into the session. A bound exact retry or attachment completion relies on the
immutable Feedback/session/attachment binding and does not re-evaluate definition activity or
version. Pausing or editing a definition after Feedback acceptance therefore prevents new sessions
without stranding a previously granted upload. Issuer-key and tenant/project lifecycle revocation
still applies to every operation.

All content and mutation queries run in that tenant transaction under FORCE RLS. A missing,
forged, cross-tenant, expired, or concurrently deleted token resolves to the same neutral denial.
Tests and the provider release gate inventory this deliberately narrow privileged query.

## Mobile Submission Contract

The public mobile routes are:

- `POST /v1/mobile/feedback`;
- `POST /v1/mobile/attachments/{attachment_id}/complete`.

Both require `Authorization: Bearer <capture-session-token>`. They do not accept a project public
key, tenant token, or caller-selected `Origin`.

The feedback request carries:

- a UUID `client_feedback_id`;
- responses defined by the bound Widget Definition;
- optional comment;
- optional reporter email plus the same complete consent assertion used by the browser widget;
- optional screenshot declaration;
- bounded `app_version`, `os_version`, and optional `device_locale` metadata.

Platform, application ID, screen, project, definition, and context URL come from the server-side
capture session. They are absent from the device submission schema. Mobile endpoint decoding
rejects unknown fields and duplicate object keys at every nesting level before typed validation, so
ambiguous JSON never reaches consent, response, or attachment handling.

The session binds the definition version and controller-profile version returned to the SDK. A
definition edit, lifecycle pause/archive, or controller-profile/contact-notice version change before
submission returns a typed `configuration_changed` conflict. The SDK requests a new session,
reconciles only still-compatible draft answers, clears any prior contact grant, and asks the user to
review and submit again. The server never validates mint-time answers or consent against a different
current definition/notice.

The session also binds the resolved `display_locale`. Any reporter-consent assertion must use that
exact locale, and the server stores it as the locale of the notice actually shown. `device_locale`
is separate diagnostic context and is never accepted as consent provenance. An app-language change
requests a new session and clears the prior grant; changing only the OS/device locale does not
silently reinterpret already-rendered copy.

The first slice accepts PNG, JPEG, or WebP screenshots through the existing presign, direct upload,
completion-token, MIME/size verification, quarantine, and AI-processing pipeline. Automatic screen
capture, audio, video, DOM evidence, and session replay are excluded until their native permission,
privacy, and redaction contracts are designed.

Reporter contact uses the same published controller profile, localized notice version, and explicit
grant contract as the browser widget. The server assigns `mobile_submission` as consent provenance;
the client cannot select it. Reporter receipts and updates accept this provenance without treating
it as browser-originated evidence. When AI processing is enabled for submitted text or screenshots,
the native sheet renders the same public-safe disclosure and policy link as the browser widget.

The accepted-feedback boundary remains the successful feedback response. If a later screenshot
upload fails, the SDK reports a per-attachment error but does not claim that feedback creation
failed or create a duplicate on retry.

### Idempotency Ownership

The existing project/client-feedback advisory lock remains the shared serialization boundary, but a
mobile session cannot adopt an existing submission merely because the UUID matches:

1. lock the capture session;
2. lock `(project_id, client_feedback_id)`;
3. if the session is unbound, require that no Feedback already owns that key;
4. create the Feedback and bind session, Feedback, client ID, and a canonical request digest in one
   transaction;
5. if the session is already bound, require the same Feedback ID, client ID, and request digest
   before returning or rotating that Feedback's incomplete attachment grants.

A collision with web feedback, another mobile session, or a changed retry payload returns `409`
without revealing or rotating the existing Feedback's attachment grants. Browser retries likewise
accept only browser-owned submissions, so a mobile-created UUID cannot enter the web idempotency
path. The lock order is shared and covered by mobile/mobile and mobile/web races.

## Feedback Source And Context

Add a constrained source to Feedback:

- `web` for all existing and browser-widget records;
- `ios`;
- `android`.

Add bounded mobile context fields for application ID, screen, app version, and OS version. Existing
rows backfill to `web` without changing `page_url`. `page_url` remains a non-null string in storage
and on the wire for backward compatibility: a mobile record without a context URL stores and returns
`""`. An optional HTTPS context URL is stored only when the customer backend supplied it while
minting the session. Database constraints require non-empty HTTP(S) `page_url` for `web`, permit an
empty value only for mobile sources, and reject mobile context on web records.

The inbox and feedback detail show a source badge and the screen label. "View page" is available
only for an absolute HTTP(S) context URL. Slack, Teams, and generic webhooks include the additive
source/context fields and omit the page action when no safe URL exists. They must never fabricate a
web URL from the application ID or screen.

Source becomes an inbox filter. Existing API clients that omit it continue to receive all sources,
and existing payload fields remain additive and compatible.

## SDK Surface

Create a publishable workspace package, `@fidibeki/mobile`, with React and React Native as peer
dependencies. Staging validation consumes the local workspace build; publishing it to a public
registry remains outside this staging-only slice. Its public API has three bounded parts:

- `FidibekiProvider`: API base, async `sessionProvider`, and storage adapter;
- `FidibekiFeedbackLauncher`: native launcher plus accessible modal sheet;
- `useFidibekiFeedback`: imperative open/close/status controls for product-owned entry points.

The provider may also receive a host-owned `screenshotProvider`. The SDK invokes it only after an
explicit "Add screenshot" action and only when the resolved definition permits screenshots. It
returns `null` for cancellation or one bounded object containing local URI, declared PNG/JPEG/WebP
content type, byte size, an async `read(): Blob`, and an optional `dispose()` cleanup callback. The
host owns camera/photo-library UI and platform permissions; the SDK requests none automatically.
Local URIs are never transmitted. The SDK validates type, ten-MiB size, read/declaration
consistency, cancellation, provider errors, and cleanup before using the existing declaration and
direct-upload contract.

The component renders the bound definition's rating, reaction, single-choice, free-text, optional
comment, optional contact consent, localized system/customer copy, and controlled appearance. It
uses native controls, stable keyboard-safe layout, visible focus/accessibility labels, reduced
motion, safe-area insets, and no WebView.

The SDK owns only anonymous functional state:

`project + definition + normalized screen -> new|submitted|dismissed|snoozed`

The host supplies an asynchronous storage adapter. An AsyncStorage adapter is provided separately
so storage is explicit and testable. Query-like screen parameters and fragments are removed, and
trailing slashes are normalized. A successful submission writes `submitted` immediately; dismissal
writes `dismissed`; opening alone never suppresses. Session, cooldown, and never recurrence follow
the existing definition values. A new eligible submission always receives a new UUID.

For `recurrence_mode=session`, the provider accepts an optional host-supplied app-session ID.
Without one, the SDK creates a random process/foreground-session ID in memory. Provider remounts in
the same JS process reuse it; a cold launch or 30 continuous minutes in the background creates a new
ID. AsyncStorage records the ID only for local comparison and never sends it to Fidibeki. A
`submitted` or `dismissed` record suppresses only while its stored ID equals the current app-session
ID. Cooldown and never modes remain timestamp/persistent-state based. Tests distinguish component
remount, brief background/foreground, the 30-minute boundary, and a cold app-session restart.

The SDK does not silently collect mobile analytics in this slice. Mobile impressions/interactions
are excluded from the browser-widget funnel rather than being counted incompletely or without the
existing consent contract. A later slice can add mobile funnel events after consent and event
identity are designed for native storage.

## Privacy And Data Minimization

- No advertising identifier, vendor identifier, IP-derived identifier, device serial, installation
  fingerprint, contacts, logs, or arbitrary app state is collected.
- `application_id`, normalized screen, app version, OS version, display/device locales, explicitly
  submitted answers, and optional screenshot are the complete first-slice context.
- Reporter contact remains optional and requires the existing versioned, localized, controller
  notice and explicit consent with server-assigned mobile provenance.
- Screenshot processing retains the existing quarantine, selective AI-processing, retention, and
  data-rights behavior.
- Capture-session rows contain no reporter content and become eligible for hard deletion after a
  24-hour grace from the latest applicable submit, retry, or bound-attachment upload deadline.
  The hourly bounded cleanup reports failures and backlog through the retention-health ledger
  rather than claiming an absolute deletion time while maintenance is unhealthy.
- The privacy data map and retention registry identify the session token metadata and mobile
  feedback context explicitly.

## Failure Semantics

- Session mint validation returns field-specific `422`; tenant/resource boundaries return the
  repository's neutral not-found/forbidden behavior.
- Expired, forged, revoked, or already rebound session tokens return a generic access denial and
  never disclose whether a project, definition, or feedback exists.
- Missing Redis/abuse enforcement fails closed for feedback creation; attachment grant degradation
  may preserve text feedback with the existing warning contract.
- A configuration/session error leaves the host application usable and exposes a typed SDK status.
- A retry reuses the same client feedback ID until a definitive response is received.
- A new capture session is requested only after expiry, terminal denial, or a new eligible
  submission.

## Validation

### Backend

- migration up/down/up, constraints, RLS, tenant isolation, and legacy `web` backfill;
- owner/admin permission backfill, authorization-version invalidation, and API-key subset behavior;
- active/inactive/deleting/expired tenant and active/inactive definition checks;
- issuer-key revocation, minimal privileged token resolution, tenant revalidation, token
  entropy/prefix/hash-only storage, separate deadlines, neutral denial, and cleanup;
- concurrent first-submit, exact request-digest retry, collision, mobile/mobile, and mobile/web
  idempotency;
- definition, lifecycle, and controller-notice mutation races after session mint;
- display-locale binding, changed app locale, and non-authoritative device-locale behavior;
- definition pause/edit after acceptance still permits only the already-bound retry/upload;
- recursive duplicate-key and unknown-field rejection;
- mobile response-block/contact/context validation and no automatic Issue;
- screenshot declaration, upload, completion, safety, and lifecycle races;
- source filter/projection and additive integration payloads.

### SDK

- iOS and Android rendering branches with the same semantic form;
- every response block, validation error, localization fallback, controlled theme, and consent;
- screen normalization and session/cooldown/never recurrence;
- accepted feedback followed by successful and failed screenshot uploads;
- host screenshot provider success, cancellation, invalid type/size/read, permission error, and
  cleanup without transmitting a local URI;
- exact retry and new-submission UUID behavior;
- keyboard, safe-area, long-copy, accessibility-label, and reduced-motion behavior;
- no WebView, private key, hardware identifier, or analytics network request.

The example application must compile and launch against both an iOS simulator and Android emulator
or equivalent device CI. Runtime smoke tests exercise session acquisition, AsyncStorage recurrence,
modal/keyboard/safe-area behavior, direct object upload, and attachment completion on both
platforms; JS mocks alone are not acceptance evidence.

### Backoffice And Staging

- desktop/mobile inbox and detail tests for web/iOS/Android source, screen, optional URL, and filter;
- responsive visual inspection without overlap;
- full backend, web, widget, and mobile package gates;
- security CI and dependency/vulnerability scans;
- staging session mint through a disposable least-privilege API key, iOS-simulator and
  Android-runtime submissions from the example app, screenshot completion, inbox visibility, and
  no Issue creation.

No production deployment, DNS, provider, or infrastructure action is part of #136.
