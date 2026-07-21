# Fidibeki Activation Flow Design

**Date:** 2026-07-21
**Status:** Approved; implementation verified locally, staging deployment pending
**Target:** Staging only; production remains deferred

## Problem

The authenticated Dispatch backoffice is implemented and its core workflows pass desktop and mobile
browser tests. Staging is not yet usable by a new customer: the wildcard smoke host has no tenant,
the web app has no signup route, and tenant-scoped proxying cannot reach the platform signup API.

The first usable milestone must take a visitor from no account to a connected feedback project. A
signup form alone is insufficient because it would leave the customer in an empty backoffice with no
configured widget.

## Goals

- Provide self-service signup at `https://staging.fidibeki.com/signup`.
- Preserve host-based tenant isolation for all tenant API traffic.
- Verify signup with the staging Turnstile configuration already present on the API service.
- Take the first administrator through project creation, a valid default widget rule, installation,
  and an objective connected signal.
- Keep the experience consistent with the selected Dispatch backoffice: compact, operational, and
  focused on the task rather than marketing content.
- Cover the complete path with backend, frontend, and browser tests before staging deployment.

## Non-goals

- No production services, domains, credentials, or deployment work.
- No marketing homepage, pricing landing page, or editorial content system.
- No organization discovery by email; workspace locations are not disclosed publicly.
- No multi-project onboarding wizard or advanced widget-rule editing inside onboarding.
- No changes to the existing 14-day trial, Stripe checkout, or plan entitlements.

## Entry And Routing

The public entry host is the environment base domain: `staging.fidibeki.com`. This matches the
backend's Turnstile hostname check because staging `BASE_DOMAIN` is `staging.fidibeki.com`.

Nuxt receives two new runtime values:

- `NUXT_BASE_DOMAIN`: private server configuration used to construct the trusted platform host.
- `NUXT_PUBLIC_BASE_DOMAIN`: browser-visible root used for workspace and widget URLs.
- `NUXT_PUBLIC_TURNSTILE_SITE_KEY`: public configuration used only to render Turnstile.

The server proxy treats only `GET /v1/plans` and `POST /v1/signup` as platform requests and forwards
them with the trusted proxy host `api.<NUXT_BASE_DOMAIN>`. Existing `/auth/*` and `/v1/*` requests
continue forwarding the browser host unchanged, preserving tenant routing. The platform allowlist is
exact so the web service cannot become a general platform-host proxy.

`/signup` bypasses tenant authentication and uses a layout-free public shell. The tenant login page
links back to the base-domain signup page. The public shell also accepts a workspace slug and opens
`https://<slug>.<base-domain>/login` for existing customers; it does not query whether that tenant
exists.

## Plan Catalog

The backend adds unauthenticated `GET /v1/plans` beside the signup route. It returns the public
Starter, Growth, and Scale definitions from `backend/internal/config/plans.go`, excluding Stripe
price IDs and Enterprise. The response contains monthly price, yearly discount, limits, and feature
flags. This keeps signup presentation aligned with the existing source of truth.

Growth is selected initially because it exposes the core feedback, AI, audio, DOM capture, and replay
experience. Customers can explicitly choose Starter or Scale before submission.

## Signup Experience

The page uses the supplied Fidibeki brand assets and the Dispatch visual system. It is a focused
application entry screen, not a landing page. The form contains:

- Workspace name.
- Workspace URL slug with the environment suffix visible beside it.
- Owner work email.
- A compact plan selector populated from `GET /v1/plans`.
- An explicitly rendered Turnstile widget with action `signup`.

Client validation mirrors the backend slug rules: lowercase, 3-24 characters, alphanumeric ends,
hyphens inside, and reserved slugs rejected. The backend remains authoritative for conflicts and all
security validation.

On success, the page shows the created workspace URL and tells the user to open the one-time email.
It does not pretend the user is authenticated or consume the magic link automatically. On an API or
Turnstile error, the form keeps non-secret fields, clears the token, resets the widget, and presents
the backend's safe error message. Turnstile tokens are never persisted.

Local development works without a site key and relies on the existing local verifier. Staging uses
the configured staging site key through a Railway reference to the API service variable. Automated
browser tests use Cloudflare's official test credentials or a mocked widget. The server-side secret
remains API-only.

## First-Run Activation

