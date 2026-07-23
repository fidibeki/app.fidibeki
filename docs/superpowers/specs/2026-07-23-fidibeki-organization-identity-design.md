# Fidibeki Organization Identity Design

**Date:** 2026-07-23
**Status:** Approved
**Issues:** #30, #31
**Target:** Staging product development

## Outcome

Each workspace has one reusable organization profile and visual identity. Owners and administrators
can manage organization data and the workspace logo, while every member can manage only their own
profile photo. The backoffice, widget defaults, and public surfaces consume the same data without
duplicating project configuration.

This design extends the existing tenant, member, widget appearance, storage, audit, and permission
boundaries. It does not create a second public upload pipeline and does not change production
infrastructure.

## Existing Contracts Reused

- `tenants.name` remains the public organization name.
- `tenants.widget_appearance_defaults` remains the organization layer of the existing
  organization -> project -> widget appearance hierarchy.
- Tenant permissions continue to use `tenant:update` for organization administration.
- Member photos are self-service; there is no implicit permission to change another member's photo.
- Storage uses the existing tenant-scoped object store.
- `tenant_access_events` remains the immutable audit ledger for organization and identity changes.
- Project `default_locale` continues to mean widget-content locale and is not repurposed as the
  backoffice locale.

## Organization Profile

The tenant gains nullable profile fields:

- `short_name`;
- `website_url`;
- `support_email`;
- `default_locale`;
- `default_timezone`;
- `privacy_policy_url`;
- `terms_url`.

`name` is edited through the same profile contract and remains required. Empty optional fields are
stored as `NULL`. `default_locale` and `default_timezone` are nullable so browser and Fidibeki
fallbacks remain distinguishable from an explicit organization choice.

Regional date/time formatting is derived from `default_locale` and `default_timezone` through
`Intl`. Arbitrary format strings and a 12/24-hour preference are not stored in this phase.

The brand accent remains the `accent_color` key in `widget_appearance_defaults`. The organization
profile editor exposes that value as the organization brand accent, so there is one source of truth
and existing project/widget overrides remain intact.

## Validation

- `name`: trimmed, 1-120 characters.
- `short_name`: trimmed, at most 32 characters.
- `website_url`, `privacy_policy_url`, and `terms_url`: absolute HTTP or HTTPS URLs, no credentials,
  at most 2048 characters.
- `support_email`: a syntactically valid email address, at most 320 characters.
- `default_locale`: normalized BCP-47 language tag supported by the application; initially `en`,
  `pt`, `pt-AO`, and `pt-PT`.
- `default_timezone`: a valid IANA timezone accepted by the runtime timezone database.
- `accent_color`: existing six-digit hex and accessible contrast validation.

Validation failures use the existing `422 validation_failed` envelope with a field map. Only active
members with `tenant:update` may mutate the organization profile. Reads remain available through
`tenant:read`.

## Identity Assets

### Upload Boundary

Identity uploads are authenticated `multipart/form-data` requests handled by the API. The backend
reads at most 2 MiB, validates the filename extension, declared MIME type, detected content, decoded
format, dimensions, and pixel count, and rejects SVG.

Accepted source formats are PNG, JPEG, and WebP. Images must be between 32x32 and 4096x4096 with no
more than 16 million decoded pixels.

The backend normalizes accepted input to PNG variants:

- `64.png` for compact navigation, lists, comments, mentions, and activity;
- `256.png` for profile and settings previews.

Avatars use a centered square crop. Logos preserve aspect ratio inside a transparent square. Image
processing is bounded and synchronous because the accepted payload and decoded dimensions are
small; this avoids exposing storage credentials and avoids introducing an asynchronous public
quarantine lifecycle for authenticated profile assets.

### Storage And Versioning

Keys are tenant-scoped and include an unpredictable UUID version:

```text
tenants/{tenant_id}/identity/logos/{version}/{size}.png
tenants/{tenant_id}/identity/members/{member_id}/{version}/{size}.png
```