After magic-link verification, members with `project:create` are sent to `/getting-started` when the
workspace has no projects. Existing workspaces and members without creation permission continue to
their first accessible Dispatch route.

The activation view has three compact stages:

1. **Project:** collect project name and allowed production/preview domains, then call
   `POST /v1/projects`.
2. **Widget rule:** create a valid enabled `all_pages` rule with immediate delay, rating and written
   feedback enabled, plan-gated capture features disabled, the standard greeting, automatic theme,
   and the Fidibeki teal accent. If rule creation fails, retain the project and provide a retry plus a
   direct route to its editor.
3. **Install:** show the environment-correct one-line loader snippet, provide a copy action, and poll
   the project for a real widget connection signal. The user may continue to the project editor
   without waiting, but the UI must distinguish `Awaiting connection` from `Connected`.

There is no separate onboarding-complete flag. A workspace with a project is no longer forced into
first-run activation, and incomplete setup remains recoverable from the project page.

## Widget Connection Signal

Add nullable `widget_last_seen_at` to `projects`. After local rule resolution, the widget sends an
authorized acknowledgement containing only the selected enabled rule ID. The backend updates the
timestamp only when the stored value is absent or older than five minutes. This provides an
objective installation signal while limiting write amplification and avoids transmitting page URLs.

Tenant project responses include `widget_last_seen_at`. The activation page polls the created project
every five seconds while visible and stops after connection, navigation, or a two-minute foreground
window. The normal project page shows the same status and timestamp near the embed code.

Only a request that passes public-key lookup, allowed-origin authorization, tenant status, plan, and
rule resolution can mark a widget as seen. Failed and unauthorized requests do not update activity.

## Failure And Recovery

- Plan-catalog failure: show retry; do not render invented prices.
- Slug or email conflict: keep form data and focus the relevant field when the API identifies it.
- Turnstile expiry/failure: clear the token and reset the challenge.
- Signup success with delayed email: keep the confirmation screen and allow a return to sign in;
  do not issue a second signup request.
- Project creation failure: remain on stage one with the API error.
- Rule creation failure after project creation: retain the project ID and retry only the rule request.
- Install not detected: keep the exact snippet visible and link to the full project configuration.
- Refresh during activation: if a project now exists, route to that project instead of creating a
  duplicate.

## Security And Privacy

- The Turnstile secret is never exposed to Nuxt or the browser.
- Platform proxy routing is limited to the two public endpoints.
- Tenant endpoints retain the original request host and existing trusted-proxy secret.
- Signup relies on mandatory backend Turnstile validation; client completion alone is never trusted.
- Workspace lookup performs only deterministic navigation and cannot enumerate tenant existence.
- Public plan responses contain no provider identifiers or credentials.
- Install activity records only project ID and timestamp; it adds no visitor identity or payload.

## Testing

Backend coverage:

- Public plan catalog excludes Enterprise and provider IDs.
- Signup routing remains platform-host only.
- Widget activity updates after an authorized matched-rule acknowledgement, is throttled, and is
  unchanged for rejected origins, keys, suspended tenants, or unmatched rules.
- Project JSON includes nullable and populated activity timestamps.
- Migration up-down-up remains clean and RLS behavior remains intact.

Frontend coverage:

- Unit tests for slug normalization/validation and base-domain URL construction.
- Browser tests for plan loading, validation, successful signup confirmation, conflicts, and
  Turnstile reset behavior.
- Browser test from magic-link verification through project creation, default-rule creation, snippet
  copy, simulated widget connection, and navigation into the project editor.
- Desktop and mobile screenshots checked for overflow, overlap, readable controls, and Dispatch
  consistency.

Staging verification:

- Configure `staging.fidibeki.com` on the web service and confirm DNS/TLS.
- Set `NUXT_BASE_DOMAIN=staging.fidibeki.com`, `NUXT_PUBLIC_BASE_DOMAIN=staging.fidibeki.com`, and reference the API's public Turnstile site key from
  the web service.
- Verify the signup page and Turnstile rendering without creating production resources.
- Complete one controlled staging signup, magic-link verification, first project, widget load from an
  allowed test origin, connected signal, and feedback submission.

## Deferred Work

Production configuration and launch remain explicitly deferred until the application is usable in
staging. Later production work will configure its own Turnstile widget and base domain, then repeat
the same activation smoke against isolated production services.