The tenant/member record stores only the current version; storage keys are derived server-side.
Replacement first writes a new immutable version, then atomically points the tenant/member record at
it and appends the previous version to a tenant-scoped cleanup queue. A bounded maintainer removes
queued objects and retries failures; cleanup never rolls back the already usable profile update.

Removing an asset clears the database reference and appends its version to the same cleanup queue.
Versioned URLs provide cache busting. UI image errors always fall back to initials.

### Access

- Workspace logo mutations require `tenant:update`.
- A member may mutate only the avatar whose member ID matches the authenticated JWT subject.
- Avatar reads require an active tenant session and verify that the requested member belongs to the
  resolved tenant.
- The workspace logo is intentionally available to widget/public surfaces through a versioned
  read-only endpoint. The endpoint resolves the current tenant/version and never accepts a storage
  key from the caller.
- Mutation limits are enforced per tenant and per member. Limiter backend failure fails closed for
  image mutation while leaving the rest of the application usable.

No administrative override for another member's avatar is introduced. That capability requires a
future explicit permission and audit policy.

## API Contracts

### Organization

```http
PATCH /v1/organization-profile
Content-Type: application/json
```

The response returns the complete normalized tenant profile and effective organization appearance.
The session's tenant state is updated client-side immediately, so navigation and settings change
without logout or refresh.

### Logo

```http
PUT    /v1/organization-profile/logo
DELETE /v1/organization-profile/logo
GET    /v1/public/workspace-logo/{tenant_id}/{version}/{size}.png
```

The public logo URL is built from `PUBLIC_BASE_URL`, never from a staging or production constant.

### Member Profile

```http
PATCH  /v1/profile
PUT    /v1/profile/avatar
DELETE /v1/profile/avatar
GET    /v1/members/{member_id}/avatar/{version}/{size}.png
```

`PATCH /v1/profile` initially edits the member display name. Locale and timezone preferences are
added by #32 on the same contract.

Tenant, member, assignee, comment actor, mention actor, notification actor, and activity actor
payloads expose asset version metadata, never storage keys.

## Widget Inheritance

An uploaded organization logo is an organization default. During public configuration resolution,
the backend constructs its environment-aware URL as the lowest logo layer, then applies the
existing `widget_appearance_defaults`, project overrides, and widget-definition overrides.

Therefore:

- an existing explicit organization/project/widget `logo_url` continues to win;
- removing an uploaded logo reveals a more specific configured override unchanged;
- removing a project or widget override falls back predictably;
- existing installations and stored overrides require no rewrite.

The organization accent continues through the current appearance resolver unchanged.

## Audit Events

The following events are appended to `tenant_access_events` with actor and safe before/after
metadata:

- `organization.profile_updated`;
- `organization.logo_updated`;
- `organization.logo_removed`;
- `member.profile_updated`;
- `member.avatar_updated`;
- `member.avatar_removed`.

Binary data, storage credentials, and signed URLs are never recorded.

## Failure Behavior

- Invalid uploads return field-specific validation without changing the current asset.
- Storage write failure leaves the database unchanged.
- Database mutation failure attempts immediate cleanup of the new immutable version.
- Old-asset cleanup failure remains queued with bounded retry state; the new version remains active.
- Asset read/storage failure returns a non-cacheable error and the UI renders initials.
- Profile updates never require a session/token rotation.

## Delivery Slices

1. Add forward migration, models, validation, and compatible response fields.
2. Deliver organization profile read/update with audit and inheritance.
3. Deliver secure logo upload/remove/read and navigation/settings rendering.
4. Deliver self profile/avatar upload/remove/read.
5. Propagate avatar versions into operational actor contracts and render shared fallbacks.
6. Run backend, frontend, browser, migration, accessibility, responsive, and staging validation.

## Out Of Scope

- Production deployment or infrastructure changes.
- Custom domains, sender verification, arbitrary email templates, CSS, HTML, or JavaScript.
- Changing another member's avatar.
- Arbitrary regional date/time patterns.
- Full backoffice localization, member locale/timezone preferences, and translated UI; those belong
  to #32 and build on this profile contract.
